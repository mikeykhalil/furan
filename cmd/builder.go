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

//go:generate stringer -type=actionType
type actionType int

// Docker action types
const (
	Build actionType = iota // Build is an image build
	Push                    // Push is a registry push
)

func buildEventTypeFromActionType(atype actionType) BuildEvent_EventType {
	switch atype {
	case Build:
		return BuildEvent_DOCKER_BUILD_STREAM
	case Push:
		return BuildEvent_DOCKER_PUSH_STREAM
	}
	return -1 // unreachable
}

// RepoBuildData contains data about a GitHub repo necessary to do a Docker build
type RepoBuildData struct {
	DockerfilePath string
	Context        io.Reader
	Tags           []string //{name}:{tag}
}

// ImageBuildClient describes a client that can build & push images
// Add whatever Docker API methods we care about here
type ImageBuildClient interface {
	ImageBuild(context.Context, io.Reader, dtypes.ImageBuildOptions) (dtypes.ImageBuildResponse, error)
	ImageInspectWithRaw(context.Context, string, bool) (dtypes.ImageInspect, []byte, error)
	ImageRemove(context.Context, string, dtypes.ImageRemoveOptions) ([]dtypes.ImageDelete, error)
	ImagePush(context.Context, string, dtypes.ImagePushOptions) (io.ReadCloser, error)
}

// ImageBuildPusher describes an object that can build and push container images
type ImageBuildPusher interface {
	Build(context.Context, *BuildRequest, gocql.UUID) (string, error)
	CleanImage(context.Context, string) error
	PushBuildToRegistry(context.Context, *BuildRequest) error
	PushBuildToS3(context.Context, *BuildRequest) error
}

// ImageBuilder is an object that builds and pushes images
type ImageBuilder struct {
	c      ImageBuildClient
	gf     CodeFetcher
	ep     EventBusProducer
	dl     DataLayer
	logger *log.Logger
}

// NewImageBuilder returns a new ImageBuilder
func NewImageBuilder(ghtoken string, eventbus EventBusProducer, datalayer DataLayer, logger *log.Logger) (*ImageBuilder, error) {
	ib := &ImageBuilder{}
	ib.gf = NewGitHubFetcher(ghtoken)
	dc, err := docker.NewEnvClient()
	if err != nil {
		return ib, err
	}
	ib.c = dc
	ib.ep = eventbus
	ib.dl = datalayer
	ib.logger = logger
	return ib, nil
}

func (ib *ImageBuilder) logf(ctx context.Context, msg string, params ...interface{}) {
	id, ok := BuildIDFromContext(ctx)
	if !ok {
		log.Printf("build id missing from context")
		return
	}
	msg = fmt.Sprintf("%v: %v", id.String(), msg)
	ib.logger.Printf(msg, params...)
	go func() {
		event, err := ib.getBuildEvent(ctx, BuildEvent_LOG, BuildEventError_NO_ERROR, fmt.Sprintf(msg, params...), false)
		if err != nil {
			log.Printf("error building event object: %v", err)
			return
		}
		err = ib.ep.PublishEvent(event)
		if err != nil {
			log.Printf("error pushing event to bus: %v", err)
		}
	}()
}

func (ib *ImageBuilder) getBuildEvent(ctx context.Context, etype BuildEvent_EventType,
	errtype BuildEventError_ErrorType, msg string, finished bool) (*BuildEvent, error) {
	var event *BuildEvent
	id, ok := BuildIDFromContext(ctx)
	if !ok {
		return event, fmt.Errorf("build id missing from context")
	}
	event = &BuildEvent{
		EventError: &BuildEventError{
			ErrorType: errtype,
			IsError:   errtype != BuildEventError_NO_ERROR,
		},
		BuildId:       id.String(),
		EventType:     etype,
		BuildFinished: finished,
		Message:       msg,
	}
	return event, nil
}

func (ib *ImageBuilder) event(ctx context.Context, etype BuildEvent_EventType,
	errtype BuildEventError_ErrorType, msg string, finished bool) (*BuildEvent, error) {
	event, err := ib.getBuildEvent(ctx, etype, errtype, msg, finished)
	if err != nil {
		return event, err
	}
	go func() {
		err := ib.ep.PublishEvent(event)
		if err != nil {
			log.Printf("error pushing event to bus: %v", err)
		}
	}()
	return event, nil
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
	ib.logf(ctx, "starting build")
	err := ib.dl.SetBuildTimeMetric(id, "docker_build_started")
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
	ib.logf(ctx, "fetching github repo: %v", req.Build.GithubRepo)
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

func (ib *ImageBuilder) saveOutput(ctx context.Context, action actionType, events []BuildEvent) error {
	if isCancelled(ctx.Done()) {
		return fmt.Errorf("build was cancelled: %v", ctx.Err())
	}
	id, ok := BuildIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("build id missing from context")
	}
	var column string
	switch action {
	case Build:
		column = "build_output"
	case Push:
		column = "push_output"
	default:
		return fmt.Errorf("unknown action: %v", action)
	}
	return ib.dl.SaveBuildOutput(id, events, column)
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
	output, err := ib.monitorDockerAction(ctx, ibr.Body, Build)
	err2 := ib.saveOutput(ctx, Build, output) // we want to save output even if error
	if err != nil {
		return imageid, err
	}
	if err2 != nil {
		return imageid, fmt.Errorf("error saving action output: %v", err2)
	}
	// Parse final stream event to find image ID
	fes := output[len(output)-1].Message
	dse := dockerStreamEvent{}
	err = json.Unmarshal([]byte(fes), &dse)
	if err != nil {
		return imageid, fmt.Errorf("error marshaling final message to determine image id: %v", err)
	}
	if strings.HasPrefix(dse.Stream, buildSuccessEventPrefix) {
		imageid = strings.TrimRight(dse.Stream[len(buildSuccessEventPrefix):len(dse.Stream)], "\n")
	} else {
		return imageid, fmt.Errorf("could not determine image id from final event: %v", dse.Stream)
	}
	ib.logf(ctx, "built image ID %v", imageid)
	err = ib.dl.SetBuildTimeMetric(id, "docker_build_completed")
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
	return ib.dl.SetDockerImageSizesMetric(id, res.Size, res.VirtualSize)
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

// monitorDockerAction reads the Docker API response stream
func (ib *ImageBuilder) monitorDockerAction(ctx context.Context, rc io.ReadCloser, atype actionType) ([]BuildEvent, error) {
	rdr := bufio.NewReader(rc)
	output := []BuildEvent{}
	var bevent *BuildEvent
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
		var event dockerStreamEvent
		err = json.Unmarshal(line, &event)
		if err != nil {
			return output, fmt.Errorf("error unmarshaling event: %v (event: %v)", err, string(line))
		}
		var errtype BuildEventError_ErrorType
		if event.Error != "" {
			errtype = BuildEventError_FATAL
		} else {
			errtype = BuildEventError_NO_ERROR
		}
		bevent, err = ib.event(ctx, buildEventTypeFromActionType(atype), errtype, string(line), false)
		if err != nil {
			return output, err
		}
		output = append(output, *bevent)
		if errtype == BuildEventError_FATAL {
			return output, fmt.Errorf("fatal error performing %v action", atype.String())
		}
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
	ib.logf(ctx, "cleaning up images")
	err := ib.dl.SetBuildTimeMetric(id, "clean_started")
	if err != nil {
		return err
	}
	opts := dtypes.ImageRemoveOptions{
		Force:         true,
		PruneChildren: true,
	}
	resp, err := ib.c.ImageRemove(ctx, imageid, opts)
	for _, r := range resp {
		ib.logf(ctx, "ImageDelete: %v", r)
	}
	if err != nil {
		return err
	}
	return ib.dl.SetBuildTimeMetric(id, "clean_completed")
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
	ib.logf(ctx, "pushing")
	err := ib.dl.SetBuildTimeMetric(id, "push_started")
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
	var output []BuildEvent
	for _, name := range ib.getFullImageNames(req) {
		if isCancelled(ctx.Done()) {
			return fmt.Errorf("push was cancelled: %v", ctx.Err())
		}
		ipr, err := ib.c.ImagePush(ctx, name, opts)
		if err != nil {
			return fmt.Errorf("error initiating registry push: %v", err)
		}
		o, err := ib.monitorDockerAction(ctx, ipr, Push)
		output = append(output, o...)
		if err != nil {
			return err
		}
	}
	err = ib.dl.SetBuildTimeMetric(id, "push_completed")
	if err != nil {
		return err
	}
	ib.logf(ctx, "push completed")
	return ib.saveOutput(ctx, Push, output)
}

// PushBuildToS3 exports and uploads the already built image to the configured S3 bucket/key
func (ib *ImageBuilder) PushBuildToS3(ctx context.Context, req *BuildRequest) error {
	if isCancelled(ctx.Done()) {
		return fmt.Errorf("push was cancelled: %v", ctx.Err())
	}
	return fmt.Errorf("not yet implemented")
}
