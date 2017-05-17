package lib

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"
	"time"

	"golang.org/x/net/context"

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

const (
	legalDockerTagChars = "abcdefghijklmnopqrtsuvwxyz-_.ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
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

// ImageBuildClient describes a client that can manipulate images
// Add whatever Docker API methods we care about here
type ImageBuildClient interface {
	ImageBuild(context.Context, io.Reader, dtypes.ImageBuildOptions) (dtypes.ImageBuildResponse, error)
	ImageInspectWithRaw(context.Context, string) (dtypes.ImageInspect, []byte, error)
	ImageRemove(context.Context, string, dtypes.ImageRemoveOptions) ([]dtypes.ImageDelete, error)
	ImagePush(context.Context, string, dtypes.ImagePushOptions) (io.ReadCloser, error)
	ImageSave(context.Context, []string) (io.ReadCloser, error)
	ImageList(context.Context, dtypes.ImageListOptions) ([]dtypes.Image, error)
}

// ImageBuildPusher describes an object that can build and push container images
type ImageBuildPusher interface {
	Build(context.Context, *BuildRequest, gocql.UUID) (string, error)
	CleanImage(context.Context, string) error
	PushBuildToRegistry(context.Context, *BuildRequest) error
	PushBuildToS3(context.Context, string, *BuildRequest) error
}

type S3ErrorLogConfig struct {
	PushToS3          bool
	PresignTTLMinutes uint
	Region            string
	Bucket            string
}

// ImageBuilder is an object that builds and pushes images
type ImageBuilder struct {
	c          ImageBuildClient
	gf         CodeFetcher
	ep         EventBusProducer
	dl         DataLayer
	mc         MetricsCollector
	is         ImageSquasher
	osm        ObjectStorageManger
	dockercfg  map[string]dtypes.AuthConfig
	s3errorcfg S3ErrorLogConfig
	logger     *log.Logger
}

// NewImageBuilder returns a new ImageBuilder
func NewImageBuilder(eventbus EventBusProducer, datalayer DataLayer, gf CodeFetcher, dc ImageBuildClient, mc MetricsCollector, osm ObjectStorageManger, is ImageSquasher, dcfg map[string]dtypes.AuthConfig, s3errorcfg S3ErrorLogConfig, logger *log.Logger) (*ImageBuilder, error) {
	ib := &ImageBuilder{}
	ib.gf = gf
	ib.c = dc
	ib.ep = eventbus
	ib.dl = datalayer
	ib.mc = mc
	ib.osm = osm
	ib.is = is
	ib.dockercfg = dcfg
	ib.s3errorcfg = s3errorcfg
	ib.logger = logger
	return ib, nil
}

func (ib *ImageBuilder) logf(ctx context.Context, msg string, params ...interface{}) {
	id, ok := BuildIDFromContext(ctx)
	if !ok {
		ib.logger.Printf("build id missing from context")
		return
	}
	msg = fmt.Sprintf("%v: %v", id.String(), msg)
	ib.logger.Printf(msg, params...)
	go func() {
		event, err := ib.getBuildEvent(ctx, BuildEvent_LOG, BuildEventError_NO_ERROR, fmt.Sprintf(msg, params...), false)
		if err != nil {
			ib.logger.Printf("error building event object: %v", err)
			return
		}
		err = ib.ep.PublishEvent(event)
		if err != nil {
			ib.logger.Printf("error pushing event to bus: %v", err)
		}
	}()
}

func (ib *ImageBuilder) getBuildEvent(ctx context.Context, etype BuildEvent_EventType, errtype BuildEventError_ErrorType, msg string, finished bool) (*BuildEvent, error) {
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

func (ib *ImageBuilder) event(bevent *BuildEvent) {
	ib.logger.Printf("event: %v", bevent.Message)
	go func() {
		err := ib.ep.PublishEvent(bevent)
		if err != nil {
			ib.logger.Printf("error pushing event to bus: %v", err)
		}
	}()
}

func (ib ImageBuilder) filterTagName(tag string) string {
	mf := func(r rune) rune {
		if strings.Contains(legalDockerTagChars, string(r)) {
			return r
		}
		return rune(-1)
	}
	return strings.Map(mf, tag)
}

func (ib ImageBuilder) validateTag(tag string) bool {
	switch {
	case strings.HasPrefix(tag, "."):
		return false
	case strings.HasPrefix(tag, "-"):
		return false
	case len(tag) > 128:
		return false
	}
	return true
}

// Returns full docker name:tag strings from the supplied repo/tags
func (ib *ImageBuilder) getFullImageNames(req *BuildRequest) ([]string, error) {
	var bname string
	names := []string{}
	if req.Push.Registry.Repo != "" {
		bname = req.Push.Registry.Repo
	} else {
		bname = req.Build.GithubRepo
	}
	for _, t := range req.Build.Tags {
		ft := ib.filterTagName(t)
		if ft != t {
			// if any illegal chars were filtered out, the image will be tagged differently from
			// what the user expects, so return error instead
			return names, fmt.Errorf("tag contains illegal characters: %v", t)
		}
		if !ib.validateTag(ft) {
			return names, fmt.Errorf("invalid tag: %v", ft)
		}
		names = append(names, fmt.Sprintf("%v:%v", bname, ft))
	}
	if req.Build.TagWithCommitSha {
		csha, err := ib.getCommitSHA(req.Build.GithubRepo, req.Build.Ref)
		if err != nil {
			return names, fmt.Errorf("error getting commit sha: %v", err)
		}
		if !ib.validateTag(csha) {
			return names, fmt.Errorf("invalid commit sha tag: %v", csha)
		}
		names = append(names, fmt.Sprintf("%v:%v", bname, csha))
	}
	return names, nil
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
	inames, err := ib.getFullImageNames(req)
	if err != nil {
		return "", err
	}
	ib.logf(ctx, "tags: %v", inames)
	rbi := &RepoBuildData{
		DockerfilePath: dp,
		Context:        contents,
		Tags:           inames,
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

// saveEventLogToS3 writes a stream of events to S3 and returns the S3 HTTP URL
func (ib *ImageBuilder) saveEventLogToS3(ctx context.Context, repo string, ref string, action actionType, events []BuildEvent) (string, error) {
	id, ok := BuildIDFromContext(ctx)
	if !ok {
		return "", fmt.Errorf("build id missing from context")
	}
	csha, err := ib.getCommitSHA(repo, ref)
	if err != nil {
		return "", err
	}
	idesc := ImageDescription{
		GitHubRepo: repo,
		CommitSHA:  csha,
	}
	b := bytes.NewBuffer([]byte{})
	for _, e := range events {
		b.Write([]byte(e.Message + "\n"))
	}
	now := time.Now().UTC()
	s3opts := &S3Options{
		Region:            ib.s3errorcfg.Region,
		Bucket:            ib.s3errorcfg.Bucket,
		KeyPrefix:         now.Round(time.Hour).Format(time.RFC3339) + "/",
		PresignTTLMinutes: ib.s3errorcfg.PresignTTLMinutes,
	}
	key := fmt.Sprintf("%v-%v-error.txt", id.String(), action.String())
	return ib.osm.WriteFile(key, idesc, "text/plain", b, s3opts)
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
		AuthConfigs: ib.dockercfg,
		NoCache:     true,
	}
	ibr, err := ib.c.ImageBuild(ctx, rbi.Context, opts)
	if err != nil {
		return imageid, fmt.Errorf("error starting build: %v", err)
	}
	output, err := ib.monitorDockerAction(ctx, ibr.Body, Build)
	err2 := ib.saveOutput(ctx, Build, output) // we want to save output even if error
	if err != nil {
		le := output[len(output)-1]
		if le.EventError.ErrorType == BuildEventError_FATAL && ib.s3errorcfg.PushToS3 {
			ib.logf(ctx, "pushing failed build log to S3: %v", id.String())
			loc, err3 := ib.saveEventLogToS3(ctx, req.Build.GithubRepo, req.Build.Ref, Build, output)
			if err3 != nil {
				ib.logf(ctx, "error saving build events to S3: %v", err3)
				return imageid, err
			}
			return imageid, fmt.Errorf("build failed: log saved to: %v", loc)
		}
		return imageid, fmt.Errorf("build failed: %v", le.Message)
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
	return imageid, ib.writeDockerImageSizeMetrics(ctx, imageid, req.Build.GithubRepo, req.Build.Ref)
}

func (ib *ImageBuilder) writeDockerImageSizeMetrics(ctx context.Context, imageid string, repo string, ref string) error {
	if isCancelled(ctx.Done()) {
		return fmt.Errorf("build was cancelled: %v", ctx.Err())
	}
	id, ok := BuildIDFromContext(ctx)
	if !ok {
		return fmt.Errorf("build id missing from context")
	}
	res, _, err := ib.c.ImageInspectWithRaw(ctx, imageid)
	if err != nil {
		return err
	}
	err = ib.mc.ImageSize(res.Size, res.VirtualSize, repo, ref)
	if err != nil {
		ib.logger.Printf("error pushing image size metrics: %v", err)
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
		bevent, err = ib.getBuildEvent(ctx, buildEventTypeFromActionType(atype), errtype, string(line), false)
		if err != nil {
			return output, err
		}
		output = append(output, *bevent)
		if errtype == BuildEventError_FATAL {
			// do not push final event (leave to upstream error handler)
			return output, fmt.Errorf("fatal error performing %v action", atype.String())
		}
		ib.event(bevent)
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
	if val, ok := ib.dockercfg[registry]; ok {
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
	inames, err := ib.getFullImageNames(req)
	if err != nil {
		return err
	}
	for _, name := range inames {
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

func (ib *ImageBuilder) getCommitSHA(repo, ref string) (string, error) {
	rl := strings.Split(repo, "/")
	if len(rl) != 2 {
		return "", fmt.Errorf("malformed GitHub repo: %v", repo)
	}
	csha, err := ib.gf.GetCommitSHA(rl[0], rl[1], ref)
	if err != nil {
		return "", fmt.Errorf("error getting commit SHA: %v", err)
	}
	return csha, nil
}

// PushBuildToS3 exports and uploads the already built image to the configured S3 bucket/key
func (ib *ImageBuilder) PushBuildToS3(ctx context.Context, imageid string, req *BuildRequest) error {
	if isCancelled(ctx.Done()) {
		return fmt.Errorf("push was cancelled: %v", ctx.Err())
	}
	csha, err := ib.getCommitSHA(req.Build.GithubRepo, req.Build.Ref)
	if err != nil {
		return err
	}
	info, _, err := ib.c.ImageInspectWithRaw(ctx, imageid)
	if err != nil {
		return err
	}
	if len(info.RepoTags) == 0 {
		return fmt.Errorf("no tags found for image")
	}
	r, err := ib.c.ImageSave(ctx, info.RepoTags)
	if err != nil {
		return fmt.Errorf("error saving image: %v", err)
	}
	defer r.Close()
	ib.logf(ctx, "squashing and pushing to S3: %v: %v/%v%v/%v.tar.gz", req.Push.S3.Region, req.Push.S3.Bucket, req.Push.S3.KeyPrefix, req.Build.GithubRepo, csha)
	done := make(chan error)
	pr, pw := io.Pipe()
	go func() {
		var err error
		var si *SquashInfo
		defer pw.CloseWithError(err)
		si, err = ib.is.Squash(ctx, r, pw)
		if err != nil {
			done <- fmt.Errorf("error squashing image: %v", err)
			return
		}
		ib.mc.Size("image.squashed.size_difference_bytes", req.Build.GithubRepo, req.Build.Ref, nil, si.SizeDifference)
		ib.mc.Float("image.squashed.size_difference_pct", req.Build.GithubRepo, req.Build.Ref, nil, si.SizePctDifference)
		ib.mc.Size("image.squashed.files_removed", req.Build.GithubRepo, req.Build.Ref, nil, int64(si.FilesRemovedCount))
		ib.mc.Size("image.squashed.layers_removed", req.Build.GithubRepo, req.Build.Ref, nil, int64(si.LayersRemoved))
		done <- nil
	}()
	go func() {
		idesc := ImageDescription{
			GitHubRepo: req.Build.GithubRepo,
			CommitSHA:  csha,
		}
		opts := &S3Options{
			Region:    req.Push.S3.Region,
			Bucket:    req.Push.S3.Bucket,
			KeyPrefix: req.Push.S3.KeyPrefix,
		}
		done <- ib.osm.Push(idesc, pr, opts)
	}()
	var failed bool
	errstrs := []string{}
	for i := 0; i < 2; i++ {
		err = <-done
		if err != nil {
			failed = true
			errstrs = append(errstrs, err.Error())
		}
	}
	if failed {
		return fmt.Errorf("squash/push failed: %v", strings.Join(errstrs, ", "))
	}
	return nil
}
