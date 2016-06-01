package cmd

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

type vaultconfig struct {
	addr                 string
	token                string
	tokenAuth            bool
	appID                string
	userIDPath           string
	vaultPathPrefix      string
	vaultGitHubTokenPath string
}

type gitconfig struct {
	tokenVaultPath string
	checkoutPath   string // filesystem path to clone/checkout the repo
	token          string // GitHub token
}

// struct to deserialize dockercfg into
type dockerAuth struct {
	Auth  string `json:"auth"`
	Email string `json:"email"`
}

type dockerconfig struct {
	dockercfgPath     string
	dockercfgContents map[string]dockerAuth
}

var vaultConfig vaultconfig
var gitConfig gitconfig
var dockerConfig dockerconfig

var nodestr string
var datacenterstr string

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
	RootCmd.PersistentFlags().StringVarP(&vaultConfig.addr, "vault-addr", "a", "https://vault-prod.shave.io:8200", "Vault URL")
	RootCmd.PersistentFlags().StringVarP(&vaultConfig.token, "vault-token", "t", os.Getenv("VAULT_TOKEN"), "Vault token (if using token auth)")
	RootCmd.PersistentFlags().BoolVarP(&vaultConfig.tokenAuth, "vault-token-auth", "k", false, "Use Vault token-based auth")
	RootCmd.PersistentFlags().StringVarP(&vaultConfig.appID, "vault-app-id", "p", os.Getenv("APP_ID"), "Vault App-ID")
	RootCmd.PersistentFlags().StringVarP(&vaultConfig.userIDPath, "vault-user-id-path", "u", os.Getenv("USER_ID_PATH"), "Path to file containing Vault User-ID")
	RootCmd.PersistentFlags().StringVarP(&gitConfig.checkoutPath, "checkout-path", "c", "/tmp/furan", "Local path where git repositories will be cloned")
	RootCmd.PersistentFlags().BoolVarP(&dbConfig.useConsul, "consul-db-svc", "z", false, "Discover Cassandra nodes through Consul")
	RootCmd.PersistentFlags().StringVarP(&dbConfig.consulServiceName, "svc-name", "v", "cassandra", "Consul service name for Cassandra")
	RootCmd.PersistentFlags().StringVarP(&nodestr, "db-nodes", "n", "", "Comma-delimited list of Cassandra nodes (if not using Consul discovery)")
	RootCmd.PersistentFlags().StringVarP(&datacenterstr, "db-dc", "d", "us-west-2", "Comma-delimited list of Cassandra datacenters (if not using Consul discovery)")
	RootCmd.PersistentFlags().StringVarP(&vaultConfig.vaultPathPrefix, "vault-prefix", "x", "secret/production/furan", "Vault path prefix for secrets")
	RootCmd.PersistentFlags().StringVarP(&gitConfig.tokenVaultPath, "github-token-path", "g", "github/token", "Vault path (appended to prefix) for GitHub token")
	RootCmd.PersistentFlags().StringVarP(&dockerConfig.dockercfgPath, "dockercfg-path", "e", ".dockercfg", "Path to .dockercfg (registry push authentication)")
}

func isCancelled(done <-chan struct{}) bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
}

func readDockercfg() error {
	f, err := os.Open(dockerConfig.dockercfgPath)
	if err != nil {
		return err
	}
	defer f.Close()
	d := json.NewDecoder(f)
	return d.Decode(&dockerConfig.dockercfgContents)
}
