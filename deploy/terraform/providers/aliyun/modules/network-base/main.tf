locals {
  node_security_group_name = var.security_group_name_prefix != "" ? "${var.security_group_name_prefix}-node-sg" : "${var.platform_name}-node-sg"

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

check "node_host_port_range" {
  assert {
    condition     = var.node_host_port_min <= var.node_host_port_max
    error_message = "node_host_port_min must not be greater than node_host_port_max."
  }
}

resource "alicloud_security_group" "node" {
  security_group_name = local.node_security_group_name
  vpc_id              = var.vpc_id
  tags                = merge(local.common_tags, { "mini-cloud/security-group-role" = "node" })
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

resource "alicloud_security_group_rule" "platform_grpc_from_node" {
  security_group_id        = var.platform_security_group_id
  type                     = "ingress"
  ip_protocol              = "tcp"
  port_range               = "${var.cloud_plane_grpc_port}/${var.cloud_plane_grpc_port}"
  source_security_group_id = alicloud_security_group.node.id
  priority                 = 1
  policy                   = "accept"
  nic_type                 = "intranet"
  description              = "cloud-plane grpc from node security group"
}

resource "alicloud_security_group_rule" "platform_workload_proxy_from_node" {
  security_group_id        = var.platform_security_group_id
  type                     = "ingress"
  ip_protocol              = "tcp"
  port_range               = "${var.workload_proxy_port}/${var.workload_proxy_port}"
  source_security_group_id = alicloud_security_group.node.id
  priority                 = 1
  policy                   = "accept"
  nic_type                 = "intranet"
  description              = "workload proxy from node security group"
}

resource "alicloud_security_group_rule" "platform_artifacts_from_node" {
  security_group_id        = var.platform_security_group_id
  type                     = "ingress"
  ip_protocol              = "tcp"
  port_range               = "${var.artifact_http_port}/${var.artifact_http_port}"
  source_security_group_id = alicloud_security_group.node.id
  priority                 = 1
  policy                   = "accept"
  nic_type                 = "intranet"
  description              = "node-agent artifact server from node security group"
}

resource "alicloud_security_group_rule" "node_host_ports_from_platform" {
  security_group_id = alicloud_security_group.node.id
  type              = "ingress"
  ip_protocol       = "tcp"
  port_range        = "${var.node_host_port_min}/${var.node_host_port_max}"
  cidr_ip           = "${var.platform_private_ip}/32"
  priority          = 1
  policy            = "accept"
  nic_type          = "intranet"
  description       = "node host ports from platform private ip"
}

resource "alicloud_ecs_key_pair" "platform" {
  key_name_prefix = "${var.platform_name}-platform-"
  public_key      = var.ssh_public_key
  tags            = local.common_tags
}
