package lib

import (
	"fmt"
	"io"
	"net/http"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"github.com/gorilla/mux"
	"golang.org/x/net/context"
)

var pbMarshaler jsonpb.Marshaler

// any null values (omitted) will be deserialized as nil, replace with empty structs
func unmarshalRequest(r io.Reader) (*BuildRequest, error) {
	req := BuildRequest{}
	err := jsonpb.Unmarshal(r, &req)
	if err != nil {
		return nil, err
	}
	if req.Build == nil {
		req.Build = &BuildDefinition{}
	}
	if req.Push == nil {
		req.Push = &PushDefinition{
			Registry: &PushRegistryDefinition{},
			S3:       &PushS3Definition{},
		}
	}
	if req.Push.Registry == nil {
		req.Push.Registry = &PushRegistryDefinition{}
	}
	if req.Push.S3 == nil {
		req.Push.S3 = &PushS3Definition{}
	}
	return &req, nil
}

func handleRPCError(w http.ResponseWriter, err error) {
	code := grpc.Code(err)
	switch code {
	case codes.InvalidArgument:
		badRequestError(w, err)
	case codes.Internal:
		internalError(w, err)
	default:
		internalError(w, err)
	}
}

// REST interface handlers (proxy to gRPC handlers)
func BuildRequestHandler(w http.ResponseWriter, r *http.Request) {
	req, err := unmarshalRequest(r.Body)
	if err != nil {
		badRequestError(w, err)
		return
	}
	resp, err := grpcSvr.StartBuild(context.Background(), req)
	if err != nil {
		handleRPCError(w, err)
		return
	}
	httpSuccess(w, resp)
}

func BuildStatusHandler(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	req := BuildStatusRequest{
		BuildId: id,
	}
	resp, err := grpcSvr.GetBuildStatus(context.Background(), &req)
	if err != nil {
		handleRPCError(w, err)
		return
	}
	httpSuccess(w, resp)
}

func BuildCancelHandler(w http.ResponseWriter, r *http.Request) {
	var req BuildCancelRequest
	err := jsonpb.Unmarshal(r.Body, &req)
	if err != nil {
		badRequestError(w, err)
		return
	}
	resp, err := grpcSvr.CancelBuild(context.Background(), &req)
	if err != nil {
		handleRPCError(w, err)
		return
	}
	httpSuccess(w, resp)
}

func httpSuccess(w http.ResponseWriter, resp proto.Message) {
	js, err := pbMarshaler.MarshalToString(resp)
	if err != nil {
		internalError(w, err)
		return
	}
	w.Header().Add("Content-Type", "application/json")
	w.Write([]byte(js))
}

func badRequestError(w http.ResponseWriter, err error) {
	httpError(w, http.StatusBadRequest, err)
}

func internalError(w http.ResponseWriter, err error) {
	httpError(w, http.StatusInternalServerError, err)
}

func httpError(w http.ResponseWriter, code int, err error) {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(code)
	w.Write([]byte(fmt.Sprintf(`{"error_details":"%v"}`, err)))
}

func HealthHandler(w http.ResponseWriter, r *http.Request) {
	if cap(grpcSvr.workerChan) > 0 {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusTooManyRequests)
	}
}
