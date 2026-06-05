// Package workload 定义 cloud-plane service 规格、状态和输入校验规则。
package workload

import (
	"errors"
	"regexp"
	"strings"
	"time"

	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
)

const (
	// StatusCreating 表示 service 正在创建初始记录或初始 revision。
	StatusCreating = "creating"
	// StatusIdle 表示 service 暂无进行中的发布流程。
	StatusIdle = "idle"
	// StatusDeploying 表示 service 正在发布或变更 revision。
	StatusDeploying = "deploying"
	// StatusRunning 表示 service 当前 revision 正常承载流量。
	StatusRunning = "running"
	// StatusDegraded 表示 service 仍可用但部分副本或探测异常。
	StatusDegraded = "degraded"
	// StatusFailed 表示 service 最近一次发布或运行检查失败。
	StatusFailed = "failed"

	// InstanceClassSmall 表示小规格实例档位。
	InstanceClassSmall = "small"
	// InstanceClassMedium 表示中规格实例档位。
	InstanceClassMedium = "medium"
	// InstanceClassLarge 表示大规格实例档位。
	InstanceClassLarge = "large"

	// ExposurePublic 表示 service 可通过对外入口访问。
	ExposurePublic = "public"
	// ExposurePrivate 表示 service 只在平台内部访问。
	ExposurePrivate = "private"

	// RolloutPhaseIdle 表示当前没有进行中的 rollout。
	RolloutPhaseIdle = "idle"
	// RolloutPhaseProgressing 表示 rollout 正在推进 candidate revision。
	RolloutPhaseProgressing = "progressing"
	// RolloutPhaseFailed 表示 rollout 失败，等待修复或再次发布。
	RolloutPhaseFailed = "failed"
)

var (
	// ErrServiceIDRequired 表示 service 输入缺少 service 标识。
	ErrServiceIDRequired = errors.New("serviceID is required")
	// ErrInvalidServiceName 表示 service 名称不符合平台命名规则。
	ErrInvalidServiceName = errors.New("name must use lowercase letters, digits, and hyphens")
	// ErrServiceIdentityConflict 表示同一个 serviceID 对应的 name 与现有记录冲突。
	ErrServiceIdentityConflict = errors.New("service identity conflicts with existing service")
	// ErrServiceDisplayNameRequired 表示 service 输入缺少展示名称。
	ErrServiceDisplayNameRequired = errors.New("displayName is required")
	// ErrServiceRegionRequired 表示 service 输入缺少目标地域。
	ErrServiceRegionRequired = errors.New("region is required")
	// ErrInvalidInstanceClass 表示实例规格档位不属于允许集合。
	ErrInvalidInstanceClass = errors.New("instanceClass must be one of small, medium, large")
	// ErrInvalidExposure 表示暴露策略不属于允许集合。
	ErrInvalidExposure = errors.New("exposure must be one of public, private")
	// ErrImageRequired 表示 service 输入缺少容器镜像。
	ErrImageRequired = errors.New("image is required")
	// ErrInvalidDefaultPort 表示默认端口不在 TCP/UDP 端口范围内。
	ErrInvalidDefaultPort = errors.New("defaultPort must be between 1 and 65535")
	// ErrInvalidReadinessPath 表示就绪探测路径不是绝对路径。
	ErrInvalidReadinessPath = errors.New("readinessPath must start with /")
	// ErrInvalidServiceStatus 表示 service 状态不属于允许集合。
	ErrInvalidServiceStatus = errors.New("invalid service status")
	// ErrInvalidRolloutPhase 表示 rollout 阶段不属于允许集合。
	ErrInvalidRolloutPhase = errors.New("invalid rollout phase")
	// ErrInvalidEnvironmentKey 表示环境变量中存在空 key。
	ErrInvalidEnvironmentKey = errors.New("env keys must not be empty")
	// ErrPersistentDirsRolloutUnsupported 表示带持久目录的 service 不支持改变 revision 的更新。
	ErrPersistentDirsRolloutUnsupported = errors.New("services with persistentDirs do not support revision-changing updates once a revision exists")
	serviceNamePattern                  = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

// Service 表示 cloud-plane 管理的一个用户工作负载服务。
type Service struct {
	// Metadata 保存 service 身份和用户可读元数据。
	Metadata Metadata `json:"metadata"`
	// Spec 保存用户声明的目标运行规格。
	Spec Spec `json:"spec"`
	// Status 保存 cloud-plane 本地 lifecycle 状态。
	Status ServiceStatus `json:"status"`
	// CreatedAt 是资源创建时间。
	CreatedAt time.Time `json:"createdAt"`
	// UpdatedAt 是资源最近更新时间。
	UpdatedAt time.Time `json:"updatedAt"`
}

// Metadata 表示 service 身份和用户可读元数据。
type Metadata struct {
	// ID 是 service 记录的唯一标识。
	ID string `json:"id"`
	// Name 是全局唯一的 service 机器可读名称。
	Name string `json:"name"`
	// DisplayName 表示面向用户展示的名称。
	DisplayName string `json:"displayName"`
}

// Spec 表示 service 的目标运行规格，不包含身份字段和展示元数据。
type Spec struct {
	// Region 是 service 期望部署的目标地域。
	Region string `json:"region"`
	// InstanceClass 表示平台抽象的实例规格档位。
	InstanceClass string `json:"instanceClass"`
	// Exposure 表示 service 对外暴露策略。
	Exposure string `json:"exposure"`
	// Image 表示容器镜像。
	Image string `json:"image"`
	// Command 表示容器启动命令。
	Command []string `json:"command"`
	// Args 表示容器启动参数。
	Args []string `json:"args"`
	// DefaultPort 表示默认服务端口。
	DefaultPort int `json:"defaultPort"`
	// ReadinessPath 表示就绪探测路径。
	ReadinessPath string `json:"readinessPath"`
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
}

// ServiceStatus 表示 cloud-plane 本地维护的 service lifecycle 状态。
type ServiceStatus struct {
	// Phase 是 service 当前运行或发布状态。
	Phase string `json:"phase"`
	// CurrentRevisionID 表示当前正式承载流量的 revision 标识。
	CurrentRevisionID string `json:"currentRevisionID"`
	// CandidateRevisionID 表示正在验证但尚未提升的 candidate revision 标识。
	CandidateRevisionID string `json:"candidateRevisionID"`
	// RolloutPhase 表示当前 rollout 状态机阶段。
	RolloutPhase string `json:"rolloutPhase"`
	// RolloutMessage 表示 rollout 最近一次状态说明。
	RolloutMessage string `json:"rolloutMessage"`
}

// ValidateIdentity 校验 service 身份字段。
func ValidateIdentity(name string) error {
	// service name 使用机器可读命名规则，按 trim 后内容校验。
	if !serviceNamePattern.MatchString(strings.TrimSpace(name)) {
		return ErrInvalidServiceName
	}
	return nil
}

// ValidateMetadata 校验 service 元数据字段。
func ValidateMetadata(name string, displayName string) error {
	if err := ValidateIdentity(name); err != nil {
		return err
	}
	if strings.TrimSpace(displayName) == "" {
		return ErrServiceDisplayNameRequired
	}
	return nil
}

// Validate 校验 service 规格是否满足 cloud-plane 领域约束。
func (in Spec) Validate() error {
	// exposure 允许空值，先归一化后再判断是否属于允许集合。
	if !IsExposure(in.NormalizedExposure()) {
		return ErrInvalidExposure
	}
	// region 是调度 runtime node 的必要位置约束。
	if strings.TrimSpace(in.Region) == "" {
		return ErrServiceRegionRequired
	}
	// instance class 决定单副本资源请求。
	if !IsInstanceClass(in.InstanceClass) {
		return ErrInvalidInstanceClass
	}
	// image 是 node-agent 启动容器的必要字段。
	if strings.TrimSpace(in.Image) == "" {
		return ErrImageRequired
	}
	// default port 必须落在 TCP/UDP 端口范围内。
	if in.DefaultPort <= 0 || in.DefaultPort > 65535 {
		return ErrInvalidDefaultPort
	}
	// readiness path 必须是绝对路径；这里只校验以 / 开头。
	if !strings.HasPrefix(strings.TrimSpace(in.ReadinessPath), "/") {
		return ErrInvalidReadinessPath
	}
	// 环境变量 key 不能是空白字符串。
	for key := range in.Env {
		if strings.TrimSpace(key) == "" {
			return ErrInvalidEnvironmentKey
		}
	}
	// projected file 规则由公共包统一校验，包括 mount path、source 和重复路径。
	if err := projectedfile.ValidateSpecs(in.ProjectedFiles); err != nil {
		return err
	}
	// persistent dir 与 projected file 的路径冲突、嵌套和命名规则由公共包统一校验。
	if err := persistentdir.ValidateContainerInputs(in.PersistentDirs, in.ProjectedFiles); err != nil {
		return err
	}
	// 所有创建输入领域约束通过。
	return nil
}

// IsStatus 判断 service 状态是否属于当前允许值。
func IsStatus(status string) bool {
	switch status {
	case StatusCreating, StatusIdle, StatusDeploying, StatusRunning, StatusDegraded, StatusFailed:
		return true
	default:
		return false
	}
}

// NormalizeExposure 将输入值规整为 cloud-plane 内部使用的标准形式。
func NormalizeExposure(exposure string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(exposure)); normalized {
	case "":
		return ExposurePublic
	default:
		return normalized
	}
}

// IsExposure 判断 service exposure 是否属于当前允许值。
func IsExposure(exposure string) bool {
	switch NormalizeExposure(exposure) {
	case ExposurePublic, ExposurePrivate:
		return true
	default:
		return false
	}
}

// NormalizeRolloutPhase 将输入值规整为 cloud-plane 内部使用的标准形式。
func NormalizeRolloutPhase(phase string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(phase)); normalized {
	case "":
		return RolloutPhaseIdle
	default:
		return normalized
	}
}

// IsRolloutPhase 判断 rollout phase 是否属于当前允许值。
func IsRolloutPhase(phase string) bool {
	switch NormalizeRolloutPhase(phase) {
	case RolloutPhaseIdle, RolloutPhaseProgressing, RolloutPhaseFailed:
		return true
	default:
		return false
	}
}

// NormalizedExposure 将规格中的 exposure 规整为 cloud-plane 内部使用的标准形式。
func (in Spec) NormalizedExposure() string {
	return NormalizeExposure(in.Exposure)
}

// SpecFromService 从持久化 service 记录提取目标运行规格。
func SpecFromService(current Service) Spec {
	return Spec{
		Region:               current.Spec.Region,
		InstanceClass:        current.Spec.InstanceClass,
		Exposure:             current.Spec.Exposure,
		Image:                current.Spec.Image,
		Command:              append([]string(nil), current.Spec.Command...),
		Args:                 append([]string(nil), current.Spec.Args...),
		DefaultPort:          current.Spec.DefaultPort,
		ReadinessPath:        current.Spec.ReadinessPath,
		Env:                  copyStringMap(current.Spec.Env),
		ConfigSetID:          current.Spec.ConfigSetID,
		SecretSetID:          current.Spec.SecretSetID,
		RegistryCredentialID: current.Spec.RegistryCredentialID,
		ProjectedFiles:       projectedfile.CloneSpecs(current.Spec.ProjectedFiles),
		PersistentDirs:       persistentdir.CloneSpecs(current.Spec.PersistentDirs),
	}
}

func copyStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return map[string]string{}
	}
	out := make(map[string]string, len(input))
	for key, value := range input {
		out[key] = value
	}
	return out
}

// IsInstanceClass 判断 instance class 是否属于当前允许值。
func IsInstanceClass(class string) bool {
	switch class {
	case InstanceClassSmall, InstanceClassMedium, InstanceClassLarge:
		return true
	default:
		return false
	}
}

// ResourceRequest 把用户看到的档位翻译成 scheduler 真正需要的 cpu / memory 请求。
func ResourceRequest(class string) (cpuMilli int, memoryMi int, err error) {
	switch class {
	case InstanceClassSmall:
		// small 档位对应 0.5 vCPU 和 512MiB。
		return 500, 512, nil
	case InstanceClassMedium:
		// medium 档位对应 1 vCPU 和 1GiB。
		return 1000, 1024, nil
	case InstanceClassLarge:
		// large 档位对应 1.5 vCPU 和 1.5GiB。
		return 1500, 1536, nil
	default:
		// 未知档位不能转换为调度资源请求。
		return 0, 0, ErrInvalidInstanceClass
	}
}

// IsInputError 判断错误是否属于当前 API 需要按输入错误处理的 workload 校验错误集合。
// 参数说明：err 是待判断的错误。
func IsInputError(err error) bool {
	// service 基础字段校验错误。
	return errors.Is(err, ErrServiceIDRequired) ||
		errors.Is(err, ErrInvalidServiceName) ||
		errors.Is(err, ErrServiceDisplayNameRequired) ||
		errors.Is(err, ErrServiceRegionRequired) ||
		errors.Is(err, ErrInvalidInstanceClass) ||
		errors.Is(err, ErrInvalidExposure) ||
		errors.Is(err, ErrImageRequired) ||
		errors.Is(err, ErrInvalidDefaultPort) ||
		errors.Is(err, ErrInvalidReadinessPath) ||
		errors.Is(err, ErrInvalidEnvironmentKey) ||
		// projected file 校验错误也属于 workload 输入错误。
		errors.Is(err, projectedfile.ErrMountPathRequired) ||
		errors.Is(err, projectedfile.ErrMountPathAbsolute) ||
		errors.Is(err, projectedfile.ErrMountPathInvalid) ||
		errors.Is(err, projectedfile.ErrSourceKindInvalid) ||
		errors.Is(err, projectedfile.ErrSourceIDRequired) ||
		errors.Is(err, projectedfile.ErrSourceKeyRequired) ||
		errors.Is(err, projectedfile.ErrDuplicateMountPath) ||
		// persistent dir 校验错误也属于 workload 输入错误。
		errors.Is(err, persistentdir.ErrNameRequired) ||
		errors.Is(err, persistentdir.ErrInvalidName) ||
		errors.Is(err, persistentdir.ErrMountPathRequired) ||
		errors.Is(err, persistentdir.ErrMountPathAbsolute) ||
		errors.Is(err, persistentdir.ErrMountPathInvalid) ||
		errors.Is(err, persistentdir.ErrDuplicateName) ||
		errors.Is(err, persistentdir.ErrDuplicateMountPath) ||
		errors.Is(err, persistentdir.ErrNestedMountPath) ||
		errors.Is(err, persistentdir.ErrProjectedConflict)
}
