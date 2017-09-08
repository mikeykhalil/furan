package config

import (
	dtypes "github.com/docker/engine-api/types"
	"github.com/dollarshaveclub/furan/lib/datalayer"
	"github.com/dollarshaveclub/furan/lib/kafka"
	"github.com/gocql/gocql"
)

var version = "0"
var description = "unknown"

type Vaultconfig struct {
	Addr            string
	Token           string
	TokenAuth       bool
	AppID           string
	UserIDPath      string
	VaultPathPrefix string
}

type Gitconfig struct {
	TokenVaultPath string
	Token          string // GitHub token
}

type Dockerconfig struct {
	DockercfgVaultPath string
	DockercfgRaw       string
	DockercfgContents  map[string]dtypes.AuthConfig
}

type Kafkaconfig struct {
	Brokers      []string
	Topic        string
	Manager      kafka.EventBusManager
	MaxOpenSends uint
}

// AWSConfig contains all information needed to access AWS services
type AWSConfig struct {
	AccessKeyID     string
	SecretAccessKey string
	Concurrency     uint
}

type DBconfig struct {
	Nodes             []string
	UseConsul         bool
	ConsulServiceName string
	DataCenters       []string
	Cluster           *gocql.ClusterConfig
	Datalayer         datalayer.DataLayer
	Keyspace          string
}

type Serverconfig struct {
	HTTPSPort               uint
	GRPCPort                uint
	PPROFPort               uint
	HTTPSAddr               string
	GRPCAddr                string
	Concurrency             uint
	Queuesize               uint
	VaultTLSCertPath        string
	VaultTLSKeyPath         string
	TLSCert                 []byte
	TLSKey                  []byte
	LogToSumo               bool
	SumoURL                 string
	VaultSumoURLPath        string
	HealthcheckHTTPport     uint
	S3ErrorLogs             bool
	S3ErrorLogBucket        string
	S3ErrorLogRegion        string
	S3PresignTTL            uint
	GCIntervalSecs          uint
	DockerDiskPath          string
	NewRelicAPIKeyVaultPath string
	NewRelicAPIKey          string
	EnableNewRelic          bool
	NewRelicApp             string
}

type Consulconfig struct {
	Addr     string
	KVPrefix string
}
