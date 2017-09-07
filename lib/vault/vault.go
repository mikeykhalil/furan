package vault

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"

	"github.com/dollarshaveclub/furan/lib/config"
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

func getVaultClient(vaultConfig *config.Vaultconfig) (*vaultclient.VaultClient, error) {
	vc, err := vaultclient.NewClient(&vaultclient.VaultConfig{
		Server: vaultConfig.Addr,
	})
	if err != nil {
		return vc, err
	}
	if vaultConfig.TokenAuth {
		vc.TokenAuth(vaultConfig.Token)
	} else {
		if err = vc.AppIDAuth(vaultConfig.AppID, vaultConfig.UserIDPath); err != nil {
			return vc, err
		}
	}
	return vc, nil
}

func vaultPath(vaultConfig *config.Vaultconfig, path string) string {
	return fmt.Sprintf("%v%v", vaultConfig.VaultPathPrefix, path)
}

// SetupVault does generic Vault setup (all subcommands)
func SetupVault(vaultConfig *config.Vaultconfig, awsConfig *config.AWSConfig, dockerConfig *config.Dockerconfig, gitConfig *config.Gitconfig, serverConfig *config.Serverconfig, awscredsprefix string) {
	vc, err := getVaultClient(vaultConfig)
	if err != nil {
		log.Fatalf("Error creating Vault client: %v", err)
	}
	ght, err := vc.GetValue(vaultPath(vaultConfig, gitConfig.TokenVaultPath))
	if err != nil {
		log.Fatalf("Error getting GitHub token: %v", err)
	}
	dcc, err := vc.GetValue(vaultPath(vaultConfig, dockerConfig.DockercfgVaultPath))
	if err != nil {
		log.Fatalf("Error getting dockercfg: %v", err)
	}
	nrkey, err := vc.GetValue(vaultPath(vaultConfig, serverConfig.NewRelicAPIKeyVaultPath))
	if err != nil {
		log.Fatalf("Error getting New Relic API key: %v", err)
	}
	gitConfig.Token = safeStringCast(ght)
	dockerConfig.DockercfgRaw = safeStringCast(dcc)
	serverConfig.NewRelicAPIKey = safeStringCast(nrkey)
	ak, sk := GetAWSCreds(vaultConfig, awscredsprefix)
	awsConfig.AccessKeyID = ak
	awsConfig.SecretAccessKey = sk
}

func GetAWSCreds(vaultConfig *config.Vaultconfig, pfx string) (string, string) {
	vc, err := getVaultClient(vaultConfig)
	if err != nil {
		log.Fatalf("Error creating Vault client: %v", err)
	}
	ak, err := vc.GetValue(vaultPath(vaultConfig, pfx+"/access_key_id"))
	if err != nil {
		log.Fatalf("Error getting AWS access key ID: %v", err)
	}
	sk, err := vc.GetValue(vaultPath(vaultConfig, pfx+"/secret_access_key"))
	if err != nil {
		log.Fatalf("Error getting AWS secret access key: %v", err)
	}
	return safeStringCast(ak), safeStringCast(sk)
}

func GetSumoURL(vaultConfig *config.Vaultconfig, serverConfig *config.Serverconfig) {
	vc, err := getVaultClient(vaultConfig)
	if err != nil {
		log.Fatalf("Error creating Vault client: %v", err)
	}
	scu, err := vc.GetValue(vaultPath(vaultConfig, serverConfig.VaultSumoURLPath))
	if err != nil {
		log.Fatalf("Error getting SumoLogic collector URL: %v", err)
	}
	serverConfig.SumoURL = safeStringCast(scu)
}

// TLS cert/key are retrieved from Vault and must be written to temp files
func WriteTLSCert(vaultConfig *config.Vaultconfig, serverConfig *config.Serverconfig) (string, string) {
	vc, err := getVaultClient(vaultConfig)
	if err != nil {
		log.Fatalf("Error creating Vault client: %v", err)
	}
	cert, err := vc.GetValue(vaultPath(vaultConfig, serverConfig.VaultTLSCertPath))
	if err != nil {
		log.Fatalf("Error getting TLS certificate: %v", err)
	}
	key, err := vc.GetValue(vaultPath(vaultConfig, serverConfig.VaultTLSKeyPath))
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
func RmTempFiles(f1 string, f2 string) {
	for _, v := range []string{f1, f2} {
		err := os.Remove(v)
		if err != nil {
			log.Printf("Error removing file: %v", v)
		}
	}
}
