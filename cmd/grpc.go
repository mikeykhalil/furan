package cmd

import (
	"fmt"
	"io"
	"log"
	"net"
	"time"

	"github.com/gocql/gocql"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// GrpcServer represents an object that responds to gRPC calls
type GrpcServer struct {
	ib         ImageBuildPusher
	dl         DataLayer
	ep         EventBusProducer
	ec         EventBusConsumer
	ls         io.Writer
	mc         MetricsCollector
	logger     *log.Logger
	workerChan chan *workerRequest
	qsize      uint
	wcnt       uint
}

var grpcSvr *GrpcServer

type workerRequest struct {
	ctx context.Context
	req *BuildRequest
}

type ctxKeyType string

var ctxIDKey ctxKeyType = "id"
var ctxStartedKey ctxKeyType = "started"
var ctxPushStartedkey ctxKeyType = "push_started"

// NewBuildIDContext returns a context with the current build ID and time started
// stored as values
func NewBuildIDContext(ctx context.Context, id gocql.UUID) context.Context {
	return context.WithValue(context.WithValue(ctx, ctxIDKey, id), ctxStartedKey, time.Now().UTC())
}

// NewPushStartedContext returns a context with the push started timestamp stored
// as a value
func NewPushStartedContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxPushStartedkey, time.Now().UTC())
}

// BuildIDFromContext returns the ID stored in ctx, if any
func BuildIDFromContext(ctx context.Context) (gocql.UUID, bool) {
	id, ok := ctx.Value(ctxIDKey).(gocql.UUID)
	return id, ok
}

// StartedFromContext returns the time the build started stored in ctx, if any
func StartedFromContext(ctx context.Context) (time.Time, bool) {
	started, ok := ctx.Value(ctxStartedKey).(time.Time)
	return started, ok
}

// PushStartedFromContext returns the time the push started stored in ctx, if any
func PushStartedFromContext(ctx context.Context) (time.Time, bool) {
	ps, ok := ctx.Value(ctxPushStartedkey).(time.Time)
	return ps, ok
}

// NewGRPCServer returns a new instance of the gRPC server
func NewGRPCServer(ib ImageBuildPusher, dl DataLayer, ep EventBusProducer, ec EventBusConsumer, mc MetricsCollector, queuesize uint, concurrency uint, logger *log.Logger) *GrpcServer {
	grs := &GrpcServer{
		ib:         ib,
		dl:         dl,
		ep:         ep,
		ec:         ec,
		mc:         mc,
		logger:     logger,
		workerChan: make(chan *workerRequest, queuesize),
		qsize:      queuesize,
		wcnt:       concurrency,
	}
	grs.runWorkers()
	return grs
}

func (gr *GrpcServer) runWorkers() {
	for i := 0; uint(i) < gr.wcnt; i++ {
		go gr.buildWorker()
	}
	gr.logf("%v workers running (queue: %v)", serverConfig.concurrency, serverConfig.queuesize)
}

func (gr *GrpcServer) buildWorker() {
	var wreq *workerRequest
	var err error
	var id gocql.UUID
	for {
		wreq = <-gr.workerChan
		if !isCancelled(wreq.ctx.Done()) {
			gr.syncBuild(wreq.ctx, wreq.req)
		} else {
			id, _ = BuildIDFromContext(wreq.ctx)
			gr.logf("context is cancelled, skipping: %v", id)
			if err = gr.dl.DeleteBuild(id); err != nil {
				gr.logf("error deleting build: %v", err)
			}
		}
	}
}

// ListenRPC starts the RPC listener on addr:port
func (gr *GrpcServer) ListenRPC(addr string, port uint) error {
	addr = fmt.Sprintf("%v:%v", addr, port)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		gr.logf("error starting gRPC listener: %v", err)
		return err
	}
	s := grpc.NewServer()
	RegisterFuranExecutorServer(s, grpcSvr)
	gr.logf("gRPC listening on: %v", addr)
	return s.Serve(l)
}

func (gr *GrpcServer) logf(msg string, params ...interface{}) {
	gr.logger.Printf(msg, params...)
}

func (gr *GrpcServer) durationFromStarted(ctx context.Context) (time.Duration, error) {
	started, ok := StartedFromContext(ctx)
	if !ok {
		return 0, fmt.Errorf("started missing from context")
	}
	return time.Now().UTC().Sub(started), nil
}

func (gr *GrpcServer) buildDuration(ctx context.Context, req *BuildRequest) error {
	d, err := gr.durationFromStarted(ctx)
	if err != nil {
		return err
	}
	return gr.mc.Duration("build.duration", req.Build.GithubRepo, req.Build.Ref, nil, d.Seconds())
}

func (gr *GrpcServer) pushDuration(ctx context.Context, req *BuildRequest, s3 bool) error {
	ps, ok := PushStartedFromContext(ctx)
	if !ok {
		return fmt.Errorf("push_started missing from context")
	}
	tags := []string{}
	if s3 {
		tags = append(tags, "s3")
	} else {
		tags = append(tags, "registry")
	}
	d := time.Now().UTC().Sub(ps).Seconds()
	return gr.mc.Duration("push.duration", req.Build.GithubRepo, req.Build.Ref, tags, d)
}

func (gr *GrpcServer) totalDuration(ctx context.Context, req *BuildRequest) error {
	d, err := gr.durationFromStarted(ctx)
	if err != nil {
		return err
	}
	return gr.mc.Duration("total.duration", req.Build.GithubRepo, req.Build.Ref, nil, d.Seconds())
}

// Performs build synchronously
func (gr *GrpcServer) syncBuild(ctx context.Context, req *BuildRequest) (outcome BuildStatusResponse_BuildState) {
	var err error // so deferred finalize function has access to any error
	id, ok := BuildIDFromContext(ctx)
	if !ok {
		gr.logf("build id missing from context")
		return
	}
	gr.logf("syncBuild started: %v", id.String())
	// Finalize build and send event. Failures should set err and return the appropriate build state.
	defer func(id gocql.UUID) {
		failed := outcome == BuildStatusResponse_BUILD_FAILURE || outcome == BuildStatusResponse_PUSH_FAILURE
		flags := map[string]bool{
			"failed":   failed,
			"finished": true,
		}
		var eet BuildEventError_ErrorType
		if failed {
			eet = BuildEventError_FATAL
			gr.mc.BuildFailed(req.Build.GithubRepo, req.Build.Ref)
		} else {
			eet = BuildEventError_NO_ERROR
			gr.mc.BuildSucceeded(req.Build.GithubRepo, req.Build.Ref)
		}
		if err := gr.totalDuration(ctx, req); err != nil {
			gr.logger.Printf("error pushing total duration: %v", err)
		}
		var msg string
		if err != nil {
			msg = err.Error()
		} else {
			msg = "build finished"
		}
		gr.logf("syncBuild: finished: %v", msg)
		if err2 := gr.dl.SetBuildState(id, outcome); err2 != nil {
			gr.logf("failBuild: error setting build state: %v", err2)
		}
		if err2 := gr.dl.SetBuildFlags(id, flags); err2 != nil {
			gr.logf("failBuild: error setting build flags: %v", err2)
		}
		event := &BuildEvent{
			EventError: &BuildEventError{
				ErrorType: eet,
				IsError:   eet != BuildEventError_NO_ERROR,
			},
			BuildId:       id.String(),
			BuildFinished: true,
			EventType:     BuildEvent_LOG,
			Message:       msg,
		}
		if err2 := gr.ep.PublishEvent(event); err2 != nil {
			gr.logf("failBuild: error publishing event: %v", err2)
		}
		gr.logf("%v: %v: failed: %v", id.String(), msg, flags["failed"])
	}(id)
	err = gr.mc.BuildStarted(req.Build.GithubRepo, req.Build.Ref)
	if err != nil {
		gr.logger.Printf("error pushing BuildStarted metric: %v", err)
	}
	if isCancelled(ctx.Done()) {
		err = fmt.Errorf("build was cancelled")
		return BuildStatusResponse_BUILD_FAILURE
	}
	err = gr.dl.SetBuildState(id, BuildStatusResponse_BUILDING)
	if err != nil {
		err = fmt.Errorf("error setting build state to building: %v", err)
		return BuildStatusResponse_BUILD_FAILURE
	}
	imageid, err := gr.ib.Build(ctx, req, id)
	if err := gr.buildDuration(ctx, req); err != nil {
		gr.logger.Printf("error pushing build duration metric: %v", err)
	}
	if err != nil {
		err = fmt.Errorf("error performing build: %v", err)
		return BuildStatusResponse_BUILD_FAILURE
	}
	err = gr.dl.SetBuildState(id, BuildStatusResponse_PUSHING)
	if err != nil {
		err = fmt.Errorf("error setting build state to pushing: %v", err)
		return BuildStatusResponse_BUILD_FAILURE
	}
	ctx = NewPushStartedContext(ctx)
	s3 := req.Push.Registry.Repo == ""
	if s3 {
		err = gr.ib.PushBuildToS3(ctx, req)
	} else {
		err = gr.ib.PushBuildToRegistry(ctx, req)
	}
	if err := gr.pushDuration(ctx, req, s3); err != nil {
		gr.logger.Printf("error pushing push duration metric: %v", err)
	}
	if err != nil {
		err = fmt.Errorf("error pushing: %v", err)
		return BuildStatusResponse_PUSH_FAILURE
	}
	err = gr.ib.CleanImage(ctx, imageid)
	if err != nil { // Cleaning images is non-fatal because concurrent builds can cause failures here
		gr.logger.Printf("CleanImage error (non-fatal): %v", err)
	}
	err = gr.dl.SetBuildState(id, BuildStatusResponse_SUCCESS)
	if err != nil {
		err = fmt.Errorf("error setting build state to success: %v", err)
		return BuildStatusResponse_PUSH_FAILURE
	}
	err = gr.dl.SetBuildCompletedTimestamp(id)
	if err != nil {
		err = fmt.Errorf("error setting build completed timestamp: %v", err)
		return BuildStatusResponse_PUSH_FAILURE
	}
	return BuildStatusResponse_SUCCESS
}

// gRPC handlers
func (gr *GrpcServer) StartBuild(ctx context.Context, req *BuildRequest) (*BuildRequestResponse, error) {
	resp := &BuildRequestResponse{}
	if req.Push.Registry.Repo == "" {
		if req.Push.S3.Bucket == "" || req.Push.S3.KeyPrefix == "" || req.Push.S3.Region == "" {
			return nil, grpc.Errorf(codes.InvalidArgument, "must specify either registry repo or S3 region/bucket/key-prefix")
		}
	}
	id, err := gr.dl.CreateBuild(req)
	if err != nil {
		return nil, grpc.Errorf(codes.Internal, "error creating build in DB: %v", err)
	}
	ctx = NewBuildIDContext(context.Background(), id)
	wreq := workerRequest{
		ctx: ctx,
		req: req,
	}
	select {
	case gr.workerChan <- &wreq:
		gr.logf("queueing build: %v", id.String())
		resp.BuildId = id.String()
		return resp, nil
	default:
		gr.logf("build id %v cannot run because queue is full", id.String())
		err = gr.dl.DeleteBuild(id)
		if err != nil {
			gr.logf("error deleting build from DB: %v", err)
		}
		return nil, grpc.Errorf(codes.ResourceExhausted, "build queue is full; try again later")
	}
}

func (gr *GrpcServer) GetBuildStatus(ctx context.Context, req *BuildStatusRequest) (*BuildStatusResponse, error) {
	resp := &BuildStatusResponse{}
	id, err := gocql.ParseUUID(req.BuildId)
	if err != nil {
		return nil, grpc.Errorf(codes.InvalidArgument, "bad id: %v", err)
	}
	resp, err = gr.dl.GetBuildByID(id)
	if err != nil {
		if err == gocql.ErrNotFound {
			return nil, grpc.Errorf(codes.InvalidArgument, "build not found")
		} else {
			return nil, grpc.Errorf(codes.Internal, "error getting build: %v", err)
		}
	}
	return resp, nil
}

// Reconstruct the stream of events for a build from the data layer
func (gr *GrpcServer) eventsFromDL(stream FuranExecutor_MonitorBuildServer, id gocql.UUID) error {
	bo, err := gr.dl.GetBuildOutput(id, "build_output")
	if err != nil {
		return fmt.Errorf("error getting build output: %v", err)
	}
	po, err := gr.dl.GetBuildOutput(id, "push_output")
	if err != nil {
		return fmt.Errorf("error getting push output: %v", err)
	}
	events := append(bo, po...)
	for _, event := range events {
		err = stream.Send(&event)
		if err != nil {
			return grpc.Errorf(codes.Internal, "error sending event: %v", err)
		}
	}
	return nil
}

func (gr *GrpcServer) eventsFromEventBus(stream FuranExecutor_MonitorBuildServer, id gocql.UUID) error {
	var err error
	output := make(chan *BuildEvent)
	done := make(chan struct{})
	defer func() {
		select {
		case <-done:
			return
		default:
			close(done)
		}
	}()
	err = gr.ec.SubscribeToTopic(output, done, id)
	if err != nil {
		return err
	}
	var event *BuildEvent
	for {
		event = <-output
		if event == nil {
			return nil
		}
		err = stream.Send(event)
		if err != nil {
			return err
		}
	}
}

// MonitorBuild streams events from a specified build
func (gr *GrpcServer) MonitorBuild(req *BuildStatusRequest, stream FuranExecutor_MonitorBuildServer) error {
	id, err := gocql.ParseUUID(req.BuildId)
	if err != nil {
		return grpc.Errorf(codes.InvalidArgument, "bad build id: %v", err)
	}
	build, err := gr.dl.GetBuildByID(id)
	if err != nil {
		return grpc.Errorf(codes.Internal, "error getting build: %v", err)
	}
	if build.Finished { // No need to use Kafka, just stream events from the data layer
		return gr.eventsFromDL(stream, id)
	}
	return gr.eventsFromEventBus(stream, id)
}

// CancelBuild stops a currently-running build
func (gr *GrpcServer) CancelBuild(ctx context.Context, req *BuildCancelRequest) (*BuildStatusResponse, error) {
	return &BuildStatusResponse{}, nil
}
