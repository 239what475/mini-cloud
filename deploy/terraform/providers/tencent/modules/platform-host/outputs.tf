output "platform_instance_id" {
  value = tencentcloud_instance.platform.id
}

output "platform_instance_name" {
  value = tencentcloud_instance.platform.instance_name
}

output "platform_instance_type" {
  value = tencentcloud_instance.platform.instance_type
}

output "platform_image_id" {
  value = tencentcloud_instance.platform.image_id
}

output "platform_private_ip" {
  value = tencentcloud_instance.platform.private_ip
}

output "platform_public_ip" {
  value = tencentcloud_instance.platform.public_ip
}

output "eip_id" {
  value = tencentcloud_eip.platform.id
}

output "eip_name" {
  value = tencentcloud_eip.platform.name
}

output "eip_public_ip" {
  value = tencentcloud_eip.platform.public_ip
}
