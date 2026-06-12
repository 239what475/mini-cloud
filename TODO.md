# TODO

## 云厂商可观测接入

- 将 control-plane、cloud-plane、node-agent 的平台日志、指标和链路追踪接入阿里云、腾讯云可观测体系。
- workload 日志也走云厂商日志服务，不默认依赖自建 Loki。
- mini-cloud 只负责产生标准可观测数据并上报，不内置 Prometheus、Loki、Jaeger、Grafana 等观测平台。

## 代码质量继续打磨

- 继续清理不必要的抽象、兼容逻辑和历史残留。
- 统一 control-plane、cloud-plane、node-agent 的配置、启动、日志、错误处理风格。
- 保持单容器 service 模型，不引入 Docker Compose runtime、多副本、复杂 release、灰度等编排能力。

## 跨云传输安全

- 补齐 control-plane 与 cloud-plane 跨云通信的 TLS 保护。
- 优先考虑 HTTPS + bearer token，必要时再评估 mTLS。
- node-agent 主要运行在 cloud-plane 同云内网中，传输安全后续单独评估。
