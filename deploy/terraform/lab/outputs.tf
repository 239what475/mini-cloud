output "provider" {
  value = local.selected_provider
}

output "platform_mode" {
  value = local.platform_mode
}

output "platform" {
  value = {
    name                 = var.platform_name
    instance_id          = local.platform_instance_id
    instance_name        = local.platform_instance_name
    instance_type        = local.platform_instance_type
    image_id             = local.platform_image_id
    public_ip            = local.platform_public_ip
    private_ip           = local.platform_private_ip
    ssh_host             = local.use_existing_lighthouse ? local.existing_lighthouse_ssh_host : ""
    key_name             = local.platform_key_name
    role_name            = local.platform_role_name
    cloud_plane_endpoint = local.cloud_plane_grpc_endpoint
  }
}

output "network" {
  value = {
    vpc_id                     = local.vpc_id
    subnet_id                  = local.subnet_id
    platform_security_group_id = local.platform_security_group_id
    runtime_security_group_id  = local.runtime_security_group_id
    vpc_cidr_block             = var.vpc_cidr_block
    subnet_cidr_block          = var.subnet_cidr_block
    cloud_plane_grpc_port      = var.cloud_plane_grpc_port
    ingress_http_port          = var.ingress_http_port
    egress_proxy_port          = var.egress_proxy_port
    artifact_http_port         = var.artifact_http_port
    runtime_host_port_min      = var.runtime_host_port_min
    runtime_host_port_max      = var.runtime_host_port_max
  }
}

output "ccn" {
  value = local.use_existing_lighthouse ? {
    id           = local.existing_ccn_id
    attached_vpc = module.tencent_network_base[0].vpc_id
  } : null
}

output "runtime_provider_spec" {
  value = jsondecode(local.runtime_provider_spec_json)
}

output "install_env" {
  value = {
    provider                  = local.selected_provider
    platform_name             = var.platform_name
    platform_instance_name    = local.platform_instance_name
    platform_instance_id      = local.platform_instance_id
    platform_instance_type    = local.platform_instance_type
    platform_public_ip        = local.platform_public_ip
    platform_private_ip       = local.platform_private_ip
    platform_ssh_host         = local.use_existing_lighthouse ? local.existing_lighthouse_ssh_host : ""
    region_id                 = local.selected_provider == "aliyun" ? var.aliyun.region_id : var.tencent.region_id
    zone_id                   = local.selected_provider == "aliyun" ? var.aliyun.zone_id : var.tencent.zone_id
    cloud_plane_grpc_port     = var.cloud_plane_grpc_port
    ingress_http_port         = var.ingress_http_port
    egress_proxy_port         = var.egress_proxy_port
    artifact_http_port        = var.artifact_http_port
    runtime_host_port_min     = var.runtime_host_port_min
    runtime_host_port_max     = var.runtime_host_port_max
    subnet_cidr_block         = var.subnet_cidr_block
    runtime_security_group_id = local.runtime_security_group_id
  }
}
