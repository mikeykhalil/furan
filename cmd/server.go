package cmd

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/spf13/cobra"
)

const (
	workerBufferSize = 1000 // buffered pending build requests
)

type serverconfig struct {
	port        uint
	addr        string
	concurrency uint
	tlsCert     []byte
	tlsKey      []byte
}

var serverConfig serverconfig

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Run Furan server",
	Long:  `Furan API server (see docs)`,
	Run:   server,
}

func init() {
	serverCmd.PersistentFlags().UintVarP(&serverConfig.port, "port", "p", 4000, "TCP port")
	serverCmd.PersistentFlags().StringVarP(&serverConfig.addr, "addr", "a", "0.0.0.0", "listen addr")
	serverCmd.PersistentFlags().UintVarP(&serverConfig.concurrency, "concurrency", "c", 10, "Max concurrent builds")
	RootCmd.AddCommand(serverCmd)
}

func server(cmd *cobra.Command, args []string) {
	setupVault()
	certPath, keyPath := writeTLSCert()
	defer rmTLSCert(certPath, keyPath)

	r := mux.NewRouter()
	r.HandleFunc("/", versionHandler).Methods("GET")
	r.HandleFunc("/build", buildRequestHandler).Methods("POST")
	r.HandleFunc("/build/{id}", buildStatusHandler).Methods("GET")

	tlsconfig := &tls.Config{MinVersion: tls.VersionTLS12}
	addr := fmt.Sprintf("%v:%v", serverConfig.addr, serverConfig.port)
	server := &http.Server{Addr: addr, Handler: r, TLSConfig: tlsconfig}
	log.Printf("Listening on: %v", addr)
	log.Println(server.ListenAndServeTLS(certPath, keyPath))
}

func runWorkers() {
	workerChan = make(chan *buildRequest, workerBufferSize)
	log.Printf("running %v workers (buffer: %v)", serverConfig.concurrency, workerBufferSize)
	for i := 0; uint(i) < serverConfig.concurrency; i++ {
		go buildWorker()
	}
}
