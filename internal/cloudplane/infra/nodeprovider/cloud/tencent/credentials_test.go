package tencent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialFromTCCLI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "default.credential")
	if err := os.WriteFile(path, []byte(`{"secretId":"sid","secretKey":"skey","token":"stok"}`), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	credential, err := credentialFromTCCLI(CredentialPaths{TCCLICredentialPath: path})
	if err != nil {
		t.Fatalf("credentialFromTCCLI returned error: %v", err)
	}
	if credential.GetSecretId() != "sid" {
		t.Fatalf("secret id = %q, want sid", credential.GetSecretId())
	}
	if credential.GetSecretKey() != "skey" {
		t.Fatalf("secret key = %q, want skey", credential.GetSecretKey())
	}
	if credential.GetToken() != "stok" {
		t.Fatalf("token = %q, want stok", credential.GetToken())
	}
}

func TestCredentialFromTCCLIINI(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "default.credential")
	content := `[default]
secret_id = sid
secret_key = skey
token = stok
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	credential, err := credentialFromTCCLI(CredentialPaths{TCCLICredentialPath: path})
	if err != nil {
		t.Fatalf("credentialFromTCCLI returned error: %v", err)
	}
	if credential.GetSecretId() != "sid" {
		t.Fatalf("secret id = %q, want sid", credential.GetSecretId())
	}
	if credential.GetSecretKey() != "skey" {
		t.Fatalf("secret key = %q, want skey", credential.GetSecretKey())
	}
	if credential.GetToken() != "stok" {
		t.Fatalf("token = %q, want stok", credential.GetToken())
	}
}

func TestCredentialFromEnv(t *testing.T) {
	t.Setenv("TENCENTCLOUD_SECRET_ID", "env-id")
	t.Setenv("TENCENTCLOUD_SECRET_KEY", "env-key")
	t.Setenv("TENCENTCLOUD_TOKEN", "env-token")

	credential := credentialFromEnv()
	if credential == nil {
		t.Fatalf("credentialFromEnv returned nil")
	}
	if credential.GetSecretId() != "env-id" {
		t.Fatalf("secret id = %q, want env-id", credential.GetSecretId())
	}
	if credential.GetSecretKey() != "env-key" {
		t.Fatalf("secret key = %q, want env-key", credential.GetSecretKey())
	}
	if credential.GetToken() != "env-token" {
		t.Fatalf("token = %q, want env-token", credential.GetToken())
	}
}
