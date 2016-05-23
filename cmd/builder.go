package cmd

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"golang.org/x/net/context"

	docker "github.com/docker/engine-api/client"
	dtypes "github.com/docker/engine-api/types"
)

// ImageBuilder represents
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

// Build builds an image accourding to the request
func (ib *ImageBuilder) Build(ctx context.Context, req *BuildDefinition) error {
	var ref string
	if req.Ref.Branch != "" {
		ref = req.Ref.Branch
	}
	if req.Ref.Sha != "" {
		ref = req.Ref.Sha
	}
	if ref == "" {
		return fmt.Errorf("branch and sha are empty")
	}
	rl := strings.Split(req.GithubRepo, "/")
	if len(rl) != 2 {
		return fmt.Errorf("malformed github repo: %v", req.GithubRepo)
	}
	owner := rl[0]
	repo := rl[1]
	select {
	case <-ctx.Done():
		return fmt.Errorf("build was cancelled")
	default:
		break
	}
	rbi, err := ib.gf.Get(owner, repo, ".", ref)
	if err != nil {
		return err
	}
	return ib.dobuild(ctx, req, rbi)
}

// doBuild executes the archive file GET and triggers the Docker build
func (ib *ImageBuilder) dobuild(ctx context.Context, req *BuildDefinition, rbi *RepoBuildData) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("build was cancelled")
	default:
		break
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
		Tags:        req.Tags,
		Remove:      true,
		ForceRemove: true,
		PullParent:  true,
		Dockerfile:  rbi.DockerfileContents,
	}
	ibr, err := ib.c.ImageBuild(ctx, gzr, opts)
	if err != nil {
		return fmt.Errorf("error starting build: %v", err)
	}
	return ib.monitorBuild(ctx, ibr.Body)
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

// monitorBuild reads the Docker API response stream and detects any errors
func (ib *ImageBuilder) monitorBuild(ctx context.Context, rc io.ReadCloser) error {
	done := ctx.Done()
	rdr := bufio.NewReader(rc)
	for {
		select {
		case <-done:
			return fmt.Errorf("build was cancelled")
		default:
			break
		}
		line, err := rdr.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("error reading event stream: %v", err)
		}
		var errormsg dockerErrorEvent
		var event dockerStreamEvent
		err = json.Unmarshal(line, &errormsg)
		if err == nil {
			// is an error
			return fmt.Errorf("build error: %v: detail: %v: %v", errormsg.Error, errormsg.ErrorDetail.Code, errormsg.ErrorDetail.Message)
		}
		err = json.Unmarshal(line, &event)
		if err != nil {
			return fmt.Errorf("error unmarshaling event: %v (event: %v)", err, string(line))
		}
		log.Printf("%v\n", event.Stream)
	}
}
