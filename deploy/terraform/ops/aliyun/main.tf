locals {
  effective_ssh_public_key = trimspace(var.ssh_public_key) != "" ? trimspace(var.ssh_public_key) : (
    trimspace(var.ssh_public_key_path) != "" ? trimspace(file(pathexpand(var.ssh_public_key_path))) : ""
  )

  platform_instance_name = trimspace(var.existing_ecs_instance_name) != "" ? trimspace(var.existing_ecs_instance_name) : var.platform_name
  platform_ssh_host      = trimspace(var.existing_ecs_ssh_host) != "" ? trimspace(var.existing_ecs_ssh_host) : trimspace(var.existing_ecs_public_ip)
  platform_grpc_endpoint = "${trimspace(var.existing_ecs_public_ip)}:${var.cloud_plane_grpc_port}"
}

check "aliyun_config" {
  assert {
    condition = (
      trimspace(var.existing_ecs_instance_id) != "" &&
      trimspace(var.existing_ecs_public_ip) != "" &&
      trimspace(var.existing_ecs_private_ip) != "" &&
      trimspace(var.existing_ecs_vpc_id) != "" &&
      trimspace(var.existing_ecs_vswitch_id) != "" &&
      trimspace(var.existing_ecs_vswitch_cidr_block) != "" &&
      trimspace(var.existing_ecs_security_group_id) != ""
    )
    error_message = "existing ECS instance, IP, VPC, VSwitch, VSwitch CIDR, and security group fields are required."
  }

  assert {
    condition     = trimspace(var.aliyun.region_id) != "" && trimspace(var.aliyun.zone_id) != "" && trimspace(var.aliyun.instance_type) != "" && trimspace(var.aliyun.image_id) != ""
    error_message = "aliyun.region_id, aliyun.zone_id, aliyun.instance_type, and aliyun.image_id are required."
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

module "network_base" {
  source = "../../providers/aliyun/modules/network-base"

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
  workload_proxy_port        = var.workload_proxy_port
  artifact_http_port         = var.artifact_http_port
  node_host_port_min         = var.node_host_port_min
  node_host_port_max         = var.node_host_port_max
  ssh_public_key             = local.effective_ssh_public_key
}
