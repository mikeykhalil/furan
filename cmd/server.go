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
	httpsPort        uint
	grpcPort         uint
	httpsAddr        string
	grpcAddr         string
	concurrency      uint
	vaultTLSCertPath string
	vaultTLSKeyPath  string
	tlsCert          []byte
	tlsKey           []byte
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
	serverCmd.PersistentFlags().UintVar(&serverConfig.httpsPort, "https-port", 4000, "REST HTTPS TCP port")
	serverCmd.PersistentFlags().UintVar(&serverConfig.grpcPort, "grpc-port", 4001, "gRPC TCP port")
	serverCmd.PersistentFlags().StringVar(&serverConfig.httpsAddr, "https-addr", "0.0.0.0", "REST HTTPS listen address")
	serverCmd.PersistentFlags().StringVar(&serverConfig.grpcAddr, "grpc-addr", "0.0.0.0", "gRPC listen address")
	serverCmd.PersistentFlags().UintVar(&serverConfig.concurrency, "concurrency", 10, "Max concurrent builds")
	serverCmd.PersistentFlags().StringVar(&serverConfig.vaultTLSCertPath, "tls-cert-path", "tls/cert", "Vault path to TLS certificate")
	serverCmd.PersistentFlags().StringVar(&serverConfig.vaultTLSKeyPath, "tls-key-path", "tls/key", "Vault path to TLS private key")
	RootCmd.AddCommand(serverCmd)
}

func server(cmd *cobra.Command, args []string) {
	setupVault()
	setupDB()
	certPath, keyPath := writeTLSCert()
	defer rmTempFiles(certPath, keyPath)
	err := readDockercfg()
	if err != nil {
		log.Fatalf("error reading dockercfg: %v", err)
	}

	go listenRPC()

	r := mux.NewRouter()
	r.HandleFunc("/", versionHandler).Methods("GET")
	r.HandleFunc("/build", buildRequestHandler).Methods("POST")
	r.HandleFunc("/build/{id}", buildStatusHandler).Methods("GET")
	r.HandleFunc("/build/{id}", buildCancelHandler).Methods("DELETE")

	tlsconfig := &tls.Config{MinVersion: tls.VersionTLS12}
	addr := fmt.Sprintf("%v:%v", serverConfig.httpsAddr, serverConfig.httpsPort)
	server := &http.Server{Addr: addr, Handler: r, TLSConfig: tlsconfig}
	log.Printf("HTTPS REST listening on: %v", addr)
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
	workerChan = make(chan *BuildRequest, workerBufferSize)
	log.Printf("running %v workers (buffer: %v)", serverConfig.concurrency, workerBufferSize)
	for i := 0; uint(i) < serverConfig.concurrency; i++ {
		go buildWorker()
	}
}
