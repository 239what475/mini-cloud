package frontdoor

import (
	"fmt"
	"strings"

	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
)

func newCDNClient(cfg cloudplaneconfig.Config) (cdnClient, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Infrastructure.Provider)) {
	case "aliyun":
		return newAliyunCDNClient(cfg)
	case "tencent":
		return newTencentCDNClient(cfg)
	default:
		return nil, fmt.Errorf("unsupported frontdoor CDN provider %q", cfg.Infrastructure.Provider)
	}
}
