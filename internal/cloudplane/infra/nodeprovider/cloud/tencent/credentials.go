package tencent

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tccommon "github.com/tencentcloud/tencentcloud-sdk-go/tencentcloud/common"
)

type tccliCredentialFile struct {
	SecretID     string `json:"secretId"`
	SecretKey    string `json:"secretKey"`
	Token        string `json:"token"`
	SessionToken string `json:"sessionToken"`
}

type CredentialPaths struct {
	TCCLICredentialPath string
}

func ResolveCredential() (tccommon.CredentialIface, error) {
	if credential := credentialFromEnv(); credential != nil {
		return credential, nil
	}

	if credential, err := credentialFromTCCLI(defaultCredentialPaths()); err == nil && credential != nil {
		return credential, nil
	}

	credential, err := tccommon.DefaultProviderChain().GetCredential()
	if err != nil {
		return nil, fmt.Errorf("resolve tencent credential: %w", err)
	}
	return credential, nil
}

func defaultCredentialPaths() CredentialPaths {
	home, err := os.UserHomeDir()
	if err != nil {
		return CredentialPaths{}
	}
	return CredentialPaths{
		TCCLICredentialPath: filepath.Join(home, ".tccli", "default.credential"),
	}
}

func credentialFromEnv() tccommon.CredentialIface {
	secretID := strings.TrimSpace(os.Getenv("TENCENTCLOUD_SECRET_ID"))
	secretKey := strings.TrimSpace(os.Getenv("TENCENTCLOUD_SECRET_KEY"))
	if secretID == "" || secretKey == "" {
		return nil
	}
	token := strings.TrimSpace(os.Getenv("TENCENTCLOUD_TOKEN"))
	return tccommon.NewTokenCredential(secretID, secretKey, token)
}

func credentialFromTCCLI(paths CredentialPaths) (tccommon.CredentialIface, error) {
	path := strings.TrimSpace(paths.TCCLICredentialPath)
	if path == "" {
		return nil, fmt.Errorf("tccli credential path is empty")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read tccli credential file %s: %w", path, err)
	}

	cfg, err := parseTCCLICredentialFile(data)
	if err != nil {
		return nil, fmt.Errorf("parse tccli credential file %s: %w", path, err)
	}

	secretID := strings.TrimSpace(cfg.SecretID)
	secretKey := strings.TrimSpace(cfg.SecretKey)
	if secretID == "" || secretKey == "" {
		return nil, fmt.Errorf("tccli credential file %s is missing secretId or secretKey", path)
	}

	token := strings.TrimSpace(cfg.SessionToken)
	if token == "" {
		token = strings.TrimSpace(cfg.Token)
	}
	return tccommon.NewTokenCredential(secretID, secretKey, token), nil
}

func parseTCCLICredentialFile(data []byte) (tccliCredentialFile, error) {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" {
		return tccliCredentialFile{}, fmt.Errorf("credential file is empty")
	}
	if strings.HasPrefix(trimmed, "{") {
		var cfg tccliCredentialFile
		if err := json.Unmarshal([]byte(trimmed), &cfg); err != nil {
			return tccliCredentialFile{}, err
		}
		return cfg, nil
	}
	return parseTCCLIINI(trimmed)
}

func parseTCCLIINI(content string) (tccliCredentialFile, error) {
	var (
		cfg     tccliCredentialFile
		section string
	)
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "["), "]")))
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		if section != "" && section != "default" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "secretid", "secret_id":
			cfg.SecretID = strings.TrimSpace(value)
		case "secretkey", "secret_key":
			cfg.SecretKey = strings.TrimSpace(value)
		case "token":
			cfg.Token = strings.TrimSpace(value)
		case "sessiontoken", "session_token":
			cfg.SessionToken = strings.TrimSpace(value)
		}
	}
	if err := scanner.Err(); err != nil {
		return tccliCredentialFile{}, err
	}
	return cfg, nil
}
