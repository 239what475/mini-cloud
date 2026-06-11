locals {
  runtime_security_group_name = var.security_group_name_prefix != "" ? "${var.security_group_name_prefix}-runtime-sg" : "${var.platform_name}-runtime-sg"

  common_tags = {
    "managed-by"             = "mini-cloud"
    "mini-cloud/platform"    = var.platform_name
    "mini-cloud/environment" = var.environment
    "mini-cloud/owner"       = var.owner
  }

  platform_control_plane_ingress_rules = [
    for cidr in var.control_plane_cidrs : {
      description = "cloud-plane grpc from control-plane cidrs"
      ip_protocol = "tcp"
      port_range  = "${var.cloud_plane_grpc_port}/${var.cloud_plane_grpc_port}"
      cidr_ip     = cidr
      priority    = 1
      policy      = "accept"
      nic_type    = "intranet"
    }
  ]

  platform_http_ingress_rules = [
    for cidr in var.ingress_cidrs : {
      description = "caddy ingress http from user or CDN cidrs"
      ip_protocol = "tcp"
      port_range  = "${var.ingress_http_port}/${var.ingress_http_port}"
      cidr_ip     = cidr
      priority    = 1
      policy      = "accept"
      nic_type    = "intranet"
    }
  ]

  platform_external_ingress_rule_map = {
    for index, rule in concat(local.platform_control_plane_ingress_rules, local.platform_http_ingress_rules) :
    format("%03d", index) => rule
  }
}

check "runtime_host_port_range" {
  assert {
    condition     = var.runtime_host_port_min <= var.runtime_host_port_max
    error_message = "runtime_host_port_min must not be greater than runtime_host_port_max."
  }
}

resource "alicloud_security_group" "runtime" {
  security_group_name = local.runtime_security_group_name
  vpc_id              = var.vpc_id
  tags                = merge(local.common_tags, { "mini-cloud/security-group-role" = "runtime" })
}

resource "alicloud_security_group_rule" "platform_external_ingress" {
  for_each = local.platform_external_ingress_rule_map

  security_group_id = var.platform_security_group_id
  type              = "ingress"
  ip_protocol       = each.value.ip_protocol
  port_range        = each.value.port_range
  cidr_ip           = each.value.cidr_ip
  priority          = each.value.priority
  policy            = each.value.policy
  nic_type          = each.value.nic_type
  description       = each.value.description
}

resource "alicloud_security_group_rule" "platform_grpc_from_runtime" {
  security_group_id        = var.platform_security_group_id
  type                     = "ingress"
  ip_protocol              = "tcp"
  port_range               = "${var.cloud_plane_grpc_port}/${var.cloud_plane_grpc_port}"
  source_security_group_id = alicloud_security_group.runtime.id
  priority                 = 1
  policy                   = "accept"
  nic_type                 = "intranet"
  description              = "cloud-plane grpc from runtime security group"
}

resource "alicloud_security_group_rule" "platform_proxy_from_runtime" {
  security_group_id        = var.platform_security_group_id
  type                     = "ingress"
  ip_protocol              = "tcp"
  port_range               = "${var.egress_proxy_port}/${var.egress_proxy_port}"
  source_security_group_id = alicloud_security_group.runtime.id
  priority                 = 1
  policy                   = "accept"
  nic_type                 = "intranet"
  description              = "egress proxy from runtime security group"
}

resource "alicloud_security_group_rule" "platform_artifacts_from_runtime" {
  security_group_id        = var.platform_security_group_id
  type                     = "ingress"
  ip_protocol              = "tcp"
  port_range               = "${var.artifact_http_port}/${var.artifact_http_port}"
  source_security_group_id = alicloud_security_group.runtime.id
  priority                 = 1
  policy                   = "accept"
  nic_type                 = "intranet"
  description              = "node-agent artifact server from runtime security group"
}

resource "alicloud_security_group_rule" "runtime_host_ports_from_platform" {
  security_group_id = alicloud_security_group.runtime.id
  type              = "ingress"
  ip_protocol       = "tcp"
  port_range        = "${var.runtime_host_port_min}/${var.runtime_host_port_max}"
  cidr_ip           = "${var.platform_private_ip}/32"
  priority          = 1
  policy            = "accept"
  nic_type          = "intranet"
  description       = "runtime host ports from platform private ip"
}

resource "alicloud_security_group_rule" "runtime_egress_grpc" {
  security_group_id = alicloud_security_group.runtime.id
  type              = "egress"
  ip_protocol       = "tcp"
  port_range        = "${var.cloud_plane_grpc_port}/${var.cloud_plane_grpc_port}"
  cidr_ip           = "${var.platform_private_ip}/32"
  priority          = 1
  policy            = "accept"
  nic_type          = "intranet"
  description       = "runtime to cloud-plane grpc"
}

resource "alicloud_security_group_rule" "runtime_egress_proxy" {
  security_group_id = alicloud_security_group.runtime.id
  type              = "egress"
  ip_protocol       = "tcp"
  port_range        = "${var.egress_proxy_port}/${var.egress_proxy_port}"
  cidr_ip           = "${var.platform_private_ip}/32"
  priority          = 1
  policy            = "accept"
  nic_type          = "intranet"
  description       = "runtime to egress proxy"
}

resource "alicloud_security_group_rule" "runtime_egress_artifacts" {
  security_group_id = alicloud_security_group.runtime.id
  type              = "egress"
  ip_protocol       = "tcp"
  port_range        = "${var.artifact_http_port}/${var.artifact_http_port}"
  cidr_ip           = "${var.platform_private_ip}/32"
  priority          = 1
  policy            = "accept"
  nic_type          = "intranet"
  description       = "runtime to node-agent artifact server"
}

resource "alicloud_security_group_rule" "runtime_egress_metadata_aliyun" {
  security_group_id = alicloud_security_group.runtime.id
  type              = "egress"
  ip_protocol       = "tcp"
  port_range        = "80/80"
  cidr_ip           = "100.100.100.200/32"
  priority          = 1
  policy            = "accept"
  nic_type          = "intranet"
  description       = "runtime to aliyun metadata"
}

resource "alicloud_security_group_rule" "runtime_egress_aliyun_internal_http" {
  security_group_id = alicloud_security_group.runtime.id
  type              = "egress"
  ip_protocol       = "tcp"
  port_range        = "80/80"
  cidr_ip           = "100.100.0.0/16"
  priority          = 2
  policy            = "accept"
  nic_type          = "intranet"
  description       = "runtime to aliyun internal package mirrors"
}

resource "alicloud_security_group_rule" "runtime_egress_aliyun_internal_https" {
  security_group_id = alicloud_security_group.runtime.id
  type              = "egress"
  ip_protocol       = "tcp"
  port_range        = "443/443"
  cidr_ip           = "100.100.0.0/16"
  priority          = 2
  policy            = "accept"
  nic_type          = "intranet"
  description       = "runtime to aliyun internal package mirrors"
}

resource "alicloud_security_group_rule" "runtime_egress_aliyun_dns_udp" {
  for_each = toset(["100.100.2.136/32", "100.100.2.138/32"])

  security_group_id = alicloud_security_group.runtime.id
  type              = "egress"
  ip_protocol       = "udp"
  port_range        = "53/53"
  cidr_ip           = each.value
  priority          = 1
  policy            = "accept"
  nic_type          = "intranet"
  description       = "runtime to aliyun vpc dns"
}

resource "alicloud_security_group_rule" "runtime_egress_aliyun_dns_tcp" {
  for_each = toset(["100.100.2.136/32", "100.100.2.138/32"])

  security_group_id = alicloud_security_group.runtime.id
  type              = "egress"
  ip_protocol       = "tcp"
  port_range        = "53/53"
  cidr_ip           = each.value
  priority          = 1
  policy            = "accept"
  nic_type          = "intranet"
  description       = "runtime to aliyun vpc dns"
}

resource "alicloud_security_group_rule" "runtime_egress_metadata_link_local" {
  security_group_id = alicloud_security_group.runtime.id
  type              = "egress"
  ip_protocol       = "tcp"
  port_range        = "80/80"
  cidr_ip           = "169.254.169.254/32"
  priority          = 1
  policy            = "accept"
  nic_type          = "intranet"
  description       = "runtime to link-local metadata"
}

resource "alicloud_security_group_rule" "runtime_egress_internal_https_link_local" {
  security_group_id = alicloud_security_group.runtime.id
  type              = "egress"
  ip_protocol       = "tcp"
  port_range        = "443/443"
  cidr_ip           = "169.254.0.0/16"
  priority          = 1
  policy            = "accept"
  nic_type          = "intranet"
  description       = "runtime to internal mirrors and object storage"
}

resource "alicloud_security_group_rule" "runtime_egress_default_drop" {
  security_group_id = alicloud_security_group.runtime.id
  type              = "egress"
  ip_protocol       = "all"
  port_range        = "-1/-1"
  cidr_ip           = "0.0.0.0/0"
  priority          = 100
  policy            = "drop"
  nic_type          = "intranet"
  description       = "deny runtime direct internet egress outside platform proxy"
}

resource "alicloud_ecs_key_pair" "platform" {
  key_name_prefix = "${var.platform_name}-platform-"
  public_key      = var.ssh_public_key
  tags            = local.common_tags
}
