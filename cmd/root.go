package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

type vaultconfig struct {
	addr       string
	token      string
	tokenAuth  bool
	appID      string
	userIDPath string
}

type gitconfig struct {
	checkoutPath     string // filesystem path to clone/checkout the repo
	privateKey       string // SSH private key for repo checkout (pulled from Vault)
	publicKey        string // SSH public key
	privKeyLocalPath string
	pubKeyLocalPath  string
}

var vaultConfig vaultconfig
var gitConfig gitconfig

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
}
