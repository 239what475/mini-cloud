package cloud

import (
	"fmt"
	"strings"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/infra/nodeprovider"
	"mini-cloud/internal/cloudplane/infra/nodeprovider/cloud/aliyun"
	"mini-cloud/internal/cloudplane/infra/nodeprovider/cloud/tencent"
)

func NewDriver(cfg cloudplaneconfig.Config) (nodeprovider.Driver, error) {
	providerName := strings.ToLower(strings.TrimSpace(cfg.Infrastructure.Provider))
	switch providerName {
	case "":
		return nil, fmt.Errorf("infrastructure.provider is required")
	case aliyun.Name:
		return aliyun.NewDriver(cfg)
	case tencent.Name:
		return tencent.NewDriver(cfg)
	default:
		return nil, fmt.Errorf("node provider driver for provider %q is not implemented", cfg.Infrastructure.Provider)
	}
}
