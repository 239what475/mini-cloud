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

output "node_security_group_id" {
  value = tencentcloud_security_group.node.id
}

output "node_security_group_name" {
  value = tencentcloud_security_group.node.name
}

output "platform_key_id" {
  value = tencentcloud_key_pair.platform.id
}

output "platform_key_name" {
  value = tencentcloud_key_pair.platform.key_name
}
