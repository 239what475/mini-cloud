output "region_id" {
  value = var.region_id
}

output "zone_id" {
  value = var.zone_id
}

output "vpc_id" {
  value = tencentcloud_vpc.platform.id
}

output "vpc_name" {
  value = tencentcloud_vpc.platform.name
}

output "subnet_id" {
  value = tencentcloud_subnet.platform.id
}

output "subnet_name" {
  value = tencentcloud_subnet.platform.name
}

output "platform_security_group_id" {
  value = tencentcloud_security_group.platform.id
}

output "platform_security_group_name" {
  value = tencentcloud_security_group.platform.name
}

output "runtime_security_group_id" {
  value = tencentcloud_security_group.runtime.id
}

output "runtime_security_group_name" {
  value = tencentcloud_security_group.runtime.name
}

output "platform_key_id" {
  value = tencentcloud_key_pair.platform.id
}

output "platform_key_name" {
  value = tencentcloud_key_pair.platform.key_name
}

output "platform_role_name" {
  value = tencentcloud_cam_role.platform.name
}

output "platform_role_id" {
  value = tencentcloud_cam_role.platform.id
}
