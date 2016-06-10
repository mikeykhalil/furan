package cmd

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"regexp"

	"github.com/gocql/gocql"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

type grpcserver struct {
	ib ImageBuildPusher
	dl DataLayer
	ep EventBusProducer
	ec EventBusConsumer
	ls io.Writer
}

var grpcServer *grpcserver
var workerChan chan *workerRequest

type workerRequest struct {
	ctx context.Context
	req *BuildRequest
}

func startgRPC() {
	imageBuilder, err := NewImageBuilder(gitConfig.token, kafkaConfig.manager, dbConfig.datalayer, os.Stdout)
	if err != nil {
		log.Fatalf("error creating image builder: %v", err)
	}

	grpcServer = NewGRPCServer(imageBuilder, dbConfig.datalayer, kafkaConfig.manager, kafkaConfig.manager, os.Stdout)

	runWorkers()
	go listenRPC()
}

// NewGRPCServer returns a new instance of the gRPC server
func NewGRPCServer(ib ImageBuildPusher, dl DataLayer, ep EventBusProducer, ec EventBusConsumer, logsink io.Writer) *grpcserver {
	return &grpcserver{
		ib: ib,
		dl: dl,
		ep: ep,
		ec: ec,
		ls: logsink,
	}
}

func buildWorker() {
	var wreq *workerRequest
	for {
		wreq = <-workerChan
		if !isCancelled(wreq.ctx.Done()) {
			grpcServer.syncBuild(wreq.ctx, wreq.req)
		}
	}
}

func listenRPC() {
	addr := fmt.Sprintf("%v:%v", serverConfig.grpcAddr, serverConfig.grpcPort)
	l, err := net.Listen("tcp", addr)
	if err != nil {
		log.Printf("error starting gRPC listener: %v", err)
		return
	}
	s := grpc.NewServer()
	RegisterFuranExecutorServer(s, grpcServer)
	log.Printf("gRPC listening on: %v", addr)
	s.Serve(l)
}

type ctxIDKeyType string

var ctxIDKey ctxIDKeyType = "id"

// NewBuildIDContext returns a context with the current build ID stored as a value
func NewBuildIDContext(ctx context.Context, id gocql.UUID) context.Context {
	return context.WithValue(ctx, ctxIDKey, id)
}

// BuildIDFromContext returns the ID stored in ctx, if any
func BuildIDFromContext(ctx context.Context) (gocql.UUID, bool) {
	id, ok := ctx.Value(ctxIDKey).(gocql.UUID)
	return id, ok
}

func (gr *grpcserver) log(msg string, params ...interface{}) {
	gr.ls.Write([]byte(fmt.Sprintf(msg, params...)))
}

// Performs build synchronously
func (gr *grpcserver) syncBuild(ctx context.Context, req *BuildRequest) (outcome BuildStatusResponse_BuildState) {
	var err error // so deferred finalize function has access to any error
	id, ok := BuildIDFromContext(ctx)
	if !ok {
		gr.log("build id missing from context")
		return
	}
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
		} else {
			eet = BuildEventError_NO_ERROR
		}
		var msg string
		if err != nil {
			msg = err.Error()
		} else {
			msg = "build finished"
		}
		if err2 := gr.dl.SetBuildState(id, outcome); err2 != nil {
			gr.log("failBuild: error setting build state: %v", err2)
		}
		if err2 := gr.dl.SetBuildFlags(id, flags); err2 != nil {
			gr.log("failBuild: error setting build flags: %v", err2)
		}
		if err2 := gr.ep.PublishEvent(id, msg, BuildEvent_LOG, eet, true); err2 != nil {
			gr.log("failBuild: error publishing event: %v", err2)
		}
		gr.log("%v: %v: failed: %v", id.String(), msg, flags["failed"])
	}(id)
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
	if err != nil {
		err = fmt.Errorf("error performing build: %v", err)
		return BuildStatusResponse_BUILD_FAILURE
	}
	err = gr.dl.SetBuildState(id, BuildStatusResponse_PUSHING)
	if err != nil {
		err = fmt.Errorf("error setting build state to pushing: %v", err)
		return BuildStatusResponse_BUILD_FAILURE
	}
	if req.Push.Registry.Repo == "" {
		err = gr.ib.PushBuildToS3(ctx, req)
	} else {
		err = gr.ib.PushBuildToRegistry(ctx, req)
	}
	if err != nil {
		err = fmt.Errorf("error pushing: %v", err)
		return BuildStatusResponse_PUSH_FAILURE
	}
	err = gr.ib.CleanImage(ctx, imageid)
	if err != nil {
		err = fmt.Errorf("error cleaning built image: %v", err)
		return BuildStatusResponse_PUSH_FAILURE
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
func (gr *grpcserver) StartBuild(ctx context.Context, req *BuildRequest) (*BuildRequestResponse, error) {
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
	ctx = NewBuildIDContext(ctx, id)
	wreq := workerRequest{
		ctx: ctx,
		req: req,
	}
	select {
	case workerChan <- &wreq:
		resp.BuildId = id.String()
		return resp, nil
	default:
		err = gr.dl.DeleteBuild(id)
		if err != nil {
			gr.log("error deleting build from DB: %v", err)
		}
		return nil, grpc.Errorf(codes.ResourceExhausted, "build queue is full; try again later")
	}
}

func (gr *grpcserver) GetBuildStatus(ctx context.Context, req *BuildStatusRequest) (*BuildStatusResponse, error) {
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

var dbPattern = regexp.MustCompile(`^{"stream":"`)
var dpPattern = regexp.MustCompile(`^{"status":".+"progressDetail"`)

func (gr *grpcserver) eventFromBytes(eb []byte) *BuildEvent {
	event := &BuildEvent{
		EventError: &BuildEventError{ErrorType: BuildEventError_NO_ERROR},
	}
	switch {
	case dbPattern.Match(eb):
		event.EventType = BuildEvent_DOCKER_BUILD_STREAM
	case dpPattern.Match(eb):
		event.EventType = BuildEvent_DOCKER_PUSH_STREAM
	default:
		event.EventType = BuildEvent_LOG
	}
	event.Message = string(eb)
	return event
}

// Reconstruct the stream of events for a build from the data layer
func (gr *grpcserver) eventsFromDL(stream FuranExecutor_MonitorBuildServer, build *BuildStatusResponse) error {
	rdr := bufio.NewReader(bytes.NewBuffer(append(build.BuildOutput, build.PushOutput...)))
	var err error
	var line []byte
	for {
		line, err = rdr.ReadBytes('\n')
		if err != nil {
			if err == io.EOF {
				break
			}
			return grpc.Errorf(codes.Internal, "error reading stored build output stream: %v", err)
		}
		err = stream.Send(gr.eventFromBytes(line))
		if err != nil {
			return grpc.Errorf(codes.Internal, "error sending event: %v", err)
		}
	}
	return nil
}

func (gr *grpcserver) eventsFromEventBus(stream FuranExecutor_MonitorBuildServer, id gocql.UUID) error {
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

func (gr *grpcserver) MonitorBuild(req *BuildStatusRequest, stream FuranExecutor_MonitorBuildServer) error {
	id, err := gocql.ParseUUID(req.BuildId)
	if err != nil {
		return grpc.Errorf(codes.InvalidArgument, "bad build id: %v", err)
	}
	build, err := gr.dl.GetBuildByID(id)
	if err != nil {
		return grpc.Errorf(codes.Internal, "error getting build: %v", err)
	}
	if build.Finished { // No need to use Kafka, just stream events from the data layer
		return gr.eventsFromDL(stream, build)
	}
	return gr.eventsFromEventBus(stream, id)
}

func (gr *grpcserver) CancelBuild(ctx context.Context, req *BuildCancelRequest) (*BuildStatusResponse, error) {
	return &BuildStatusResponse{}, nil
}
