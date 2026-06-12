locals {
  selected_provider = lower(trimspace(var.provider_name))
  platform_mode     = lower(trimspace(var.platform_mode))

  use_existing_ecs = (
    local.selected_provider == "aliyun" && local.platform_mode == "existing_ecs"
  )
  use_existing_lighthouse = (
    local.selected_provider == "tencent" && local.platform_mode == "existing_lighthouse"
  )

  effective_ssh_public_key = trimspace(var.ssh_public_key) != "" ? trimspace(var.ssh_public_key) : (
    trimspace(var.ssh_public_key_path) != "" ? trimspace(file(pathexpand(var.ssh_public_key_path))) : ""
  )

  existing_ecs_ssh_host        = trimspace(var.existing_ecs_ssh_host) != "" ? trimspace(var.existing_ecs_ssh_host) : trimspace(var.existing_ecs_public_ip)
  existing_lighthouse_ssh_host = trimspace(var.existing_lighthouse_ssh_host) != "" ? trimspace(var.existing_lighthouse_ssh_host) : trimspace(var.existing_lighthouse_public_ip)
  existing_ccn_id              = trimspace(var.existing_ccn_id)

  platform_private_ip = local.use_existing_ecs ? trimspace(var.existing_ecs_private_ip) : trimspace(var.existing_lighthouse_private_ip)
  platform_public_ip  = local.use_existing_ecs ? trimspace(var.existing_ecs_public_ip) : trimspace(var.existing_lighthouse_public_ip)
  platform_ssh_host   = local.use_existing_ecs ? local.existing_ecs_ssh_host : local.existing_lighthouse_ssh_host

  platform_instance_id = local.use_existing_ecs ? trimspace(var.existing_ecs_instance_id) : trimspace(var.existing_lighthouse_instance_id)
  platform_instance_name = local.use_existing_ecs ? (
    trimspace(var.existing_ecs_instance_name) != "" ? trimspace(var.existing_ecs_instance_name) : var.platform_name
  ) : var.platform_name
  platform_instance_type = local.use_existing_ecs ? trimspace(var.existing_ecs_instance_type) : "LIGHTHOUSE"
  platform_image_id      = local.use_existing_ecs ? trimspace(var.existing_ecs_image_id) : ""
  platform_role_name     = local.use_existing_ecs ? trimspace(var.existing_ecs_role_name) : ""

  vpc_id = local.use_existing_ecs ? trimspace(var.existing_ecs_vpc_id) : module.tencent_network_base[0].vpc_id
  subnet_id = local.use_existing_ecs ? (
    trimspace(var.existing_ecs_vswitch_id)
  ) : module.tencent_network_base[0].subnet_id
  subnet_cidr_block = local.use_existing_ecs ? trimspace(var.existing_ecs_vswitch_cidr_block) : var.subnet_cidr_block
  platform_security_group_id = local.use_existing_ecs ? (
    trimspace(var.existing_ecs_security_group_id)
  ) : module.tencent_network_base[0].platform_security_group_id
  node_security_group_id = local.selected_provider == "aliyun" ? (
    module.aliyun_network_base[0].node_security_group_id
  ) : module.tencent_network_base[0].node_security_group_id
  platform_key_name = local.selected_provider == "aliyun" ? (
    module.aliyun_network_base[0].platform_key_pair_name
  ) : module.tencent_network_base[0].platform_key_name

  cloud_plane_grpc_endpoint = "${local.platform_public_ip}:${var.cloud_plane_grpc_port}"

  node_provider_config_json = local.selected_provider == "aliyun" ? jsonencode({
    provider           = "aliyun"
    instanceType       = var.aliyun.instance_type
    imageId            = var.aliyun.image_id
    keyPairName        = module.aliyun_network_base[0].platform_key_pair_name
    vSwitchId          = module.aliyun_network_base[0].vswitch_id
    securityGroupId    = module.aliyun_network_base[0].node_security_group_id
    systemDiskCategory = var.aliyun.system_disk_category
    systemDiskSizeGiB  = var.aliyun.system_disk_size
    }) : jsonencode({
    provider          = "tencent"
    instanceType      = var.tencent.instance_type
    imageId           = var.tencent.image_id
    keyIds            = [module.tencent_network_base[0].platform_key_id]
    vpcId             = module.tencent_network_base[0].vpc_id
    subnetId          = module.tencent_network_base[0].subnet_id
    securityGroupIds  = [module.tencent_network_base[0].node_security_group_id]
    systemDiskType    = var.tencent.system_disk_type
    systemDiskSizeGiB = var.tencent.system_disk_size
  })
}

check "selected_provider_config" {
  assert {
    condition = (
      (local.selected_provider == "aliyun" && local.platform_mode == "existing_ecs") ||
      (local.selected_provider == "tencent" && local.platform_mode == "existing_lighthouse")
    )
    error_message = "aliyun uses platform_mode=existing_ecs; tencent uses platform_mode=existing_lighthouse."
  }

  assert {
    condition = !local.use_existing_ecs || (
      trimspace(var.existing_ecs_instance_id) != "" &&
      trimspace(var.existing_ecs_public_ip) != "" &&
      trimspace(var.existing_ecs_private_ip) != "" &&
      trimspace(var.existing_ecs_vpc_id) != "" &&
      trimspace(var.existing_ecs_vswitch_id) != "" &&
      trimspace(var.existing_ecs_vswitch_cidr_block) != "" &&
      trimspace(var.existing_ecs_security_group_id) != ""
    )
    error_message = "platform_mode=existing_ecs requires existing ECS instance, IP, VPC, VSwitch, VSwitch CIDR, and security group fields."
  }

  assert {
    condition = !local.use_existing_lighthouse || (
      trimspace(var.existing_lighthouse_public_ip) != "" &&
      trimspace(var.existing_lighthouse_private_ip) != "" &&
      trimspace(var.existing_lighthouse_instance_id) != "" &&
      local.existing_ccn_id != ""
    )
    error_message = "platform_mode=existing_lighthouse requires existing_lighthouse_public_ip, existing_lighthouse_private_ip, existing_lighthouse_instance_id, and existing_ccn_id."
  }

  assert {
    condition     = local.selected_provider != "aliyun" || (trimspace(var.aliyun.region_id) != "" && trimspace(var.aliyun.zone_id) != "" && trimspace(var.aliyun.instance_type) != "" && trimspace(var.aliyun.image_id) != "")
    error_message = "provider_name=aliyun requires aliyun.region_id, aliyun.zone_id, aliyun.instance_type, and aliyun.image_id."
  }

  assert {
    condition     = local.selected_provider != "tencent" || (trimspace(var.tencent.region_id) != "" && trimspace(var.tencent.zone_id) != "" && trimspace(var.tencent.instance_type) != "" && trimspace(var.tencent.image_id) != "")
    error_message = "provider_name=tencent requires tencent.region_id, tencent.zone_id, tencent.instance_type, and tencent.image_id."
  }

  assert {
    condition     = trimspace(local.effective_ssh_public_key) != ""
    error_message = "ssh_public_key or ssh_public_key_path is required."
  }

  assert {
    condition     = var.node_host_port_min <= var.node_host_port_max
    error_message = "node_host_port_min must not be greater than node_host_port_max."
  }
}

module "aliyun_network_base" {
  count  = local.selected_provider == "aliyun" ? 1 : 0
  source = "../providers/aliyun/modules/network-base"

  region_id                  = var.aliyun.region_id
  zone_id                    = var.aliyun.zone_id
  platform_name              = var.platform_name
  environment                = var.environment
  owner                      = var.owner
  vpc_id                     = var.existing_ecs_vpc_id
  vswitch_id                 = var.existing_ecs_vswitch_id
  vswitch_cidr_block         = var.existing_ecs_vswitch_cidr_block
  platform_security_group_id = var.existing_ecs_security_group_id
  platform_private_ip        = var.existing_ecs_private_ip
  security_group_name_prefix = var.security_group_name_prefix
  control_plane_cidrs        = var.control_plane_cidrs
  ingress_cidrs              = var.ingress_cidrs
  cloud_plane_grpc_port      = var.cloud_plane_grpc_port
  ingress_http_port          = var.ingress_http_port
  egress_proxy_port          = var.egress_proxy_port
  artifact_http_port         = var.artifact_http_port
  node_host_port_min         = var.node_host_port_min
  node_host_port_max         = var.node_host_port_max
  ssh_public_key             = local.effective_ssh_public_key
}

module "tencent_network_base" {
  count  = local.selected_provider == "tencent" ? 1 : 0
  source = "../providers/tencent/modules/network-base"

  region_id                      = var.tencent.region_id
  zone_id                        = var.tencent.zone_id
  platform_name                  = var.platform_name
  create_platform_host_resources = false
  environment                    = var.environment
  owner                          = var.owner
  vpc_name                       = var.vpc_name
  vpc_cidr_block                 = var.vpc_cidr_block
  subnet_name                    = var.subnet_name
  subnet_cidr_block              = var.subnet_cidr_block
  security_group_name_prefix     = var.security_group_name_prefix
  admin_cidrs                    = var.admin_cidrs
  control_plane_cidrs            = var.control_plane_cidrs
  ingress_cidrs                  = var.ingress_cidrs
  cloud_plane_grpc_port          = var.cloud_plane_grpc_port
  ingress_http_port              = var.ingress_http_port
  egress_proxy_port              = var.egress_proxy_port
  artifact_http_port             = var.artifact_http_port
  node_host_port_min             = var.node_host_port_min
  node_host_port_max             = var.node_host_port_max
  platform_role_policy_names     = var.tencent.platform_role_policy_names
  platform_private_cidrs         = ["${trimspace(var.existing_lighthouse_private_ip)}/32"]
  ssh_public_key                 = local.effective_ssh_public_key
}

resource "tencentcloud_ccn_attachment_v2" "node_vpc" {
  count = local.use_existing_lighthouse ? 1 : 0

  ccn_id          = local.existing_ccn_id
  instance_id     = module.tencent_network_base[0].vpc_id
  instance_region = var.tencent.region_id
  instance_type   = "VPC"
  description     = "mini-cloud node vpc"
}
