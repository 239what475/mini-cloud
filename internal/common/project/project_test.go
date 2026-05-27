package project

import "testing"

func TestCreateProjectInputValidateRequiresOwnerUserID(t *testing.T) {
	t.Parallel()

	err := (CreateProjectInput{
		Name:        "demo-project",
		DisplayName: "Demo Project",
	}).Validate()
	if err != ErrOwnerUserIDRequired {
		t.Fatalf("expected ErrOwnerUserIDRequired, got %v", err)
	}
}

func TestCreateProjectInputResolveQuotaUsesDefaults(t *testing.T) {
	t.Parallel()

	quota, err := (CreateProjectInput{}).ResolveQuota()
	if err != nil {
		t.Fatalf("ResolveQuota returned error: %v", err)
	}

	if quota != DefaultQuota() {
		t.Fatalf("expected default quota %+v, got %+v", DefaultQuota(), quota)
	}
}

func TestCreateProjectInputResolveQuotaAllowsPartialOverride(t *testing.T) {
	t.Parallel()

	quota, err := (CreateProjectInput{
		Quota: Quota{
			MaxServices: 2,
			MemoryMi:    16384,
		},
	}).ResolveQuota()
	if err != nil {
		t.Fatalf("ResolveQuota returned error: %v", err)
	}

	if quota.MaxServices != 2 {
		t.Fatalf("expected maxServices=2, got %d", quota.MaxServices)
	}
	if quota.CPUMilli != DefaultQuotaCPUMilli {
		t.Fatalf("expected default cpuMilli=%d, got %d", DefaultQuotaCPUMilli, quota.CPUMilli)
	}
	if quota.MemoryMi != 16384 {
		t.Fatalf("expected memoryMi=16384, got %d", quota.MemoryMi)
	}
}

func TestCreateProjectInputResolveQuotaRejectsInvalidQuota(t *testing.T) {
	t.Parallel()

	_, err := (CreateProjectInput{
		Quota: Quota{
			CPUMilli: -1,
		},
	}).ResolveQuota()
	if err != ErrQuotaCPUMilliInvalid {
		t.Fatalf("expected ErrQuotaCPUMilliInvalid, got %v", err)
	}
}
