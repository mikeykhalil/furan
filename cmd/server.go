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

type serverconfig struct {
	httpsPort        uint
	grpcPort         uint
	httpsAddr        string
	grpcAddr         string
	concurrency      uint
	queuesize        uint
	vaultTLSCertPath string
	vaultTLSKeyPath  string
	tlsCert          []byte
	tlsKey           []byte
}

type kafkaconfig struct {
	brokers      []string
	topic        string
	producer     *KafkaProducer
	maxOpenSends uint
}

var serverConfig serverconfig
var kafkaConfig kafkaconfig

var version = "0"
var description = "unknown"

var kafkaBrokerStr string

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
	serverCmd.PersistentFlags().UintVar(&serverConfig.queuesize, "queue", 100, "Max queue size for buffered build requests")
	serverCmd.PersistentFlags().StringVar(&serverConfig.vaultTLSCertPath, "tls-cert-path", "/tls/cert", "Vault path to TLS certificate")
	serverCmd.PersistentFlags().StringVar(&serverConfig.vaultTLSKeyPath, "tls-key-path", "/tls/key", "Vault path to TLS private key")
	serverCmd.PersistentFlags().StringVar(&kafkaBrokerStr, "kafka-brokers", "localhost:9092", "Comma-delimited list of Kafka brokers")
	serverCmd.PersistentFlags().StringVar(&kafkaConfig.topic, "kafka-topic", "furan-events", "Kafka topic to publish build events (required for build monitoring)")
	serverCmd.PersistentFlags().UintVar(&kafkaConfig.maxOpenSends, "kafka-max-open-sends", 100, "Max number of simultaneous in-flight Kafka message sends")
	RootCmd.AddCommand(serverCmd)
}

func server(cmd *cobra.Command, args []string) {
	setupVault()
	setupDB()
	setupKafka()
	certPath, keyPath := writeTLSCert()
	defer rmTempFiles(certPath, keyPath)
	err := readDockercfg()
	if err != nil {
		log.Fatalf("error reading dockercfg: %v", err)
	}

	startgRPC()

	r := mux.NewRouter()
	r.HandleFunc("/", versionHandler).Methods("GET")
	r.HandleFunc("/build", buildRequestHandler).Methods("POST")
	r.HandleFunc("/build/{id}", buildStatusHandler).Methods("GET")
	r.HandleFunc("/build/{id}", buildCancelHandler).Methods("DELETE")
	r.HandleFunc("/health", healthHandler).Methods("GET")

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
	workerChan = make(chan *workerRequest, serverConfig.queuesize)
	for i := 0; uint(i) < serverConfig.concurrency; i++ {
		go buildWorker()
	}
	log.Printf("%v workers running (queue: %v)", serverConfig.concurrency, serverConfig.queuesize)
}
