package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"

	"golang.org/x/net/context"
)

var workerChan chan *BuildRequest

func buildWorker() {

}

type grpcServer struct {
}

func (gr *grpcServer) StartBuild(ctx context.Context, req *BuildRequest) (*BuildRequestResponse, error) {
	return &BuildRequestResponse{}, nil
}

func (gr *grpcServer) GetBuildStatus(ctx context.Context, req *BuildStatusRequest) (*BuildStatusResponse, error) {
	return &BuildStatusResponse{}, nil
}

func (gr *grpcServer) CancelBuild(ctx context.Context, req *BuildCancelRequest) (*BuildStatusResponse, error) {
	return &BuildStatusResponse{}, nil
}

func buildRequestHandler(w http.ResponseWriter, r *http.Request) {

}

func buildStatusHandler(w http.ResponseWriter, r *http.Request) {

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
