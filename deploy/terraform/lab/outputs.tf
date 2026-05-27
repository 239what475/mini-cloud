output "platform_provider" {
  value = local.selected_provider
}

output "platform_public_ip" {
  value = local.cloud_plane_public_ip
}

output "platform_instance_id" {
  value = local.selected_provider == "aliyun" ? module.aliyun_platform_host[0].platform_instance_id : module.tencent_platform_host[0].platform_instance_id
}

output "platform_security_group_id" {
  value = local.selected_provider == "aliyun" ? module.aliyun_network_base[0].platform_security_group_id : module.tencent_network_base[0].platform_security_group_id
}

output "runtime_security_group_id" {
  value = local.selected_provider == "aliyun" ? module.aliyun_network_base[0].runtime_security_group_id : module.tencent_network_base[0].runtime_security_group_id
}

output "cloud_plane_grpc_endpoint" {
  value = local.cloud_plane_grpc_endpoint
}

output "control_plane_southbound_token" {
  value     = local.effective_control_plane_southbound_token
  sensitive = true
}

output "node_agent_bootstrap_token" {
  value     = local.effective_node_agent_bootstrap_token
  sensitive = true
}

output "admin_token" {
  value     = local.effective_admin_token
  sensitive = true
}
