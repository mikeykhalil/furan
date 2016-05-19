package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
)

var workerChan chan *buildRequest

func buildWorker() {

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
