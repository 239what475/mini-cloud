package workload

import (
	"errors"
	"testing"

	"mini-cloud/internal/common/persistentdir"
	"mini-cloud/internal/common/projectedfile"
)

// TestInstanceClassResourceRequest 验证 workload 实例规格到资源请求的映射。
func TestInstanceClassResourceRequest(t *testing.T) {
	t.Parallel()

	// 表驱动覆盖平台支持的实例规格到资源请求的映射。
	cases := []struct {
		class   string
		wantCPU int
		wantMem int
	}{
		{class: InstanceClassSmall, wantCPU: 500, wantMem: 512},
		{class: InstanceClassMedium, wantCPU: 1000, wantMem: 1024},
		{class: InstanceClassLarge, wantCPU: 1500, wantMem: 1536},
	}

	// 每个规格都应返回固定 CPU/内存请求，不能产生校验错误。
	for _, tc := range cases {
		cpu, memory, err := ResourceRequest(tc.class)
		if err != nil {
			t.Fatalf("ResourceRequest(%s) returned error: %v", tc.class, err)
		}
		if cpu != tc.wantCPU || memory != tc.wantMem {
			t.Fatalf("ResourceRequest(%s) = (%d, %d), want (%d, %d)", tc.class, cpu, memory, tc.wantCPU, tc.wantMem)
		}
	}
}

// TestSpecValidateAllowsMultipleReplicas 验证无状态 service 规格允许多副本。
func TestSpecValidateAllowsMultipleReplicas(t *testing.T) {
	t.Parallel()

	// 无 persistent dir 的普通 workload 允许多个副本。
	err := (Spec{
		Region:        "cn-beijing",
		Replicas:      2,
		InstanceClass: InstanceClassSmall,
		Image:         "nginx:1.27",
		DefaultPort:   8080,
		ReadinessPath: "/healthz",
	}).Validate()
	// 多副本基础场景通过校验，防止副本限制误伤无状态服务。
	if err != nil {
		t.Fatalf("Validate() returned error: %v", err)
	}
}

// TestSpecValidateRejectsPersistentDirsWithMultipleReplicas 验证持久目录 service 暂不允许多副本。
func TestSpecValidateRejectsPersistentDirsWithMultipleReplicas(t *testing.T) {
	t.Parallel()

	// 带 persistent dir 的服务当前只允许单副本，避免多个副本写同一持久目录。
	err := (Spec{
		Region:        "cn-beijing",
		Replicas:      2,
		InstanceClass: InstanceClassSmall,
		Image:         "nginx:1.27",
		DefaultPort:   8080,
		ReadinessPath: "/healthz",
		PersistentDirs: []persistentdir.Spec{
			{Name: "data", MountPath: "/var/lib/service"},
		},
	}).Validate()
	// 期望返回专用错误，供 API 层准确转换为请求拒绝。
	if !errors.Is(err, ErrPersistentDirsReplicaLimit) {
		t.Fatalf("Validate() error = %v, want %v", err, ErrPersistentDirsReplicaLimit)
	}
}

// TestSpecValidateRejectsProjectedFileOverlapWithPersistentDir 验证 projected file 不能挂载到 persistent dir 内部。
func TestSpecValidateRejectsProjectedFileOverlapWithPersistentDir(t *testing.T) {
	t.Parallel()

	// projected file 的挂载路径落在 persistent dir 内，会被持久目录覆盖或混用。
	err := (Spec{
		Region:        "cn-beijing",
		Replicas:      1,
		InstanceClass: InstanceClassSmall,
		Image:         "nginx:1.27",
		DefaultPort:   8080,
		ReadinessPath: "/healthz",
		ProjectedFiles: []projectedfile.Spec{
			{MountPath: "/var/lib/service/config.yaml", SourceKind: projectedfile.SourceKindConfigSet, SourceID: "cfg-1", SourceKey: "config.yaml"},
		},
		PersistentDirs: []persistentdir.Spec{
			{Name: "data", MountPath: "/var/lib/service"},
		},
	}).Validate()
	// 校验应拒绝路径重叠，避免 node-agent 下发无法安全执行的挂载组合。
	if !errors.Is(err, persistentdir.ErrProjectedConflict) {
		t.Fatalf("Validate() error = %v, want %v", err, persistentdir.ErrProjectedConflict)
	}
}
