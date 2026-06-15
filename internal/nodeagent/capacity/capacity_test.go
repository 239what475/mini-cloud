package capacity

import "testing"

func TestResolveNodeCapacitySubtractsReservedResources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		total           Resources
		wantAllocatable Resources
	}{
		{name: "large node", total: Resources{CPUMilli: 4000, MemoryMi: 8192}, wantAllocatable: Resources{CPUMilli: 3800, MemoryMi: 7424}},
		{name: "small node", total: Resources{CPUMilli: 2000, MemoryMi: 4096}, wantAllocatable: Resources{CPUMilli: 1800, MemoryMi: 3328}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, err := ResolveNodeCapacity(tt.total)
			if err != nil {
				t.Fatalf("ResolveNodeCapacity returned error: %v", err)
			}
			if resolved.Total != tt.total {
				t.Fatalf("Total = %+v, want %+v", resolved.Total, tt.total)
			}
			if resolved.Allocatable != tt.wantAllocatable {
				t.Fatalf("Allocatable = %+v, want %+v", resolved.Allocatable, tt.wantAllocatable)
			}
		})
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
