output "region_id" {
  value = var.region_id
}

output "zone_id" {
  value = var.zone_id
}

output "vpc_id" {
  value = alicloud_vpc.platform.id
}

output "vpc_name" {
  value = alicloud_vpc.platform.vpc_name
}

output "vswitch_id" {
  value = alicloud_vswitch.platform.id
}

output "vswitch_name" {
  value = alicloud_vswitch.platform.vswitch_name
}

output "platform_security_group_id" {
  value = alicloud_security_group.platform.id
}

output "platform_security_group_name" {
  value = alicloud_security_group.platform.security_group_name
}

output "runtime_security_group_id" {
  value = alicloud_security_group.runtime.id
}

output "runtime_security_group_name" {
  value = alicloud_security_group.runtime.security_group_name
}

output "platform_key_pair_name" {
  value = alicloud_ecs_key_pair.platform.key_pair_name
}

output "platform_key_pair_id" {
  value = alicloud_ecs_key_pair.platform.id
}

output "platform_role_name" {
  value = alicloud_ram_role.platform.role_name
}

output "platform_role_arn" {
  value = alicloud_ram_role.platform.arn
}
