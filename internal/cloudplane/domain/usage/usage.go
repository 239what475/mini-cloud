// Package usage 计算 project 配额、service 资源用量和变更预演结果。
package usage

import (
	"errors"
	"fmt"
	"strings"

	"mini-cloud/internal/cloudplane/domain/workload"
	"mini-cloud/internal/common/project"
)

// ErrProjectQuotaExceeded 表示本次变更会超过 project 配额。
var ErrProjectQuotaExceeded = errors.New("project quota exceeded")

const (
	// RejectReasonServicesQuotaExceeded 表示 service 数量会超过 project 配额。
	RejectReasonServicesQuotaExceeded = "services_quota_exceeded"
	// RejectReasonCPUQuotaExceeded 表示 CPU 请求量会超过 project 配额。
	RejectReasonCPUQuotaExceeded = "cpu_quota_exceeded"
	// RejectReasonMemoryQuotaExceeded 表示内存请求量会超过 project 配额。
	RejectReasonMemoryQuotaExceeded = "memory_quota_exceeded"
)

// RejectReason 描述某一项配额拒绝原因和计算过程。
type RejectReason struct {
	// Code 是配额拒绝原因的机器可读编码。
	Code string `json:"code"`
	// Message 是面向用户展示的拒绝原因。
	Message string `json:"message"`
	// Current 是变更前该资源维度的当前用量。
	Current int `json:"current"`
	// RequestedDelta 是本次候选变更带来的用量增量。
	RequestedDelta int `json:"requestedDelta"`
	// Projected 是应用本次变更后的预计用量。
	Projected int `json:"projected"`
	// Limit 是该资源维度的配额上限。
	Limit int `json:"limit"`
}

// ServiceUsageItem 表示单个 service 当前占用的配额资源。
type ServiceUsageItem struct {
	// ServiceID 表示所属 service 的唯一标识。
	ServiceID string `json:"serviceID"`
	// ServiceName 表示 service 名称。
	ServiceName string `json:"serviceName"`
	// DisplayName 表示面向用户展示的名称。
	DisplayName string `json:"displayName"`
	// InstanceClass 表示平台抽象的实例规格档位。
	InstanceClass string `json:"instanceClass"`
	// Replicas 表示期望副本数。
	Replicas int `json:"replicas"`
	// RequestedCPUMilli 是该 service plan 请求的 CPU 毫核数。
	RequestedCPUMilli int `json:"requestedCPUMilli"`
	// RequestedMemoryMi 是该 service plan 请求的内存 MiB 数。
	RequestedMemoryMi int `json:"requestedMemoryMi"`
}

// ProjectUsage 汇总一个 project 当前已使用的服务数、CPU 和内存。
type ProjectUsage struct {
	// Services 是当前已占用配额的 service 数量。
	Services int `json:"services"`
	// CPUMilli 是当前已占用配额的 CPU 毫核数。
	CPUMilli int `json:"cpuMilli"`
	// MemoryMi 是当前已占用配额的内存 MiB 数。
	MemoryMi int `json:"memoryMi"`
}

// ProjectUsageDelta 表示一次变更对 project 用量造成的增量。
type ProjectUsageDelta struct {
	// Services 是本次变更带来的 service 数量增量。
	Services int `json:"services"`
	// CPUMilli 是本次变更带来的 CPU 毫核数增量，可为负数。
	CPUMilli int `json:"cpuMilli"`
	// MemoryMi 是本次变更带来的内存 MiB 数增量，可为负数。
	MemoryMi int `json:"memoryMi"`
}

// ProjectRemaining 表示配额上限减去用量后的差值，超限时字段可能为负数。
type ProjectRemaining struct {
	// Services 是 service 数量配额剩余差值，超限时为负数。
	Services int `json:"services"`
	// CPUMilli 是 CPU 配额剩余差值，超限时为负数。
	CPUMilli int `json:"cpuMilli"`
	// MemoryMi 是内存配额剩余差值，超限时为负数。
	MemoryMi int `json:"memoryMi"`
}

// ProjectUsageSummary 汇总 project 配额、当前用量、quota-usage 差值和 service 明细。
type ProjectUsageSummary struct {
	// Project 是本次统计所属的 project。
	Project project.Project `json:"project"`
	// Guardrails 是 project 配置的配额上限。
	Guardrails project.Quota `json:"guardrails"`
	// Usage 是 project 当前总用量。
	Usage ProjectUsage `json:"usage"`
	// Remaining 是 project 当前 quota-usage 差值，超限时字段可能为负数。
	Remaining ProjectRemaining `json:"remaining"`
	// Services 是参与统计的 service 用量明细。
	Services []ServiceUsageItem `json:"services"`
}

// ServicePlanPreviewInput 是预演单个 service plan 资源请求的输入。
type ServicePlanPreviewInput struct {
	// InstanceClass 表示平台抽象的实例规格档位。
	InstanceClass string `json:"instanceClass"`
	// Replicas 表示期望副本数。
	Replicas int `json:"replicas"`
}

// ServicePlanPreview 表示某个 service plan 对 CPU 和内存的资源请求。
type ServicePlanPreview struct {
	// InstanceClass 表示平台抽象的实例规格档位。
	InstanceClass string `json:"instanceClass"`
	// Replicas 表示期望副本数。
	Replicas int `json:"replicas"`
	// RequestedCPUMilli 是该 service plan 请求的 CPU 毫核数。
	RequestedCPUMilli int `json:"requestedCPUMilli"`
	// RequestedMemoryMi 是该 service plan 请求的内存 MiB 数。
	RequestedMemoryMi int `json:"requestedMemoryMi"`
}

// ProjectQuotaPreview 表示一次候选变更对 project 配额的影响预演。
type ProjectQuotaPreview struct {
	// Project 是本次统计所属的 project。
	Project project.Project `json:"project"`
	// Guardrails 是 project 配置的配额上限。
	Guardrails project.Quota `json:"guardrails"`
	// Candidate 是本次预演的候选 service plan。
	Candidate ServicePlanPreview `json:"candidate"`
	// CurrentUsage 是变更前 project 当前用量。
	CurrentUsage ProjectUsage `json:"currentUsage"`
	// RequestedDelta 是本次候选变更带来的用量增量。
	RequestedDelta ProjectUsageDelta `json:"requestedDelta"`
	// ProjectedUsage 是应用候选变更后的预计 project 用量。
	ProjectedUsage ProjectUsage `json:"projectedUsage"`
	// Remaining 是应用候选变更后的 quota-usage 差值，超限时字段可能为负数。
	Remaining ProjectRemaining `json:"remaining"`
	// Allowed 表示候选变更是否未超过配额。
	Allowed bool `json:"allowed"`
	// RejectReasons 记录候选变更被拒绝的具体配额原因。
	RejectReasons []RejectReason `json:"rejectReasons"`
}

// QuotaExceededError 携带一个或多个配额拒绝原因。
type QuotaExceededError struct {
	// RejectReasons 记录候选变更被拒绝的具体配额原因。
	RejectReasons []RejectReason
}

// Error 返回错误的文本描述。
func (e *QuotaExceededError) Error() string {
	// 没有具体拒绝原因时，返回通用配额超限错误文本。
	if len(e.RejectReasons) == 0 {
		return ErrProjectQuotaExceeded.Error()
	}
	// 将每条拒绝原因转换为 code/message 片段。
	parts := make([]string, 0, len(e.RejectReasons))
	for _, reason := range e.RejectReasons {
		parts = append(parts, fmt.Sprintf("%s: %s", reason.Code, reason.Message))
	}
	// 用分号拼接多维度配额拒绝原因。
	return fmt.Sprintf("%s: %s", ErrProjectQuotaExceeded.Error(), strings.Join(parts, "; "))
}

// Unwrap 返回包装的底层错误。
func (e *QuotaExceededError) Unwrap() error {
	return ErrProjectQuotaExceeded
}

// BuildProjectUsageSummary 根据 project 配额和 service 列表计算当前用量、quota-usage 差值和明细。
// 参数说明：prj 是目标 project；services 是参与配额计算的 service 列表。
func BuildProjectUsageSummary(prj project.Project, services []workload.Service) (ProjectUsageSummary, error) {
	// items 保存每个 service 的用量明细，aggregate 累加项目总用量。
	items := make([]ServiceUsageItem, 0, len(services))
	aggregate := ProjectUsage{}

	// 逐个 service 计算当前规格对应的资源请求。
	for _, item := range services {
		serviceUsage, err := buildServiceUsageItem(item)
		if err != nil {
			return ProjectUsageSummary{}, err
		}
		// 明细用于展示，aggregate 用于 quota-usage 差值和配额判断。
		items = append(items, serviceUsage)
		accumulateUsage(&aggregate, serviceUsage)
	}

	// 返回当前用量、quota-usage 差值和每个 service 的用量明细。
	return ProjectUsageSummary{
		Project:    prj,
		Guardrails: prj.Quota,
		Usage:      aggregate,
		Remaining:  buildRemaining(prj.Quota, aggregate),
		Services:   items,
	}, nil
}

// PreviewServicePlan 预演 service plan 是否会超过项目配额。
// 参数说明：prj 是目标 project；services 是参与配额计算的 service 列表；input 是候选 service plan。
func PreviewServicePlan(prj project.Project, services []workload.Service, input ServicePlanPreviewInput) (ProjectQuotaPreview, error) {
	candidate, err := DescribeServicePlan(input)
	if err != nil {
		return ProjectQuotaPreview{}, err
	}
	return PreviewCapacityChange(prj, services, candidate, projectUsageDeltaFromPlan(candidate))
}

// PreviewCapacityChange 预演容量变更后的项目用量和 quota-usage 差值。
// 参数说明：prj 是目标 project；services 是参与配额计算的 service 列表；candidate 是候选 service 规格；delta 是本次变更的用量增量。
func PreviewCapacityChange(prj project.Project, services []workload.Service, candidate ServicePlanPreview, delta ProjectUsageDelta) (ProjectQuotaPreview, error) {
	// 先基于当前 service 列表计算现有用量。
	summary, err := BuildProjectUsageSummary(prj, services)
	if err != nil {
		return ProjectQuotaPreview{}, err
	}
	// projected 从当前用量复制，再叠加本次变更增量。
	projected := summary.Usage
	projected.Services += delta.Services
	projected.CPUMilli += delta.CPUMilli
	projected.MemoryMi += delta.MemoryMi

	// 根据 projected 用量判断是否超过各配额维度。
	rejectReasons := evaluateQuota(prj.Quota, summary.Usage, delta, projected)

	// 返回预演视图；Allowed 只取决于是否产生拒绝原因。
	return ProjectQuotaPreview{
		Project:        prj,
		Guardrails:     prj.Quota,
		Candidate:      candidate,
		CurrentUsage:   summary.Usage,
		RequestedDelta: delta,
		ProjectedUsage: projected,
		Remaining:      buildRemaining(prj.Quota, projected),
		Allowed:        len(rejectReasons) == 0,
		RejectReasons:  append([]RejectReason(nil), rejectReasons...),
	}, nil
}

// DeltaBetweenPlans 计算两个 service plan 之间的用量差异。
// 参数说明：current 是变更前 plan；desired 是变更后 plan；serviceCountDelta 是 service 数量增量。
func DeltaBetweenPlans(current ServicePlanPreview, desired ServicePlanPreview, serviceCountDelta int) ProjectUsageDelta {
	return ProjectUsageDelta{
		Services: serviceCountDelta,
		CPUMilli: desired.RequestedCPUMilli - current.RequestedCPUMilli,
		MemoryMi: desired.RequestedMemoryMi - current.RequestedMemoryMi,
	}
}

// HasPositiveCapacityDelta 判断本次用量增量是否会增加任一配额维度。
// 参数说明：delta 是本次变更的用量增量。
func HasPositiveCapacityDelta(delta ProjectUsageDelta) bool {
	return delta.Services > 0 || delta.CPUMilli > 0 || delta.MemoryMi > 0
}

// NewQuotaExceededError 根据拒绝原因构造配额超限错误；没有拒绝原因时返回 nil。
// 参数说明：rejectReasons 是配额预演产生的拒绝原因列表。
func NewQuotaExceededError(rejectReasons []RejectReason) error {
	if len(rejectReasons) == 0 {
		return nil
	}
	return &QuotaExceededError{RejectReasons: rejectReasons}
}

// buildServiceUsageItem 根据 service 当前规格计算单服务用量明细。
// 参数说明：item 是要纳入配额统计的 service 当前规格。
func buildServiceUsageItem(item workload.Service) (ServiceUsageItem, error) {
	// 复用 DescribeServicePlan，将 instance class + replicas 转为资源请求。
	preview, err := DescribeServicePlan(ServicePlanPreviewInput{
		InstanceClass: item.Spec.InstanceClass,
		Replicas:      item.Spec.Replicas,
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
		Replicas:          item.Spec.Replicas,
		RequestedCPUMilli: preview.RequestedCPUMilli,
		RequestedMemoryMi: preview.RequestedMemoryMi,
	}, nil
}

// DescribeServicePlan 根据 instance class 和副本数计算 service plan 用量。
// 参数说明：input 是待计算的 service plan。
func DescribeServicePlan(input ServicePlanPreviewInput) (ServicePlanPreview, error) {
	// instance class 决定单副本 CPU/内存请求。
	cpuMilli, memoryMi, err := workload.ResourceRequest(input.InstanceClass)
	if err != nil {
		return ServicePlanPreview{}, err
	}
	// 副本数必须为正数，才能计算总请求量。
	if input.Replicas <= 0 {
		return ServicePlanPreview{}, workload.ErrInvalidReplicas
	}

	// 总请求量等于单副本请求乘以副本数。
	return ServicePlanPreview{
		InstanceClass:     input.InstanceClass,
		Replicas:          input.Replicas,
		RequestedCPUMilli: cpuMilli * input.Replicas,
		RequestedMemoryMi: memoryMi * input.Replicas,
	}, nil
}

// projectUsageDeltaFromPlan 把 service plan 转换为项目用量增量。
// 参数说明：plan 是待转换的 service plan。
func projectUsageDeltaFromPlan(plan ServicePlanPreview) ProjectUsageDelta {
	return ProjectUsageDelta{
		Services: 1,
		CPUMilli: plan.RequestedCPUMilli,
		MemoryMi: plan.RequestedMemoryMi,
	}
}

// accumulateUsage 把单个 service 用量累加到项目总用量。
// 参数说明：aggregate 是项目用量累计器；item 是单个 service 的配额用量。
func accumulateUsage(aggregate *ProjectUsage, item ServiceUsageItem) {
	aggregate.Services++
	aggregate.CPUMilli += item.RequestedCPUMilli
	aggregate.MemoryMi += item.RequestedMemoryMi
}

// buildRemaining 根据配额和用量计算 quota-usage 差值，超限时会返回负数。
// 参数说明：quota 是项目配额；usage 是当前用量。
func buildRemaining(quota project.Quota, usage ProjectUsage) ProjectRemaining {
	return ProjectRemaining{
		Services: quota.MaxServices - usage.Services,
		CPUMilli: quota.CPUMilli - usage.CPUMilli,
		MemoryMi: quota.MemoryMi - usage.MemoryMi,
	}
}

// evaluateQuota 根据 quota、当前用量和增量生成拒绝原因。
// 参数说明：quota 是项目配额；current 是变更前当前用量；delta 是本次变更的用量增量；projected 是变更后的预计用量。
func evaluateQuota(quota project.Quota, current ProjectUsage, delta ProjectUsageDelta, projected ProjectUsage) []RejectReason {
	// reasons 收集所有超限维度，而不是遇到第一个超限就返回。
	var reasons []RejectReason
	// service 数量超过上限时记录 services 维度拒绝原因。
	if projected.Services > quota.MaxServices {
		reasons = append(reasons, RejectReason{
			Code:           RejectReasonServicesQuotaExceeded,
			Message:        "project services quota would be exceeded",
			Current:        current.Services,
			RequestedDelta: delta.Services,
			Projected:      projected.Services,
			Limit:          quota.MaxServices,
		})
	}
	// CPU projected 用量超过项目 CPU 配额时记录 CPU 维度拒绝原因。
	if projected.CPUMilli > quota.CPUMilli {
		reasons = append(reasons, RejectReason{
			Code:           RejectReasonCPUQuotaExceeded,
			Message:        "project cpu quota would be exceeded",
			Current:        current.CPUMilli,
			RequestedDelta: delta.CPUMilli,
			Projected:      projected.CPUMilli,
			Limit:          quota.CPUMilli,
		})
	}
	// 内存 projected 用量超过项目内存配额时记录 memory 维度拒绝原因。
	if projected.MemoryMi > quota.MemoryMi {
		reasons = append(reasons, RejectReason{
			Code:           RejectReasonMemoryQuotaExceeded,
			Message:        "project memory quota would be exceeded",
			Current:        current.MemoryMi,
			RequestedDelta: delta.MemoryMi,
			Projected:      projected.MemoryMi,
			Limit:          quota.MemoryMi,
		})
	}

	// 没有任何超限时返回 nil 或空切片，调用方用 len 判断是否允许。
	return reasons
}
