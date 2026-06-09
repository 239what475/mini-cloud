package nodeprovider

import (
	"context"
)

// CreateRequest 描述 cloud-plane 需要 node provider driver 在云厂商侧创建的 node。
type CreateRequest struct {
	// Name 是要创建的云主机名称。
	Name string
	// ClientToken 是本次创建请求的云厂商幂等 token。
	ClientToken string
	// CPUMilli 是单个 run 需要的 CPU 资源，单位为 millicore。
	CPUMilli int
	// MemoryMi 是单个 run 需要的内存资源，单位为 MiB。
	MemoryMi int
}

// CreateResult 描述 node provider driver 创建 node 后返回的可持久化信息。
type CreateResult struct {
	// InstanceID 表示 instance 的唯一标识。
	InstanceID string `json:"instanceID"`
	// InstanceName 是资源名称或展示名。
	InstanceName string `json:"instanceName"`
	// InstanceType 是云厂商实例规格。
	InstanceType string `json:"instanceType"`
}

// DeleteRequest 描述 cloud-plane 需要 node provider driver 在云厂商侧删除的 node。
type DeleteRequest struct {
	// InstanceID 表示要删除的云实例唯一标识。
	InstanceID string
}

// Driver 是 cloud-plane 与具体云厂商 node 生命周期能力之间的边界。
type Driver interface {
	Create(ctx context.Context, request CreateRequest) (CreateResult, error)
	Delete(ctx context.Context, request DeleteRequest) error
}
