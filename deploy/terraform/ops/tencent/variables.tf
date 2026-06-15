variable "platform_name" {
  type = string
}

variable "existing_lighthouse_public_ip" {
  type = string
}

variable "existing_lighthouse_private_ip" {
  type = string
}

variable "existing_lighthouse_instance_id" {
  type = string
}

variable "existing_lighthouse_ssh_host" {
  type    = string
  default = ""
}

variable "existing_ccn_id" {
  type = string
}

variable "environment" {
  type    = string
  default = "ops"
}

variable "owner" {
  type    = string
  default = "operator"
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

variable "tencent" {
  type = object({
    region_id                      = string
    zone_id                        = string
    instance_type                  = string
    image_id                       = string
    system_disk_type               = optional(string, "CLOUD_PREMIUM")
    system_disk_size               = optional(number, 50)
    eip_name                       = optional(string, "")
    eip_type                       = optional(string, "EIP")
    eip_internet_charge_type       = optional(string, "TRAFFIC_POSTPAID_BY_HOUR")
    eip_internet_max_bandwidth_out = optional(number, 5)
  })
}
