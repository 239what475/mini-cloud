locals {
  effective_ssh_public_key = trimspace(var.ssh_public_key) != "" ? trimspace(var.ssh_public_key) : (
    trimspace(var.ssh_public_key_path) != "" ? trimspace(file(pathexpand(var.ssh_public_key_path))) : ""
  )

  platform_ssh_host      = trimspace(var.existing_lighthouse_ssh_host) != "" ? trimspace(var.existing_lighthouse_ssh_host) : trimspace(var.existing_lighthouse_public_ip)
  platform_grpc_endpoint = "${trimspace(var.existing_lighthouse_public_ip)}:${var.cloud_plane_grpc_port}"
}

check "tencent_config" {
  assert {
    condition = (
      trimspace(var.existing_lighthouse_public_ip) != "" &&
      trimspace(var.existing_lighthouse_private_ip) != "" &&
      trimspace(var.existing_lighthouse_instance_id) != "" &&
      trimspace(var.existing_ccn_id) != ""
    )
    error_message = "existing Lighthouse public IP, private IP, instance id, and CCN id are required."
  }

  assert {
    condition     = trimspace(var.tencent.region_id) != "" && trimspace(var.tencent.zone_id) != "" && trimspace(var.tencent.instance_type) != "" && trimspace(var.tencent.image_id) != ""
    error_message = "tencent.region_id, tencent.zone_id, tencent.instance_type, and tencent.image_id are required."
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
  source = "../../providers/tencent/modules/network-base"

  region_id                  = var.tencent.region_id
  zone_id                    = var.tencent.zone_id
  platform_name              = var.platform_name
  environment                = var.environment
  owner                      = var.owner
  vpc_name                   = var.vpc_name
  vpc_cidr_block             = var.vpc_cidr_block
  subnet_name                = var.subnet_name
  subnet_cidr_block          = var.subnet_cidr_block
  security_group_name_prefix = var.security_group_name_prefix
  control_plane_cidrs        = var.control_plane_cidrs
  ingress_cidrs              = var.ingress_cidrs
  cloud_plane_grpc_port      = var.cloud_plane_grpc_port
  ingress_http_port          = var.ingress_http_port
  artifact_http_port         = var.artifact_http_port
  node_host_port_min         = var.node_host_port_min
  node_host_port_max         = var.node_host_port_max
  platform_private_cidrs     = ["${trimspace(var.existing_lighthouse_private_ip)}/32"]
  ssh_public_key             = local.effective_ssh_public_key
}

resource "tencentcloud_ccn_attachment_v2" "node_vpc" {
  ccn_id          = trimspace(var.existing_ccn_id)
  instance_id     = module.network_base.vpc_id
  instance_region = var.tencent.region_id
  instance_type   = "VPC"
  description     = "mini-cloud node vpc"
}
