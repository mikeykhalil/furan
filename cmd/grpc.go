package cmd

import (
	"fmt"
	"log"
	"net"

	"github.com/gocql/gocql"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

type grpcserver struct {
	ib ImageBuildPusher
	dl DataLayer
}

var grpcServer grpcserver
var workerChan chan *workerRequest

type workerRequest struct {
	ctx context.Context
	req *BuildRequest
}

func startgRPC() {
	imageBuilder, err := NewImageBuilder(gitConfig.token, kafkaConfig.producer, dbConfig.datalayer)
	if err != nil {
		log.Fatalf("error creating image builder: %v", err)
	}

	grpcServer = grpcserver{
		ib: imageBuilder,
		dl: dbConfig.datalayer,
	}

	runWorkers()
	go listenRPC()
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
	RegisterFuranExecutorServer(s, &grpcServer)
	log.Printf("gRPC listening on: %v", addr)
	s.Serve(l)
}

func (gr *grpcserver) finishBuild(id gocql.UUID, failed bool) error {
	flags := map[string]bool{
		"failed":   failed,
		"finished": true,
	}
	return gr.dl.SetBuildFlags(id, flags)
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

// Performs build synchronously
func (gr *grpcserver) syncBuild(ctx context.Context, req *BuildRequest) {
	if isCancelled(ctx.Done()) {
		log.Printf("build was cancelled")
	}
	id, ok := BuildIDFromContext(ctx)
	if !ok {
		log.Printf("build id missing from context")
		return
	}
	err := gr.dl.SetBuildState(id, BuildStatusResponse_BUILDING)
	if err != nil {
		gr.finishBuild(id, true)
		log.Printf("error setting build state to building: %v", err)
		return
	}
	imageid, err := gr.ib.Build(ctx, req, id)
	if err != nil {
		log.Printf("error performing build: %v", err)
		gr.dl.SetBuildState(id, BuildStatusResponse_BUILD_FAILURE)
		gr.finishBuild(id, true)
		return
	}
	err = gr.dl.SetBuildState(id, BuildStatusResponse_PUSHING)
	if err != nil {
		gr.finishBuild(id, true)
		log.Printf("error setting build state to pushing: %v", err)
		return
	}
	if req.Push.Registry.Repo == "" {
		err = gr.ib.PushBuildToS3(ctx, req)
	} else {
		err = gr.ib.PushBuildToRegistry(ctx, req)
	}
	if err != nil {
		gr.finishBuild(id, true)
		gr.dl.SetBuildState(id, BuildStatusResponse_PUSH_FAILURE)
		log.Printf("error pushing: %v", err)
		return
	}
	err = gr.ib.CleanImage(ctx, imageid)
	if err != nil {
		gr.finishBuild(id, true)
		gr.dl.SetBuildState(id, BuildStatusResponse_PUSH_FAILURE)
		log.Printf("error cleaning built image: %v", err)
		return
	}
	err = gr.dl.SetBuildState(id, BuildStatusResponse_SUCCESS)
	if err != nil {
		gr.finishBuild(id, true)
		log.Printf("error setting build state to success: %v", err)
		return
	}
	err = gr.finishBuild(id, false)
	if err != nil {
		log.Printf("error finalizing build: %v", err)
	}
	err = gr.dl.SetBuildCompletedTimestamp(id)
	if err != nil {
		log.Printf("error setting build completed timestamp: %v", err)
	}
	log.Printf("build success for %v", id.String())
}

// gRPC handlers
func (gr *grpcserver) StartBuild(ctx context.Context, req *BuildRequest) (*BuildRequestResponse, error) {
	resp := &BuildRequestResponse{
		Error: &RPCError{},
	}
	if req.Push.Registry.Repo == "" {
		if req.Push.S3.Bucket == "" || req.Push.S3.KeyPrefix == "" || req.Push.S3.Region == "" {
			resp.Error.ErrorType = RPCError_BAD_REQUEST
			resp.Error.IsError = true
			resp.Error.ErrorMsg = "push registry and S3 configuration are both empty (at least one is required)"
			return resp, nil
		}
	}
	id, err := gr.dl.CreateBuild(req)
	if err != nil {
		resp.Error.ErrorType = RPCError_INTERNAL_ERROR
		resp.Error.IsError = true
		resp.Error.ErrorMsg = fmt.Sprintf("error creating build in DB: %v", err)
		return resp, nil
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
			log.Printf("error deleting build from DB: %v", err)
		}
		resp.Error.IsError = true
		resp.Error.ErrorType = RPCError_BAD_REQUEST
		resp.Error.ErrorMsg = "build queue is full; try again later"
		return resp, nil
	}
}

func (gr *grpcserver) GetBuildStatus(ctx context.Context, req *BuildStatusRequest) (*BuildStatusResponse, error) {
	resp := &BuildStatusResponse{
		Error: &RPCError{},
	}
	id, err := gocql.ParseUUID(req.BuildId)
	if err != nil {
		resp.Error.IsError = true
		resp.Error.ErrorMsg = fmt.Sprintf("bad id: %v", err)
		resp.Error.ErrorType = RPCError_BAD_REQUEST
		return resp, nil
	}
	resp, err = gr.dl.GetBuildByID(id)
	if err != nil {
		if err == gocql.ErrNotFound {
			resp.Error.ErrorType = RPCError_BAD_REQUEST
		} else {
			resp.Error.ErrorType = RPCError_INTERNAL_ERROR
		}
		resp.Error.IsError = true
		resp.Error.ErrorMsg = fmt.Sprintf("error getting build: %v", err)
	}
	return resp, nil
}

func (gr *grpcserver) CancelBuild(ctx context.Context, req *BuildCancelRequest) (*BuildStatusResponse, error) {
	return &BuildStatusResponse{}, nil
}
