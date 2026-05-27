package cloudplaneapi

import (
	cloudplaneconfig "mini-cloud/internal/cloudplane/config"
	"mini-cloud/internal/cloudplane/infra/runtimepool"
)

// Options 汇总 cloud-plane 内部 gRPC 服务端构造时需要的依赖。
type Options struct {
	// Config 表示从单一 YAML 文件读取出的 cloud-plane 有效配置。
	Config cloudplaneconfig.Config
	// RuntimeDriver 表示云厂商 runtime node 生命周期驱动。
	RuntimeDriver runtimepool.RuntimeDriver
}
