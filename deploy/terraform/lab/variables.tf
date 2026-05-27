variable "provider_name" {
  type = string

  validation {
    condition     = contains(["aliyun", "tencent"], lower(trimspace(var.provider_name)))
    error_message = "provider_name 只能是 aliyun 或 tencent。"
  }
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

variable "ingress_base_domain" {
  type    = string
  default = "apps.example.test"
}

variable "install_root" {
  type    = string
  default = "/opt/mini-cloud"
}

variable "admin_token" {
  type      = string
  default   = ""
  sensitive = true
}

variable "control_plane_southbound_token" {
  type      = string
  default   = ""
  sensitive = true
}

variable "node_agent_bootstrap_token" {
  type      = string
  default   = ""
  sensitive = true
}

variable "ssh_public_key" {
  type    = string
  default = ""
}

variable "ssh_public_key_path" {
  type    = string
  default = ""
}

variable "trust_auth_proxy_headers" {
  type    = bool
  default = false
}

variable "cloud_plane_binary_url" {
  type = string

  validation {
    condition     = startswith(var.cloud_plane_binary_url, "https://") || startswith(var.cloud_plane_binary_url, "http://")
    error_message = "cloud_plane_binary_url 必须是可下载的 http(s) 地址。"
  }
}

variable "cloud_plane_binary_sha256" {
  type    = string
  default = ""
}

variable "agent_binary_url" {
  type = string

  validation {
    condition     = startswith(var.agent_binary_url, "https://") || startswith(var.agent_binary_url, "http://")
    error_message = "agent_binary_url 必须是可下载的 http(s) 地址。"
  }
}

variable "agent_binary_sha256" {
  type    = string
  default = ""
}


variable "workload_log_loki_url" {
  type    = string
  default = ""
}

variable "workload_log_loki_tenant_id" {
  type    = string
  default = ""
}

variable "workload_otlp_endpoint" {
  type    = string
  default = ""
}

variable "image_pull_registry_mirror" {
  type    = string
  default = ""
}

variable "runtime_node_heartbeat_interval_seconds" {
  type    = number
  default = 15
}

variable "runtime_node_work_interval_seconds" {
  type    = number
  default = 5
}

variable "platform_node_system_reserved_cpu_milli" {
  type    = number
  default = 1000
}

variable "platform_node_system_reserved_memory_mi" {
  type    = number
  default = 1024
}

variable "aliyun" {
  type = object({
    region_id                         = optional(string, "")
    zone_id                           = optional(string, "")
    platform_role_policy_names        = optional(list(string), ["AliyunECSFullAccess", "AliyunVPCFullAccess"])
    platform_instance_name            = optional(string, "")
    instance_type                     = optional(string, "")
    image_id                          = optional(string, "")
    system_disk_category              = optional(string, "cloud_essd")
    system_disk_size                  = optional(number, 40)
    eip_name                          = optional(string, "")
    eip_bandwidth                     = optional(number, 5)
    eip_internet_charge_type          = optional(string, "PayByTraffic")
    runtime_node_instance_type        = optional(string, "")
    runtime_node_image_id             = optional(string, "")
    runtime_node_system_disk_category = optional(string, "")
    runtime_node_system_disk_size     = optional(number, 0)
  })
  default = {}

  validation {
    condition = var.aliyun.runtime_node_instance_type == "" || contains([
      "ecs.e-c1m1.large",
      "ecs.e-c1m2.large",
      "ecs.e-c1m4.large",
      "ecs.e-c1m2.xlarge",
      "ecs.e-c1m4.xlarge",
      "ecs.e-c1m2.2xlarge",
      "ecs.u1-c1m1.large",
      "ecs.u1-c1m2.large",
      "ecs.u1-c1m2.xlarge",
      "ecs.u1-c1m2.2xlarge",
      "ecs.u2a-c1m1.large",
      "ecs.u2a-c1m2.large",
      "ecs.u2a-c1m2.xlarge",
      "ecs.u2a-c1m2.2xlarge",
      "ecs.u2i-c1m1.large",
      "ecs.u2i-c1m2.large",
      "ecs.u2i-c1m2.xlarge",
      "ecs.u2i-c1m2.2xlarge"
    ], var.aliyun.runtime_node_instance_type)
    error_message = "aliyun.runtime_node_instance_type 必须落在当前教学允许的实例规格集合内，或者留空复用 platform 主机规格。"
  }
}

variable "tencent" {
  type = object({
    region_id                      = optional(string, "")
    zone_id                        = optional(string, "")
    platform_role_policy_names     = optional(list(string), ["QcloudCVMFullAccess", "QcloudCVMFinanceAccess"])
    platform_instance_name         = optional(string, "")
    instance_type                  = optional(string, "")
    image_id                       = optional(string, "")
    system_disk_type               = optional(string, "CLOUD_PREMIUM")
    system_disk_size               = optional(number, 50)
    user_data_replace_on_change    = optional(bool, false)
    eip_name                       = optional(string, "")
    eip_type                       = optional(string, "EIP")
    eip_internet_charge_type       = optional(string, "TRAFFIC_POSTPAID_BY_HOUR")
    eip_internet_max_bandwidth_out = optional(number, 5)
    runtime_node_instance_type     = optional(string, "")
    runtime_node_image_id          = optional(string, "")
    runtime_node_system_disk_type  = optional(string, "")
    runtime_node_system_disk_size  = optional(number, 0)
  })
  default = {}
}
