package cmd

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	DockerfilePath string
	Context        io.Reader
	Tags           []string //{name}:{tag}
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
func (ib *ImageBuilder) Build(ctx context.Context, req *BuildRequest, id gocql.UUID) (string, error) {
	log.Printf("starting build %v", id.String())
	err := setBuildTimeMetric(dbConfig.session, id, "docker_build_started")
	if err != nil {
		return "", err
	}
	rl := strings.Split(req.Build.GithubRepo, "/")
	if len(rl) != 2 {
		return "", fmt.Errorf("malformed github repo: %v", req.Build.GithubRepo)
	}
	owner := rl[0]
	repo := rl[1]
	if isCancelled(ctx.Done()) {
		return "", fmt.Errorf("build was cancelled: %v", ctx.Err())
	}
	log.Printf("%v: fetching github repo: %v", id.String(), req.Build.GithubRepo)
	contents, err := ib.gf.Get(owner, repo, req.Build.Ref)
	if err != nil {
		return "", err
	}
	var dp string
	if req.Build.DockerfilePath == "" {
		dp = "Dockerfile"
	} else {
		dp = req.Build.DockerfilePath
	}
	rbi := &RepoBuildData{
		DockerfilePath: dp,
		Context:        contents,
		Tags:           ib.getFullImageNames(req),
	}
	return ib.dobuild(ctx, req, rbi)
}

func (ib *ImageBuilder) saveOutput(ctx context.Context, action actionType, events []dockerStreamEvent) error {
	if isCancelled(ctx.Done()) {
		return fmt.Errorf("build was cancelled: %v", ctx.Err())
	}
	output := []byte{}
	for _, e := range events {
		j, err := json.Marshal(&e)
		if err != nil {
			return fmt.Errorf("error marshaling dockerStreamEvent: %v", err)
		}
		j = append(j, byte('\n'))
		output = append(output, j...)
	}
	id, ok := BuildIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("build id missing from context")
	}
	switch action {
	case Build:
		return setBuildImageBuildOutput(dbConfig.session, id, output)
	case Push:
		return setBuildPushOutput(dbConfig.session, id, output)
	default:
		return fmt.Errorf("unknown action: %v", action)
	}
}

const (
	buildSuccessEventPrefix = "Successfully built "
)

// doBuild executes the archive file GET and triggers the Docker build
func (ib *ImageBuilder) dobuild(ctx context.Context, req *BuildRequest, rbi *RepoBuildData) (string, error) {
	var imageid string
	if isCancelled(ctx.Done()) {
		return imageid, fmt.Errorf("build was cancelled: %v", ctx.Err())
	}
	id, ok := BuildIDFromContext(ctx)
	if !ok {
		return imageid, fmt.Errorf("build id missing from context")
	}
	opts := dtypes.ImageBuildOptions{
		Tags:        rbi.Tags,
		Remove:      true,
		ForceRemove: true,
		PullParent:  true,
		Dockerfile:  rbi.DockerfilePath,
		AuthConfigs: dockerConfig.dockercfgContents,
	}
	ibr, err := ib.c.ImageBuild(ctx, rbi.Context, opts)
	if err != nil {
		return imageid, fmt.Errorf("error starting build: %v", err)
	}
	output, err := ib.monitorDockerAction(ctx, ibr.Body)
	err2 := ib.saveOutput(ctx, Build, output) // we want to save output even if error
	if err != nil {
		return imageid, err
	}
	if err2 != nil {
		return imageid, err2
	}
	// Parse final stream event to find image ID
	fe := output[len(output)-1]
	if strings.HasPrefix(fe.Stream, buildSuccessEventPrefix) {
		imageid = strings.TrimRight(fe.Stream[len(buildSuccessEventPrefix):len(fe.Stream)], "\n")
	} else {
		return imageid, fmt.Errorf("could not determine image id from final event: %v", fe.Stream)
	}
	log.Printf("built image ID %v", imageid)
	err = setBuildTimeMetric(dbConfig.session, id, "docker_build_completed")
	if err != nil {
		return imageid, err
	}
	return imageid, ib.writeDockerImageSizeMetrics(ctx, imageid)
}

func (ib *ImageBuilder) writeDockerImageSizeMetrics(ctx context.Context, imageid string) error {
	if isCancelled(ctx.Done()) {
		return fmt.Errorf("build was cancelled: %v", ctx.Err())
	}
	id, ok := BuildIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("build id missing from context")
	}
	res, _, err := ib.c.ImageInspectWithRaw(ctx, imageid, true)
	if err != nil {
		return err
	}
	return setDockerImageSizesMetric(dbConfig.session, id, res.Size, res.VirtualSize)
}

// Models for the JSON objects the Docker API returns
// This is a combination of all fields we may be interested in
// Each Docker API endpoint returns a different response schema :-\
type dockerStreamEvent struct {
	Stream         string                 `json:"stream,omitempty"`
	Status         string                 `json:"status,omitempty"`
	ProgressDetail map[string]interface{} `json:"progressDetail,omitempty"`
	Progress       string                 `json:"progress,omitempty"`
	ID             string                 `json:"id,omitempty"`
	Aux            map[string]interface{} `json:"aux,omitempty"`
	Error          string                 `json:"error,omitempty"`
	ErrorDetail    dockerErrorDetail      `json:"errorDetail,omitempty"`
}

type dockerErrorDetail struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// monitorDockerAction reads the Docker API response stream and detects any errors
func (ib *ImageBuilder) monitorDockerAction(ctx context.Context, rc io.ReadCloser) ([]dockerStreamEvent, error) {
	rdr := bufio.NewReader(rc)
	output := []dockerStreamEvent{}
	id, ok := BuildIDFromContext(ctx)
	if !ok {
		return output, fmt.Errorf("build id missing from context")
	}
	for {
		if isCancelled(ctx.Done()) {
			return output, fmt.Errorf("action was cancelled: %v", ctx.Err())
		}
		line, err := rdr.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return output, nil
			}
			return output, fmt.Errorf("error reading event stream: %v", err)
		}
		log.Printf("%v: %v", id.String(), string(line))
		var event dockerStreamEvent
		err = json.Unmarshal(line, &event)
		if err != nil {
			return output, fmt.Errorf("error unmarshaling event: %v (event: %v)", err, string(line))
		}
		output = append(output, event)
	}
}

// CleanImage cleans up the built image after it's been pushed
func (ib *ImageBuilder) CleanImage(ctx context.Context, imageid string) error {
	if isCancelled(ctx.Done()) {
		return fmt.Errorf("clean was cancelled: %v", ctx.Err())
	}
	id, ok := BuildIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("build id missing from context")
	}
	log.Printf("Cleaning up images for %v", id.String())
	err := setBuildTimeMetric(dbConfig.session, id, "clean_started")
	if err != nil {
		return err
	}
	opts := dtypes.ImageRemoveOptions{
		Force:         true,
		PruneChildren: true,
	}
	resp, err := ib.c.ImageRemove(ctx, imageid, opts)
	for _, r := range resp {
		log.Printf("ImageDelete: %v", r)
	}
	if err != nil {
		return err
	}
	return setBuildTimeMetric(dbConfig.session, id, "clean_completed")
}

// PushBuildToRegistry pushes the already built image and all associated tags to the
// configured remote Docker registry. Caller must ensure the image has already
// been built successfully
func (ib *ImageBuilder) PushBuildToRegistry(ctx context.Context, req *BuildRequest) error {
	if isCancelled(ctx.Done()) {
		return fmt.Errorf("push was cancelled: %v", ctx.Err())
	}
	id, ok := BuildIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("build id missing from context")
	}
	log.Printf("doing push for %v", id.String())
	err := setBuildTimeMetric(dbConfig.session, id, "push_started")
	if err != nil {
		return err
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
		j, err := json.Marshal(&val)
		if err != nil {
			return fmt.Errorf("error marshaling auth: %v", err)
		}
		auth = base64.StdEncoding.EncodeToString(j)
	} else {
		return fmt.Errorf("auth not found in dockercfg for %v", registry)
	}
	opts := dtypes.ImagePushOptions{
		All:          true,
		RegistryAuth: auth,
	}
	var output []dockerStreamEvent
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
	err = setBuildTimeMetric(dbConfig.session, id, "push_completed")
	if err != nil {
		return err
	}
	return ib.saveOutput(ctx, Push, output)
}

// PushBuildToS3 exports and uploads the already built image to the configured S3 bucket/key
func (ib *ImageBuilder) PushBuildToS3(ctx context.Context, req *BuildRequest) error {
	if isCancelled(ctx.Done()) {
		return fmt.Errorf("push was cancelled: %v", ctx.Err())
	}
	return fmt.Errorf("not yet implemented")
}
