output "provider" {
  value = "aliyun"
}

output "platform_mode" {
  value = "existing_ecs"
}

output "platform" {
  value = {
    name                 = var.platform_name
    instance_id          = var.existing_ecs_instance_id
    instance_name        = local.platform_instance_name
    instance_type        = var.existing_ecs_instance_type
    image_id             = var.existing_ecs_image_id
    public_ip            = var.existing_ecs_public_ip
    private_ip           = var.existing_ecs_private_ip
    ssh_host             = local.platform_ssh_host
    key_name             = module.network_base.platform_key_pair_name
    role_name            = var.existing_ecs_role_name
    cloud_plane_endpoint = local.platform_grpc_endpoint
  }
}

output "network" {
  value = {
    vpc_id                     = var.existing_ecs_vpc_id
    subnet_id                  = module.network_base.vswitch_id
    platform_security_group_id = module.network_base.platform_security_group_id
    node_security_group_id     = module.network_base.node_security_group_id
    vpc_cidr_block             = ""
    subnet_cidr_block          = module.network_base.vswitch_cidr_block
    cloud_plane_grpc_port      = var.cloud_plane_grpc_port
    ingress_http_port          = var.ingress_http_port
    workload_proxy_port        = var.workload_proxy_port
    artifact_http_port         = var.artifact_http_port
    node_host_port_min         = var.node_host_port_min
    node_host_port_max         = var.node_host_port_max
  }
}

output "ccn" {
  value = null
}

output "node_provider_config" {
  value = {
    provider           = "aliyun"
    instanceType       = var.aliyun.instance_type
    imageId            = var.aliyun.image_id
    keyPairName        = module.network_base.platform_key_pair_name
    vSwitchId          = module.network_base.vswitch_id
    securityGroupId    = module.network_base.node_security_group_id
    systemDiskCategory = var.aliyun.system_disk_category
    systemDiskSizeGiB  = var.aliyun.system_disk_size
  }
}

output "install_env" {
  value = {
    provider               = "aliyun"
    platform_name          = var.platform_name
    platform_instance_name = local.platform_instance_name
    platform_instance_id   = var.existing_ecs_instance_id
    platform_instance_type = var.existing_ecs_instance_type
    platform_public_ip     = var.existing_ecs_public_ip
    platform_private_ip    = var.existing_ecs_private_ip
    platform_ssh_host      = local.platform_ssh_host
    region_id              = var.aliyun.region_id
    zone_id                = var.aliyun.zone_id
    cloud_plane_grpc_port  = var.cloud_plane_grpc_port
    ingress_http_port      = var.ingress_http_port
    workload_proxy_port    = var.workload_proxy_port
    artifact_http_port     = var.artifact_http_port
    node_host_port_min     = var.node_host_port_min
    node_host_port_max     = var.node_host_port_max
    subnet_cidr_block      = module.network_base.vswitch_cidr_block
    node_security_group_id = module.network_base.node_security_group_id
  }
}
