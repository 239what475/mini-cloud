output "platform_instance_id" {
  value = alicloud_instance.platform.id
}

output "platform_instance_name" {
  value = alicloud_instance.platform.instance_name
}

output "platform_instance_type" {
  value = alicloud_instance.platform.instance_type
}

output "platform_image_id" {
  value = alicloud_instance.platform.image_id
}

output "platform_private_ip" {
  value = alicloud_instance.platform.primary_ip_address
}

output "platform_instance_status" {
  value = alicloud_instance.platform.status
}

output "eip_allocation_id" {
  value = alicloud_eip_address.platform.id
}

output "eip_name" {
  value = alicloud_eip_address.platform.address_name
}

output "eip_ip_address" {
  value = alicloud_eip_address.platform.ip_address
}
