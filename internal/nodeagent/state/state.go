package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// schemaVersion 是当前状态文件落盘格式版本。
const schemaVersion = 2

// State 是 node-agent 在内存中使用的本地持久化状态。
type State struct {
	// NodeID 是控制面分配给当前节点的 ID。
	NodeID string `json:"nodeID"`
	// SessionToken 是当前节点注册后获得的会话令牌。
	SessionToken string `json:"sessionToken"`
	// LastSyncedAt 是状态最近一次与控制面同步的时间。
	LastSyncedAt time.Time `json:"lastSyncedAt"`
	// Executions 保存尚需恢复、清理或继续跟踪的执行状态，key 为 executionID。
	Executions map[string]ExecutionState `json:"executions,omitempty"`
}

// ExecutionState 描述单个执行在 node-agent 本地的恢复状态。
type ExecutionState struct {
	// ExecutionID 是执行 ID。
	ExecutionID string `json:"executionID"`
	// DeploymentID 是执行所属部署 ID。
	DeploymentID string `json:"deploymentID"`
	// ProjectID 是执行所属项目 ID。
	ProjectID string `json:"projectID,omitempty"`
	// ServiceID 是执行所属服务 ID。
	ServiceID string `json:"serviceID"`
	// ServiceName 是执行所属服务名称。
	ServiceName string `json:"serviceName,omitempty"`
	// ReplicaIndex 是该执行对应的副本序号。
	ReplicaIndex int `json:"replicaIndex,omitempty"`
	// RevisionID 是该执行使用的服务修订 ID。
	RevisionID string `json:"revisionID,omitempty"`
	// NodeID 是承载该执行的节点 ID。
	NodeID string `json:"nodeID"`
	// Phase 是执行状态机最近持久化的阶段。
	Phase string `json:"phase"`
	// ContainerID 是运行时创建的容器 ID。
	ContainerID string `json:"containerID,omitempty"`
	// ContainerName 是运行时创建的容器名称。
	ContainerName string `json:"containerName,omitempty"`
	// HostPort 是容器端口映射到宿主机后的端口。
	HostPort int `json:"hostPort,omitempty"`
	// ProjectionRef 是投影文件布局的本地引用。
	ProjectionRef string `json:"projectionRef,omitempty"`
	// LastReportStatus 是最近一次已上报给控制面的执行状态。
	LastReportStatus string `json:"lastReportStatus,omitempty"`
	// UpdatedAt 是该执行状态最近一次更新的时间。
	UpdatedAt time.Time `json:"updatedAt"`
}

// diskState 是带 schemaVersion 的状态文件落盘格式。
type diskState struct {
	// SchemaVersion 标识状态文件格式版本。
	SchemaVersion int `json:"schemaVersion"`
	// NodeID 是控制面分配给当前节点的 ID。
	NodeID string `json:"nodeID"`
	// SessionToken 是当前节点注册后获得的会话令牌。
	SessionToken string `json:"sessionToken"`
	// LastSyncedAt 是状态最近一次与控制面同步的时间。
	LastSyncedAt time.Time `json:"lastSyncedAt"`
	// Executions 保存尚未完成本地恢复处理的执行状态。
	Executions map[string]ExecutionState `json:"executions,omitempty"`
}

// Load 从指定路径读取状态文件，并隔离无法解析或 schema 不支持的状态文件。
func Load(path string) (State, error) {
	if path == "" {
		return State{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("read agent state %q: %w", path, err)
	}

	state, err := parseState(data)
	if err != nil {
		_, quarantineErr := quarantine(path)
		if quarantineErr != nil {
			return State{}, fmt.Errorf("quarantine corrupt agent state %q after parse error %v: %w", path, err, quarantineErr)
		}
		return State{}, nil
	}
	return state, nil
}

// parseState 解析当前 schema 的状态文件内容。
func parseState(data []byte) (State, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return State{}, err
	}

	if _, ok := raw["schemaVersion"]; !ok {
		return State{}, fmt.Errorf("missing schemaVersion")
	}

	var onDisk diskState
	if err := json.Unmarshal(data, &onDisk); err != nil {
		return State{}, err
	}
	if onDisk.SchemaVersion != schemaVersion {
		return State{}, fmt.Errorf("unsupported schemaVersion %d", onDisk.SchemaVersion)
	}
	return State{
		NodeID:       onDisk.NodeID,
		SessionToken: onDisk.SessionToken,
		LastSyncedAt: onDisk.LastSyncedAt,
		Executions:   cloneExecutions(onDisk.Executions),
	}, nil
}

// Save 将状态以当前 schema 原子写入指定路径。
func Save(path string, state State) error {
	if path == "" {
		return nil
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create agent state dir for %q: %w", path, err)
	}

	data, err := json.MarshalIndent(diskState{
		SchemaVersion: schemaVersion,
		NodeID:        state.NodeID,
		SessionToken:  state.SessionToken,
		LastSyncedAt:  state.LastSyncedAt,
		Executions:    cloneExecutions(state.Executions),
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal agent state: %w", err)
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temp agent state for %q: %w", path, err)
	}
	tmpPath := tmp.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tmpPath)
		}
	}()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod temp agent state %q: %w", tmpPath, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write temp agent state %q: %w", tmpPath, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync temp agent state %q: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp agent state %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("replace agent state %q: %w", path, err)
	}
	committed = true
	if err := syncDir(dir); err != nil {
		return fmt.Errorf("sync agent state dir %q: %w", dir, err)
	}
	return nil
}

// Clear 删除指定路径上的本地状态文件。
func Clear(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove agent state %q: %w", path, err)
	}
	return nil
}

// quarantine 将损坏的状态文件重命名为带 corrupt 时间戳的隔离文件。
func quarantine(path string) (string, error) {
	suffix := time.Now().UTC().Format("20060102T150405.000000000Z")
	base := path + ".corrupt." + suffix
	candidate := base
	for i := 1; ; i++ {
		err := os.Rename(path, candidate)
		if err == nil {
			return candidate, nil
		}
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
		if errors.Is(err, os.ErrExist) || strings.Contains(err.Error(), "file exists") {
			candidate = fmt.Sprintf("%s.%d", base, i)
			continue
		}
		return "", err
	}
}

// syncDir fsync 目录，确保原子替换后的目录项持久化。
func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	return f.Sync()
}

// cloneExecutions 复制执行状态 map，空输入返回 nil。
func cloneExecutions(input map[string]ExecutionState) map[string]ExecutionState {
	if len(input) == 0 {
		return nil
	}
	out := make(map[string]ExecutionState, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}
