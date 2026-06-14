locals {
  vpc_name                 = var.vpc_name != "" ? var.vpc_name : "${var.platform_name}-vpc"
  subnet_name              = var.subnet_name != "" ? var.subnet_name : "${var.platform_name}-subnet"
  node_security_group_name = var.security_group_name_prefix != "" ? "${var.security_group_name_prefix}-node-sg" : "${var.platform_name}-node-sg"
  key_name_core_raw        = trim(replace(replace(replace(lower(var.platform_name), "-", "_"), ".", "_"), "/", "_"), "_")
  key_name_core            = local.key_name_core_raw != "" ? substr(local.key_name_core_raw, 0, min(14, length(local.key_name_core_raw))) : "platform"
  platform_key_name        = "mc_${local.key_name_core}_${random_id.platform_suffix.hex}"

  common_tags = {
    "managed-by"             = "mini-cloud"
    "mini-cloud/platform"    = var.platform_name
    "mini-cloud/environment" = var.environment
    "mini-cloud/owner"       = var.owner
  }

  node_ingress_from_platform_cidr_rules = [
    for cidr in var.platform_private_cidrs : {
      action      = "ACCEPT"
      cidr_block  = cidr
      protocol    = "TCP"
      port        = "${var.node_host_port_min}-${var.node_host_port_max}"
      description = "node host ports from platform private cidr"
    }
  ]

  node_egress_to_platform_cidr_rules = flatten([
    for cidr in var.platform_private_cidrs : [
      {
        action       = "ACCEPT"
        cidr_block   = cidr
        protocol     = "TCP"
        port         = tostring(var.cloud_plane_grpc_port)
        policy_index = 20 + index(var.platform_private_cidrs, cidr) * 3
        description  = "node to cloud-plane grpc on platform private cidr"
      },
      {
        action       = "ACCEPT"
        cidr_block   = cidr
        protocol     = "TCP"
        port         = tostring(var.egress_proxy_port)
        policy_index = 21 + index(var.platform_private_cidrs, cidr) * 3
        description  = "node to egress proxy on platform private cidr"
      },
      {
        action       = "ACCEPT"
        cidr_block   = cidr
        protocol     = "TCP"
        port         = tostring(var.artifact_http_port)
        policy_index = 22 + index(var.platform_private_cidrs, cidr) * 3
        description  = "node to node-agent artifact server on platform private cidr"
      }
    ]
  ])

  node_egress_rules = concat(
    local.node_egress_to_platform_cidr_rules,
    [
      {
        action       = "ACCEPT"
        cidr_block   = "183.60.82.98/32"
        protocol     = "UDP"
        port         = "53"
        policy_index = 8
        description  = "node to tencent vpc dns"
      },
      {
        action       = "ACCEPT"
        cidr_block   = "183.60.82.98/32"
        protocol     = "TCP"
        port         = "53"
        policy_index = 9
        description  = "node to tencent vpc dns"
      },
      {
        action       = "ACCEPT"
        cidr_block   = "183.60.83.19/32"
        protocol     = "UDP"
        port         = "53"
        policy_index = 10
        description  = "node to tencent vpc dns"
      },
      {
        action       = "ACCEPT"
        cidr_block   = "183.60.83.19/32"
        protocol     = "TCP"
        port         = "53"
        policy_index = 11
        description  = "node to tencent vpc dns"
      },
      {
        action       = "ACCEPT"
        cidr_block   = "169.254.0.0/16"
        protocol     = "TCP"
        port         = "80"
        policy_index = 12
        description  = "node to cloud metadata and internal mirrors"
      },
      {
        action       = "ACCEPT"
        cidr_block   = "169.254.0.0/16"
        protocol     = "TCP"
        port         = "443"
        policy_index = 13
        description  = "node to internal mirrors and object storage"
      },
      {
        action       = "DROP"
        cidr_block   = "0.0.0.0/0"
        protocol     = "ALL"
        port         = "ALL"
        policy_index = 100
        description  = "deny node direct internet egress outside platform proxy"
      }
  ])
}

check "node_host_port_range" {
  assert {
    condition     = var.node_host_port_min <= var.node_host_port_max
    error_message = "node_host_port_min 不能大于 node_host_port_max。"
  }
}

resource "random_id" "platform_suffix" {
  byte_length = 3
}

resource "tencentcloud_vpc" "platform" {
  name         = local.vpc_name
  cidr_block   = var.vpc_cidr_block
  is_multicast = false
  tags         = local.common_tags
}

resource "tencentcloud_subnet" "platform" {
  name              = local.subnet_name
  availability_zone = var.zone_id
  cidr_block        = var.subnet_cidr_block
  is_multicast      = false
  vpc_id            = tencentcloud_vpc.platform.id
}

resource "tencentcloud_security_group" "node" {
  name        = local.node_security_group_name
  description = "${var.platform_name} node security group"
  tags        = merge(local.common_tags, { "mini-cloud/security-group-role" = "node" })
}

resource "tencentcloud_security_group_rule_set" "node" {
  security_group_id = tencentcloud_security_group.node.id

  dynamic "ingress" {
    for_each = local.node_ingress_from_platform_cidr_rules

    content {
      action             = ingress.value.action
      cidr_block         = try(ingress.value.cidr_block, null)
      source_security_id = try(ingress.value.source_security_id, null)
      protocol           = ingress.value.protocol
      port               = ingress.value.port
      description        = ingress.value.description
    }
  }

  dynamic "egress" {
    for_each = local.node_egress_rules

    content {
      action             = egress.value.action
      cidr_block         = try(egress.value.cidr_block, null)
      source_security_id = try(egress.value.source_security_id, null)
      protocol           = egress.value.protocol
      port               = egress.value.port
      description        = egress.value.description
    }
  }
}

resource "tencentcloud_key_pair" "platform" {
  key_name   = local.platform_key_name
  public_key = var.ssh_public_key
}
