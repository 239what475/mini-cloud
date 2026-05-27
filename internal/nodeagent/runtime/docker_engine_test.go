package runtime

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/docker/api/types/registry"

	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
)

// TestBuildContainerCreateConfigBuildsPublishedPortAndAutoRemove 验证容器创建配置包含端口发布、自动删除和 mini-cloud 标签。
func TestBuildContainerCreateConfigBuildsPublishedPortAndAutoRemove(t *testing.T) {
	t.Parallel()

	config, hostConfig, portSpec, err := buildContainerCreateConfig(RunInput{
		NodeID:        "node-a",
		ExecutionID:   "exec-a",
		DeploymentID:  "deploy-a",
		ProjectionRef: "proj-a",
		Image:         "nginx:1.27-alpine",
		ContainerPort: 80,
		HostBindIP:    "10.0.0.20",
		HostPort:      31080,
		Env: map[string]string{
			"Z_KEY": "z",
			"A_KEY": "a",
		},
	})
	if err != nil {
		t.Fatalf("buildContainerCreateConfig returned error: %v", err)
	}

	if portSpec != "80/tcp" {
		t.Fatalf("portSpec = %q, want 80/tcp", portSpec)
	}
	if len(config.Env) != 2 || config.Env[0] != "A_KEY=a" || config.Env[1] != "Z_KEY=z" {
		t.Fatalf("unexpected env ordering: %+v", config.Env)
	}
	if config.Labels[dockerLabelManagedBy] != "node-agent" ||
		config.Labels[dockerLabelNodeID] != "node-a" ||
		config.Labels[dockerLabelExecutionID] != "exec-a" ||
		config.Labels[dockerLabelDeploymentID] != "deploy-a" ||
		config.Labels[dockerLabelProjectionRef] != "proj-a" {
		t.Fatalf("unexpected mini-cloud labels: %+v", config.Labels)
	}
	if !hostConfig.AutoRemove {
		t.Fatalf("expected AutoRemove=true")
	}
	if len(hostConfig.ExtraHosts) != 1 || hostConfig.ExtraHosts[0] != "host.docker.internal:host-gateway" {
		t.Fatalf("unexpected extra hosts: %+v", hostConfig.ExtraHosts)
	}
	bindings := hostConfig.PortBindings[portSpec]
	if len(bindings) != 1 || bindings[0].HostIP != "10.0.0.20" || bindings[0].HostPort != "31080" {
		t.Fatalf("unexpected port bindings: %+v", bindings)
	}
}

// TestBuildContainerCreateConfigRequiresSelectedPortForRange 验证配置 hostPortRange 后必须先选择具体端口。
func TestBuildContainerCreateConfigRequiresSelectedPortForRange(t *testing.T) {
	t.Parallel()

	_, _, _, err := buildContainerCreateConfig(RunInput{
		Image:         "nginx:1.27-alpine",
		ContainerPort: 80,
		HostBindIP:    "127.0.0.1",
		HostPortMin:   30000,
		HostPortMax:   30999,
	})
	if err == nil {
		t.Fatal("buildContainerCreateConfig returned nil error for range without selected host port")
	}
}

// TestSelectAvailableHostPortRejectsInvalidRange 验证 hostPort 选择器拒绝非法端口范围。
func TestSelectAvailableHostPortRejectsInvalidRange(t *testing.T) {
	t.Parallel()

	if _, err := selectAvailableHostPort("127.0.0.1", 30010, 30000); err == nil {
		t.Fatal("selectAvailableHostPort returned nil error for invalid range")
	}
}

// TestNewRejectsUnsupportedRuntimeType 验证未知运行时类型会被拒绝。
func TestNewRejectsUnsupportedRuntimeType(t *testing.T) {
	t.Parallel()

	if _, err := New(nil, Config{Type: "containerd"}); err == nil {
		t.Fatal("New returned nil error for unsupported runtime type")
	}
}

// TestResolveContainerCommandKeepsCurrentCliSemantics 验证 Command 和 Args 的组合语义保持不变。
func TestResolveContainerCommandKeepsCurrentCliSemantics(t *testing.T) {
	t.Parallel()

	got := resolveContainerCommand(RunInput{
		Command: []string{"python"},
		Args:    []string{"service.py", "--port", "8080"},
	})

	want := []string{"python", "service.py", "--port", "8080"}
	if len(got) != len(want) {
		t.Fatalf("resolveContainerCommand length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("resolveContainerCommand[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestBuildRegistryAuthEncodesCredential 验证镜像仓库认证信息会编码为 Docker RegistryAuth。
func TestBuildRegistryAuthEncodesCredential(t *testing.T) {
	t.Parallel()

	encoded, err := buildRegistryAuth(&ImageCredential{
		Server:   "registry.example.com",
		Username: "demo",
		Password: "secret",
	})
	if err != nil {
		t.Fatalf("buildRegistryAuth returned error: %v", err)
	}

	data, err := base64.URLEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("decode registry auth returned error: %v", err)
	}

	var decoded registry.AuthConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal registry auth returned error: %v", err)
	}
	if decoded.ServerAddress != "registry.example.com" || decoded.Username != "demo" || decoded.Password != "secret" {
		t.Fatalf("decoded registry auth = %+v", decoded)
	}
}

// TestLogLineWriterParsesTimestampedLinesAcrossWrites 验证日志行 writer 可以跨 Write 解析带时间戳日志。
func TestLogLineWriterParsesTimestampedLinesAcrossWrites(t *testing.T) {
	t.Parallel()

	var emitted []LogRecord
	writer := newLogLineWriter("stdout", func(record LogRecord) {
		emitted = append(emitted, record)
	})

	_, _ = writer.Write([]byte("2026-04-16T12:00:00Z first line\n2026-04-16T12:00:01"))
	_, _ = writer.Write([]byte("Z second line\n"))
	writer.Flush()

	if len(emitted) != 2 {
		t.Fatalf("emitted line count = %d, want 2", len(emitted))
	}
	if emitted[0].Stream != "stdout" || emitted[0].Line != "first line" {
		t.Fatalf("first emitted record = %+v, want stdout first line", emitted[0])
	}
	if emitted[1].Stream != "stdout" || emitted[1].Line != "second line" {
		t.Fatalf("second emitted record = %+v, want stdout second line", emitted[1])
	}
	if emitted[0].Timestamp.Format(time.RFC3339Nano) != "2026-04-16T12:00:00Z" {
		t.Fatalf("first emitted timestamp = %s, want 2026-04-16T12:00:00Z", emitted[0].Timestamp.Format(time.RFC3339Nano))
	}
	if emitted[1].Timestamp.Format(time.RFC3339Nano) != "2026-04-16T12:00:01Z" {
		t.Fatalf("second emitted timestamp = %s, want 2026-04-16T12:00:01Z", emitted[1].Timestamp.Format(time.RFC3339Nano))
	}
}

// TestPrepareProjectedMountsMaterializesReadonlyFiles 验证投影文件会落盘并生成只读 bind mount。
func TestPrepareProjectedMountsMaterializesReadonlyFiles(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	executionDir, mounts, err := prepareProjectedMountsInRoot(root, RunInput{
		ExecutionID: "exec-demo",
		ProjectedFiles: []projectedfile.File{
			{
				MountPath: "/etc/cliproxy/config.yaml",
				Content:   "listen: :8317\n",
			},
			{
				MountPath: "/etc/cliproxy/auth/token",
				Content:   "secret-token",
				Sensitive: true,
			},
		},
	})
	if err != nil {
		t.Fatalf("prepareProjectedMounts returned error: %v", err)
	}
	defer func() { _ = os.RemoveAll(executionDir) }()
	wantExecutionDir := filepath.Join(root, projectedExecutionsDir, "exec-demo")
	if executionDir != wantExecutionDir {
		t.Fatalf("projected execution dir = %q, want %q", executionDir, wantExecutionDir)
	}

	if len(mounts) != 2 {
		t.Fatalf("mounts len = %d, want 2", len(mounts))
	}
	if mounts[0].Target != "/etc/cliproxy/auth/token" || !mounts[0].ReadOnly {
		t.Fatalf("first mount = %+v, want readonly auth token mount", mounts[0])
	}
	if mounts[0].Source != filepath.Join(executionDir, projectedFilesDir, "etc", "cliproxy", "auth", "token") {
		t.Fatalf("first mount source = %q, want deterministic execution files path", mounts[0].Source)
	}
	if mounts[1].Target != "/etc/cliproxy/config.yaml" || !mounts[1].ReadOnly {
		t.Fatalf("second mount = %+v, want readonly config mount", mounts[1])
	}

	tokenPath := filepath.Join(executionDir, projectedFilesDir, "etc", "cliproxy", "auth", "token")
	tokenContent, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("ReadFile(token) returned error: %v", err)
	}
	if string(tokenContent) != "secret-token" {
		t.Fatalf("token content = %q, want secret-token", string(tokenContent))
	}
	info, err := os.Stat(tokenPath)
	if err != nil {
		t.Fatalf("Stat(token) returned error: %v", err)
	}
	if info.Mode().Perm() != os.FileMode(projectedfile.DefaultSecretMode) {
		t.Fatalf("token mode = %#o, want %#o", info.Mode().Perm(), projectedfile.DefaultSecretMode)
	}
	entries, err := os.ReadDir(executionDir)
	if err != nil {
		t.Fatalf("ReadDir(executionDir) returned error: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), projectedStagingPrefix) {
			t.Fatalf("staging dir %q was not cleaned up", entry.Name())
		}
	}
}

// TestPrepareProjectedMountsRejectsEmptyRoot 验证投影文件根目录为空时会报错。
func TestPrepareProjectedMountsRejectsEmptyRoot(t *testing.T) {
	t.Parallel()

	_, _, err := prepareProjectedMountsInRoot("", RunInput{
		ExecutionID: "exec-demo",
		ProjectedFiles: []projectedfile.File{{
			MountPath: "/etc/workload/config.yaml",
			Content:   "demo",
		}},
	})
	if err == nil {
		t.Fatal("expected error for empty projected files root dir")
	}
}

// TestPrepareProjectedMountsRequiresSafeExecutionID 验证投影文件路径要求安全的 executionID。
func TestPrepareProjectedMountsRequiresSafeExecutionID(t *testing.T) {
	t.Parallel()

	_, _, err := prepareProjectedMountsInRoot(t.TempDir(), RunInput{
		ExecutionID: "../exec-demo",
		ProjectedFiles: []projectedfile.File{{
			MountPath: "/etc/workload/config.yaml",
			Content:   "demo",
		}},
	})
	if err == nil {
		t.Fatal("expected error for unsafe executionID")
	}
}

// TestDockerEngineCloseCleansTrackedProjectionDirs 验证关闭 DockerEngine 会清理已跟踪的投影目录。
func TestDockerEngineCloseCleansTrackedProjectionDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	projectionDir := filepath.Join(root, projectedExecutionsDir, "exec-close")
	if err := os.MkdirAll(filepath.Join(projectionDir, projectedFilesDir), 0o700); err != nil {
		t.Fatalf("MkdirAll(projectionDir) returned error: %v", err)
	}

	engine := &DockerEngine{
		projectionDirs: map[string]trackedProjection{
			"container-close": {
				ExecutionID: "exec-close",
				Dir:         projectionDir,
			},
		},
	}
	if err := engine.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
	if _, err := os.Stat(projectionDir); !os.IsNotExist(err) {
		t.Fatalf("projection dir still exists after Close, stat err=%v", err)
	}
	if len(engine.projectionDirs) != 0 {
		t.Fatalf("tracked projection dirs len = %d, want 0", len(engine.projectionDirs))
	}
}

// TestPreparePersistentDirMountsCreatesWritableHostDirs 验证持久化目录会在宿主机创建并转换为可写 bind mount。
func TestPreparePersistentDirMountsCreatesWritableHostDirs(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	mounts, err := preparePersistentDirMounts([]persistentdir.Mount{
		{
			Name:       "auth-dir",
			MountPath:  "/var/lib/cliproxy/auth",
			SourcePath: filepath.Join(root, "svc-123", "auth-dir"),
		},
	})
	if err != nil {
		t.Fatalf("preparePersistentDirMounts returned error: %v", err)
	}
	if len(mounts) != 1 {
		t.Fatalf("mounts len = %d, want 1", len(mounts))
	}
	if mounts[0].Target != "/var/lib/cliproxy/auth" || mounts[0].Source != filepath.Join(root, "svc-123", "auth-dir") || mounts[0].ReadOnly {
		t.Fatalf("unexpected mount: %+v", mounts[0])
	}
	info, err := os.Stat(filepath.Join(root, "svc-123", "auth-dir"))
	if err != nil {
		t.Fatalf("Stat(persistent dir) returned error: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("persistent dir path is not a directory")
	}
}
