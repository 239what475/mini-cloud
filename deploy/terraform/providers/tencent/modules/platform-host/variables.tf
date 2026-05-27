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

variable "platform_instance_name" {
  type    = string
  default = ""
}

variable "instance_type" {
  type = string
}

variable "image_id" {
  type = string
}

variable "vpc_id" {
  type = string
}

variable "subnet_id" {
  type = string
}

variable "security_group_ids" {
  type = list(string)
}

variable "key_ids" {
  type = list(string)
}

variable "system_disk_type" {
  type    = string
  default = "CLOUD_PREMIUM"
}

variable "system_disk_size" {
  type    = number
  default = 50
}

variable "cam_role_name" {
  type = string
}

variable "user_data" {
  type    = string
  default = ""
}

variable "user_data_replace_on_change" {
  type    = bool
  default = false
}

variable "eip_name" {
  type    = string
  default = ""
}

variable "eip_type" {
  type    = string
  default = "EIP"
}

variable "eip_internet_charge_type" {
  type    = string
  default = "TRAFFIC_POSTPAID_BY_HOUR"
}

variable "eip_internet_max_bandwidth_out" {
  type    = number
  default = 5
}
