locals {
  selected_provider = lower(trimspace(var.provider_name))
  platform_mode     = lower(trimspace(var.platform_mode))
  use_existing_lighthouse = (
    local.selected_provider == "tencent" && local.platform_mode == "existing_lighthouse"
  )
  effective_ssh_public_key = trimspace(var.ssh_public_key) != "" ? trimspace(var.ssh_public_key) : (
    trimspace(var.ssh_public_key_path) != "" ? trimspace(file(pathexpand(var.ssh_public_key_path))) : ""
  )
  existing_lighthouse_ssh_host = trimspace(var.existing_lighthouse_ssh_host) != "" ? trimspace(var.existing_lighthouse_ssh_host) : trimspace(var.existing_lighthouse_public_ip)
  existing_ccn_id              = trimspace(var.existing_ccn_id)
  platform_private_ip = local.selected_provider == "aliyun" ? module.aliyun_platform_host[0].platform_private_ip : (
    local.use_existing_lighthouse ? trimspace(var.existing_lighthouse_private_ip) : module.tencent_platform_host[0].platform_private_ip
  )
  platform_public_ip = local.selected_provider == "aliyun" ? module.aliyun_platform_host[0].eip_ip_address : (
    local.use_existing_lighthouse ? trimspace(var.existing_lighthouse_public_ip) : module.tencent_platform_host[0].eip_public_ip
  )
  platform_instance_id = local.selected_provider == "aliyun" ? (
    module.aliyun_platform_host[0].platform_instance_id
  ) : (local.use_existing_lighthouse ? trimspace(var.existing_lighthouse_instance_id) : module.tencent_platform_host[0].platform_instance_id)
  platform_instance_name = local.selected_provider == "aliyun" ? (
    module.aliyun_platform_host[0].platform_instance_name
  ) : (local.use_existing_lighthouse ? var.platform_name : module.tencent_platform_host[0].platform_instance_name)
  platform_instance_type = local.selected_provider == "aliyun" ? (
    module.aliyun_platform_host[0].platform_instance_type
  ) : (local.use_existing_lighthouse ? "LIGHTHOUSE" : module.tencent_platform_host[0].platform_instance_type)
  platform_image_id = local.selected_provider == "aliyun" ? (
    module.aliyun_platform_host[0].platform_image_id
  ) : (local.use_existing_lighthouse ? "" : module.tencent_platform_host[0].platform_image_id)
  vpc_id = local.selected_provider == "aliyun" ? module.aliyun_network_base[0].vpc_id : module.tencent_network_base[0].vpc_id
  subnet_id = local.selected_provider == "aliyun" ? (
    module.aliyun_network_base[0].vswitch_id
  ) : module.tencent_network_base[0].subnet_id
  platform_security_group_id = local.selected_provider == "aliyun" ? (
    module.aliyun_network_base[0].platform_security_group_id
  ) : module.tencent_network_base[0].platform_security_group_id
  runtime_security_group_id = local.selected_provider == "aliyun" ? (
    module.aliyun_network_base[0].runtime_security_group_id
  ) : module.tencent_network_base[0].runtime_security_group_id
  platform_key_name = local.selected_provider == "aliyun" ? (
    module.aliyun_network_base[0].platform_key_pair_name
  ) : module.tencent_network_base[0].platform_key_name
  platform_role_name = local.selected_provider == "aliyun" ? (
    module.aliyun_network_base[0].platform_role_name
  ) : module.tencent_network_base[0].platform_role_name
  cloud_plane_grpc_endpoint = "${local.platform_public_ip}:${var.cloud_plane_grpc_port}"
  common_tags = {
    "managed-by"             = "mini-cloud"
    "mini-cloud/platform"    = var.platform_name
    "mini-cloud/environment" = var.environment
    "mini-cloud/owner"       = var.owner
  }
  runtime_provider_spec_json = local.selected_provider == "aliyun" ? jsonencode({
    provider           = "aliyun"
    instanceType       = var.aliyun.instance_type
    imageId            = var.aliyun.image_id
    keyPairName        = module.aliyun_network_base[0].platform_key_pair_name
    vSwitchId          = module.aliyun_network_base[0].vswitch_id
    securityGroupId    = module.aliyun_network_base[0].runtime_security_group_id
    systemDiskCategory = var.aliyun.system_disk_category
    systemDiskSizeGiB  = var.aliyun.system_disk_size
    }) : jsonencode({
    provider          = "tencent"
    instanceType      = var.tencent.instance_type
    imageId           = var.tencent.image_id
    keyIds            = [module.tencent_network_base[0].platform_key_id]
    vpcId             = module.tencent_network_base[0].vpc_id
    subnetId          = module.tencent_network_base[0].subnet_id
    securityGroupIds  = [module.tencent_network_base[0].runtime_security_group_id]
    systemDiskType    = var.tencent.system_disk_type
    systemDiskSizeGiB = var.tencent.system_disk_size
  })
}

check "selected_provider_config" {
  assert {
    condition     = local.selected_provider == "tencent" || local.platform_mode == "managed_cvm"
    error_message = "platform_mode=existing_lighthouse is only supported with provider_name=tencent."
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
    condition = !local.use_existing_lighthouse || (
      trimspace(var.existing_lighthouse_public_ip) != "" &&
      trimspace(var.existing_lighthouse_private_ip) != "" &&
      trimspace(var.existing_lighthouse_instance_id) != ""
    )
    error_message = "platform_mode=existing_lighthouse requires existing_lighthouse_public_ip, existing_lighthouse_private_ip, and existing_lighthouse_instance_id."
  }

  assert {
    condition     = !local.use_existing_lighthouse || local.existing_ccn_id != ""
    error_message = "platform_mode=existing_lighthouse requires existing_ccn_id."
  }

  assert {
    condition     = var.runtime_host_port_min <= var.runtime_host_port_max
    error_message = "runtime_host_port_min must not be greater than runtime_host_port_max."
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
  vpc_name                   = var.vpc_name
  vpc_cidr_block             = var.vpc_cidr_block
  vswitch_name               = var.subnet_name
  vswitch_cidr_block         = var.subnet_cidr_block
  security_group_name_prefix = var.security_group_name_prefix
  admin_cidrs                = var.admin_cidrs
  control_plane_cidrs        = var.control_plane_cidrs
  ingress_cidrs              = var.ingress_cidrs
  cloud_plane_grpc_port      = var.cloud_plane_grpc_port
  ingress_http_port          = var.ingress_http_port
  egress_proxy_port          = var.egress_proxy_port
  artifact_http_port         = var.artifact_http_port
  runtime_host_port_min      = var.runtime_host_port_min
  runtime_host_port_max      = var.runtime_host_port_max
  platform_role_policy_names = var.aliyun.platform_role_policy_names
  ssh_public_key             = local.effective_ssh_public_key
}

module "aliyun_platform_host" {
  count  = local.selected_provider == "aliyun" ? 1 : 0
  source = "../providers/aliyun/modules/platform-host"
  depends_on = [
    module.aliyun_network_base,
  ]

  zone_id                  = var.aliyun.zone_id
  platform_name            = var.platform_name
  environment              = var.environment
  owner                    = var.owner
  vswitch_id               = module.aliyun_network_base[0].vswitch_id
  security_group_ids       = [module.aliyun_network_base[0].platform_security_group_id]
  role_name                = module.aliyun_network_base[0].platform_role_name
  platform_instance_name   = var.aliyun.platform_instance_name
  instance_type            = var.aliyun.instance_type
  image_id                 = var.aliyun.image_id
  key_pair_name            = module.aliyun_network_base[0].platform_key_pair_name
  system_disk_category     = var.aliyun.system_disk_category
  system_disk_size         = var.aliyun.system_disk_size
  eip_name                 = var.aliyun.eip_name
  eip_bandwidth            = var.aliyun.eip_bandwidth
  eip_internet_charge_type = var.aliyun.eip_internet_charge_type
}

module "tencent_network_base" {
  count  = local.selected_provider == "tencent" ? 1 : 0
  source = "../providers/tencent/modules/network-base"

  region_id                      = var.tencent.region_id
  zone_id                        = var.tencent.zone_id
  platform_name                  = var.platform_name
  create_platform_host_resources = !local.use_existing_lighthouse
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
  runtime_host_port_min          = var.runtime_host_port_min
  runtime_host_port_max          = var.runtime_host_port_max
  platform_role_policy_names     = var.tencent.platform_role_policy_names
  platform_private_cidrs         = local.use_existing_lighthouse ? ["${trimspace(var.existing_lighthouse_private_ip)}/32"] : []
  ssh_public_key                 = local.effective_ssh_public_key
}

module "tencent_platform_host" {
  count  = local.selected_provider == "tencent" && !local.use_existing_lighthouse ? 1 : 0
  source = "../providers/tencent/modules/platform-host"
  depends_on = [
    module.tencent_network_base,
  ]

  region_id                      = var.tencent.region_id
  zone_id                        = var.tencent.zone_id
  platform_name                  = var.platform_name
  environment                    = var.environment
  owner                          = var.owner
  platform_instance_name         = var.tencent.platform_instance_name
  instance_type                  = var.tencent.instance_type
  image_id                       = var.tencent.image_id
  vpc_id                         = module.tencent_network_base[0].vpc_id
  subnet_id                      = module.tencent_network_base[0].subnet_id
  security_group_ids             = [module.tencent_network_base[0].platform_security_group_id]
  key_ids                        = [module.tencent_network_base[0].platform_key_id]
  system_disk_type               = var.tencent.system_disk_type
  system_disk_size               = var.tencent.system_disk_size
  cam_role_name                  = module.tencent_network_base[0].platform_role_name
  eip_name                       = var.tencent.eip_name
  eip_type                       = var.tencent.eip_type
  eip_internet_charge_type       = var.tencent.eip_internet_charge_type
  eip_internet_max_bandwidth_out = var.tencent.eip_internet_max_bandwidth_out
}

resource "tencentcloud_ccn_attachment_v2" "runtime_vpc" {
  count = local.use_existing_lighthouse ? 1 : 0

  ccn_id          = local.existing_ccn_id
  instance_id     = module.tencent_network_base[0].vpc_id
  instance_region = var.tencent.region_id
  instance_type   = "VPC"
  description     = "mini-cloud runtime vpc"
}
