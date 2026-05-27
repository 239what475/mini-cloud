package snapshot

import (
	"context"
	"io"
	"log/slog"
	"testing"

	controlplane "mini-cloud/internal/cloudplane/api/controlplane"
	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/testutil"

	"google.golang.org/grpc/metadata"
	"google.golang.org/protobuf/types/known/emptypb"
)

// TestGRPCControlPlaneSouthboundSnapshotReportsDatabaseHealth 验证 snapshot 会报告数据库健康状态。
func TestGRPCControlPlaneSouthboundSnapshotReportsDatabaseHealth(t *testing.T) {
	// 使用真实测试数据库创建 service，确保 snapshot health 走实际 PingContext。
	db := testutil.OpenCloudPlaneTestDatabase(t)
	server := NewServer(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		db.DB,
		db.Store,
		cloudplaneconfig.Config{},
		controlplane.NewAuthenticator("plane-southbound-token"),
	)

	// 构造带 southbound bearer token 的 incoming context，模拟 control-plane 调用。
	ctx := metadata.NewIncomingContext(context.Background(), metadata.Pairs("authorization", "Bearer plane-southbound-token"))
	resp, err := server.GetSnapshot(ctx, &emptypb.Empty{})
	if err != nil {
		t.Fatalf("GetSnapshot returned error: %v", err)
	}
	// 数据库可用时 service 和 database health 都应为 ok。
	if resp.GetHealth().GetService() != "ok" {
		t.Fatalf("snapshot health.service = %q, want ok", resp.GetHealth().GetService())
	}
	if resp.GetHealth().GetDatabase() != "ok" {
		t.Fatalf("snapshot health.database = %q, want ok", resp.GetHealth().GetDatabase())
	}
}
