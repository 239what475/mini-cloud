package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestSaveAndLoadRoundTrip 验证状态文件保存后可以按当前 schema 完整读回。
func TestSaveAndLoadRoundTrip(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "agent-state.json")
	original := State{
		NodeID:       "node_test_123",
		SessionToken: "mcws_test_secret",
		LastSyncedAt: time.Date(2026, 4, 27, 1, 2, 3, 0, time.UTC),
	}

	if err := Save(path, original); err != nil {
		t.Fatalf("Save returned error: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.NodeID != original.NodeID {
		t.Fatalf("loaded nodeID = %q, want %q", loaded.NodeID, original.NodeID)
	}
	if loaded.SessionToken != original.SessionToken {
		t.Fatalf("loaded session token = %q, want %q", loaded.SessionToken, original.SessionToken)
	}
	if !loaded.LastSyncedAt.Equal(original.LastSyncedAt) {
		t.Fatalf("loaded lastSyncedAt = %s, want %s", loaded.LastSyncedAt, original.LastSyncedAt)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat state file returned error: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("state file permissions = %#o, want 0600", got)
	}

	var raw struct {
		// SchemaVersion 是状态文件落盘格式版本。
		SchemaVersion int `json:"schemaVersion"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved state: %v", err)
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal saved state: %v", err)
	}
	if raw.SchemaVersion != 2 {
		t.Fatalf("schemaVersion = %d, want 2", raw.SchemaVersion)
	}
}

// TestLoadQuarantinesStateWithoutSchemaVersion 验证无 schemaVersion 的状态文件会被隔离。
func TestLoadQuarantinesStateWithoutSchemaVersion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "agent-state.json")
	oldState := []byte(`{"nodeID":"node_old","sessionToken":"old-secret","lastSyncedAt":"2026-04-27T01:02:03Z"}` + "\n")
	if err := os.WriteFile(path, oldState, 0o600); err != nil {
		t.Fatalf("write old state: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.NodeID != "" || loaded.SessionToken != "" || !loaded.LastSyncedAt.IsZero() || len(loaded.Executions) != 0 {
		t.Fatalf("loaded = %+v, want empty state", loaded)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("state path exists after quarantine or stat error = %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "agent-state.json.corrupt.*"))
	if err != nil {
		t.Fatalf("glob quarantine files: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("quarantine files = %v, want exactly one", matches)
	}
}

// TestLoadQuarantinesCorruptState 验证损坏状态文件会被隔离且加载返回空状态。
func TestLoadQuarantinesCorruptState(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "agent-state.json")
	if err := os.WriteFile(path, []byte(`{"schemaVersion":`), 0o600); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}

	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if loaded.NodeID != "" || loaded.SessionToken != "" || !loaded.LastSyncedAt.IsZero() || len(loaded.Executions) != 0 {
		t.Fatalf("loaded = %+v, want empty state", loaded)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("state path exists after quarantine or stat error = %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "agent-state.json.corrupt.*"))
	if err != nil {
		t.Fatalf("glob quarantine files: %v", err)
	}
	if len(matches) != 1 {
		t.Fatalf("quarantine files = %v, want exactly one", matches)
	}
}
