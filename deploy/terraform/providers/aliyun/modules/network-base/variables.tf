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

variable "vpc_id" {
  type = string
}

variable "vswitch_id" {
  type = string
}

variable "vswitch_cidr_block" {
  type = string
}

variable "platform_security_group_id" {
  type = string
}

variable "platform_private_ip" {
  type = string
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

variable "runtime_host_port_min" {
  type    = number
  default = 30000

  validation {
    condition     = var.runtime_host_port_min >= 1 && var.runtime_host_port_min <= 65535
    error_message = "runtime_host_port_min must be between 1 and 65535."
  }
}

variable "runtime_host_port_max" {
  type    = number
  default = 60999

  validation {
    condition     = var.runtime_host_port_max >= 1 && var.runtime_host_port_max <= 65535
    error_message = "runtime_host_port_max must be between 1 and 65535."
  }
}

variable "ssh_public_key" {
  type = string
}
