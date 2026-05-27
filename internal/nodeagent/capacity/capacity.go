package capacity

import (
	"fmt"
	"runtime"

	"github.com/pbnjay/memory"
)

// Resources 表示一组 CPU / 内存资源数量。
type Resources struct {
	// CPUMilli 是 CPU 数量，单位为 millicore。
	CPUMilli int
	// MemoryMi 是内存数量，单位为 MiB。
	MemoryMi int
}

// Input 描述解析节点容量模型所需的全部输入。
type Input struct {
	// Total 是 node-agent 纳入 mini-cloud 资源账本的总资源池；字段为 0 时自动探测，负数非法。
	Total Resources
	// SystemReserved 是预留给 OS、sshd、journald、用户进程等非 mini-cloud 负载的资源。
	SystemReserved Resources
	// AgentReserved 是预留给 node-agent、容器运行时、日志采集等节点管理组件的资源。
	AgentReserved Resources
	// EvictionReserved 是为压力保护预留的资源；当前只允许配置内存，CPU 必须为 0。
	EvictionReserved Resources
}

// Resolved 是 node-agent 最终用于注册和心跳的容量模型。
type Resolved struct {
	// Total 是 node-agent 纳入 mini-cloud 资源账本的总资源池，不强制等同于宿主机物理总容量。
	Total Resources
	// Allocatable 是 mini-cloud 可调度静态预算，用于心跳和调度供给。
	Allocatable Resources
}

// ResolveNodeCapacity 解析 mini-cloud 资源账本总量并计算允许调度的静态 allocatable 预算。
//
// 算法参考 Kubernetes Node Allocatable：
//
//	allocatable = total - systemReserved - agentReserved - evictionReserved
//
// 这里的 allocatable 不是宿主机实时 free：它不读取 CPU idle，也不读取 MemAvailable。
// 它只是 node-agent 承诺交给 mini-cloud 调度器使用的容量预算。
// 用户如果只想把宿主机一部分资源纳入 mini-cloud，应直接配置 total 为这部分资源账本总量；
// 用户如果在这部分资源账本里还要扣除系统或管理组件开销，应继续配置各类 reserved。
// 当前 evictionReserved 只对内存生效，CPU eviction 预算必须保持为 0。
func ResolveNodeCapacity(input Input) (Resolved, error) {
	total, err := resolveTotal(input.Total)
	if err != nil {
		return Resolved{}, err
	}
	if err := validateResources("capacity.systemReserved", input.SystemReserved, true); err != nil {
		return Resolved{}, err
	}
	if err := validateResources("capacity.agentReserved", input.AgentReserved, true); err != nil {
		return Resolved{}, err
	}
	if err := validateResources("capacity.evictionReserved", input.EvictionReserved, true); err != nil {
		return Resolved{}, err
	}
	if input.EvictionReserved.CPUMilli != 0 {
		return Resolved{}, fmt.Errorf("capacity.evictionReserved.cpuMilli must be 0 because CPU eviction budget is not supported")
	}
	allocatableCPU := total.CPUMilli - input.SystemReserved.CPUMilli - input.AgentReserved.CPUMilli - input.EvictionReserved.CPUMilli
	allocatableMemory := total.MemoryMi - input.SystemReserved.MemoryMi - input.AgentReserved.MemoryMi - input.EvictionReserved.MemoryMi
	if allocatableCPU <= 0 {
		return Resolved{}, fmt.Errorf("capacity.cpuMilliAllocatable must be greater than 0 after reservations")
	}
	if allocatableMemory <= 0 {
		return Resolved{}, fmt.Errorf("capacity.memoryMiAllocatable must be greater than 0 after reservations")
	}
	return Resolved{
		Total:       total,
		Allocatable: Resources{CPUMilli: allocatableCPU, MemoryMi: allocatableMemory},
	}, nil
}

// resolveTotal 返回 mini-cloud 资源账本总量；字段为 0 时从本机探测补齐，负数直接拒绝。
func resolveTotal(total Resources) (Resources, error) {
	if total.CPUMilli < 0 {
		return Resources{}, fmt.Errorf("capacity.total.cpuMilli must be greater than or equal to 0")
	}
	if total.MemoryMi < 0 {
		return Resources{}, fmt.Errorf("capacity.total.memoryMi must be greater than or equal to 0")
	}
	if total.CPUMilli > 0 && total.MemoryMi > 0 {
		return total, nil
	}

	detected, err := detectHostCapacity()
	if err != nil {
		return Resources{}, err
	}
	if total.CPUMilli == 0 {
		total.CPUMilli = detected.CPUMilli
	}
	if total.MemoryMi == 0 {
		total.MemoryMi = detected.MemoryMi
	}
	return total, nil
}

// validateResources 校验资源数量；allowZero 为 true 时允许字段为 0。
func validateResources(name string, value Resources, allowZero bool) error {
	if allowZero {
		if value.CPUMilli < 0 {
			return fmt.Errorf("%s.cpuMilli must be greater than or equal to 0", name)
		}
		if value.MemoryMi < 0 {
			return fmt.Errorf("%s.memoryMi must be greater than or equal to 0", name)
		}
		return nil
	}
	if value.CPUMilli <= 0 {
		return fmt.Errorf("%s.cpuMilli must be greater than 0", name)
	}
	if value.MemoryMi <= 0 {
		return fmt.Errorf("%s.memoryMi must be greater than 0", name)
	}
	return nil
}

// detectHostCapacity 按上报给控制面的单位读取当前系统可见的 CPU 和内存总量。
func detectHostCapacity() (Resources, error) {
	cpuMilli := runtime.NumCPU() * 1000
	if cpuMilli <= 0 {
		return Resources{}, fmt.Errorf("detect host cpu capacity: runtime.NumCPU returned %d", runtime.NumCPU())
	}

	memoryBytes := memory.TotalMemory()
	if memoryBytes == 0 {
		return Resources{}, fmt.Errorf("detect host memory capacity: memory.TotalMemory returned 0")
	}
	memoryMi := int(memoryBytes / 1024 / 1024)
	if memoryMi <= 0 {
		return Resources{}, fmt.Errorf("detect host memory capacity: resolved memoryMi=%d", memoryMi)
	}

	return Resources{CPUMilli: cpuMilli, MemoryMi: memoryMi}, nil
}
