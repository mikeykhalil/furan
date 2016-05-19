package cmd

import (
	"crypto/tls"
	"fmt"
	"log"
	"net/http"
	"os"

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

var version = "0"
var description = "unknown"

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
	setupVault()
	certPath, keyPath := writeTLSCert()
	gitConfig.pubKeyLocalPath, gitConfig.privKeyLocalPath = writeSSHKeypair()
	defer rmTempFiles(certPath, keyPath)
	defer rmTempFiles(gitConfig.pubKeyLocalPath, gitConfig.privKeyLocalPath)

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

func setupVersion() {
	bv := make([]byte, 20)
	bd := make([]byte, 2048)
	fv, err := os.Open("VERSION.txt")
	if err != nil {
		return
	}
	defer fv.Close()
	sv, err := fv.Read(bv)
	if err != nil {
		return
	}
	fd, err := os.Open("DESCRIPTION.txt")
	if err != nil {
		return
	}
	defer fd.Close()
	sd, err := fd.Read(bd)
	if err != nil {
		return
	}
	version = string(bv[:sv])
	description = string(bd[:sd])
}

func runWorkers() {
	workerChan = make(chan *buildRequest, workerBufferSize)
	log.Printf("running %v workers (buffer: %v)", serverConfig.concurrency, workerBufferSize)
	for i := 0; uint(i) < serverConfig.concurrency; i++ {
		go buildWorker()
	}
}
