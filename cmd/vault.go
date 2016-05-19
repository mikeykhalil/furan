package cmd

import (
	"io/ioutil"
	"log"
	"os"

	"github.com/dollarshaveclub/go-lib/vaultclient"
)

const (
	vaultTLSKeyPath   = "secret/furan/tls/key"
	vaultTLSCertPath  = "secret/furan/tls/cert"
	sshPrivateKeyPath = "secret/furan/github/ssh_private_key"
	sshPublicKeyPath  = "secret/furan/github/ssh_public_key"
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

func setupVault() {
	vc, err := getVaultClient()
	if err != nil {
		log.Fatalf("Error creating Vault client; %v", err)
	}
	cert, err := vc.GetValue(vaultTLSCertPath)
	if err != nil {
		log.Fatalf("Error getting TLS certificate: %v", err)
	}
	key, err := vc.GetValue(vaultTLSKeyPath)
	if err != nil {
		log.Fatalf("Error getting TLS key: %v", err)
	}
	pk, err := vc.GetValue(sshPrivateKeyPath)
	if err != nil {
		log.Fatalf("Error getting SSH private key: %v", err)
	}
	pbk, err := vc.GetValue(sshPublicKeyPath)
	if err != nil {
		log.Fatalf("Error getting SSH public key: %v", err)
	}
	serverConfig.tlsCert = []byte(safeStringCast(cert))
	serverConfig.tlsKey = []byte(safeStringCast(key))
	gitConfig.privateKey = safeStringCast(pk)
	gitConfig.publicKey = safeStringCast(pbk)
}

// TLS cert/key are retrieved from Vault and must be written to temp files
func writeTLSCert() (string, string) {
	cf, err := ioutil.TempFile("", "tls-cert")
	if err != nil {
		log.Fatalf("Error creating TLS certificate temp file: %v", err)
	}
	defer cf.Close()
	_, err = cf.Write(serverConfig.tlsCert)
	if err != nil {
		log.Fatalf("Error writing TLS certificate temp file: %v", err)
	}
	kf, err := ioutil.TempFile("", "tls-key")
	if err != nil {
		log.Fatalf("Error creating TLS key temp file: %v", err)
	}
	defer kf.Close()
	_, err = kf.Write(serverConfig.tlsKey)
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

// SSH keypair must be written to temp files
func writeSSHKeypair() (string, string) {
	bf, err := ioutil.TempFile("", "ssh-pubkey")
	if err != nil {
		log.Fatalf("Error creating SSH pubkey temp file: %v", err)
	}
	defer bf.Close()
	_, err = bf.Write([]byte(gitConfig.publicKey))
	if err != nil {
		log.Fatalf("Error writing SSH pubkey temp file: %v", err)
	}
	vf, err := ioutil.TempFile("", "ssh-privkey")
	if err != nil {
		log.Fatalf("Error creating SSH privkey temp file: %v", err)
	}
	defer vf.Close()
	_, err = vf.Write([]byte(gitConfig.privateKey))
	if err != nil {
		log.Fatalf("Error writing SSH privkey temp file: %v", err)
	}
	return bf.Name(), vf.Name()
}
