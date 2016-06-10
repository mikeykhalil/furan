package cmd

import (
	"encoding/json"
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
func buildRequestHandler(w http.ResponseWriter, r *http.Request) {
	req, err := unmarshalRequest(r.Body)
	if err != nil {
		badRequestError(w, err)
		return
	}
	resp, err := grpcServer.StartBuild(context.TODO(), req)
	if err != nil {
		handleRPCError(w, err)
		return
	}
	httpSuccess(w, resp)
}

func buildStatusHandler(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	req := BuildStatusRequest{
		BuildId: id,
	}
	resp, err := grpcServer.GetBuildStatus(context.TODO(), &req)
	if err != nil {
		handleRPCError(w, err)
		return
	}
	resp.BuildOutput = []byte{}
	resp.PushOutput = []byte{}
	httpSuccess(w, resp)
}

func buildCancelHandler(w http.ResponseWriter, r *http.Request) {
	var req BuildCancelRequest
	err := jsonpb.Unmarshal(r.Body, &req)
	if err != nil {
		badRequestError(w, err)
		return
	}
	resp, err := grpcServer.CancelBuild(context.TODO(), &req)
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

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if cap(workerChan) > 0 {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusTooManyRequests)
	}
}

func versionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Content-Type", "application/json")
	version := struct {
		Name        string `json:"name"`
		Version     string `json:"version"`
		Description string `json:"description"`
	}{
		Name:        "furan",
		Version:     version,
		Description: description,
	}
	vb, err := json.Marshal(version)
	if err != nil {
		w.Write([]byte(fmt.Sprintf(`{"error": "error marshalling version: %v"}`, err)))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	w.Write(vb)
}
