locals {
  platform_instance_name = var.platform_instance_name != "" ? var.platform_instance_name : var.platform_name
  eip_name               = var.eip_name != "" ? var.eip_name : "${var.platform_name}-eip"
  user_data_checksum     = sha256(var.user_data)

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

resource "terraform_data" "platform_bootstrap" {
  triggers_replace = local.user_data_checksum
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
  user_data                  = var.user_data
  tags                       = local.platform_tags

  lifecycle {
    // cloud-init 只会在实例首启时处理 user_data，
    // 所以脚本内容一旦变化，就必须重建 ECS 才能真正重新执行。
    replace_triggered_by = [terraform_data.platform_bootstrap]
  }
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
