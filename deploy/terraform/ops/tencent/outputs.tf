output "provider" {
  value = "tencent"
}

output "platform_mode" {
  value = "existing_lighthouse"
}

output "platform" {
  value = {
    name                 = var.platform_name
    instance_id          = var.existing_lighthouse_instance_id
    instance_name        = var.platform_name
    instance_type        = "LIGHTHOUSE"
    image_id             = ""
    public_ip            = var.existing_lighthouse_public_ip
    private_ip           = var.existing_lighthouse_private_ip
    ssh_host             = local.platform_ssh_host
    key_name             = module.network_base.platform_key_name
    role_name            = ""
    cloud_plane_endpoint = local.platform_grpc_endpoint
  }
}

output "network" {
  value = {
    vpc_id                     = module.network_base.vpc_id
    subnet_id                  = module.network_base.subnet_id
    platform_security_group_id = ""
    node_security_group_id     = module.network_base.node_security_group_id
    vpc_cidr_block             = var.vpc_cidr_block
    subnet_cidr_block          = var.subnet_cidr_block
    cloud_plane_grpc_port      = var.cloud_plane_grpc_port
    ingress_http_port          = var.ingress_http_port
    workload_proxy_port        = var.workload_proxy_port
    artifact_http_port         = var.artifact_http_port
    node_host_port_min         = var.node_host_port_min
    node_host_port_max         = var.node_host_port_max
  }
}

output "ccn" {
  value = {
    id           = trimspace(var.existing_ccn_id)
    attached_vpc = module.network_base.vpc_id
  }
}

output "node_provider_config" {
  value = {
    provider          = "tencent"
    instanceType      = var.tencent.instance_type
    imageId           = var.tencent.image_id
    keyIds            = [module.network_base.platform_key_id]
    vpcId             = module.network_base.vpc_id
    subnetId          = module.network_base.subnet_id
    securityGroupIds  = [module.network_base.node_security_group_id]
    systemDiskType    = var.tencent.system_disk_type
    systemDiskSizeGiB = var.tencent.system_disk_size
  }
}

output "install_env" {
  value = {
    provider               = "tencent"
    platform_name          = var.platform_name
    platform_instance_name = var.platform_name
    platform_instance_id   = var.existing_lighthouse_instance_id
    platform_instance_type = "LIGHTHOUSE"
    platform_public_ip     = var.existing_lighthouse_public_ip
    platform_private_ip    = var.existing_lighthouse_private_ip
    platform_ssh_host      = local.platform_ssh_host
    region_id              = var.tencent.region_id
    zone_id                = var.tencent.zone_id
    cloud_plane_grpc_port  = var.cloud_plane_grpc_port
    ingress_http_port      = var.ingress_http_port
    workload_proxy_port    = var.workload_proxy_port
    artifact_http_port     = var.artifact_http_port
    node_host_port_min     = var.node_host_port_min
    node_host_port_max     = var.node_host_port_max
    subnet_cidr_block      = var.subnet_cidr_block
    node_security_group_id = module.network_base.node_security_group_id
  }
}
