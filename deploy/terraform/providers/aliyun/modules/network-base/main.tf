locals {
  vpc_name                     = var.vpc_name != "" ? var.vpc_name : "${var.platform_name}-vpc"
  vswitch_name                 = var.vswitch_name != "" ? var.vswitch_name : "${var.platform_name}-vsw"
  platform_security_group_name = var.security_group_name_prefix != "" ? "${var.security_group_name_prefix}-platform-sg" : "${var.platform_name}-platform-sg"
  runtime_security_group_name  = var.security_group_name_prefix != "" ? "${var.security_group_name_prefix}-runtime-sg" : "${var.platform_name}-runtime-sg"
  platform_role_name           = var.platform_role_name != "" ? var.platform_role_name : "${var.platform_name}-role"

  role_document = jsonencode({
    Version = "1"
    Statement = [
      {
        Action = "sts:AssumeRole"
        Effect = "Allow"
        Principal = {
          Service = [
            "ecs.aliyuncs.com"
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
      description = "ssh from admin cidrs"
      ip_protocol = "tcp"
      port_range  = "22/22"
      cidr_ip     = cidr
      priority    = 1
      policy      = "accept"
      nic_type    = "intranet"
    }
  ]

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

  platform_external_ingress_rules = concat(
    local.platform_admin_ingress_rules,
    local.platform_control_plane_ingress_rules,
    local.platform_http_ingress_rules,
  )

  platform_external_ingress_rule_map = {
    for index, rule in local.platform_external_ingress_rules :
    format("%03d", index) => rule
  }
}

check "runtime_host_port_range" {
  assert {
    condition     = var.runtime_host_port_min <= var.runtime_host_port_max
    error_message = "runtime_host_port_min 不能大于 runtime_host_port_max。"
  }
}

resource "alicloud_vpc" "platform" {
  vpc_name   = local.vpc_name
  cidr_block = var.vpc_cidr_block
  tags       = local.common_tags
}

resource "alicloud_vswitch" "platform" {
  vpc_id       = alicloud_vpc.platform.id
  cidr_block   = var.vswitch_cidr_block
  zone_id      = var.zone_id
  vswitch_name = local.vswitch_name
  tags         = local.common_tags
}

resource "alicloud_security_group" "platform" {
  security_group_name = local.platform_security_group_name
  vpc_id              = alicloud_vpc.platform.id
  tags                = merge(local.common_tags, { "mini-cloud/security-group-role" = "platform" })
}

resource "alicloud_security_group" "runtime" {
  security_group_name = local.runtime_security_group_name
  vpc_id              = alicloud_vpc.platform.id
  tags                = merge(local.common_tags, { "mini-cloud/security-group-role" = "runtime" })
}

resource "alicloud_security_group_rule" "platform_external_ingress" {
  for_each = local.platform_external_ingress_rule_map

  security_group_id = alicloud_security_group.platform.id
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
  security_group_id        = alicloud_security_group.platform.id
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
  security_group_id        = alicloud_security_group.platform.id
  type                     = "ingress"
  ip_protocol              = "tcp"
  port_range               = "${var.egress_proxy_port}/${var.egress_proxy_port}"
  source_security_group_id = alicloud_security_group.runtime.id
  priority                 = 1
  policy                   = "accept"
  nic_type                 = "intranet"
  description              = "egress proxy from runtime security group"
}

resource "alicloud_security_group_rule" "runtime_host_ports_from_platform" {
  security_group_id        = alicloud_security_group.runtime.id
  type                     = "ingress"
  ip_protocol              = "tcp"
  port_range               = "${var.runtime_host_port_min}/${var.runtime_host_port_max}"
  source_security_group_id = alicloud_security_group.platform.id
  priority                 = 1
  policy                   = "accept"
  nic_type                 = "intranet"
  description              = "runtime host ports from platform security group"
}

resource "alicloud_security_group_rule" "platform_egress_all" {
  security_group_id = alicloud_security_group.platform.id
  type              = "egress"
  ip_protocol       = "all"
  port_range        = "-1/-1"
  cidr_ip           = "0.0.0.0/0"
  priority          = 1
  policy            = "accept"
  nic_type          = "intranet"
  description       = "platform host can reach internet and cloud APIs"
}

resource "alicloud_security_group_rule" "runtime_egress_grpc" {
  security_group_id = alicloud_security_group.runtime.id
  type              = "egress"
  ip_protocol       = "tcp"
  port_range        = "${var.cloud_plane_grpc_port}/${var.cloud_plane_grpc_port}"
  cidr_ip           = var.vswitch_cidr_block
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
  cidr_ip           = var.vswitch_cidr_block
  priority          = 1
  policy            = "accept"
  nic_type          = "intranet"
  description       = "runtime to egress proxy"
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

resource "alicloud_ram_role" "platform" {
  role_name                   = local.platform_role_name
  assume_role_policy_document = local.role_document
  description                 = "${var.platform_name} platform role"
  tags                        = local.common_tags
}

resource "alicloud_ram_role_policy_attachment" "platform" {
  for_each = toset(var.platform_role_policy_names)

  policy_name = each.value
  policy_type = "System"
  role_name   = alicloud_ram_role.platform.role_name
}
