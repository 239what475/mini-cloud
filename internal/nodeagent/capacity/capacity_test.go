package capacity

import "testing"

func TestResolveNodeCapacitySubtractsReservedResources(t *testing.T) {
	t.Parallel()

	resolved, err := ResolveNodeCapacity(Resources{CPUMilli: 4000, MemoryMi: 8192})
	if err != nil {
		t.Fatalf("ResolveNodeCapacity returned error: %v", err)
	}
	if resolved.Total.CPUMilli != 4000 || resolved.Total.MemoryMi != 8192 {
		t.Fatalf("Total = %+v, want 4000/8192", resolved.Total)
	}
	if resolved.Allocatable.CPUMilli != 3800 {
		t.Fatalf("Allocatable.CPUMilli = %d, want 3800", resolved.Allocatable.CPUMilli)
	}
	if resolved.Allocatable.MemoryMi != 7424 {
		t.Fatalf("Allocatable.MemoryMi = %d, want 7424", resolved.Allocatable.MemoryMi)
	}
}

func TestResolveNodeCapacityRejectsNegativeTotal(t *testing.T) {
	t.Parallel()

	_, err := ResolveNodeCapacity(Resources{CPUMilli: -1, MemoryMi: 1024})
	if err == nil {
		t.Fatal("ResolveNodeCapacity returned nil error for negative total")
	}
}

func TestResolveNodeCapacityRejectsNonPositiveComputedAllocatable(t *testing.T) {
	t.Parallel()

	_, err := ResolveNodeCapacity(Resources{CPUMilli: 200, MemoryMi: 1024})
	if err == nil {
		t.Fatal("ResolveNodeCapacity returned nil error for non-positive CPU allocatable")
	}
}

func TestResolveNodeCapacityUsesExplicitTotalValues(t *testing.T) {
	t.Parallel()

	resolved, err := ResolveNodeCapacity(Resources{CPUMilli: 2000, MemoryMi: 4096})
	if err != nil {
		t.Fatalf("ResolveNodeCapacity returned error: %v", err)
	}
	if resolved.Total.CPUMilli != 2000 || resolved.Total.MemoryMi != 4096 {
		t.Fatalf("Total = %+v, want 2000/4096", resolved.Total)
	}
	if resolved.Allocatable.CPUMilli != 1800 || resolved.Allocatable.MemoryMi != 3328 {
		t.Fatalf("Allocatable = %+v, want 1800/3328", resolved.Allocatable)
	}
}
