#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TERRAFORM_DIR="${TERRAFORM_DIR:-$ROOT_DIR/deploy/terraform/lab}"

PLATFORM_HOST="${PLATFORM_HOST:-}"
SSH_USER="${SSH_USER:-root}"
SSH_KEY="${SSH_KEY:-}"
SSH_OPTS="${SSH_OPTS:-}"
INSTALL_ROOT="${INSTALL_ROOT:-/opt/mini-cloud}"
CONTROL_PLANE_BINARY_PATH="${CONTROL_PLANE_BINARY_PATH:-$ROOT_DIR/dist/release/linux-amd64/control-plane}"
CLOUD_PLANE_BINARY_PATH="${CLOUD_PLANE_BINARY_PATH:-$ROOT_DIR/dist/release/linux-amd64/cloud-plane}"
NODE_AGENT_BINARY_PATH="${NODE_AGENT_BINARY_PATH:-$ROOT_DIR/dist/release/linux-amd64/node-agent}"

PLATFORM_NAME="${PLATFORM_NAME:-}"
PLATFORM_PRIVATE_IP="${PLATFORM_PRIVATE_IP:-}"
PROVIDER="${PROVIDER:-}"
REGION_ID="${REGION_ID:-}"
ZONE_ID="${ZONE_ID:-}"
SUBNET_CIDR_BLOCK="${SUBNET_CIDR_BLOCK:-}"
RUNTIME_PROVIDER_SPEC_JSON="${RUNTIME_PROVIDER_SPEC_JSON:-}"
RUNTIME_INSTANCE_TYPE="${RUNTIME_INSTANCE_TYPE:-}"

CONTROL_PLANE_HTTP_ADDR="${CONTROL_PLANE_HTTP_ADDR:-127.0.0.1:18080}"
CLOUD_PLANE_GRPC_PORT="${CLOUD_PLANE_GRPC_PORT:-18081}"
INGRESS_HTTP_PORT="${INGRESS_HTTP_PORT:-80}"
EGRESS_PROXY_PORT="${EGRESS_PROXY_PORT:-3128}"
ARTIFACT_HTTP_PORT="${ARTIFACT_HTTP_PORT:-18082}"
INGRESS_BASE_DOMAIN="${INGRESS_BASE_DOMAIN:-apps.example.com}"
REGISTRY_MIRROR="${REGISTRY_MIRROR:-}"
WORKLOAD_LOG_LOKI_URL="${WORKLOAD_LOG_LOKI_URL:-}"
WORKLOAD_LOG_LOKI_TENANT_ID="${WORKLOAD_LOG_LOKI_TENANT_ID:-}"
WORKLOAD_OTLP_ENDPOINT="${WORKLOAD_OTLP_ENDPOINT:-}"
TENCENTCLOUD_CREDENTIAL_FILE="${TENCENTCLOUD_CREDENTIAL_FILE:-$HOME/.tccli/default.credential}"
TENCENTCLOUD_SECRET_ID="${TENCENTCLOUD_SECRET_ID:-}"
TENCENTCLOUD_SECRET_KEY="${TENCENTCLOUD_SECRET_KEY:-}"
TENCENTCLOUD_TOKEN="${TENCENTCLOUD_TOKEN:-}"

CONTROL_PLANE_ADMIN_TOKEN="${CONTROL_PLANE_ADMIN_TOKEN:-}"
CONTROL_PLANE_SOUTHBOUND_TOKEN="${CONTROL_PLANE_SOUTHBOUND_TOKEN:-}"
NODE_AGENT_BOOTSTRAP_TOKEN="${NODE_AGENT_BOOTSTRAP_TOKEN:-}"

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    echo "$name is required" >&2
    exit 1
  fi
}

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "$1 is required" >&2
    exit 1
  fi
}

require_file() {
  local path="$1"
  if [[ ! -f "$path" ]]; then
    echo "$path does not exist; run make build-release first or set the binary path explicitly" >&2
    exit 1
  fi
}

json_path() {
  local json="$1"
  local path="$2"
  JSON_INPUT="$json" JSON_PATH="$path" python3 - <<'PY'
import json
import os

value = json.loads(os.environ["JSON_INPUT"])
for part in os.environ["JSON_PATH"].split("."):
    if part:
        value = value.get(part) if isinstance(value, dict) else None
        if value is None:
            break
if value is None:
    print("")
elif isinstance(value, (dict, list)):
    print(json.dumps(value, separators=(",", ":")))
else:
    print(value)
PY
}

ssh_args() {
  local args=()
  if [[ -n "$SSH_KEY" ]]; then
    args+=("-i" "$SSH_KEY")
  fi
  # shellcheck disable=SC2206
  args+=($SSH_OPTS)
  if ((${#args[@]} > 0)); then
    printf '%s\n' "${args[@]}"
  fi
}

load_terraform_outputs() {
  require_cmd terraform

  local output_json
  output_json="$(terraform -chdir="$TERRAFORM_DIR" output -json)"

  PROVIDER="$(json_path "$output_json" "provider.value")"
  local ssh_host
  ssh_host="$(json_path "$output_json" "platform.value.ssh_host")"
  if [[ -n "$ssh_host" ]]; then
    PLATFORM_HOST="$ssh_host"
  else
    ssh_host="$(json_path "$output_json" "platform.value.public_ip")"
    PLATFORM_HOST="${SSH_USER}@${ssh_host}"
  fi
  eval "$(
    JSON_INPUT="$output_json" python3 - <<'PY'
import json
import shlex
import os

data = json.loads(os.environ["JSON_INPUT"])
values = {
    "PLATFORM_NAME": data["platform"]["value"]["name"],
    "PLATFORM_PRIVATE_IP": data["platform"]["value"]["private_ip"],
    "REGION_ID": data["install_env"]["value"]["region_id"],
    "ZONE_ID": data["install_env"]["value"]["zone_id"],
    "CLOUD_PLANE_GRPC_PORT": data["network"]["value"]["cloud_plane_grpc_port"],
    "INGRESS_HTTP_PORT": data["network"]["value"]["ingress_http_port"],
    "EGRESS_PROXY_PORT": data["network"]["value"]["egress_proxy_port"],
    "ARTIFACT_HTTP_PORT": data["network"]["value"]["artifact_http_port"],
    "SUBNET_CIDR_BLOCK": data["network"]["value"]["subnet_cidr_block"],
    "RUNTIME_PROVIDER_SPEC_JSON": json.dumps(data["runtime_provider_spec"]["value"], separators=(",", ":")),
}
for key, value in values.items():
    print(f"{key}={shlex.quote(str(value))}")
PY
  )"
}

detect_platform_private_ip() {
  local detected
  detected="$(ssh "${SSH_ARGS_ARRAY[@]}" "$PLATFORM_HOST" "hostname -I | tr ' ' '\n' | awk '/^(10\\.|172\\.(1[6-9]|2[0-9]|3[0-1])\\.|192\\.168\\.)/ { print; exit }'")"
  if [[ -z "$detected" ]]; then
    echo "PLATFORM_PRIVATE_IP is required because it could not be detected from $PLATFORM_HOST" >&2
    exit 1
  fi
  PLATFORM_PRIVATE_IP="$detected"
}

write_remote_env() {
  printf '%s=%q\n' "$1" "$2" >>"$REMOTE_ENV"
}

require_cmd ssh
require_cmd scp
require_cmd python3
require_file "$CONTROL_PLANE_BINARY_PATH"
require_file "$CLOUD_PLANE_BINARY_PATH"
require_file "$NODE_AGENT_BINARY_PATH"
require_env CONTROL_PLANE_ADMIN_TOKEN
require_env CONTROL_PLANE_SOUTHBOUND_TOKEN
require_env NODE_AGENT_BOOTSTRAP_TOKEN

if [[ -z "$PLATFORM_HOST" ]]; then
  load_terraform_outputs
else
  : "${PROVIDER:=tencent}"
  require_env PLATFORM_NAME
  require_env REGION_ID
  require_env ZONE_ID
  require_env SUBNET_CIDR_BLOCK
  require_env RUNTIME_PROVIDER_SPEC_JSON
fi

if [[ "$PROVIDER" == "tencent" && ( -z "$TENCENTCLOUD_SECRET_ID" || -z "$TENCENTCLOUD_SECRET_KEY" ) ]]; then
  if [[ ! -f "$TENCENTCLOUD_CREDENTIAL_FILE" ]]; then
    echo "TENCENTCLOUD_SECRET_ID/TENCENTCLOUD_SECRET_KEY or TENCENTCLOUD_CREDENTIAL_FILE is required for tencent" >&2
    exit 1
  fi
  TENCENTCLOUD_SECRET_ID="$(python3 - "$TENCENTCLOUD_CREDENTIAL_FILE" <<'PY'
import json
import sys
print(json.load(open(sys.argv[1])).get("secretId", ""))
PY
)"
  TENCENTCLOUD_SECRET_KEY="$(python3 - "$TENCENTCLOUD_CREDENTIAL_FILE" <<'PY'
import json
import sys
print(json.load(open(sys.argv[1])).get("secretKey", ""))
PY
)"
  TENCENTCLOUD_TOKEN="$(python3 - "$TENCENTCLOUD_CREDENTIAL_FILE" <<'PY'
import json
import sys
data = json.load(open(sys.argv[1]))
print(data.get("sessionToken") or data.get("token") or "")
PY
)"
fi

if [[ "$PROVIDER" == "tencent" ]]; then
  require_env TENCENTCLOUD_SECRET_ID
  require_env TENCENTCLOUD_SECRET_KEY
fi

if [[ "$REGISTRY_MIRROR" == "" ]]; then
  if [[ "$PROVIDER" == "tencent" ]]; then
    REGISTRY_MIRROR="https://mirror.ccs.tencentyun.com"
  else
    REGISTRY_MIRROR="https://docker.m.daocloud.io"
  fi
fi

if [[ -z "$RUNTIME_INSTANCE_TYPE" ]]; then
  RUNTIME_INSTANCE_TYPE="$(python3 - "$RUNTIME_PROVIDER_SPEC_JSON" <<'PY'
import json
import sys
print(json.loads(sys.argv[1]).get("instanceType", ""))
PY
)"
fi

mapfile -t SSH_ARGS_ARRAY < <(ssh_args)
if [[ -z "$PLATFORM_PRIVATE_IP" ]]; then
  detect_platform_private_ip
fi
NODE_AGENT_BINARY_URL="http://$PLATFORM_PRIVATE_IP:$ARTIFACT_HTTP_PORT/node-agent-linux-amd64"

REMOTE_ENV="$(mktemp)"
REMOTE_SCRIPT=""
cleanup_local_files() {
  rm -f "$REMOTE_ENV"
  if [[ -n "$REMOTE_SCRIPT" ]]; then
    rm -f "$REMOTE_SCRIPT"
  fi
}
trap cleanup_local_files EXIT

: >"$REMOTE_ENV"
write_remote_env INSTALL_ROOT "$INSTALL_ROOT"
write_remote_env PROVIDER "$PROVIDER"
write_remote_env PLATFORM_NAME "$PLATFORM_NAME"
write_remote_env PLATFORM_PRIVATE_IP "$PLATFORM_PRIVATE_IP"
write_remote_env REGION_ID "$REGION_ID"
write_remote_env ZONE_ID "$ZONE_ID"
write_remote_env CONTROL_PLANE_HTTP_ADDR "$CONTROL_PLANE_HTTP_ADDR"
write_remote_env CLOUD_PLANE_GRPC_PORT "$CLOUD_PLANE_GRPC_PORT"
write_remote_env INGRESS_HTTP_PORT "$INGRESS_HTTP_PORT"
write_remote_env EGRESS_PROXY_PORT "$EGRESS_PROXY_PORT"
write_remote_env ARTIFACT_HTTP_PORT "$ARTIFACT_HTTP_PORT"
write_remote_env SUBNET_CIDR_BLOCK "$SUBNET_CIDR_BLOCK"
write_remote_env INGRESS_BASE_DOMAIN "$INGRESS_BASE_DOMAIN"
write_remote_env REGISTRY_MIRROR "$REGISTRY_MIRROR"
write_remote_env WORKLOAD_LOG_LOKI_URL "$WORKLOAD_LOG_LOKI_URL"
write_remote_env WORKLOAD_LOG_LOKI_TENANT_ID "$WORKLOAD_LOG_LOKI_TENANT_ID"
write_remote_env WORKLOAD_OTLP_ENDPOINT "$WORKLOAD_OTLP_ENDPOINT"
write_remote_env TENCENTCLOUD_SECRET_ID "$TENCENTCLOUD_SECRET_ID"
write_remote_env TENCENTCLOUD_SECRET_KEY "$TENCENTCLOUD_SECRET_KEY"
write_remote_env TENCENTCLOUD_TOKEN "$TENCENTCLOUD_TOKEN"
write_remote_env CONTROL_PLANE_ADMIN_TOKEN "$CONTROL_PLANE_ADMIN_TOKEN"
write_remote_env CONTROL_PLANE_SOUTHBOUND_TOKEN "$CONTROL_PLANE_SOUTHBOUND_TOKEN"
write_remote_env NODE_AGENT_BOOTSTRAP_TOKEN "$NODE_AGENT_BOOTSTRAP_TOKEN"
write_remote_env NODE_AGENT_BINARY_URL "$NODE_AGENT_BINARY_URL"
write_remote_env RUNTIME_PROVIDER_SPEC_JSON "$RUNTIME_PROVIDER_SPEC_JSON"
write_remote_env RUNTIME_INSTANCE_TYPE "$RUNTIME_INSTANCE_TYPE"

REMOTE_SCRIPT="$(mktemp)"
cat >"$REMOTE_SCRIPT" <<'REMOTE'
#!/usr/bin/env bash
set -euo pipefail

set -a
source /tmp/mini-cloud-lab-install.env
set +a
trap 'rm -f /tmp/mini-cloud-lab-install.env /tmp/mini-cloud-control-plane /tmp/mini-cloud-cloud-plane /tmp/mini-cloud-node-agent' EXIT

BOOTSTRAP_LOG="$INSTALL_ROOT/install.log"
CONTROL_PLANE_CONFIG_DIR=/etc/mini-cloud/control-plane
CLOUD_PLANE_CONFIG_DIR=/etc/mini-cloud/cloud-plane
POSTGRES_IMAGE="docker.io/library/postgres:17"
CADDY_IMAGE="docker.io/library/caddy:2"

log() {
  install -d -m 0755 "$INSTALL_ROOT"
  printf '[mini-cloud install] %s\n' "$*" | tee -a "$BOOTSTRAP_LOG"
}

docker_pull_with_retry() {
  local image="$1"
  for attempt in 1 2 3 4 5; do
    if timeout 600 docker pull "$image"; then
      return 0
    fi
    log "docker pull $image failed on attempt $attempt; retrying"
    sleep 10
  done
  return 1
}

install_packages() {
  if ! command -v apt-get >/dev/null 2>&1; then
    log "this installer expects a Debian/Ubuntu image with apt-get"
    exit 1
  fi
  export DEBIAN_FRONTEND=noninteractive
  apt-get update
  apt-get install -y curl ca-certificates python3 jq tinyproxy
  if ! command -v docker >/dev/null 2>&1; then
    apt-get install -y docker.io
  fi
}

configure_docker() {
  if [[ -z "$REGISTRY_MIRROR" ]]; then
    return
  fi
  install -d -m 0755 /etc/docker
  python3 - "$REGISTRY_MIRROR" >/etc/docker/daemon.json <<'PY'
import json
import sys
print(json.dumps({"registry-mirrors": [sys.argv[1]]}, indent=2))
PY
}

ensure_tinyproxy() {
  cat >/etc/tinyproxy/tinyproxy.conf <<EOF
User tinyproxy
Group tinyproxy
Port $EGRESS_PROXY_PORT
Listen 0.0.0.0
Timeout 600
DefaultErrorFile "/usr/share/tinyproxy/default.html"
StatFile "/usr/share/tinyproxy/stats.html"
LogFile "/var/log/tinyproxy/tinyproxy.log"
LogLevel Info
PidFile "/run/tinyproxy/tinyproxy.pid"
MaxClients 100
Allow 127.0.0.1
Allow $SUBNET_CIDR_BLOCK
ViaProxyName "mini-cloud-egress-proxy"
EOF
  systemctl enable --now tinyproxy
  systemctl restart tinyproxy
}

ensure_caddy() {
  install -d -m 0755 /etc/mini-cloud/ingress "$INSTALL_ROOT/artifacts"
  cat >/etc/mini-cloud/ingress/Caddyfile <<EOF
{
  auto_https off
  admin localhost:2019
}

:$INGRESS_HTTP_PORT {
  respond "mini-cloud ingress is waiting for cloud-plane routes" 404
}

:$ARTIFACT_HTTP_PORT {
  root * $INSTALL_ROOT/artifacts
  file_server
}
EOF
  if docker ps -a --format '{{.Names}}' | grep -qx mini-cloud-caddy; then
    docker rm -f mini-cloud-caddy >/dev/null
  fi
  docker_pull_with_retry "$CADDY_IMAGE"
  docker run -d --name mini-cloud-caddy --restart unless-stopped --network host -v /etc/mini-cloud/ingress:/etc/caddy:ro -v "$INSTALL_ROOT:$INSTALL_ROOT:ro" "$CADDY_IMAGE"
}

ensure_postgres() {
  if docker ps -a --format '{{.Names}}' | grep -qx mini-cloud-postgres; then
    docker start mini-cloud-postgres || true
  else
    docker volume create mini-cloud-postgres-data >/dev/null
    docker_pull_with_retry "$POSTGRES_IMAGE"
    docker run -d \
      --name mini-cloud-postgres \
      --restart unless-stopped \
      -p 127.0.0.1:5432:5432 \
      -e POSTGRES_USER=mini_cloud \
      -e POSTGRES_PASSWORD=mini_cloud \
      -e POSTGRES_DB=postgres \
      -v mini-cloud-postgres-data:/var/lib/postgresql/data \
      "$POSTGRES_IMAGE"
  fi
}

wait_for_postgres() {
  for _ in $(seq 1 60); do
    if docker exec mini-cloud-postgres pg_isready -h 127.0.0.1 -p 5432 -U mini_cloud -d postgres >/dev/null 2>&1; then
      return
    fi
    sleep 2
  done
  docker logs mini-cloud-postgres || true
  exit 1
}

ensure_databases() {
  docker exec -i mini-cloud-postgres psql -U mini_cloud -d postgres <<'SQL'
SELECT 'CREATE DATABASE mini_cloud_control_plane'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'mini_cloud_control_plane')\gexec
SELECT 'CREATE DATABASE mini_cloud_cloud_plane'
WHERE NOT EXISTS (SELECT FROM pg_database WHERE datname = 'mini_cloud_cloud_plane')\gexec
SQL
}

install_binaries() {
  install -d -m 0755 "$INSTALL_ROOT/bin"
  install -d -m 0755 "$INSTALL_ROOT/artifacts"
  install -m 0755 /tmp/mini-cloud-control-plane "$INSTALL_ROOT/bin/control-plane"
  install -m 0755 /tmp/mini-cloud-cloud-plane "$INSTALL_ROOT/bin/cloud-plane"
  install -m 0755 /tmp/mini-cloud-node-agent "$INSTALL_ROOT/artifacts/node-agent-linux-amd64"
}

write_provider_env() {
  if [[ "$PROVIDER" != "tencent" ]]; then
    rm -f "$CLOUD_PLANE_CONFIG_DIR/provider.env"
    return
  fi
  install -d -m 0755 "$CLOUD_PLANE_CONFIG_DIR"
  cat >"$CLOUD_PLANE_CONFIG_DIR/provider.env" <<EOF
TENCENTCLOUD_SECRET_ID=$TENCENTCLOUD_SECRET_ID
TENCENTCLOUD_SECRET_KEY=$TENCENTCLOUD_SECRET_KEY
TENCENTCLOUD_TOKEN=$TENCENTCLOUD_TOKEN
EOF
  chmod 0600 "$CLOUD_PLANE_CONFIG_DIR/provider.env"
}

write_control_plane_config() {
  install -d -m 0755 "$CONTROL_PLANE_CONFIG_DIR"
  python3 >"$CONTROL_PLANE_CONFIG_DIR/control-plane.yaml" <<'PY'
import json
import os

lines = [
    "server:",
    f"  httpAddr: {json.dumps(os.environ['CONTROL_PLANE_HTTP_ADDR'])}",
    "",
    "ui:",
    f"  dir: {json.dumps(os.environ['INSTALL_ROOT'] + '/web')}",
    "",
    "database:",
    "  url: postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_control_plane?sslmode=disable",
    "",
    "auth:",
    f"  adminToken: {json.dumps(os.environ['CONTROL_PLANE_ADMIN_TOKEN'])}",
    "",
    "sync:",
    "  planeIntervalSeconds: 30",
    "",
    "service:",
    "  reconcileTimeoutSeconds: 1200",
    "",
    "logs:",
    "  loki:",
    f"    url: {json.dumps(os.environ['WORKLOAD_LOG_LOKI_URL'])}",
    f"    tenantID: {json.dumps(os.environ['WORKLOAD_LOG_LOKI_TENANT_ID'])}",
    "    queryTimeoutSeconds: 5",
]
print("\n".join(lines))
PY
  chmod 0600 "$CONTROL_PLANE_CONFIG_DIR/control-plane.yaml"
}

write_cloud_plane_config() {
  install -d -m 0755 "$CLOUD_PLANE_CONFIG_DIR"
  python3 - "$RUNTIME_PROVIDER_SPEC_JSON" >"$CLOUD_PLANE_CONFIG_DIR/cloud-plane.yaml" <<'PY'
import json
import os
import sys

provider_spec = json.loads(sys.argv[1])
provider_spec.pop("provider", None)

lines = [
    "server:",
    f"  listenGRPCAddr: 0.0.0.0:{os.environ['CLOUD_PLANE_GRPC_PORT']}",
    "",
    "database:",
    "  url: postgres://mini_cloud:mini_cloud@127.0.0.1:5432/mini_cloud_cloud_plane?sslmode=disable",
    "",
    "plane:",
    f"  name: {json.dumps(os.environ['PLATFORM_NAME'])}",
    "",
    "controlPlane:",
    f"  bearerToken: {json.dumps(os.environ['CONTROL_PLANE_SOUTHBOUND_TOKEN'])}",
    "",
    "nodeAgent:",
    f"  connectEndpoint: {json.dumps(os.environ['PLATFORM_PRIVATE_IP'] + ':' + os.environ['CLOUD_PLANE_GRPC_PORT'])}",
    f"  bootstrapToken: {json.dumps(os.environ['NODE_AGENT_BOOTSTRAP_TOKEN'])}",
    f"  binaryUrl: {json.dumps(os.environ['NODE_AGENT_BINARY_URL'])}",
    "",
    "infrastructure:",
    f"  provider: {json.dumps(os.environ['PROVIDER'])}",
    f"  regionId: {json.dumps(os.environ['REGION_ID'])}",
    f"  zoneId: {json.dumps(os.environ['ZONE_ID'])}",
    "",
    "runtimeProvisioning:",
    f"  instanceType: {json.dumps(os.environ['RUNTIME_INSTANCE_TYPE'])}",
    "  registryMirrors:",
    f"    - {json.dumps(os.environ['REGISTRY_MIRROR'])}",
    f"  workloadEgressProxyEndpoint: {json.dumps('http://' + os.environ['PLATFORM_PRIVATE_IP'] + ':' + os.environ['EGRESS_PROXY_PORT'])}",
    "  providerSpec:",
]

for key, value in provider_spec.items():
    if key in ("instanceType",):
        continue
    if isinstance(value, list):
        lines.append(f"    {key}:")
        for item in value:
            lines.append(f"      - {json.dumps(item)}")
    else:
        lines.append(f"    {key}: {json.dumps(value)}")

lines.extend([
    "",
    "ingress:",
    f"  baseDomain: {json.dumps(os.environ['INGRESS_BASE_DOMAIN'])}",
    "  caddyAdminURL: http://127.0.0.1:2019",
    "",
    "observability:",
    f"  lokiURL: {json.dumps(os.environ['WORKLOAD_LOG_LOKI_URL'])}",
    f"  otlpEndpoint: {json.dumps(os.environ['WORKLOAD_OTLP_ENDPOINT'])}",
])
print("\n".join(lines))
PY
  chmod 0600 "$CLOUD_PLANE_CONFIG_DIR/cloud-plane.yaml"
}

write_systemd_units() {
  cat >/etc/systemd/system/mini-cloud-control-plane.service <<EOF
[Unit]
Description=mini-cloud control-plane
After=network-online.target docker.service mini-cloud-postgres.service
Wants=network-online.target docker.service

[Service]
Type=simple
WorkingDirectory=$INSTALL_ROOT
ExecStart=$INSTALL_ROOT/bin/control-plane --config $CONTROL_PLANE_CONFIG_DIR/control-plane.yaml
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

  cat >/etc/systemd/system/mini-cloud-cloud-plane.service <<EOF
[Unit]
Description=mini-cloud cloud-plane
After=network-online.target docker.service mini-cloud-control-plane.service
Wants=network-online.target docker.service

[Service]
Type=simple
WorkingDirectory=$INSTALL_ROOT
EnvironmentFile=-$CLOUD_PLANE_CONFIG_DIR/provider.env
ExecStart=$INSTALL_ROOT/bin/cloud-plane --config $CLOUD_PLANE_CONFIG_DIR/cloud-plane.yaml
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF
}

wait_for_tcp() {
  local host="$1"
  local port="$2"
  local service="$3"
  for _ in $(seq 1 60); do
    if timeout 1 bash -c "</dev/tcp/$host/$port" >/dev/null 2>&1; then
      return
    fi
    sleep 2
  done
  journalctl -u "$service" --no-pager -n 120 || true
  exit 1
}

install -d -m 0755 "$INSTALL_ROOT"
touch "$BOOTSTRAP_LOG"
install_packages
configure_docker
systemctl enable --now docker
systemctl restart docker
ensure_tinyproxy
ensure_caddy
ensure_postgres
wait_for_postgres
ensure_databases
install_binaries
write_provider_env
write_control_plane_config
write_cloud_plane_config
write_systemd_units
systemctl daemon-reload
systemctl enable mini-cloud-control-plane.service
systemctl restart mini-cloud-control-plane.service
wait_for_tcp 127.0.0.1 "${CONTROL_PLANE_HTTP_ADDR##*:}" mini-cloud-control-plane.service
systemctl enable mini-cloud-cloud-plane.service
systemctl restart mini-cloud-cloud-plane.service
wait_for_tcp 127.0.0.1 "$CLOUD_PLANE_GRPC_PORT" mini-cloud-cloud-plane.service
log "mini-cloud platform install completed"
REMOTE

scp "${SSH_ARGS_ARRAY[@]}" "$REMOTE_ENV" "$PLATFORM_HOST:/tmp/mini-cloud-lab-install.env"
scp "${SSH_ARGS_ARRAY[@]}" "$CONTROL_PLANE_BINARY_PATH" "$PLATFORM_HOST:/tmp/mini-cloud-control-plane"
scp "${SSH_ARGS_ARRAY[@]}" "$CLOUD_PLANE_BINARY_PATH" "$PLATFORM_HOST:/tmp/mini-cloud-cloud-plane"
scp "${SSH_ARGS_ARRAY[@]}" "$NODE_AGENT_BINARY_PATH" "$PLATFORM_HOST:/tmp/mini-cloud-node-agent"

if [[ "$PLATFORM_HOST" == root@* || "$PLATFORM_HOST" == "root@"* ]]; then
  ssh "${SSH_ARGS_ARRAY[@]}" "$PLATFORM_HOST" 'bash -s' <"$REMOTE_SCRIPT"
else
  ssh "${SSH_ARGS_ARRAY[@]}" "$PLATFORM_HOST" 'sudo -n bash -s' <"$REMOTE_SCRIPT"
fi
