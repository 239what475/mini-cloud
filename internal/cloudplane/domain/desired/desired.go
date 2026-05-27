// Package desired 定义 cloud-plane 已接受 desired state 的领域模型。
package desired

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"mini-cloud/internal/cloudplane/domain/workload"
)

const (
	// PhaseAccepted 表示 cloud-plane 已接受 desired state，但 reconciler 尚未处理。
	PhaseAccepted = "accepted"
	// PhaseReconciling 表示 reconciler 正在把 desired state 推进到本地 service/deployment 控制状态。
	PhaseReconciling = "reconciling"
	// PhaseObserved 表示当前 desired generation 已被本地 lifecycle 消费。
	PhaseObserved = "observed"
	// PhaseRetrying 表示上一轮推进失败，reconciler 会继续重试并在 Message 中保留最近错误。
	PhaseRetrying = "retrying"
)

var (
	// ErrServiceIDRequired 表示 service desired 缺少目标 serviceID。
	ErrServiceIDRequired = errors.New("serviceID is required")
	// ErrSpecHashFailed 表示 desired spec 归一化或哈希计算失败。
	ErrSpecHashFailed = errors.New("calculate desired spec hash")
)

// Service 表示 cloud-plane 已接受的 service desired state。
type Service struct {
	// ServiceID 是该 desired 对应的 service 唯一标识；service_desired 以它作为主键。
	ServiceID string
	// ProjectID 表示所属 project 的唯一标识。
	ProjectID string
	// Name 表示目标 service 的机器可读名称。
	Name string
	// DisplayName 表示目标 service 的用户可读名称。
	DisplayName string
	// Generation 是该 service desired state 在 cloud-plane 本地递增的版本号。
	Generation int64
	// ObservedGeneration 是本地 lifecycle 已消费到的 desired generation。
	ObservedGeneration int64
	// Spec 是目标 service 的完整运行规格。
	Spec workload.Spec
	// SpecHash 是 Spec 按 HashSpec 规则计算出的哈希，用于判断 accepted desired spec 是否变化。
	SpecHash string
	// Phase 表示该 desired 当前被 reconciler 处理到的阶段。
	Phase string
	// Message 记录 desired 当前阶段的可读说明或失败原因。
	Message string
	// AcceptedAt 是 cloud-plane 接受该 desired 的时间。
	AcceptedAt time.Time
	// ObservedAt 是本地 lifecycle 消费该 desired 的时间；尚未 observed 时为空。
	ObservedAt *time.Time
	// CreatedAt 是资源创建时间。
	CreatedAt time.Time
	// UpdatedAt 是资源最近更新时间。
	UpdatedAt time.Time
}

// AcceptInput 是接受 service desired state 的输入。
type AcceptInput struct {
	// ProjectID 表示所属 project 的唯一标识。
	ProjectID string
	// ServiceID 表示目标 service 标识；cloud-plane 直接使用 control-plane serviceID。
	ServiceID string
	// Name 表示目标 service 的机器可读名称。
	Name string
	// DisplayName 表示目标 service 的用户可读名称。
	DisplayName string
	// Spec 是目标 service 的完整运行规格。
	Spec workload.Spec
}

// AcceptResult 描述一次 desired accept 的结果。
type AcceptResult struct {
	// Desired 是被创建、更新或复用的 desired state。
	Desired Service
	// Action 表示本次 accept 的幂等结果。
	Action string
}

const (
	// ActionCreated 表示本次 accept 创建了新的 desired state。
	ActionCreated = "accepted_created"
	// ActionUpdated 表示本次 accept 更新了已有 desired state。
	ActionUpdated = "accepted_updated"
	// ActionUnchanged 表示本次 accept 与已有 desired state 内容一致。
	ActionUnchanged = "accepted_unchanged"
)

// HashSpec 对部分字段做归一化后计算 desired spec hash，用于 desired accept 的幂等判断和变更检测。
func HashSpec(input workload.Spec) (string, error) {
	// 复制输入，避免哈希归一化过程修改调用方持有的 Spec。
	normalized := input
	// 对部分顶层字符串字段清理外围空白。
	normalized.Region = strings.TrimSpace(normalized.Region)
	// exposure 的空值语义需要先归一化，否则默认 internal 与空字符串会得到不同 hash。
	normalized.Exposure = normalized.NormalizedExposure()
	// 使用 JSON 编码生成确定性字节序列；encoding/json 会按 map key 排序。
	raw, err := json.Marshal(normalized)
	if err != nil {
		return "", errors.Join(ErrSpecHashFailed, err)
	}
	// SHA256 用于得到固定长度 hash，便于 store 判断 spec 是否变化。
	sum := sha256.Sum256(raw)
	// 以十六进制字符串持久化和比较 hash。
	return hex.EncodeToString(sum[:]), nil
}
