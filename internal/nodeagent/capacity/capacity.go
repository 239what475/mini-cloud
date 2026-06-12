package capacity

import (
	"fmt"
	"runtime"

	"github.com/pbnjay/memory"
)

type Resources struct {
	CPUMilli int
	MemoryMi int
}

type Resolved struct {
	Total       Resources
	Allocatable Resources
}

const (
	agentReservedCPUMilli = 200
	agentReservedMemoryMi = 256
	evictionReservedMi    = 512
)

func ResolveNodeCapacity(totalInput Resources) (Resolved, error) {
	total, err := resolveTotal(totalInput)
	if err != nil {
		return Resolved{}, err
	}
	allocatableCPU := total.CPUMilli - agentReservedCPUMilli
	allocatableMemory := total.MemoryMi - agentReservedMemoryMi - evictionReservedMi
	if allocatableCPU <= 0 {
		return Resolved{}, fmt.Errorf("capacity.cpuMilliAllocatable must be greater than 0 after node-agent reservation")
	}
	if allocatableMemory <= 0 {
		return Resolved{}, fmt.Errorf("capacity.memoryMiAllocatable must be greater than 0 after node-agent reservation")
	}
	return Resolved{
		Total:       total,
		Allocatable: Resources{CPUMilli: allocatableCPU, MemoryMi: allocatableMemory},
	}, nil
}

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
