package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	dtypes "github.com/docker/engine-api/types"
	"github.com/spf13/cobra"
)

type vaultconfig struct {
	addr            string
	token           string
	tokenAuth       bool
	appID           string
	userIDPath      string
	vaultPathPrefix string
}

type gitconfig struct {
	tokenVaultPath string
	token          string // GitHub token
}

type dockerconfig struct {
	dockercfgVaultPath string
	dockercfgRaw       string
	dockercfgContents  map[string]dtypes.AuthConfig
}

var vaultConfig vaultconfig
var gitConfig gitconfig
var dockerConfig dockerconfig

var nodestr string
var datacenterstr string
var initializeDB bool
var kafkaBrokerStr string

// RootCmd represents the base command when called without any subcommands
var RootCmd = &cobra.Command{
	Use:   "furan",
	Short: "Docker image builder",
	Long:  `API application to build Docker images on command`,
}

// Execute is the entry point for the app
func Execute() {
	if err := RootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(-1)
	}
}

func init() {
	home := os.Getenv("HOME")
	if home != "" {
		home += "/"
	}

	RootCmd.PersistentFlags().StringVarP(&vaultConfig.addr, "vault-addr", "a", "https://vault-prod.shave.io:8200", "Vault URL")
	RootCmd.PersistentFlags().StringVarP(&vaultConfig.token, "vault-token", "t", os.Getenv("VAULT_TOKEN"), "Vault token (if using token auth)")
	RootCmd.PersistentFlags().BoolVarP(&vaultConfig.tokenAuth, "vault-token-auth", "k", false, "Use Vault token-based auth")
	RootCmd.PersistentFlags().StringVarP(&vaultConfig.appID, "vault-app-id", "p", os.Getenv("APP_ID"), "Vault App-ID")
	RootCmd.PersistentFlags().StringVarP(&vaultConfig.userIDPath, "vault-user-id-path", "u", os.Getenv("USER_ID_PATH"), "Path to file containing Vault User-ID")
	RootCmd.PersistentFlags().BoolVarP(&dbConfig.useConsul, "consul-db-svc", "z", false, "Discover Cassandra nodes through Consul")
	RootCmd.PersistentFlags().StringVarP(&dbConfig.consulServiceName, "svc-name", "v", "cassandra", "Consul service name for Cassandra")
	RootCmd.PersistentFlags().StringVarP(&nodestr, "db-nodes", "n", "", "Comma-delimited list of Cassandra nodes (if not using Consul discovery)")
	RootCmd.PersistentFlags().StringVarP(&datacenterstr, "db-dc", "d", "us-west-2", "Comma-delimited list of Cassandra datacenters (if not using Consul discovery)")
	RootCmd.PersistentFlags().BoolVarP(&initializeDB, "db-init", "i", false, "Initialize DB keyspace and tables (only necessary on first run)")
	RootCmd.PersistentFlags().StringVarP(&dbConfig.keyspace, "db-keyspace", "b", "furan", "Cassandra keyspace")
	RootCmd.PersistentFlags().UintVarP(&dbConfig.rfPerDC, "db-rf-per-dc", "l", 2, "Cassandra replication factor per DC (if initializing DB)")
	RootCmd.PersistentFlags().StringVarP(&vaultConfig.vaultPathPrefix, "vault-prefix", "x", "secret/production/furan", "Vault path prefix for secrets")
	RootCmd.PersistentFlags().StringVarP(&gitConfig.tokenVaultPath, "github-token-path", "g", "/github/token", "Vault path (appended to prefix) for GitHub token")
	RootCmd.PersistentFlags().StringVarP(&dockerConfig.dockercfgVaultPath, "vault-dockercfg-path", "e", "/dockercfg", "Vault path to .dockercfg contents")
	RootCmd.PersistentFlags().StringVarP(&kafkaBrokerStr, "kafka-brokers", "f", "localhost:9092", "Comma-delimited list of Kafka brokers")
	RootCmd.PersistentFlags().StringVarP(&kafkaConfig.topic, "kafka-topic", "m", "furan-events", "Kafka topic to publish build events (required for build monitoring)")
	RootCmd.PersistentFlags().UintVarP(&kafkaConfig.maxOpenSends, "kafka-max-open-sends", "j", 100, "Max number of simultaneous in-flight Kafka message sends")
}

func isCancelled(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func getDockercfg() error {
	err := json.Unmarshal([]byte(dockerConfig.dockercfgRaw), &dockerConfig.dockercfgContents)
	if err != nil {
		return err
	}
	for k, v := range dockerConfig.dockercfgContents {
		if v.Auth != "" && v.Username == "" && v.Password == "" {
			// Auth is a base64-encoded string of the form USERNAME:PASSWORD
			ab, err := base64.StdEncoding.DecodeString(v.Auth)
			if err != nil {
				return fmt.Errorf("dockercfg: couldn't decode auth string: %v: %v", k, err)
			}
			as := strings.Split(string(ab), ":")
			if len(as) != 2 {
				return fmt.Errorf("dockercfg: malformed auth string: %v: %v: %v", k, v.Auth, string(ab))
			}
			v.Username = as[0]
			v.Password = as[1]
			v.Auth = ""
		}
		v.ServerAddress = k
		dockerConfig.dockercfgContents[k] = v
	}
	return nil
}
