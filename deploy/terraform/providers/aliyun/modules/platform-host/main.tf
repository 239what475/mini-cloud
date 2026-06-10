locals {
  platform_instance_name = var.platform_instance_name != "" ? var.platform_instance_name : var.platform_name
  eip_name               = var.eip_name != "" ? var.eip_name : "${var.platform_name}-eip"
  common_tags = {
    "managed-by"             = "mini-cloud"
    "mini-cloud/platform"    = var.platform_name
    "mini-cloud/environment" = var.environment
    "mini-cloud/owner"       = var.owner
  }

  platform_tags = merge(local.common_tags, {
    "mini-cloud/role" = "platform"
  })
}

resource "alicloud_instance" "platform" {
  availability_zone          = var.zone_id
  instance_name              = local.platform_instance_name
  image_id                   = var.image_id
  instance_type              = var.instance_type
  instance_charge_type       = "PostPaid"
  security_groups            = var.security_group_ids
  vswitch_id                 = var.vswitch_id
  key_name                   = var.key_pair_name
  role_name                  = var.role_name
  internet_max_bandwidth_out = 0
  system_disk_category       = var.system_disk_category
  system_disk_size           = var.system_disk_size
  tags                       = local.platform_tags
}

resource "alicloud_eip_address" "platform" {
  address_name         = local.eip_name
  bandwidth            = var.eip_bandwidth
  internet_charge_type = var.eip_internet_charge_type
  payment_type         = "PayAsYouGo"
  description          = "${var.platform_name} platform eip"
  tags                 = local.common_tags
}

resource "alicloud_eip_association" "platform" {
  allocation_id = alicloud_eip_address.platform.id
  instance_id   = alicloud_instance.platform.id
}
