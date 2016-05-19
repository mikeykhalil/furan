package cmd

import "net/http"

var workerChan chan *buildRequest

func buildWorker() {

}

func buildRequestHandler(w http.ResponseWriter, r *http.Request) {

}

func buildStatusHandler(w http.ResponseWriter, r *http.Request) {

}

func versionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Add("Location", "https://github.com/dollarshaveclub/furan")
	w.WriteHeader(http.StatusTemporaryRedirect)
	w.Write([]byte{})
}
