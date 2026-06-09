package cloud

import (
	"fmt"
	"strings"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/infra/runtimepool"
	"mini-cloud/internal/cloudplane/infra/runtimepool/cloud/aliyun"
	"mini-cloud/internal/cloudplane/infra/runtimepool/cloud/tencent"
)

// NewRuntimeDriver 根据配置中的 provider 名称创建对应的云厂商 runtime driver。
// 参数说明：cfg 是创建 runtime driver 所需的 cloud-plane 配置。
func NewRuntimeDriver(cfg cloudplaneconfig.Config) (runtimepool.RuntimeDriver, error) {
	// provider 名称来自配置文件，选择 driver 前统一 trim/lower。
	providerName := strings.ToLower(strings.TrimSpace(cfg.Infrastructure.Provider))
	switch providerName {
	case "":
		// 空 provider 无法决定具体云厂商实现。
		return nil, fmt.Errorf("infrastructure.provider is required")
	case aliyun.Name:
		// aliyun.NewRuntimeDriver 会解析 aliyun providerSpec 并返回 ECS runtime driver。
		return aliyun.NewRuntimeDriver(cfg)
	case tencent.Name:
		// tencent.NewRuntimeDriver 会解析 tencent providerSpec 并返回 CVM runtime driver。
		return tencent.NewRuntimeDriver(cfg)
	default:
		// 未注册 provider 直接报错；当前不保留 local/manual driver。
		return nil, fmt.Errorf("runtime driver for provider %q is not implemented", cfg.Infrastructure.Provider)
	}
}
