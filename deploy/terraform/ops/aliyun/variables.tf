variable "aliyun_profile" {
  type    = string
  default = "default"
}

variable "platform_name" {
  type = string
}

variable "existing_ecs_instance_id" {
  type = string
}

variable "existing_ecs_instance_name" {
  type    = string
  default = ""
}

variable "existing_ecs_instance_type" {
  type    = string
  default = ""
}

variable "existing_ecs_image_id" {
  type    = string
  default = ""
}

variable "existing_ecs_public_ip" {
  type = string
}

variable "existing_ecs_private_ip" {
  type = string
}

variable "existing_ecs_ssh_host" {
  type    = string
  default = ""
}

variable "existing_ecs_vpc_id" {
  type = string
}

variable "existing_ecs_vswitch_id" {
  type = string
}

variable "existing_ecs_vswitch_cidr_block" {
  type = string
}

variable "existing_ecs_security_group_id" {
  type = string
}

variable "existing_ecs_role_name" {
  type    = string
  default = ""
}

variable "environment" {
  type    = string
  default = "ops"
}

variable "owner" {
  type    = string
  default = "operator"
}

variable "security_group_name_prefix" {
  type    = string
  default = ""
}

variable "control_plane_cidrs" {
  type = list(string)
}

variable "ingress_cidrs" {
  type = list(string)
}

variable "cloud_plane_grpc_port" {
  type    = number
  default = 18081
}

variable "ingress_http_port" {
  type    = number
  default = 80
}

variable "workload_proxy_port" {
  type    = number
  default = 3128
}

variable "artifact_http_port" {
  type    = number
  default = 18082
}

variable "node_host_port_min" {
  type    = number
  default = 30000

  validation {
    condition     = var.node_host_port_min >= 1 && var.node_host_port_min <= 65535
    error_message = "node_host_port_min must be between 1 and 65535."
  }
}

variable "node_host_port_max" {
  type    = number
  default = 60999

  validation {
    condition     = var.node_host_port_max >= 1 && var.node_host_port_max <= 65535
    error_message = "node_host_port_max must be between 1 and 65535."
  }
}

variable "ssh_public_key" {
  type    = string
  default = ""
}

variable "ssh_public_key_path" {
  type    = string
  default = ""
}

variable "aliyun" {
  type = object({
    region_id            = string
    zone_id              = string
    instance_type        = string
    image_id             = string
    system_disk_category = optional(string, "cloud_essd")
    system_disk_size     = optional(number, 40)
  })
}
