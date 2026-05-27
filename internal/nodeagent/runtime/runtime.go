package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
)

const (
	// TypeDocker 表示使用本机 Docker Engine 作为工作负载运行时。
	TypeDocker = "docker"
)

// Config 配置 node-agent 使用的工作负载运行时。
type Config struct {
	// Type 是运行时类型；为空时默认使用 docker。
	Type string
}

// RunInput 描述一次工作负载容器启动请求。
type RunInput struct {
	// ContainerName 是运行时创建容器时使用的容器名称。
	ContainerName string `json:"containerName"`

	// NodeID 是拥有该容器的 node-agent 节点 ID，会写入运行时标签。
	NodeID string `json:"nodeID,omitempty"`
	// ExecutionID 是拥有该容器的执行 ID，会用于运行时标签和投影文件路径。
	ExecutionID string `json:"executionID,omitempty"`
	// DeploymentID 是执行所属部署 ID，会写入运行时标签。
	DeploymentID string `json:"deploymentID,omitempty"`
	// ProjectID 是执行所属项目 ID，会写入运行时标签。
	ProjectID string `json:"projectID,omitempty"`
	// ServiceID 是执行所属服务 ID，会写入运行时标签。
	ServiceID string `json:"serviceID,omitempty"`
	// RevisionID 是执行使用的服务修订 ID，会写入运行时标签。
	RevisionID string `json:"revisionID,omitempty"`
	// ProjectionRef 是写入运行时标签的投影文件引用；为空时标签使用 ExecutionID。
	ProjectionRef string `json:"projectionRef,omitempty"`

	// Image 是要运行的容器镜像。
	Image string `json:"image"`
	// Command 是写入 Docker Cmd 的命令前缀，不覆盖镜像 Entrypoint。
	Command []string `json:"command"`
	// Args 是追加到 Command 后或单独作为容器命令的参数。
	Args []string `json:"args"`
	// Env 是注入容器的环境变量。
	Env map[string]string `json:"env"`
	// ProjectedFiles 是以只读 bind mount 方式投影进容器的文件。
	ProjectedFiles []projectedfile.File `json:"projectedFiles,omitempty"`
	// PersistentDirs 是以 bind mount 方式挂载进容器的持久化目录。
	PersistentDirs []persistentdir.Mount `json:"persistentDirs,omitempty"`
	// ImageCredential 是拉取私有镜像时使用的认证信息。
	ImageCredential *ImageCredential `json:"imageCredential,omitempty"`
	// ContainerPort 是容器内需要暴露并映射到宿主机的 TCP 端口。
	ContainerPort int `json:"containerPort"`
	// HostBindIP 是宿主机端口绑定地址；生产路径使用节点私网 IP，避免 workload 监听到所有网卡。
	HostBindIP string `json:"hostBindIP"`
	// HostPortMin 是可分配宿主机端口池最小值；和 HostPortMax 同时配置时 Docker 绑定显式端口。
	HostPortMin int `json:"hostPortMin"`
	// HostPortMax 是可分配宿主机端口池最大值；和 HostPortMin 同时配置时 Docker 绑定显式端口。
	HostPortMax int `json:"hostPortMax"`
	// HostPort 是本次启动指定的宿主机端口；为空时由 runtime 在 HostPortMin/Max 内选择。
	HostPort int `json:"hostPort"`
}

// RunResult 描述运行时成功启动容器后的结果。
type RunResult struct {
	// ContainerID 是运行时返回的容器 ID。
	ContainerID string `json:"containerID"`
	// ContainerName 是实际创建的容器名称。
	ContainerName string `json:"containerName"`
	// HostPort 是映射到宿主机上的端口。
	HostPort int `json:"hostPort"`
}

// LogRecord 表示从运行时日志流中解析出的一行容器日志。
type LogRecord struct {
	// Timestamp 是日志记录时间。
	Timestamp time.Time
	// Stream 是日志流名称，例如 stdout 或 stderr。
	Stream string
	// Line 是不含末尾换行符的日志内容。
	Line string
}

// LogEmitter 接收运行时解析出的容器日志记录。
type LogEmitter func(LogRecord)

// LogFollower 表示支持持续跟随容器日志的运行时能力。
type LogFollower interface {
	// FollowLogs 从指定容器读取持续日志流，并将每行日志交给 emit。
	FollowLogs(context.Context, string, LogEmitter) error
}

// ImageCredential 描述拉取私有镜像时使用的 registry 认证信息。
type ImageCredential struct {
	// Server 是 registry server 地址。
	Server string `json:"server"`
	// Username 是 registry 用户名。
	Username string `json:"username"`
	// Password 是 registry 密码或访问令牌。
	Password string `json:"password"`
}

// New 根据配置创建工作负载运行时实现。
func New(logger *slog.Logger, cfg Config) (*DockerEngine, error) {
	runtimeType := strings.TrimSpace(cfg.Type)
	if runtimeType == "" {
		runtimeType = TypeDocker
	}

	switch runtimeType {
	case TypeDocker:
		return NewDockerEngine(logger)
	default:
		return nil, fmt.Errorf("unsupported node runtime type %q", runtimeType)
	}
}
