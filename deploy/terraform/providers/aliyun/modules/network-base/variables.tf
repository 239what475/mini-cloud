variable "region_id" {
  type = string

  validation {
    condition = contains([
      "cn-wulanchabu",
      "cn-heyuan",
      "cn-hangzhou",
      "cn-beijing",
      "us-east-1"
    ], var.region_id)
    error_message = "region_id 必须落在当前教学允许的地域集合内。"
  }
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

variable "vswitch_name" {
  type    = string
  default = ""
}

variable "vswitch_cidr_block" {
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

variable "runtime_host_port_min" {
  type    = number
  default = 30000

  validation {
    condition     = var.runtime_host_port_min >= 1 && var.runtime_host_port_min <= 65535
    error_message = "runtime_host_port_min 必须是 1 到 65535 之间的 TCP 端口。"
  }
}

variable "runtime_host_port_max" {
  type    = number
  default = 60999

  validation {
    condition     = var.runtime_host_port_max >= 1 && var.runtime_host_port_max <= 65535
    error_message = "runtime_host_port_max 必须是 1 到 65535 之间的 TCP 端口。"
  }
}

variable "platform_role_name" {
  type    = string
  default = ""
}

variable "platform_role_policy_names" {
  type = list(string)
  default = [
    "AliyunECSFullAccess",
    "AliyunVPCFullAccess"
  ]
}

variable "ssh_public_key" {
  type = string
}
