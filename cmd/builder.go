package cmd

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"golang.org/x/net/context"

	docker "github.com/docker/engine-api/client"
	dtypes "github.com/docker/engine-api/types"
	"github.com/gocql/gocql"
)

type actionType int

// Docker action types
const (
	Build actionType = iota // Build is an image build
	Push                    // Push is a registry push
)

// RepoBuildData contains data about a GitHub repo necessary to do a Docker build
type RepoBuildData struct {
	DockerfileContents *string
	ArchiveLink        *url.URL
	Tags               []string //{name}:{tag}
}

// ImageBuilder is an object that builds and pushes images
type ImageBuilder struct {
	c  *docker.Client
	gf *GitHubFetcher
}

// NewImageBuilder returns a new ImageBuilder
func NewImageBuilder(ghtoken string) (*ImageBuilder, error) {
	ib := &ImageBuilder{}
	dc, err := docker.NewEnvClient()
	ib.gf = NewGitHubFetcher(ghtoken)
	if err != nil {
		return ib, err
	}
	ib.c = dc
	return ib, nil
}

// Returns full docker name:tag strings from the supplied repo/tags
func (ib *ImageBuilder) getFullImageNames(req *BuildRequest) []string {
	var bname string
	names := []string{}
	if req.Push.Registry.Repo != "" {
		bname = req.Push.Registry.Repo
	} else {
		bname = req.Build.GithubRepo
	}
	for _, t := range req.Build.Tags {
		names = append(names, fmt.Sprintf("%v:%v", bname, t))
	}
	return names
}

// Build builds an image accourding to the request
func (ib *ImageBuilder) Build(ctx context.Context, req *BuildRequest, id gocql.UUID) error {
	rl := strings.Split(req.Build.GithubRepo, "/")
	if len(rl) != 2 {
		return fmt.Errorf("malformed github repo: %v", req.Build.GithubRepo)
	}
	owner := rl[0]
	repo := rl[1]
	if isCancelled(ctx.Done()) {
		return fmt.Errorf("build was cancelled: %v", ctx.Err())
	}
	ctx = context.WithValue(ctx, "id", id)
	dockerfile, archiveLink, err := ib.gf.Get(owner, repo, ".", req.Build.Ref)
	if err != nil {
		return err
	}
	rbi := &RepoBuildData{
		DockerfileContents: dockerfile,
		ArchiveLink:        archiveLink,
		Tags:               ib.getFullImageNames(req),
	}
	return ib.dobuild(ctx, req, rbi)
}

func (ib *ImageBuilder) saveOutput(ctx context.Context, action actionType, output []byte) error {
	if isCancelled(ctx.Done()) {
		return fmt.Errorf("build was cancelled: %v", ctx.Err())
	}
	val := ctx.Value("id")
	switch val.(type) {
	case gocql.UUID:
		break
	default:
		return fmt.Errorf("id missing or bad type: %T", val)
	}
	id := val.(gocql.UUID)
	switch action {
	case Build:
		return setBuildImageBuildOutput(dbConfig.session, id, output)
	case Push:
		return setBuildPushOutput(dbConfig.session, id, output)
	default:
		return fmt.Errorf("unknown action: %v", action)
	}
}

// doBuild executes the archive file GET and triggers the Docker build
func (ib *ImageBuilder) dobuild(ctx context.Context, req *BuildRequest, rbi *RepoBuildData) error {
	if isCancelled(ctx.Done()) {
		return fmt.Errorf("build was cancelled: %v", ctx.Err())
	}
	hc := http.Client{}
	hr, err := http.NewRequest("GET", rbi.ArchiveLink.String(), nil)
	if err != nil {
		return fmt.Errorf("error creating http request: %v", err)
	}
	resp, err := hc.Do(hr)
	if err != nil {
		return fmt.Errorf("error performing archive http request: %v", err)
	}
	if resp.StatusCode > 299 {
		return fmt.Errorf("archive http request failed: %v", resp.StatusCode)
	}
	defer resp.Body.Close()
	gzr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("error creating gzip reader: %v", err)
	}
	defer gzr.Close()
	opts := dtypes.ImageBuildOptions{
		Tags:        rbi.Tags,
		Remove:      true,
		ForceRemove: true,
		PullParent:  true,
		Dockerfile:  *rbi.DockerfileContents,
	}
	ibr, err := ib.c.ImageBuild(ctx, gzr, opts)
	if err != nil {
		return fmt.Errorf("error starting build: %v", err)
	}
	output, err := ib.monitorDockerAction(ctx, ibr.Body)
	err2 := ib.saveOutput(ctx, Build, output) // we want to save output even if error
	if err != nil {
		return err
	}
	if err2 != nil {
		return err2
	}
	return nil
}

// Models for the JSON objects the Docker API returns
type dockerStreamEvent struct {
	Stream string `json:"stream"`
}

type dockerErrorDetail struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type dockerErrorEvent struct {
	Error       string            `json:"error"`
	ErrorDetail dockerErrorDetail `json:"errorDetail"`
}

// monitorDockerAction reads the Docker API response stream and detects any errors
func (ib *ImageBuilder) monitorDockerAction(ctx context.Context, rc io.ReadCloser) ([]byte, error) {
	rdr := bufio.NewReader(rc)
	wtr := bytes.NewBuffer(nil)
	for {
		if isCancelled(ctx.Done()) {
			return wtr.Bytes(), fmt.Errorf("action was cancelled: %v", ctx.Err())
		}
		line, err := rdr.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return wtr.Bytes(), nil
			}
			return wtr.Bytes(), fmt.Errorf("error reading event stream: %v", err)
		}
		log.Printf("%v: %v", ctx.Value("id").(string), string(line))
		wtr.Write(line)
		var errormsg dockerErrorEvent
		var event dockerStreamEvent
		err = json.Unmarshal(line, &errormsg)
		if err == nil {
			// is an error
			return wtr.Bytes(), fmt.Errorf("action error: %v: detail: %v: %v", errormsg.Error, errormsg.ErrorDetail.Code, errormsg.ErrorDetail.Message)
		}
		err = json.Unmarshal(line, &event)
		if err != nil {
			return wtr.Bytes(), fmt.Errorf("error unmarshaling event: %v (event: %v)", err, string(line))
		}
		log.Printf("%v\n", event.Stream)
	}
}

// PushBuildToRegistry pushes the already built image and all associated tags to the
// configured remote Docker registry. Caller must ensure the image has already
// been built successfully
func (ib *ImageBuilder) PushBuildToRegistry(ctx context.Context, req *BuildRequest) error {
	if isCancelled(ctx.Done()) {
		return fmt.Errorf("push was cancelled: %v", ctx.Err())
	}
	repo := req.Push.Registry.Repo
	if repo == "" {
		return fmt.Errorf("PushBuildToRegistry called but repo is empty")
	}
	rsl := strings.Split(repo, "/")
	var registry string
	switch len(rsl) {
	case 2: // Docker Hub
		registry = "https://index.docker.io/v2/"
	case 3: // private registry
		registry = rsl[0]
	default:
		return fmt.Errorf("cannot determine base registry URL from %v", repo)
	}
	var auth string
	if val, ok := dockerConfig.dockercfgContents[registry]; ok {
		auth = val.Auth
	} else {
		return fmt.Errorf("auth not found in dockercfg for %v", registry)
	}
	opts := dtypes.ImagePushOptions{
		All:          true,
		RegistryAuth: auth,
	}
	var output []byte
	defer ib.saveOutput(ctx, Push, output) // we want to save output even if error
	for _, name := range ib.getFullImageNames(req) {
		if isCancelled(ctx.Done()) {
			return fmt.Errorf("push was cancelled: %v", ctx.Err())
		}
		ipr, err := ib.c.ImagePush(ctx, name, opts)
		if err != nil {
			return fmt.Errorf("error initiating registry push: %v", err)
		}
		o, err := ib.monitorDockerAction(ctx, ipr)
		output = append(output, o...)
		if err != nil {
			return err
		}
	}
	return nil
}

// PushBuildToS3 exports and uploads the already built image to the configured S3 bucket/key
func (ib *ImageBuilder) PushBuildToS3(ctx context.Context, req *BuildRequest) error {
	if isCancelled(ctx.Done()) {
		return fmt.Errorf("push was cancelled: %v", ctx.Err())
	}
	return fmt.Errorf("not yet implemented")
}
