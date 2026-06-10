#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TERRAFORM_DIR="${TERRAFORM_DIR:-$ROOT_DIR/deploy/terraform/lab}"
TERRAFORM_APPLY_ARGS="${TERRAFORM_APPLY_ARGS:--auto-approve}"
LAB_DNS_DOMAIN="${LAB_DNS_DOMAIN:-}"
LAB_DNS_SUBDOMAIN="${LAB_DNS_SUBDOMAIN:-}"
LAB_DNS_VALUE="${LAB_DNS_VALUE:-}"

if ! command -v terraform >/dev/null 2>&1; then
  echo "terraform is required" >&2
  exit 1
fi
if ! command -v python3 >/dev/null 2>&1; then
  echo "python3 is required" >&2
  exit 1
fi

json_path() {
  local json="$1"
  local path="$2"
  JSON_INPUT="$json" JSON_PATH="$path" python3 - <<'PY'
import json
import os

value = json.loads(os.environ["JSON_INPUT"])
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

lighthouse_attached_ccn() {
  local payload="$1"
  JSON_INPUT="$payload" python3 - <<'PY'
import json
import os

data = json.loads(os.environ["JSON_INPUT"])
for item in data.get("CcnAttachedInstanceSet") or []:
    if item.get("State") != "DELETED":
        print(item.get("CcnId", ""))
        break
PY
}

lighthouse_ccn_state() {
  local payload="$1"
  local ccn_id="$2"
  JSON_INPUT="$payload" CCN_ID="$ccn_id" python3 - <<'PY'
import json
import os

data = json.loads(os.environ["JSON_INPUT"])
for item in data.get("CcnAttachedInstanceSet") or []:
    if item.get("CcnId") == os.environ["CCN_ID"]:
        print(item.get("State", ""))
        break
PY
}

ccn_pending_vpc_attachment_id() {
  local payload="$1"
  local ccn_id="$2"
  JSON_INPUT="$payload" CCN_ID="$ccn_id" python3 - <<'PY'
import json
import os

data = json.loads(os.environ["JSON_INPUT"])
for item in data.get("InstanceSet") or []:
    if (
        item.get("CcnId") == os.environ["CCN_ID"]
        and item.get("InstanceType") == "VPC"
        and item.get("State") == "PENDING"
        and item.get("Description") == "Lighthouse VPC"
    ):
        print(item.get("InstanceId", ""))
        break
PY
}

ensure_lighthouse_firewall_rules() {
  local output_json="$1"
  local provider platform_mode region_id instance_id subnet_cidr grpc_port proxy_port artifact_port
  provider="$(json_path "$output_json" "provider.value")"
  platform_mode="$(json_path "$output_json" "platform_mode.value")"

  if [[ "$provider" != "tencent" || "$platform_mode" != "existing_lighthouse" ]]; then
    return
  fi
  region_id="$(json_path "$output_json" "install_env.value.region_id")"
  instance_id="$(json_path "$output_json" "platform.value.instance_id")"
  subnet_cidr="$(json_path "$output_json" "network.value.subnet_cidr_block")"
  grpc_port="$(json_path "$output_json" "network.value.cloud_plane_grpc_port")"
  proxy_port="$(json_path "$output_json" "network.value.egress_proxy_port")"
  artifact_port="$(json_path "$output_json" "network.value.artifact_http_port")"
  if [[ -z "$region_id" || -z "$instance_id" || -z "$subnet_cidr" || -z "$grpc_port" || -z "$proxy_port" || -z "$artifact_port" ]]; then
    echo "terraform outputs are incomplete for Lighthouse firewall rules" >&2
    exit 1
  fi
  if ! command -v tccli >/dev/null 2>&1; then
    echo "tccli is required for existing_lighthouse mode" >&2
    exit 1
  fi

  local rules missing
  rules="$(tccli lighthouse DescribeFirewallRules --region "$region_id" --InstanceId "$instance_id")"
  missing="$(JSON_INPUT="$rules" SUBNET_CIDR="$subnet_cidr" GRPC_PORT="$grpc_port" PROXY_PORT="$proxy_port" ARTIFACT_PORT="$artifact_port" python3 - <<'PY'
import json
import os

data = json.loads(os.environ["JSON_INPUT"])
wanted = {
    ("TCP", os.environ["GRPC_PORT"], os.environ["SUBNET_CIDR"]),
    ("TCP", os.environ["PROXY_PORT"], os.environ["SUBNET_CIDR"]),
    ("TCP", os.environ["ARTIFACT_PORT"], os.environ["SUBNET_CIDR"]),
}
existing = {
    (item.get("Protocol"), item.get("Port"), item.get("CidrBlock"))
    for item in data.get("FirewallRuleSet") or []
    if item.get("Action") == "ACCEPT"
}
missing = [item for item in wanted if item not in existing]
print(json.dumps(missing, separators=(",", ":")))
PY
)"
  if [[ "$missing" == "[]" ]]; then
    return
  fi
  MISSING_RULES="$missing" GRPC_PORT="$grpc_port" PROXY_PORT="$proxy_port" python3 - <<'PY' | while IFS= read -r line; do
import json
import os

for protocol, port, cidr in json.loads(os.environ["MISSING_RULES"]):
    if port == os.environ.get("GRPC_PORT"):
        description = "mini-cloud runtime to cloud-plane"
    elif port == os.environ.get("PROXY_PORT"):
        description = "mini-cloud runtime to workload egress proxy"
    else:
        description = "mini-cloud runtime to node-agent artifact server"
    print(json.dumps({
        "Protocol": protocol,
        "Port": port,
        "CidrBlock": cidr,
        "Action": "ACCEPT",
        "FirewallRuleDescription": description,
    }, separators=(",", ":")))
PY
    tccli lighthouse CreateFirewallRules --region "$region_id" --InstanceId "$instance_id" --FirewallRules "[$line]" >/dev/null
  done
}

ensure_dns_record() {
  if [[ -z "$LAB_DNS_DOMAIN" || -z "$LAB_DNS_SUBDOMAIN" || -z "$LAB_DNS_VALUE" ]]; then
    return
  fi
  if ! command -v tccli >/dev/null 2>&1; then
    echo "tccli is required when LAB_DNS_DOMAIN is set" >&2
    exit 1
  fi

  local records existing_id existing_value
  if ! records="$(tccli dnspod DescribeRecordList --cli-unfold-argument --Domain "$LAB_DNS_DOMAIN" --Subdomain "$LAB_DNS_SUBDOMAIN" 2>/dev/null)"; then
    records='{"RecordList":[]}'
  fi
  existing_id="$(json_path "$records" "RecordList.0.RecordId")"
  existing_value="$(json_path "$records" "RecordList.0.Value")"
  if [[ -n "$existing_id" ]]; then
    if [[ "$existing_value" == "$LAB_DNS_VALUE" || "$existing_value" == "$LAB_DNS_VALUE." ]]; then
      return
    fi
    echo "$LAB_DNS_SUBDOMAIN.$LAB_DNS_DOMAIN already exists with value $existing_value" >&2
    exit 1
  fi

  tccli dnspod CreateRecord --cli-unfold-argument \
    --Domain "$LAB_DNS_DOMAIN" \
    --SubDomain "$LAB_DNS_SUBDOMAIN" \
    --RecordType CNAME \
    --RecordLine 默认 \
    --Value "$LAB_DNS_VALUE" \
    --TTL 600 \
    --Remark mini-cloud-lab >/dev/null
}

attach_lighthouse_ccn() {
  local output_json="$1"
  local provider platform_mode region_id ccn_id
  provider="$(json_path "$output_json" "provider.value")"
  platform_mode="$(json_path "$output_json" "platform_mode.value")"
  ccn_id="$(json_path "$output_json" "ccn.value.id")"
  region_id="$(json_path "$output_json" "install_env.value.region_id")"

  if [[ "$provider" != "tencent" || "$platform_mode" != "existing_lighthouse" ]]; then
    return
  fi
  if [[ -z "$ccn_id" ]]; then
    echo "ccn output is required for existing_lighthouse mode" >&2
    exit 1
  fi
  if ! command -v tccli >/dev/null 2>&1; then
    echo "tccli is required for existing_lighthouse mode" >&2
    exit 1
  fi

  local current_ccn
  current_ccn="$(lighthouse_attached_ccn "$(tccli lighthouse DescribeCcnAttachedInstances --region "$region_id")")"
  if [[ -n "$current_ccn" && "$current_ccn" != "$ccn_id" ]]; then
    echo "lighthouse is already attached to $current_ccn; detach it before attaching $ccn_id" >&2
    exit 1
  fi
  if [[ "$current_ccn" != "$ccn_id" ]]; then
    tccli lighthouse AttachCcn --region "$region_id" --CcnId "$ccn_id" >/dev/null
  fi

  local state pending_vpc_id
  for _ in $(seq 1 12); do
    state="$(lighthouse_ccn_state "$(tccli lighthouse DescribeCcnAttachedInstances --region "$region_id")" "$ccn_id")"
    if [[ "$state" == "ACTIVE" ]]; then
      return
    fi
    if [[ "$state" == "PENDING" ]]; then
      pending_vpc_id="$(ccn_pending_vpc_attachment_id "$(tccli vpc DescribeCcnAttachedInstances --region "$region_id" --CcnId "$ccn_id")" "$ccn_id")"
      if [[ -n "$pending_vpc_id" ]]; then
        tccli vpc AcceptAttachCcnInstances --region "$region_id" --cli-unfold-argument \
          --CcnId "$ccn_id" \
          --Instances.0.InstanceType VPC \
          --Instances.0.InstanceId "$pending_vpc_id" \
          --Instances.0.InstanceRegion "$region_id" >/dev/null
      fi
    fi
    sleep 5
  done

  if [[ "$state" == "PENDING" ]]; then
    echo "lighthouse ccn attachment is still PENDING after automatic accept attempt" >&2
    exit 1
  fi
  echo "lighthouse ccn attachment did not become ACTIVE; current state: ${state:-unknown}" >&2
  exit 1
}

terraform -chdir="$TERRAFORM_DIR" init
terraform_apply_args=()
if [[ -n "$TERRAFORM_APPLY_ARGS" ]]; then
  # shellcheck disable=SC2206
  terraform_apply_args=($TERRAFORM_APPLY_ARGS)
fi
terraform -chdir="$TERRAFORM_DIR" apply "${terraform_apply_args[@]}"

OUTPUT_JSON="$(terraform -chdir="$TERRAFORM_DIR" output -json)"
attach_lighthouse_ccn "$OUTPUT_JSON"
ensure_lighthouse_firewall_rules "$OUTPUT_JSON"
ensure_dns_record

echo
echo "platform output:"
terraform -chdir="$TERRAFORM_DIR" output platform
