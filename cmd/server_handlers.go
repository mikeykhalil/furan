package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/golang/protobuf/jsonpb"
	"github.com/golang/protobuf/proto"
	"golang.org/x/net/context"
)

var pbMarshaler jsonpb.Marshaler

// REST interface handlers (proxy to gRPC handlers)
func buildRequestHandler(w http.ResponseWriter, r *http.Request) {
	var req BuildRequest
	err := jsonpb.Unmarshal(r.Body, &req)
	if err != nil {
		badRequestError(w, err)
		return
	}
	resp, err := grpcServer.StartBuild(context.TODO(), &req)
	if err != nil {
		internalError(w, err)
		return
	}
	httpSuccess(w, resp)
}

func buildStatusHandler(w http.ResponseWriter, r *http.Request) {
	var req BuildStatusRequest
	err := jsonpb.Unmarshal(r.Body, &req)
	if err != nil {
		badRequestError(w, err)
		return
	}
	resp, err := grpcServer.GetBuildStatus(context.TODO(), &req)
	if err != nil {
		internalError(w, err)
		return
	}
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
		internalError(w, err)
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
