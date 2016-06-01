package cmd

import (
	"fmt"
	"log"
	"net"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

type grpcserver struct {
}

var grpcServer grpcserver

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

// gRPC handlers
func (gr *grpcserver) StartBuild(ctx context.Context, req *BuildRequest) (*BuildRequestResponse, error) {
	resp := &BuildRequestResponse{}
	if req.Push.Registry.Repo == "" {
		if req.Push.S3.Bucket == "" || req.Push.S3.KeyPrefix == "" || req.Push.S3.Region == "" {
			return resp, fmt.Errorf("push registry and S3 configuration are both empty (at least one is required)")
		}
	}
	builder, err := NewImageBuilder(gitConfig.token)
	if err != nil {
		return resp, fmt.Errorf("error creating image builder: %v", err)
	}
	err = builder.Build(ctx, req)
	if err != nil {
		return resp, err
	}
	if req.Push.Registry.Repo == "" {
		err = builder.PushBuildToS3(ctx, req)
	} else {
		err = builder.PushBuildToRegistry(ctx, req)
	}
	if err != nil {
		return resp, fmt.Errorf("error pushing: %v", err)
	}
	return resp, nil
}

func (gr *grpcserver) GetBuildStatus(ctx context.Context, req *BuildStatusRequest) (*BuildStatusResponse, error) {
	return &BuildStatusResponse{}, nil
}

func (gr *grpcserver) CancelBuild(ctx context.Context, req *BuildCancelRequest) (*BuildStatusResponse, error) {
	return &BuildStatusResponse{}, nil
}
