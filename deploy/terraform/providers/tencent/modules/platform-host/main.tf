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

resource "tencentcloud_instance" "platform" {
  instance_name           = local.platform_instance_name
  availability_zone       = var.zone_id
  image_id                = var.image_id
  instance_type           = var.instance_type
  instance_charge_type    = "POSTPAID_BY_HOUR"
  vpc_id                  = var.vpc_id
  subnet_id               = var.subnet_id
  orderly_security_groups = var.security_group_ids
  key_ids                 = var.key_ids
  system_disk_type        = var.system_disk_type
  system_disk_size        = var.system_disk_size
  user_data_raw           = var.user_data

  internet_max_bandwidth_out  = 0
  allocate_public_ip          = false
  user_data_replace_on_change = var.user_data_replace_on_change

  cam_role_name = var.cam_role_name

  tags = local.platform_tags

  lifecycle {
    # 带 EIP 的实例会由腾讯云侧回填出站带宽视图，
    # 这个值不能再通过实例资源直接改回去，否则 destroy 前会先卡在一次无意义的 update。
    ignore_changes = [
      internet_max_bandwidth_out,
    ]
  }
}

resource "tencentcloud_eip" "platform" {
  name                       = local.eip_name
  type                       = var.eip_type
  internet_charge_type       = var.eip_internet_charge_type
  internet_max_bandwidth_out = var.eip_internet_max_bandwidth_out
}

resource "tencentcloud_eip_association" "platform" {
  eip_id      = tencentcloud_eip.platform.id
  instance_id = tencentcloud_instance.platform.id
}
