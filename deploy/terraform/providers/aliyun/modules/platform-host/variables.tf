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

variable "vswitch_id" {
  type = string
}

variable "security_group_ids" {
  type = list(string)
}

variable "role_name" {
  type = string
}

variable "platform_instance_name" {
  type    = string
  default = ""
}

variable "instance_type" {
  type = string

  validation {
    condition = contains([
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
    ], var.instance_type)
    error_message = "instance_type 必须落在当前教学允许的实例规格集合内。"
  }
}

variable "image_id" {
  type = string
}

variable "key_pair_name" {
  type = string
}

variable "system_disk_category" {
  type    = string
  default = "cloud_essd"
}

variable "system_disk_size" {
  type    = number
  default = 40
}

variable "user_data" {
  type = string
}

variable "eip_name" {
  type    = string
  default = ""
}

variable "eip_bandwidth" {
  type    = number
  default = 5
}

variable "eip_internet_charge_type" {
  type    = string
  default = "PayByTraffic"
}
