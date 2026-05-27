// Package revision 定义 service 每次发布生成的不可变规格快照。
package revision

import (
	"fmt"
	"time"

	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
)

// Revision 表示 service 某次发布使用的不可变 workload 规格快照。
type Revision struct {
	// ID 是 revision 记录的唯一标识。
	ID string `json:"id"`
	// ServiceID 表示所属 service 的唯一标识。
	ServiceID string `json:"serviceID"`
	// Number 是 revision 在 service 内递增的序号。
	Number int `json:"number"`
	// Label 是展示用 revision 标签。
	Label string `json:"label"`
	// Image 表示容器镜像。
	Image string `json:"image"`
	// Command 表示容器启动命令。
	Command []string `json:"command"`
	// Args 表示容器启动参数。
	Args []string `json:"args"`
	// Env 表示容器环境变量。
	Env map[string]string `json:"env"`
	// ConfigSetID 表示引用的 config set 标识。
	ConfigSetID string `json:"configSetID"`
	// SecretSetID 表示引用的 secret set 标识。
	SecretSetID string `json:"secretSetID"`
	// RegistryCredentialID 表示引用的镜像仓库凭据标识。
	RegistryCredentialID string `json:"registryCredentialID"`
	// ProjectedFiles 表示由 config/secret 渲染到容器内的文件列表。
	ProjectedFiles []projectedfile.Spec `json:"projectedFiles,omitempty"`
	// PersistentDirs 表示需要跨 revision 保持的持久目录列表。
	PersistentDirs []persistentdir.Spec `json:"persistentDirs,omitempty"`
	// Port 记录服务监听或转发端口。
	Port int `json:"port"`
	// ReadinessPath 表示就绪探测路径。
	ReadinessPath string `json:"readinessPath"`
	// CreatedAt 是资源创建时间。
	CreatedAt time.Time `json:"createdAt"`
}

// LabelForNumber 根据 revision 序号生成稳定展示标签。
// 参数说明：number 是 service 内单调递增的 revision 序号。
func LabelForNumber(number int) string {
	return fmt.Sprintf("r%06d", number)
}
