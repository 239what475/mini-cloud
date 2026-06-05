// Package usage 计算全局资源 guardrail、service 资源用量和变更预演结果。
package usage

import (
	"errors"
	"fmt"
	"strings"

	"mini-cloud/internal/cloudplane/domain/workload"
)

// ErrResourceGuardrailExceeded 表示本次变更会超过 resource guardrail。
var ErrResourceGuardrailExceeded = errors.New("resource guardrail exceeded")

const (
	// RejectReasonServicesQuotaExceeded 表示 service 数量会超过 resource guardrail。
	RejectReasonServicesQuotaExceeded = "services_quota_exceeded"
	// RejectReasonCPUQuotaExceeded 表示 CPU 请求量会超过 resource guardrail。
	RejectReasonCPUQuotaExceeded = "cpu_quota_exceeded"
	// RejectReasonMemoryQuotaExceeded 表示内存请求量会超过 resource guardrail。
	RejectReasonMemoryQuotaExceeded = "memory_quota_exceeded"
)

// RejectReason 描述某一项 resource guardrail 拒绝原因和计算过程。
type RejectReason struct {
	// Code 是 resource guardrail 拒绝原因的机器可读编码。
	Code string `json:"code"`
	// Message 是面向用户展示的拒绝原因。
	Message string `json:"message"`
	// Current 是变更前该资源维度的当前用量。
	Current int `json:"current"`
	// RequestedDelta 是本次候选变更带来的用量增量。
	RequestedDelta int `json:"requestedDelta"`
	// Projected 是应用本次变更后的预计用量。
	Projected int `json:"projected"`
	// Limit 是该资源维度的 resource guardrail 上限。
	Limit int `json:"limit"`
}

// ServiceUsageItem 表示单个 service 当前占用的资源。
type ServiceUsageItem struct {
	// ServiceID 表示所属 service 的唯一标识。
	ServiceID string `json:"serviceID"`
	// ServiceName 表示 service 名称。
	ServiceName string `json:"serviceName"`
	// DisplayName 表示面向用户展示的名称。
	DisplayName string `json:"displayName"`
	// InstanceClass 表示平台抽象的实例规格档位。
	InstanceClass string `json:"instanceClass"`
	// RequestedCPUMilli 是该 service plan 请求的 CPU 毫核数。
	RequestedCPUMilli int `json:"requestedCPUMilli"`
	// RequestedMemoryMi 是该 service plan 请求的内存 MiB 数。
	RequestedMemoryMi int `json:"requestedMemoryMi"`
}

// Guardrails 描述 single-tenant control plane 的全局资源上限。
// 字段为 0 时表示该维度不设置软件层上限。
type Guardrails struct {
	MaxServices int `json:"maxServices"`
	CPUMilli    int `json:"cpuMilli"`
	MemoryMi    int `json:"memoryMi"`
}

// ResourceUsage 汇总当前已使用的服务数、CPU 和内存。
type ResourceUsage struct {
	// Services 是当前已运行的 service 数量。
	Services int `json:"services"`
	// CPUMilli 是当前已占用的 CPU 毫核数。
	CPUMilli int `json:"cpuMilli"`
	// MemoryMi 是当前已占用的内存 MiB 数。
	MemoryMi int `json:"memoryMi"`
}

// ResourceUsageDelta 表示一次变更对 resource usage 造成的增量。
type ResourceUsageDelta struct {
	// Services 是本次变更带来的 service 数量增量。
	Services int `json:"services"`
	// CPUMilli 是本次变更带来的 CPU 毫核数增量，可为负数。
	CPUMilli int `json:"cpuMilli"`
	// MemoryMi 是本次变更带来的内存 MiB 数增量，可为负数。
	MemoryMi int `json:"memoryMi"`
}

// ResourceRemaining 表示 resource guardrail 上限减去用量后的差值，超限时字段可能为负数。
type ResourceRemaining struct {
	// Services 是 service 数量剩余差值，超限时为负数。
	Services int `json:"services"`
	// CPUMilli 是 CPU 剩余差值，超限时为负数。
	CPUMilli int `json:"cpuMilli"`
	// MemoryMi 是内存剩余差值，超限时为负数。
	MemoryMi int `json:"memoryMi"`
}

// ResourceUsageSummary 汇总全局 guardrails、当前用量、guardrail-usage 差值和 service 明细。
type ResourceUsageSummary struct {
	// Guardrails 是当前 control plane 配置的资源上限。
	Guardrails Guardrails `json:"guardrails"`
	// Usage 是当前全局总用量。
	Usage ResourceUsage `json:"usage"`
	// Remaining 是当前全局 guardrail-usage 差值，超限时字段可能为负数。
	Remaining ResourceRemaining `json:"remaining"`
	// Services 是参与统计的 service 用量明细。
	Services []ServiceUsageItem `json:"services"`
}

// ServicePlanPreviewInput 是预演单个 service plan 资源请求的输入。
type ServicePlanPreviewInput struct {
	// InstanceClass 表示平台抽象的实例规格档位。
	InstanceClass string `json:"instanceClass"`
}

// ServicePlanPreview 表示某个 service plan 对 CPU 和内存的资源请求。
type ServicePlanPreview struct {
	// InstanceClass 表示平台抽象的实例规格档位。
	InstanceClass string `json:"instanceClass"`
	// RequestedCPUMilli 是该 service plan 请求的 CPU 毫核数。
	RequestedCPUMilli int `json:"requestedCPUMilli"`
	// RequestedMemoryMi 是该 service plan 请求的内存 MiB 数。
	RequestedMemoryMi int `json:"requestedMemoryMi"`
}

// ResourceGuardrailPreview 表示一次候选变更对全局资源 guardrail 的影响预演。
type ResourceGuardrailPreview struct {
	// Guardrails 是当前 control plane 配置的资源上限。
	Guardrails Guardrails `json:"guardrails"`
	// Candidate 是本次预演的候选 service plan。
	Candidate ServicePlanPreview `json:"candidate"`
	// CurrentUsage 是变更前全局用量。
	CurrentUsage ResourceUsage `json:"currentUsage"`
	// RequestedDelta 是本次候选变更带来的用量增量。
	RequestedDelta ResourceUsageDelta `json:"requestedDelta"`
	// ProjectedUsage 是应用候选变更后的预计 resource usage。
	ProjectedUsage ResourceUsage `json:"projectedUsage"`
	// Remaining 是应用候选变更后的 guardrail-usage 差值，超限时字段可能为负数。
	Remaining ResourceRemaining `json:"remaining"`
	// Allowed 表示候选变更是否未超过 resource guardrail。
	Allowed bool `json:"allowed"`
	// RejectReasons 记录候选变更被拒绝的具体 resource guardrail 原因。
	RejectReasons []RejectReason `json:"rejectReasons"`
}

// QuotaExceededError 携带一个或多个 resource guardrail 拒绝原因。
type QuotaExceededError struct {
	// RejectReasons 记录候选变更被拒绝的具体 resource guardrail 原因。
	RejectReasons []RejectReason
}

// Error 返回错误的文本描述。
func (e *QuotaExceededError) Error() string {
	// 没有具体拒绝原因时，返回通用 resource guardrail 超限错误文本。
	if len(e.RejectReasons) == 0 {
		return ErrResourceGuardrailExceeded.Error()
	}
	// 将每条拒绝原因转换为 code/message 片段。
	parts := make([]string, 0, len(e.RejectReasons))
	for _, reason := range e.RejectReasons {
		parts = append(parts, fmt.Sprintf("%s: %s", reason.Code, reason.Message))
	}
	// 用分号拼接多维度 resource guardrail 拒绝原因。
	return fmt.Sprintf("%s: %s", ErrResourceGuardrailExceeded.Error(), strings.Join(parts, "; "))
}

// Unwrap 返回包装的底层错误。
func (e *QuotaExceededError) Unwrap() error {
	return ErrResourceGuardrailExceeded
}

// BuildResourceUsageSummary 根据全局 guardrails 和 service 列表计算当前用量、guardrail-usage 差值和明细。
// 参数说明：guardrails 是全局资源上限；services 是参与 resource guardrail 计算的 service 列表。
func BuildResourceUsageSummary(guardrails Guardrails, services []workload.Service) (ResourceUsageSummary, error) {
	// items 保存每个 service 的用量明细，aggregate 累加全局用量。
	items := make([]ServiceUsageItem, 0, len(services))
	aggregate := ResourceUsage{}

	// 逐个 service 计算当前规格对应的资源请求。
	for _, item := range services {
		serviceUsage, err := buildServiceUsageItem(item)
		if err != nil {
			return ResourceUsageSummary{}, err
		}
		// 明细用于展示，aggregate 用于 guardrail-usage 差值和 resource guardrail 判断。
		items = append(items, serviceUsage)
		accumulateUsage(&aggregate, serviceUsage)
	}

	// 返回当前用量、guardrail-usage 差值和每个 service 的用量明细。
	return ResourceUsageSummary{
		Guardrails: guardrails,
		Usage:      aggregate,
		Remaining:  buildRemaining(guardrails, aggregate),
		Services:   items,
	}, nil
}

// PreviewServicePlan 预演 service plan 是否会超过全局资源 guardrail。
// 参数说明：guardrails 是全局资源上限；services 是参与 resource guardrail 计算的 service 列表；input 是候选 service plan。
func PreviewServicePlan(guardrails Guardrails, services []workload.Service, input ServicePlanPreviewInput) (ResourceGuardrailPreview, error) {
	candidate, err := DescribeServicePlan(input)
	if err != nil {
		return ResourceGuardrailPreview{}, err
	}
	return PreviewCapacityChange(guardrails, services, candidate, resourceUsageDeltaFromPlan(candidate))
}

// PreviewCapacityChange 预演容量变更后的全局用量和 guardrail-usage 差值。
// 参数说明：guardrails 是全局资源上限；services 是参与 resource guardrail 计算的 service 列表；candidate 是候选 service 规格；delta 是本次变更的用量增量。
func PreviewCapacityChange(guardrails Guardrails, services []workload.Service, candidate ServicePlanPreview, delta ResourceUsageDelta) (ResourceGuardrailPreview, error) {
	// 先基于当前 service 列表计算现有用量。
	summary, err := BuildResourceUsageSummary(guardrails, services)
	if err != nil {
		return ResourceGuardrailPreview{}, err
	}
	// projected 从当前用量复制，再叠加本次变更增量。
	projected := summary.Usage
	projected.Services += delta.Services
	projected.CPUMilli += delta.CPUMilli
	projected.MemoryMi += delta.MemoryMi

	// 根据 projected 用量判断是否超过各 resource guardrail 维度。
	rejectReasons := evaluateQuota(guardrails, summary.Usage, delta, projected)

	// 返回预演视图；Allowed 只取决于是否产生拒绝原因。
	return ResourceGuardrailPreview{
		Guardrails:     guardrails,
		Candidate:      candidate,
		CurrentUsage:   summary.Usage,
		RequestedDelta: delta,
		ProjectedUsage: projected,
		Remaining:      buildRemaining(guardrails, projected),
		Allowed:        len(rejectReasons) == 0,
		RejectReasons:  append([]RejectReason(nil), rejectReasons...),
	}, nil
}

// DeltaBetweenPlans 计算两个 service plan 之间的用量差异。
// 参数说明：current 是变更前 plan；desired 是变更后 plan；serviceCountDelta 是 service 数量增量。
func DeltaBetweenPlans(current ServicePlanPreview, desired ServicePlanPreview, serviceCountDelta int) ResourceUsageDelta {
	return ResourceUsageDelta{
		Services: serviceCountDelta,
		CPUMilli: desired.RequestedCPUMilli - current.RequestedCPUMilli,
		MemoryMi: desired.RequestedMemoryMi - current.RequestedMemoryMi,
	}
}

// HasPositiveCapacityDelta 判断本次用量增量是否会增加任一 resource guardrail 维度。
// 参数说明：delta 是本次变更的用量增量。
func HasPositiveCapacityDelta(delta ResourceUsageDelta) bool {
	return delta.Services > 0 || delta.CPUMilli > 0 || delta.MemoryMi > 0
}

// NewQuotaExceededError 根据拒绝原因构造 resource guardrail 超限错误；没有拒绝原因时返回 nil。
// 参数说明：rejectReasons 是 resource guardrail 预演产生的拒绝原因列表。
func NewQuotaExceededError(rejectReasons []RejectReason) error {
	if len(rejectReasons) == 0 {
		return nil
	}
	return &QuotaExceededError{RejectReasons: rejectReasons}
}

// buildServiceUsageItem 根据 service 当前规格计算单服务用量明细。
// 参数说明：item 是要纳入 resource guardrail 统计的 service 当前规格。
func buildServiceUsageItem(item workload.Service) (ServiceUsageItem, error) {
	// 复用 DescribeServicePlan，将 instance class 转为资源请求。
	preview, err := DescribeServicePlan(ServicePlanPreviewInput{
		InstanceClass: item.Spec.InstanceClass,
	})
	if err != nil {
		return ServiceUsageItem{}, err
	}

	// 返回 service 元数据和计算出的 CPU/内存请求。
	return ServiceUsageItem{
		ServiceID:         item.Metadata.ID,
		ServiceName:       item.Metadata.Name,
		DisplayName:       item.Metadata.DisplayName,
		InstanceClass:     item.Spec.InstanceClass,
		RequestedCPUMilli: preview.RequestedCPUMilli,
		RequestedMemoryMi: preview.RequestedMemoryMi,
	}, nil
}

// DescribeServicePlan 根据 instance class 计算 service plan 用量。
// 参数说明：input 是待计算的 service plan。
func DescribeServicePlan(input ServicePlanPreviewInput) (ServicePlanPreview, error) {
	// instance class 决定单副本 CPU/内存请求。
	cpuMilli, memoryMi, err := workload.ResourceRequest(input.InstanceClass)
	if err != nil {
		return ServicePlanPreview{}, err
	}
	return ServicePlanPreview{
		InstanceClass:     input.InstanceClass,
		RequestedCPUMilli: cpuMilli,
		RequestedMemoryMi: memoryMi,
	}, nil
}

// resourceUsageDeltaFromPlan 把 service plan 转换为 resource usage delta。
// 参数说明：plan 是待转换的 service plan。
func resourceUsageDeltaFromPlan(plan ServicePlanPreview) ResourceUsageDelta {
	return ResourceUsageDelta{
		Services: 1,
		CPUMilli: plan.RequestedCPUMilli,
		MemoryMi: plan.RequestedMemoryMi,
	}
}

// accumulateUsage 把单个 service 用量累加到全局用量。
// 参数说明：aggregate 是全局用量累计器；item 是单个 service 的资源用量。
func accumulateUsage(aggregate *ResourceUsage, item ServiceUsageItem) {
	aggregate.Services++
	aggregate.CPUMilli += item.RequestedCPUMilli
	aggregate.MemoryMi += item.RequestedMemoryMi
}

// buildRemaining 根据 resource guardrail 和用量计算 guardrail-usage 差值，超限时会返回负数。
// 参数说明：guardrails 是全局资源护栏；usage 是当前用量。
func buildRemaining(guardrails Guardrails, usage ResourceUsage) ResourceRemaining {
	return ResourceRemaining{
		Services: guardrails.MaxServices - usage.Services,
		CPUMilli: guardrails.CPUMilli - usage.CPUMilli,
		MemoryMi: guardrails.MemoryMi - usage.MemoryMi,
	}
}

// evaluateQuota 根据资源护栏、当前用量和增量生成拒绝原因。
// 参数说明：guardrails 是全局资源护栏；current 是变更前当前用量；delta 是本次变更的用量增量；projected 是变更后的预计用量。
func evaluateQuota(guardrails Guardrails, current ResourceUsage, delta ResourceUsageDelta, projected ResourceUsage) []RejectReason {
	// reasons 收集所有超限维度，而不是遇到第一个超限就返回。
	var reasons []RejectReason
	// service 数量超过上限时记录 services 维度拒绝原因。
	if guardrails.MaxServices > 0 && projected.Services > guardrails.MaxServices {
		reasons = append(reasons, RejectReason{
			Code:           RejectReasonServicesQuotaExceeded,
			Message:        "service count guardrail would be exceeded",
			Current:        current.Services,
			RequestedDelta: delta.Services,
			Projected:      projected.Services,
			Limit:          guardrails.MaxServices,
		})
	}
	// CPU projected 用量超过全局 CPU 护栏时记录 CPU 维度拒绝原因。
	if guardrails.CPUMilli > 0 && projected.CPUMilli > guardrails.CPUMilli {
		reasons = append(reasons, RejectReason{
			Code:           RejectReasonCPUQuotaExceeded,
			Message:        "cpu guardrail would be exceeded",
			Current:        current.CPUMilli,
			RequestedDelta: delta.CPUMilli,
			Projected:      projected.CPUMilli,
			Limit:          guardrails.CPUMilli,
		})
	}
	// 内存 projected 用量超过全局内存护栏时记录 memory 维度拒绝原因。
	if guardrails.MemoryMi > 0 && projected.MemoryMi > guardrails.MemoryMi {
		reasons = append(reasons, RejectReason{
			Code:           RejectReasonMemoryQuotaExceeded,
			Message:        "memory guardrail would be exceeded",
			Current:        current.MemoryMi,
			RequestedDelta: delta.MemoryMi,
			Projected:      projected.MemoryMi,
			Limit:          guardrails.MemoryMi,
		})
	}

	// 没有任何超限时返回 nil 或空切片，调用方用 len 判断是否允许。
	return reasons
}
