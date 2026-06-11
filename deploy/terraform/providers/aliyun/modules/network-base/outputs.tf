output "region_id" {
  value = var.region_id
}

output "zone_id" {
  value = var.zone_id
}

output "vpc_id" {
  value = var.vpc_id
}

output "vswitch_id" {
  value = var.vswitch_id
}

output "vswitch_cidr_block" {
  value = var.vswitch_cidr_block
}

output "platform_security_group_id" {
  value = var.platform_security_group_id
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
