package capacity

import "testing"

// TestResolveNodeCapacitySubtractsReservedResources 验证默认算法按总容量扣除各类预留量。
func TestResolveNodeCapacitySubtractsReservedResources(t *testing.T) {
	t.Parallel()

	resolved, err := ResolveNodeCapacity(Input{
		Total:            Resources{CPUMilli: 4000, MemoryMi: 8192},
		SystemReserved:   Resources{CPUMilli: 500, MemoryMi: 1024},
		AgentReserved:    Resources{CPUMilli: 250, MemoryMi: 256},
		EvictionReserved: Resources{MemoryMi: 512},
	})
	if err != nil {
		t.Fatalf("ResolveNodeCapacity returned error: %v", err)
	}
	if resolved.Total.CPUMilli != 4000 || resolved.Total.MemoryMi != 8192 {
		t.Fatalf("Total = %+v, want 4000/8192", resolved.Total)
	}
	if resolved.Allocatable.CPUMilli != 3250 {
		t.Fatalf("Allocatable.CPUMilli = %d, want 3250", resolved.Allocatable.CPUMilli)
	}
	if resolved.Allocatable.MemoryMi != 6400 {
		t.Fatalf("Allocatable.MemoryMi = %d, want 6400", resolved.Allocatable.MemoryMi)
	}
}

// TestResolveNodeCapacityComputesAllocatableFromReservations 验证 allocatable 只能由总量扣除预留量得到。
func TestResolveNodeCapacityComputesAllocatableFromReservations(t *testing.T) {
	t.Parallel()

	resolved, err := ResolveNodeCapacity(Input{
		Total:            Resources{CPUMilli: 4000, MemoryMi: 8192},
		SystemReserved:   Resources{CPUMilli: 500, MemoryMi: 1024},
		AgentReserved:    Resources{CPUMilli: 250, MemoryMi: 256},
		EvictionReserved: Resources{MemoryMi: 512},
	})
	if err != nil {
		t.Fatalf("ResolveNodeCapacity returned error: %v", err)
	}
	if resolved.Allocatable.CPUMilli != 3250 {
		t.Fatalf("Allocatable.CPUMilli = %d, want 3250", resolved.Allocatable.CPUMilli)
	}
	if resolved.Allocatable.MemoryMi != 6400 {
		t.Fatalf("Allocatable.MemoryMi = %d, want 6400", resolved.Allocatable.MemoryMi)
	}
}

// TestResolveNodeCapacityRejectsNegativeTotal 验证总容量负数不是自动探测，而是非法配置。
func TestResolveNodeCapacityRejectsNegativeTotal(t *testing.T) {
	t.Parallel()

	_, err := ResolveNodeCapacity(Input{Total: Resources{CPUMilli: -1, MemoryMi: 1024}})
	if err == nil {
		t.Fatal("ResolveNodeCapacity returned nil error for negative total")
	}
}

// TestResolveNodeCapacityRejectsEvictionCPU 验证当前不允许为 eviction 配置 CPU 预算。
func TestResolveNodeCapacityRejectsEvictionCPU(t *testing.T) {
	t.Parallel()

	_, err := ResolveNodeCapacity(Input{
		Total:            Resources{CPUMilli: 4000, MemoryMi: 8192},
		EvictionReserved: Resources{CPUMilli: 100, MemoryMi: 512},
	})
	if err == nil {
		t.Fatal("ResolveNodeCapacity returned nil error for eviction CPU")
	}
}

// TestResolveNodeCapacityRejectsNonPositiveComputedAllocatable 验证预留量耗尽容量时会拒绝配置。
func TestResolveNodeCapacityRejectsNonPositiveComputedAllocatable(t *testing.T) {
	t.Parallel()

	_, err := ResolveNodeCapacity(Input{
		Total:          Resources{CPUMilli: 1000, MemoryMi: 1024},
		SystemReserved: Resources{CPUMilli: 1000, MemoryMi: 512},
	})
	if err == nil {
		t.Fatal("ResolveNodeCapacity returned nil error for non-positive CPU allocatable")
	}
}

// TestResolveNodeCapacityUsesExplicitTotalValues 验证显式总容量会直接返回，不依赖宿主机探测。
func TestResolveNodeCapacityUsesExplicitTotalValues(t *testing.T) {
	t.Parallel()

	resolved, err := ResolveNodeCapacity(Input{Total: Resources{CPUMilli: 2000, MemoryMi: 4096}})
	if err != nil {
		t.Fatalf("ResolveNodeCapacity returned error: %v", err)
	}
	if resolved.Total.CPUMilli != 2000 || resolved.Total.MemoryMi != 4096 {
		t.Fatalf("Total = %+v, want 2000/4096", resolved.Total)
	}
	if resolved.Allocatable.CPUMilli != 2000 || resolved.Allocatable.MemoryMi != 4096 {
		t.Fatalf("Allocatable = %+v, want 2000/4096", resolved.Allocatable)
	}
}
