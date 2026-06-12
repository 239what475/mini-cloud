variable "region_id" {
  type = string
}

variable "zone_id" {
  type = string
}

variable "platform_name" {
  type = string
}

variable "environment" {
  type = string
}

variable "owner" {
  type = string
}

variable "vpc_name" {
  type    = string
  default = ""
}

variable "vpc_cidr_block" {
  type = string
}

variable "subnet_name" {
  type    = string
  default = ""
}

variable "subnet_cidr_block" {
  type = string
}

variable "security_group_name_prefix" {
  type    = string
  default = ""
}

variable "admin_cidrs" {
  type = list(string)
}

variable "control_plane_cidrs" {
  type = list(string)
}

variable "ingress_cidrs" {
  type = list(string)
}

variable "platform_private_cidrs" {
  type    = list(string)
  default = []
}

variable "cloud_plane_grpc_port" {
  type    = number
  default = 8080
}

variable "ingress_http_port" {
  type    = number
  default = 80
}

variable "egress_proxy_port" {
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
    error_message = "node_host_port_min 必须是 1 到 65535 之间的 TCP 端口。"
  }
}

variable "node_host_port_max" {
  type    = number
  default = 60999

  validation {
    condition     = var.node_host_port_max >= 1 && var.node_host_port_max <= 65535
    error_message = "node_host_port_max 必须是 1 到 65535 之间的 TCP 端口。"
  }
}

variable "ssh_public_key" {
  type = string
}
