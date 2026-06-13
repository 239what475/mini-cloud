# control-plane container

`control-plane` can run as a stateless web container. The container image is a deployment snapshot: it contains the binary, the bundled web UI, and `/etc/mini-cloud/control-plane.yaml`.

For Tencent Cloud SCF WebServer image functions, use the example config in this directory and keep the HTTP listener on `0.0.0.0:9000`. DNSPod credentials should come from the bound SCF runtime role. SCF forwards temporary role credentials through request headers, and mini-cloud reads those headers when it needs to update DNS records.

Build locally:

```bash
make image-control-plane CONTROL_PLANE_IMAGE=mini-cloud/control-plane:local
```

The build target emits a single `linux/amd64` image without provenance metadata by default, because Tencent Cloud SCF does not accept the OCI manifest index produced by some default Docker BuildKit builds.

Build a deployment snapshot with a real config:

```bash
make image-control-plane \
  CONTROL_PLANE_IMAGE=ccr.ccs.tencentyun.com/example/mini-cloud-control-plane:20260614 \
  CONTROL_PLANE_CONFIG=deploy/container/scf/control-plane.yaml
```
