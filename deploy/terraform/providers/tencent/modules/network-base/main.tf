locals {
  vpc_name                     = var.vpc_name != "" ? var.vpc_name : "${var.platform_name}-vpc"
  subnet_name                  = var.subnet_name != "" ? var.subnet_name : "${var.platform_name}-subnet"
  platform_security_group_name = var.security_group_name_prefix != "" ? "${var.security_group_name_prefix}-platform-sg" : "${var.platform_name}-platform-sg"
  runtime_security_group_name  = var.security_group_name_prefix != "" ? "${var.security_group_name_prefix}-runtime-sg" : "${var.platform_name}-runtime-sg"
  naming_prefix_raw            = replace(replace(replace(lower(var.platform_name), "_", "-"), ".", "-"), "/", "-")
  naming_prefix                = trim(substr(local.naming_prefix_raw, 0, min(32, length(local.naming_prefix_raw))), "-")
  effective_name_prefix        = local.naming_prefix != "" ? local.naming_prefix : "mini-cloud"
  key_name_core_raw            = trim(replace(replace(replace(lower(var.platform_name), "-", "_"), ".", "_"), "/", "_"), "_")
  key_name_core                = local.key_name_core_raw != "" ? substr(local.key_name_core_raw, 0, min(14, length(local.key_name_core_raw))) : "platform"
  platform_key_name            = "mc_${local.key_name_core}_${random_id.platform_suffix.hex}"
  platform_role_name           = var.platform_role_name != "" ? var.platform_role_name : "${local.effective_name_prefix}-platform-role-${random_id.platform_suffix.hex}"

  role_document = jsonencode({
    version = "2.0"
    statement = [
      {
        action = "name/sts:AssumeRole"
        effect = "allow"
        principal = {
          service = [
            "cvm.qcloud.com"
          ]
        }
      }
    ]
  })

  common_tags = {
    "managed-by"             = "mini-cloud"
    "mini-cloud/platform"    = var.platform_name
    "mini-cloud/environment" = var.environment
    "mini-cloud/owner"       = var.owner
  }

  platform_admin_ingress_rules = [
    for cidr in var.admin_cidrs : {
      action      = "ACCEPT"
      cidr_block  = cidr
      protocol    = "TCP"
      port        = "22"
      description = "ssh from admin cidrs"
    }
  ]

  platform_control_plane_ingress_rules = [
    for cidr in var.control_plane_cidrs : {
      action      = "ACCEPT"
      cidr_block  = cidr
      protocol    = "TCP"
      port        = tostring(var.cloud_plane_grpc_port)
      description = "cloud-plane grpc from control-plane cidrs"
    }
  ]

  platform_http_ingress_rules = [
    for cidr in var.ingress_cidrs : {
      action      = "ACCEPT"
      cidr_block  = cidr
      protocol    = "TCP"
      port        = tostring(var.ingress_http_port)
      description = "caddy ingress http from user or CDN cidrs"
    }
  ]

  platform_runtime_ingress_rules = [
    {
      action             = "ACCEPT"
      source_security_id = tencentcloud_security_group.runtime.id
      protocol           = "TCP"
      port               = tostring(var.cloud_plane_grpc_port)
      description        = "cloud-plane grpc from runtime security group"
    },
    {
      action             = "ACCEPT"
      source_security_id = tencentcloud_security_group.runtime.id
      protocol           = "TCP"
      port               = tostring(var.egress_proxy_port)
      description        = "egress proxy from runtime security group"
    }
  ]

  runtime_ingress_rules = [
    {
      action             = "ACCEPT"
      source_security_id = tencentcloud_security_group.platform.id
      protocol           = "TCP"
      port               = "${var.runtime_host_port_min}-${var.runtime_host_port_max}"
      description        = "runtime host ports from platform security group"
    }
  ]

  runtime_egress_rules = [
    {
      action             = "ACCEPT"
      source_security_id = tencentcloud_security_group.platform.id
      protocol           = "TCP"
      port               = tostring(var.cloud_plane_grpc_port)
      policy_index       = 10
      description        = "runtime to cloud-plane grpc"
    },
    {
      action             = "ACCEPT"
      source_security_id = tencentcloud_security_group.platform.id
      protocol           = "TCP"
      port               = tostring(var.egress_proxy_port)
      policy_index       = 11
      description        = "runtime to egress proxy"
    },
    {
      action       = "ACCEPT"
      cidr_block   = "169.254.0.0/16"
      protocol     = "TCP"
      port         = "80"
      policy_index = 12
      description  = "runtime to cloud metadata"
    },
    {
      action       = "DROP"
      cidr_block   = "0.0.0.0/0"
      protocol     = "ALL"
      port         = "ALL"
      policy_index = 100
      description  = "deny runtime direct internet egress outside platform proxy"
    }
  ]
}

check "runtime_host_port_range" {
  assert {
    condition     = var.runtime_host_port_min <= var.runtime_host_port_max
    error_message = "runtime_host_port_min 不能大于 runtime_host_port_max。"
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

resource "tencentcloud_security_group" "platform" {
  name        = local.platform_security_group_name
  description = "${var.platform_name} platform security group"
  tags        = merge(local.common_tags, { "mini-cloud/security-group-role" = "platform" })
}

resource "tencentcloud_security_group" "runtime" {
  name        = local.runtime_security_group_name
  description = "${var.platform_name} runtime security group"
  tags        = merge(local.common_tags, { "mini-cloud/security-group-role" = "runtime" })
}

resource "tencentcloud_security_group_rule_set" "platform" {
  security_group_id = tencentcloud_security_group.platform.id

  dynamic "ingress" {
    for_each = concat(
      local.platform_admin_ingress_rules,
      local.platform_control_plane_ingress_rules,
      local.platform_http_ingress_rules,
      local.platform_runtime_ingress_rules,
    )

    content {
      action             = ingress.value.action
      cidr_block         = try(ingress.value.cidr_block, null)
      source_security_id = try(ingress.value.source_security_id, null)
      protocol           = ingress.value.protocol
      port               = ingress.value.port
      description        = ingress.value.description
    }
  }

  egress {
    action      = "ACCEPT"
    cidr_block  = "0.0.0.0/0"
    protocol    = "ALL"
    port        = "ALL"
    description = "platform host can reach internet and cloud APIs"
  }
}

resource "tencentcloud_security_group_rule_set" "runtime" {
  security_group_id = tencentcloud_security_group.runtime.id

  dynamic "ingress" {
    for_each = local.runtime_ingress_rules

    content {
      action             = ingress.value.action
      source_security_id = ingress.value.source_security_id
      protocol           = ingress.value.protocol
      port               = ingress.value.port
      description        = ingress.value.description
    }
  }

  dynamic "egress" {
    for_each = local.runtime_egress_rules

    content {
      action             = egress.value.action
      cidr_block         = try(egress.value.cidr_block, null)
      source_security_id = try(egress.value.source_security_id, null)
      protocol           = egress.value.protocol
      port               = egress.value.port
      policy_index       = try(egress.value.policy_index, null)
      description        = egress.value.description
    }
  }
}

resource "tencentcloud_key_pair" "platform" {
  key_name   = local.platform_key_name
  public_key = var.ssh_public_key
}

resource "tencentcloud_cam_role" "platform" {
  name             = local.platform_role_name
  document         = local.role_document
  description      = "${var.platform_name} platform role"
  console_login    = false
  session_duration = 43200
}

resource "tencentcloud_cam_role_policy_attachment_by_name" "platform" {
  for_each = toset(var.platform_role_policy_names)

  role_name   = tencentcloud_cam_role.platform.name
  policy_name = each.value
}
