package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	cloudplaneidentity "mini-cloud/internal/cloudplane/control/identity"
	"mini-cloud/internal/cloudplane/infra/store"
	cloudmodel "mini-cloud/internal/cloudplane/model"
	"mini-cloud/internal/testutil"
)

// TestIssueNodeAgentSessionTokenReplacesPreviousToken 验证同一 node 重签 token 会替换旧 token。
func TestIssueNodeAgentSessionTokenReplacesPreviousToken(t *testing.T) {
	// 使用真实测试数据库验证 session token 的唯一有效 token 语义。
	db := testutil.OpenCloudPlaneTestDatabase(t)

	// 先注册 node，后续 session token 都绑定到该 node。
	registered, err := db.Store.RegisterNode(context.Background(), cloudmodel.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "node-token-test",
		PrivateIP:     "10.0.0.21",
		InstanceID:    "i-node-token-test",
		InstanceType:  "ecs.u1-c1m1.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("register node returned error: %v", err)
	}

	// 第一次签发 token 后应能解析回同一个 nodeID。
	token1, err := cloudplaneidentity.NewService(db.Store).IssueNodeAgentSessionToken(context.Background(), registered.ID, time.Hour)
	if err != nil {
		t.Fatalf("issue first session token returned error: %v", err)
	}
	resolved1, err := cloudplaneidentity.NewService(db.Store).ResolveNodeAgentSessionTokenBySecret(context.Background(), token1)
	if err != nil {
		t.Fatalf("resolve first session token returned error: %v", err)
	}
	if resolved1 != registered.ID {
		t.Fatalf("resolved nodeID = %q, want %q", resolved1, registered.ID)
	}

	// 第二次签发应替换前一个 token，并生成不同 secret。
	token2, err := cloudplaneidentity.NewService(db.Store).IssueNodeAgentSessionToken(context.Background(), registered.ID, time.Hour)
	if err != nil {
		t.Fatalf("issue second session token returned error: %v", err)
	}
	if token2 == token1 {
		t.Fatalf("expected reissued session token to change, got %q", token2)
	}

	// 旧 token 被替换后不能再解析，避免 node-agent 使用过期会话继续操作。
	_, err = cloudplaneidentity.NewService(db.Store).ResolveNodeAgentSessionTokenBySecret(context.Background(), token1)
	if !errors.Is(err, store.ErrNodeAgentSessionTokenNotFound) {
		t.Fatalf("resolve stale session token error = %v, want %v", err, store.ErrNodeAgentSessionTokenNotFound)
	}

	// 新 token 仍应解析到原 node。
	resolved2, err := cloudplaneidentity.NewService(db.Store).ResolveNodeAgentSessionTokenBySecret(context.Background(), token2)
	if err != nil {
		t.Fatalf("resolve second session token returned error: %v", err)
	}
	if resolved2 != registered.ID {
		t.Fatalf("resolved nodeID = %q, want %q", resolved2, registered.ID)
	}
}

// TestIssueNodeAgentSessionTokenExpires 验证过期 node-agent session token 不能解析。
func TestIssueNodeAgentSessionTokenExpires(t *testing.T) {
	// 使用真实测试数据库验证过期 token 不可解析。
	db := testutil.OpenCloudPlaneTestDatabase(t)

	// 先注册 node，作为 token 绑定对象。
	registered, err := db.Store.RegisterNode(context.Background(), cloudmodel.RegisterInput{
		Provider:      "aliyun",
		Region:        "cn-beijing",
		Name:          "node-token-expiry-test",
		PrivateIP:     "10.0.0.22",
		InstanceID:    "i-node-token-expiry-test",
		InstanceType:  "ecs.u1-c1m1.large",
		CPUMilliTotal: 2000,
		MemoryMiTotal: 4096,
	})
	if err != nil {
		t.Fatalf("register node returned error: %v", err)
	}

	// 使用负 TTL 签发已经过期的 token，模拟过期记录。
	token, err := cloudplaneidentity.NewService(db.Store).IssueNodeAgentSessionToken(context.Background(), registered.ID, -time.Second)
	if err != nil {
		t.Fatalf("issue expiring session token returned error: %v", err)
	}

	// 解析过期 token 应返回 not found，调用方会映射为 unauthenticated。
	_, err = cloudplaneidentity.NewService(db.Store).ResolveNodeAgentSessionTokenBySecret(context.Background(), token)
	if !errors.Is(err, store.ErrNodeAgentSessionTokenNotFound) {
		t.Fatalf("resolve expired session token error = %v, want %v", err, store.ErrNodeAgentSessionTokenNotFound)
	}
}
