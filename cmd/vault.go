package cmd

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/dollarshaveclub/go-lib/vaultclient"
)

func safeStringCast(v interface{}) string {
	switch v := v.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		log.Printf("Unknown type for Vault value: %T: %v", v, v)
		return ""
	}
}

func getVaultClient() (*vaultclient.VaultClient, error) {
	vc, err := vaultclient.NewClient(&vaultclient.VaultConfig{
		Server: vaultConfig.addr,
	})
	if err != nil {
		return vc, err
	}
	if vaultConfig.tokenAuth {
		vc.TokenAuth(vaultConfig.token)
	} else {
		if err = vc.AppIDAuth(vaultConfig.appID, vaultConfig.userIDPath); err != nil {
			return vc, err
		}
	}
	return vc, nil
}

func vaultPath(path string) string {
	return fmt.Sprintf("%v%v", vaultConfig.vaultPathPrefix, path)
}

func setupVault() {
	vc, err := getVaultClient()
	if err != nil {
		log.Fatalf("Error creating Vault client; %v", err)
	}
	ght, err := vc.GetValue(vaultPath(gitConfig.tokenVaultPath))
	if err != nil {
		log.Fatalf("Error getting GitHub token: %v", err)
	}
	dcc, err := vc.GetValue(vaultPath(dockerConfig.dockercfgVaultPath))
	if err != nil {
		log.Fatalf("Error getting dockercfg: %v", err)
	}
	gitConfig.token = safeStringCast(ght)
	dockerConfig.dockercfgRaw = safeStringCast(dcc)
}

// TLS cert/key are retrieved from Vault and must be written to temp files
func writeTLSCert() (string, string) {
	vc, err := getVaultClient()
	if err != nil {
		log.Fatalf("Error creating Vault client; %v", err)
	}
	cert, err := vc.GetValue(vaultPath(serverConfig.vaultTLSCertPath))
	if err != nil {
		log.Fatalf("Error getting TLS certificate: %v", err)
	}
	key, err := vc.GetValue(vaultPath(serverConfig.vaultTLSKeyPath))
	if err != nil {
		log.Fatalf("Error getting TLS key: %v", err)
	}
	cf, err := ioutil.TempFile("", "tls-cert")
	if err != nil {
		log.Fatalf("Error creating TLS certificate temp file: %v", err)
	}
	defer cf.Close()
	_, err = cf.Write([]byte(safeStringCast(cert)))
	if err != nil {
		log.Fatalf("Error writing TLS certificate temp file: %v", err)
	}
	kf, err := ioutil.TempFile("", "tls-key")
	if err != nil {
		log.Fatalf("Error creating TLS key temp file: %v", err)
	}
	defer kf.Close()
	_, err = kf.Write([]byte(safeStringCast(key)))
	if err != nil {
		log.Fatalf("Error writing TLS key temp file: %v", err)
	}
	return cf.Name(), kf.Name()
}

// Clean up temp files
func rmTempFiles(f1 string, f2 string) {
	for _, v := range []string{f1, f2} {
		err := os.Remove(v)
		if err != nil {
			log.Printf("Error removing file: %v", v)
		}
	}
}
