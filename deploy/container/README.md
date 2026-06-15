# control-plane container

`deploy/container` 只负责构建 control-plane 的 SCF WebServer 容器镜像。

镜像是一个部署快照，包含：

- `control-plane` linux/amd64 二进制。
- `web/dist` 静态文件。
- `/etc/mini-cloud/control-plane.yaml` 配置快照。

默认配置示例是 `deploy/container/control-plane.yaml.example`。真实部署时，`minictl deploy` 会把根据 `deploy/ops/config.yaml` 渲染出的 control-plane 配置复制到 `dist/ops/control-plane.yaml`，然后作为 Docker build arg 打进镜像。

## 本地构建

```bash
make image-control-plane CONTROL_PLANE_IMAGE=mini-cloud/control-plane:local
```

## 指定配置构建

```bash
make image-control-plane \
  CONTROL_PLANE_IMAGE=ccr.ccs.tencentyun.com/example/mini-cloud-control-plane:20260614 \
  CONTROL_PLANE_CONFIG=dist/ops/control-plane.yaml
```

腾讯云 SCF WebServer 镜像函数要求进程监听 `0.0.0.0:9000`。Docker build 默认使用 `--provenance=false`，避免生成 SCF 不接受的 OCI manifest index。

DNSPod 凭据不写入镜像配置；control-plane 通过绑定到 SCF 的运行角色获取临时凭据。
