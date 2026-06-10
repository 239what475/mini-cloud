#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TERRAFORM_DIR="${TERRAFORM_DIR:-$ROOT_DIR/deploy/terraform/lab}"
TERRAFORM_DESTROY_ARGS="${TERRAFORM_DESTROY_ARGS:--auto-approve}"
SSH_USER="${SSH_USER:-root}"
SSH_KEY="${SSH_KEY:-}"
SSH_OPTS="${SSH_OPTS:-}"
PLATFORM_HOST="${PLATFORM_HOST:-}"
LAB_DNS_DOMAIN="${LAB_DNS_DOMAIN:-}"
LAB_DNS_SUBDOMAIN="${LAB_DNS_SUBDOMAIN:-}"
LAB_DNS_VALUE="${LAB_DNS_VALUE:-}"

require_cmd() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "$1 is required" >&2
    exit 1
  fi
}

json_path_file() {
  local file="$1"
  local path="$2"
  JSON_FILE="$file" JSON_PATH="$path" python3 - <<'PY'
import json
import os

with open(os.environ["JSON_FILE"]) as source:
    value = json.load(source)
for part in os.environ["JSON_PATH"].split("."):
    if part:
        if isinstance(value, dict):
            value = value.get(part)
        elif isinstance(value, list) and part.isdigit():
            index = int(part)
            value = value[index] if index < len(value) else None
        else:
            value = None
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

json_runtime_node_ids() {
  local payload="$1"
  local platform_instance_id="$2"
  JSON_INPUT="$payload" PLATFORM_INSTANCE_ID="$platform_instance_id" python3 - <<'PY'
import json
import os

data = json.loads(os.environ["JSON_INPUT"])
platform_instance_id = os.environ["PLATFORM_INSTANCE_ID"]
ids = []
for item in data.get("InstanceSet") or []:
    instance_id = item.get("InstanceId")
    if instance_id and instance_id != platform_instance_id:
        ids.append(instance_id)
print(json.dumps(ids, separators=(",", ":")))
PY
}

json_aliyun_runtime_node_ids() {
  local payload="$1"
  local platform_instance_id="$2"
  JSON_INPUT="$payload" PLATFORM_INSTANCE_ID="$platform_instance_id" python3 - <<'PY'
import json
import os

data = json.loads(os.environ["JSON_INPUT"])
platform_instance_id = os.environ["PLATFORM_INSTANCE_ID"]
ids = []
for item in ((data.get("Instances") or {}).get("Instance") or []):
    instance_id = item.get("InstanceId")
    if instance_id and instance_id != platform_instance_id:
        ids.append(instance_id)
print(json.dumps(ids, separators=(",", ":")))
PY
}

lighthouse_ccn_attached() {
  local payload="$1"
  local ccn_id="$2"
  JSON_INPUT="$payload" CCN_ID="$ccn_id" python3 - <<'PY'
import json
import os
import sys

data = json.loads(os.environ["JSON_INPUT"])
for item in data.get("CcnAttachedInstanceSet") or []:
    if item.get("CcnId") == os.environ["CCN_ID"] and item.get("State") != "DELETED":
        sys.exit(0)
sys.exit(1)
PY
}

delete_aliyun_runtime_nodes() {
  require_cmd aliyun
  local platform_name="$1"
  local region_id="$2"
  local platform_instance_id="$3"
  local ids_json
  ids_json="$(json_aliyun_runtime_node_ids "$(aliyun ecs DescribeInstances \
    --RegionId "$region_id" \
    --Tag.1.Key managed-by \
    --Tag.1.Value mini-cloud \
    --Tag.2.Key mini-cloud/platform \
    --Tag.2.Value "$platform_name")" "$platform_instance_id")"
  if [[ "$ids_json" == "[]" || -z "$ids_json" ]]; then
    return
  fi
  aliyun ecs DeleteInstances --RegionId "$region_id" --InstanceIds "$ids_json" --Force true
}

delete_tencent_runtime_nodes() {
  require_cmd tccli
  local platform_name="$1"
  local region_id="$2"
  local platform_instance_id="$3"
  local ids_json
  ids_json="$(json_runtime_node_ids "$(tccli cvm DescribeInstances \
    --region "$region_id" \
    --Filters '[{"Name":"tag:managed-by","Values":["mini-cloud"]},{"Name":"tag:mini-cloud/platform","Values":["'"$platform_name"'"]}]')" "$platform_instance_id")"
  if [[ "$ids_json" == "[]" || -z "$ids_json" ]]; then
    return
  fi
  tccli cvm TerminateInstances --region "$region_id" --InstanceIds "$ids_json"
  for _ in $(seq 1 60); do
    ids_json="$(json_runtime_node_ids "$(tccli cvm DescribeInstances \
      --region "$region_id" \
      --Filters '[{"Name":"tag:managed-by","Values":["mini-cloud"]},{"Name":"tag:mini-cloud/platform","Values":["'"$platform_name"'"]}]')" "$platform_instance_id")"
    if [[ "$ids_json" == "[]" || -z "$ids_json" ]]; then
      return
    fi
    sleep 5
  done
  echo "timed out waiting for Tencent runtime nodes to terminate: $ids_json" >&2
  exit 1
}

detach_lighthouse_ccn() {
  require_cmd tccli
  local region_id="$1"
  local ccn_id="$2"
  if [[ -z "$ccn_id" || "$ccn_id" == "null" ]]; then
    return
  fi
  if lighthouse_ccn_attached "$(tccli lighthouse DescribeCcnAttachedInstances --region "$region_id")" "$ccn_id"; then
    tccli lighthouse DetachCcn --region "$region_id" --CcnId "$ccn_id" >/dev/null
  fi
  for _ in $(seq 1 24); do
    if ! lighthouse_ccn_attached "$(tccli lighthouse DescribeCcnAttachedInstances --region "$region_id")" "$ccn_id"; then
      return
    fi
    sleep 5
  done
  echo "timed out waiting for Lighthouse to detach from CCN $ccn_id" >&2
  exit 1
}

delete_lighthouse_firewall_rules() {
  require_cmd tccli
  local region_id="$1"
  local instance_id="$2"
  local subnet_cidr="$3"
  local grpc_port="$4"
  local proxy_port="$5"
  local artifact_port="$6"
  if [[ -z "$region_id" || -z "$instance_id" || -z "$subnet_cidr" || -z "$grpc_port" || -z "$proxy_port" || -z "$artifact_port" ]]; then
    return
  fi

  local rules existing
  rules="$(tccli lighthouse DescribeFirewallRules --region "$region_id" --InstanceId "$instance_id")"
  existing="$(JSON_INPUT="$rules" SUBNET_CIDR="$subnet_cidr" GRPC_PORT="$grpc_port" PROXY_PORT="$proxy_port" ARTIFACT_PORT="$artifact_port" python3 - <<'PY'
import json
import os

data = json.loads(os.environ["JSON_INPUT"])
wanted = {
    ("TCP", os.environ["GRPC_PORT"], os.environ["SUBNET_CIDR"]),
    ("TCP", os.environ["PROXY_PORT"], os.environ["SUBNET_CIDR"]),
    ("TCP", os.environ["ARTIFACT_PORT"], os.environ["SUBNET_CIDR"]),
}
existing = []
for item in data.get("FirewallRuleSet") or []:
    key = (item.get("Protocol"), item.get("Port"), item.get("CidrBlock"))
    if key in wanted and item.get("Action") == "ACCEPT":
        existing.append({
            "Protocol": item.get("Protocol"),
            "Port": item.get("Port"),
            "CidrBlock": item.get("CidrBlock"),
            "Action": item.get("Action"),
            "FirewallRuleDescription": item.get("FirewallRuleDescription", ""),
        })
print(json.dumps(existing, separators=(",", ":")))
PY
)"
  if [[ "$existing" == "[]" || -z "$existing" ]]; then
    return
  fi
  tccli lighthouse DeleteFirewallRules --region "$region_id" --InstanceId "$instance_id" --FirewallRules "$existing" >/dev/null
}

delete_lab_dns_record() {
  if [[ -z "$LAB_DNS_DOMAIN" || -z "$LAB_DNS_SUBDOMAIN" ]]; then
    return
  fi
  require_cmd tccli

  local records record_id record_value
  if ! records="$(tccli dnspod DescribeRecordList --cli-unfold-argument --Domain "$LAB_DNS_DOMAIN" --Subdomain "$LAB_DNS_SUBDOMAIN" 2>/dev/null)"; then
    records='{"RecordList":[]}'
  fi
  record_id="$(JSON_INPUT="$records" python3 - <<'PY'
import json
import os

items = json.loads(os.environ["JSON_INPUT"]).get("RecordList") or []
print(items[0].get("RecordId", "") if items else "")
PY
)"
  record_value="$(JSON_INPUT="$records" python3 - <<'PY'
import json
import os

items = json.loads(os.environ["JSON_INPUT"]).get("RecordList") or []
print(items[0].get("Value", "") if items else "")
PY
)"
  if [[ -z "$record_id" ]]; then
    return
  fi
  if [[ -n "$LAB_DNS_VALUE" && "$record_value" != "$LAB_DNS_VALUE" && "$record_value" != "$LAB_DNS_VALUE." ]]; then
    echo "$LAB_DNS_SUBDOMAIN.$LAB_DNS_DOMAIN points to $record_value; refusing to delete it" >&2
    exit 1
  fi
  tccli dnspod DeleteRecord --cli-unfold-argument --Domain "$LAB_DNS_DOMAIN" --RecordId "$record_id" >/dev/null
}

uninstall_existing_lighthouse_platform() {
  local platform_host="$1"
  if [[ -z "$platform_host" ]]; then
    return
  fi
  require_cmd ssh

  mapfile -t SSH_ARGS_ARRAY < <(ssh_args)
  if [[ "$platform_host" != *@* && "$platform_host" != myserver ]]; then
    platform_host="${SSH_USER}@${platform_host}"
  fi
  if [[ "$platform_host" == root@* || "$platform_host" == "root@"* ]]; then
    ssh "${SSH_ARGS_ARRAY[@]}" "$platform_host" 'bash -s' <<'REMOTE'
set -euo pipefail
systemctl disable --now mini-cloud-cloud-plane.service mini-cloud-control-plane.service 2>/dev/null || true
rm -f /etc/systemd/system/mini-cloud-cloud-plane.service /etc/systemd/system/mini-cloud-control-plane.service
systemctl daemon-reload
if command -v docker >/dev/null 2>&1; then
  if docker ps -a --format '{{.Names}}' | grep -qx mini-cloud-caddy; then docker rm -f mini-cloud-caddy >/dev/null; fi
  if docker ps -a --format '{{.Names}}' | grep -qx mini-cloud-postgres; then docker rm -f mini-cloud-postgres >/dev/null; fi
  if docker volume ls --format '{{.Name}}' | grep -qx mini-cloud-postgres-data; then docker volume rm mini-cloud-postgres-data >/dev/null; fi
fi
systemctl disable --now tinyproxy 2>/dev/null || true
rm -rf /etc/mini-cloud /opt/mini-cloud
REMOTE
  else
    ssh "${SSH_ARGS_ARRAY[@]}" "$platform_host" 'sudo -n bash -s' <<'REMOTE'
set -euo pipefail
systemctl disable --now mini-cloud-cloud-plane.service mini-cloud-control-plane.service 2>/dev/null || true
rm -f /etc/systemd/system/mini-cloud-cloud-plane.service /etc/systemd/system/mini-cloud-control-plane.service
systemctl daemon-reload
if command -v docker >/dev/null 2>&1; then
  if docker ps -a --format '{{.Names}}' | grep -qx mini-cloud-caddy; then docker rm -f mini-cloud-caddy >/dev/null; fi
  if docker ps -a --format '{{.Names}}' | grep -qx mini-cloud-postgres; then docker rm -f mini-cloud-postgres >/dev/null; fi
  if docker volume ls --format '{{.Name}}' | grep -qx mini-cloud-postgres-data; then docker volume rm mini-cloud-postgres-data >/dev/null; fi
fi
systemctl disable --now tinyproxy 2>/dev/null || true
rm -rf /etc/mini-cloud /opt/mini-cloud
REMOTE
  fi
}

require_cmd terraform
require_cmd python3

OUTPUT_JSON="$(mktemp)"
trap 'rm -f "$OUTPUT_JSON"' EXIT
PLATFORM_UNINSTALLED=0

if terraform -chdir="$TERRAFORM_DIR" output -json >"$OUTPUT_JSON" 2>/dev/null; then
  PROVIDER="$(json_path_file "$OUTPUT_JSON" "provider.value")"
  PLATFORM_NAME="$(json_path_file "$OUTPUT_JSON" "platform.value.name")"
  PLATFORM_INSTANCE_ID="$(json_path_file "$OUTPUT_JSON" "platform.value.instance_id")"
  PLATFORM_SSH_HOST="$(json_path_file "$OUTPUT_JSON" "platform.value.ssh_host")"
  PLATFORM_PUBLIC_IP="$(json_path_file "$OUTPUT_JSON" "platform.value.public_ip")"
  PLATFORM_MODE="$(json_path_file "$OUTPUT_JSON" "platform_mode.value")"
  REGION_ID="$(json_path_file "$OUTPUT_JSON" "install_env.value.region_id")"
  case "$PROVIDER" in
    aliyun)
      delete_aliyun_runtime_nodes "$PLATFORM_NAME" "$REGION_ID" "$PLATFORM_INSTANCE_ID"
      ;;
    tencent)
      if [[ "$PLATFORM_MODE" == "existing_lighthouse" ]]; then
        uninstall_existing_lighthouse_platform "${PLATFORM_SSH_HOST:-$PLATFORM_PUBLIC_IP}"
        PLATFORM_UNINSTALLED=1
      fi
      delete_tencent_runtime_nodes "$PLATFORM_NAME" "$REGION_ID" "$PLATFORM_INSTANCE_ID"
      if [[ "$PLATFORM_MODE" == "existing_lighthouse" ]]; then
        CCN_ID="$(json_path_file "$OUTPUT_JSON" "ccn.value.id")"
        SUBNET_CIDR_BLOCK="$(json_path_file "$OUTPUT_JSON" "network.value.subnet_cidr_block")"
        CLOUD_PLANE_GRPC_PORT="$(json_path_file "$OUTPUT_JSON" "network.value.cloud_plane_grpc_port")"
        EGRESS_PROXY_PORT="$(json_path_file "$OUTPUT_JSON" "network.value.egress_proxy_port")"
        ARTIFACT_HTTP_PORT="$(json_path_file "$OUTPUT_JSON" "network.value.artifact_http_port")"
        delete_lighthouse_firewall_rules "$REGION_ID" "$PLATFORM_INSTANCE_ID" "$SUBNET_CIDR_BLOCK" "$CLOUD_PLANE_GRPC_PORT" "$EGRESS_PROXY_PORT" "$ARTIFACT_HTTP_PORT"
        detach_lighthouse_ccn "$REGION_ID" "$CCN_ID"
      fi
      ;;
  esac
fi

if [[ "$PLATFORM_UNINSTALLED" == "0" && -n "$PLATFORM_HOST" ]]; then
  uninstall_existing_lighthouse_platform "$PLATFORM_HOST"
fi

delete_lab_dns_record

# shellcheck disable=SC2206
terraform_destroy_args=($TERRAFORM_DESTROY_ARGS)
terraform -chdir="$TERRAFORM_DIR" destroy "${terraform_destroy_args[@]}"
