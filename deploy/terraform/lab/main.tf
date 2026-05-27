locals {
  selected_provider                        = lower(trimspace(var.provider_name))
  effective_admin_token                    = trimspace(var.admin_token) != "" ? var.admin_token : random_password.admin_token[0].result
  effective_control_plane_southbound_token = trimspace(var.control_plane_southbound_token) != "" ? var.control_plane_southbound_token : random_password.control_plane_southbound_token[0].result
  effective_node_agent_bootstrap_token     = trimspace(var.node_agent_bootstrap_token) != "" ? var.node_agent_bootstrap_token : random_password.node_agent_bootstrap_token[0].result
  effective_ssh_public_key = trimspace(var.ssh_public_key) != "" ? trimspace(var.ssh_public_key) : (
    trimspace(var.ssh_public_key_path) != "" ? trimspace(file(pathexpand(var.ssh_public_key_path))) : ""
  )
  aliyun_platform_instance_name  = trimspace(var.aliyun.platform_instance_name) != "" ? trimspace(var.aliyun.platform_instance_name) : var.platform_name
  tencent_platform_instance_name = trimspace(var.tencent.platform_instance_name) != "" ? trimspace(var.tencent.platform_instance_name) : var.platform_name

  default_image_pull_registry_mirror   = local.selected_provider == "tencent" ? "https://mirror.ccs.tencentyun.com" : "https://docker.m.daocloud.io"
  effective_image_pull_registry_mirror = trimspace(var.image_pull_registry_mirror) != "" ? var.image_pull_registry_mirror : local.default_image_pull_registry_mirror

  aliyun_runtime_node_instance_type = trimspace(var.aliyun.runtime_node_instance_type) != "" ? var.aliyun.runtime_node_instance_type : var.aliyun.instance_type
  aliyun_runtime_node_image_id      = trimspace(var.aliyun.runtime_node_image_id) != "" ? var.aliyun.runtime_node_image_id : var.aliyun.image_id
  aliyun_runtime_node_disk_category = trimspace(var.aliyun.runtime_node_system_disk_category) != "" ? var.aliyun.runtime_node_system_disk_category : var.aliyun.system_disk_category
  aliyun_runtime_node_disk_size     = var.aliyun.runtime_node_system_disk_size > 0 ? var.aliyun.runtime_node_system_disk_size : var.aliyun.system_disk_size

  tencent_runtime_node_instance_type      = trimspace(var.tencent.runtime_node_instance_type) != "" ? var.tencent.runtime_node_instance_type : var.tencent.instance_type
  tencent_runtime_node_image_id           = trimspace(var.tencent.runtime_node_image_id) != "" ? var.tencent.runtime_node_image_id : var.tencent.image_id
  tencent_runtime_node_system_disk_type   = trimspace(var.tencent.runtime_node_system_disk_type) != "" ? var.tencent.runtime_node_system_disk_type : var.tencent.system_disk_type
  tencent_runtime_node_system_disk_size   = var.tencent.runtime_node_system_disk_size > 0 ? var.tencent.runtime_node_system_disk_size : var.tencent.system_disk_size
  tencent_runtime_node_security_group_ids = local.selected_provider == "tencent" ? [module.tencent_network_base[0].runtime_security_group_id] : []

  cloud_plane_config_seed_json = local.selected_provider == "aliyun" ? jsonencode({
    plane = {
      identity = {
        name        = var.platform_name
        environment = var.environment
        owner       = var.owner
      }
    }
    nodeAgent = {
      defaults = {
        nodeNamePrefix           = "${var.platform_name}-runtime-node"
        heartbeatIntervalSeconds = var.runtime_node_heartbeat_interval_seconds
        workIntervalSeconds      = var.runtime_node_work_interval_seconds
        hostPortRange = {
          min = var.runtime_host_port_min
          max = var.runtime_host_port_max
        }
      }
    }
    infrastructure = {
      provider = "aliyun"
      location = {
        regionId = var.aliyun.region_id
        zoneId   = var.aliyun.zone_id
      }
    }
    observability = {
      logs = {
        lokiURL      = var.workload_log_loki_url
        lokiTenantID = var.workload_log_loki_tenant_id
      }
      traces = {
        otlpEndpoint = var.workload_otlp_endpoint
      }
    }
    runtimeProvisioning = {
      imagePull = {
        registryMirrors = [local.effective_image_pull_registry_mirror]
      }
      egress = {
        proxy = {
          enabled  = false
          endpoint = ""
          noProxy = [
            "127.0.0.1",
            "localhost",
            var.subnet_cidr_block,
            "169.254.169.254",
            "100.100.100.200"
          ]
        }
      }
      providerSpec = {
        instanceType       = local.aliyun_runtime_node_instance_type
        imageId            = local.aliyun_runtime_node_image_id
        keyPairName        = module.aliyun_network_base[0].platform_key_pair_name
        vSwitchId          = module.aliyun_network_base[0].vswitch_id
        securityGroupId    = module.aliyun_network_base[0].runtime_security_group_id
        systemDiskCategory = local.aliyun_runtime_node_disk_category
        systemDiskSizeGiB  = local.aliyun_runtime_node_disk_size
      }
    }
    ingress = {
      enabled    = true
      baseDomain = var.ingress_base_domain
      caddy = {
        listenHTTPAddr = "0.0.0.0:${var.ingress_http_port}"
        configPath     = "/etc/mini-cloud/ingress/Caddyfile"
        reloadCommand  = ["docker", "exec", "mini-cloud-caddy", "caddy", "reload", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
      }
    }
    }) : jsonencode({
    plane = {
      identity = {
        name        = var.platform_name
        environment = var.environment
        owner       = var.owner
      }
    }
    nodeAgent = {
      defaults = {
        nodeNamePrefix           = "${var.platform_name}-runtime-node"
        heartbeatIntervalSeconds = var.runtime_node_heartbeat_interval_seconds
        workIntervalSeconds      = var.runtime_node_work_interval_seconds
        hostPortRange = {
          min = var.runtime_host_port_min
          max = var.runtime_host_port_max
        }
      }
    }
    infrastructure = {
      provider = "tencent"
      location = {
        regionId = var.tencent.region_id
        zoneId   = var.tencent.zone_id
      }
    }
    observability = {
      logs = {
        lokiURL      = var.workload_log_loki_url
        lokiTenantID = var.workload_log_loki_tenant_id
      }
      traces = {
        otlpEndpoint = var.workload_otlp_endpoint
      }
    }
    runtimeProvisioning = {
      imagePull = {
        registryMirrors = [local.effective_image_pull_registry_mirror]
      }
      egress = {
        proxy = {
          enabled  = false
          endpoint = ""
          noProxy = [
            "127.0.0.1",
            "localhost",
            var.subnet_cidr_block,
            "169.254.169.254",
            "100.100.100.200"
          ]
        }
      }
      providerSpec = {
        instanceType      = local.tencent_runtime_node_instance_type
        imageId           = local.tencent_runtime_node_image_id
        keyIds            = [module.tencent_network_base[0].platform_key_id]
        vpcId             = module.tencent_network_base[0].vpc_id
        subnetId          = module.tencent_network_base[0].subnet_id
        securityGroupIds  = local.tencent_runtime_node_security_group_ids
        systemDiskType    = local.tencent_runtime_node_system_disk_type
        systemDiskSizeGiB = local.tencent_runtime_node_system_disk_size
      }
    }
    ingress = {
      enabled    = true
      baseDomain = var.ingress_base_domain
      caddy = {
        listenHTTPAddr = "0.0.0.0:${var.ingress_http_port}"
        configPath     = "/etc/mini-cloud/ingress/Caddyfile"
        reloadCommand  = ["docker", "exec", "mini-cloud-caddy", "caddy", "reload", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
      }
    }
  })

  cloud_plane_public_ip     = local.selected_provider == "aliyun" ? module.aliyun_platform_host[0].eip_ip_address : module.tencent_platform_host[0].eip_public_ip
  cloud_plane_private_ip    = local.selected_provider == "aliyun" ? module.aliyun_platform_host[0].platform_private_ip : module.tencent_platform_host[0].platform_private_ip
  cloud_plane_grpc_endpoint = "${local.cloud_plane_public_ip}:${var.cloud_plane_grpc_port}"
}

resource "random_password" "admin_token" {
  count   = trimspace(var.admin_token) == "" ? 1 : 0
  length  = 32
  lower   = true
  upper   = true
  numeric = true
  special = false
}

resource "random_password" "control_plane_southbound_token" {
  count   = trimspace(var.control_plane_southbound_token) == "" ? 1 : 0
  length  = 32
  lower   = true
  upper   = true
  numeric = true
  special = false
}

resource "random_password" "node_agent_bootstrap_token" {
  count   = trimspace(var.node_agent_bootstrap_token) == "" ? 1 : 0
  length  = 32
  lower   = true
  upper   = true
  numeric = true
  special = false
}

check "selected_provider_config" {
  assert {
    condition     = local.selected_provider != "aliyun" || (trimspace(var.aliyun.region_id) != "" && trimspace(var.aliyun.zone_id) != "" && trimspace(var.aliyun.instance_type) != "" && trimspace(var.aliyun.image_id) != "")
    error_message = "provider_name=aliyun 时，aliyun.region_id、aliyun.zone_id、aliyun.instance_type、aliyun.image_id 都必须填写。"
  }

  assert {
    condition     = local.selected_provider != "tencent" || (trimspace(var.tencent.region_id) != "" && trimspace(var.tencent.zone_id) != "" && trimspace(var.tencent.instance_type) != "" && trimspace(var.tencent.image_id) != "")
    error_message = "provider_name=tencent 时，tencent.region_id、tencent.zone_id、tencent.instance_type、tencent.image_id 都必须填写。"
  }

  assert {
    condition     = trimspace(local.effective_ssh_public_key) != ""
    error_message = "必须提供 ssh_public_key 或 ssh_public_key_path，Terraform 会据此自动创建云侧登录密钥对。"
  }

  assert {
    condition     = var.runtime_host_port_min <= var.runtime_host_port_max
    error_message = "runtime_host_port_min 不能大于 runtime_host_port_max。"
  }
}

module "aliyun_network_base" {
  count  = local.selected_provider == "aliyun" ? 1 : 0
  source = "../providers/aliyun/modules/network-base"

  region_id                  = var.aliyun.region_id
  zone_id                    = var.aliyun.zone_id
  platform_name              = var.platform_name
  environment                = var.environment
  owner                      = var.owner
  vpc_name                   = var.vpc_name
  vpc_cidr_block             = var.vpc_cidr_block
  vswitch_name               = var.subnet_name
  vswitch_cidr_block         = var.subnet_cidr_block
  security_group_name_prefix = var.security_group_name_prefix
  admin_cidrs                = var.admin_cidrs
  control_plane_cidrs        = var.control_plane_cidrs
  ingress_cidrs              = var.ingress_cidrs
  cloud_plane_grpc_port      = var.cloud_plane_grpc_port
  ingress_http_port          = var.ingress_http_port
  egress_proxy_port          = var.egress_proxy_port
  runtime_host_port_min      = var.runtime_host_port_min
  runtime_host_port_max      = var.runtime_host_port_max
  platform_role_policy_names = var.aliyun.platform_role_policy_names
  ssh_public_key             = local.effective_ssh_public_key
}

module "aliyun_platform_host" {
  count  = local.selected_provider == "aliyun" ? 1 : 0
  source = "../providers/aliyun/modules/platform-host"
  depends_on = [
    module.aliyun_network_base,
  ]

  zone_id                = var.aliyun.zone_id
  platform_name          = var.platform_name
  environment            = var.environment
  owner                  = var.owner
  vswitch_id             = module.aliyun_network_base[0].vswitch_id
  security_group_ids     = [module.aliyun_network_base[0].platform_security_group_id]
  role_name              = module.aliyun_network_base[0].platform_role_name
  platform_instance_name = var.aliyun.platform_instance_name
  instance_type          = var.aliyun.instance_type
  image_id               = var.aliyun.image_id
  key_pair_name          = module.aliyun_network_base[0].platform_key_pair_name
  system_disk_category   = var.aliyun.system_disk_category
  system_disk_size       = var.aliyun.system_disk_size
  user_data = base64encode(templatefile("${path.module}/../providers/aliyun/templates/user-data.sh.tftpl", {
    install_root                            = var.install_root
    platform_name                           = var.platform_name
    platform_instance_name                  = local.aliyun_platform_instance_name
    provider_name                           = "aliyun"
    region_id                               = var.aliyun.region_id
    platform_instance_type                  = var.aliyun.instance_type
    platform_node_system_reserved_cpu_milli = var.platform_node_system_reserved_cpu_milli
    platform_node_system_reserved_memory_mi = var.platform_node_system_reserved_memory_mi
    cloud_plane_grpc_port                   = var.cloud_plane_grpc_port
    cloud_plane_binary_url                  = var.cloud_plane_binary_url
    cloud_plane_binary_sha256               = var.cloud_plane_binary_sha256
    agent_binary_url                        = var.agent_binary_url
    agent_binary_sha256                     = var.agent_binary_sha256
    image_pull_registry_mirror              = local.effective_image_pull_registry_mirror
    ingress_http_port                       = var.ingress_http_port
    egress_proxy_port                       = var.egress_proxy_port
    runtime_host_port_min                   = var.runtime_host_port_min
    runtime_host_port_max                   = var.runtime_host_port_max
    subnet_cidr_block                       = var.subnet_cidr_block
    workload_log_loki_url                   = var.workload_log_loki_url
    workload_log_loki_tenant_id             = var.workload_log_loki_tenant_id
    workload_otlp_endpoint                  = var.workload_otlp_endpoint
    admin_token                             = local.effective_admin_token
    control_plane_southbound_token          = local.effective_control_plane_southbound_token
    node_agent_bootstrap_token              = local.effective_node_agent_bootstrap_token
    trust_auth_proxy_headers                = var.trust_auth_proxy_headers
    cloud_plane_config_seed_json            = local.cloud_plane_config_seed_json
  }))
  eip_name                 = var.aliyun.eip_name
  eip_bandwidth            = var.aliyun.eip_bandwidth
  eip_internet_charge_type = var.aliyun.eip_internet_charge_type
}

module "tencent_network_base" {
  count  = local.selected_provider == "tencent" ? 1 : 0
  source = "../providers/tencent/modules/network-base"

  region_id                  = var.tencent.region_id
  zone_id                    = var.tencent.zone_id
  platform_name              = var.platform_name
  environment                = var.environment
  owner                      = var.owner
  vpc_name                   = var.vpc_name
  vpc_cidr_block             = var.vpc_cidr_block
  subnet_name                = var.subnet_name
  subnet_cidr_block          = var.subnet_cidr_block
  security_group_name_prefix = var.security_group_name_prefix
  admin_cidrs                = var.admin_cidrs
  control_plane_cidrs        = var.control_plane_cidrs
  ingress_cidrs              = var.ingress_cidrs
  cloud_plane_grpc_port      = var.cloud_plane_grpc_port
  ingress_http_port          = var.ingress_http_port
  egress_proxy_port          = var.egress_proxy_port
  runtime_host_port_min      = var.runtime_host_port_min
  runtime_host_port_max      = var.runtime_host_port_max
  platform_role_policy_names = var.tencent.platform_role_policy_names
  ssh_public_key             = local.effective_ssh_public_key
}

module "tencent_platform_host" {
  count  = local.selected_provider == "tencent" ? 1 : 0
  source = "../providers/tencent/modules/platform-host"
  depends_on = [
    module.tencent_network_base,
  ]

  region_id              = var.tencent.region_id
  zone_id                = var.tencent.zone_id
  platform_name          = var.platform_name
  environment            = var.environment
  owner                  = var.owner
  platform_instance_name = var.tencent.platform_instance_name
  instance_type          = var.tencent.instance_type
  image_id               = var.tencent.image_id
  vpc_id                 = module.tencent_network_base[0].vpc_id
  subnet_id              = module.tencent_network_base[0].subnet_id
  security_group_ids     = [module.tencent_network_base[0].platform_security_group_id]
  key_ids                = [module.tencent_network_base[0].platform_key_id]
  system_disk_type       = var.tencent.system_disk_type
  system_disk_size       = var.tencent.system_disk_size
  cam_role_name          = module.tencent_network_base[0].platform_role_name
  user_data = templatefile("${path.module}/../providers/tencent/templates/user-data.sh.tftpl", {
    install_root                            = var.install_root
    platform_name                           = var.platform_name
    platform_instance_name                  = local.tencent_platform_instance_name
    provider_name                           = "tencent"
    region_id                               = var.tencent.region_id
    platform_instance_type                  = var.tencent.instance_type
    platform_node_system_reserved_cpu_milli = var.platform_node_system_reserved_cpu_milli
    platform_node_system_reserved_memory_mi = var.platform_node_system_reserved_memory_mi
    cloud_plane_grpc_port                   = var.cloud_plane_grpc_port
    cloud_plane_binary_url                  = var.cloud_plane_binary_url
    cloud_plane_binary_sha256               = var.cloud_plane_binary_sha256
    agent_binary_url                        = var.agent_binary_url
    agent_binary_sha256                     = var.agent_binary_sha256
    image_pull_registry_mirror              = local.effective_image_pull_registry_mirror
    ingress_http_port                       = var.ingress_http_port
    egress_proxy_port                       = var.egress_proxy_port
    runtime_host_port_min                   = var.runtime_host_port_min
    runtime_host_port_max                   = var.runtime_host_port_max
    subnet_cidr_block                       = var.subnet_cidr_block
    workload_log_loki_url                   = var.workload_log_loki_url
    workload_log_loki_tenant_id             = var.workload_log_loki_tenant_id
    workload_otlp_endpoint                  = var.workload_otlp_endpoint
    admin_token                             = local.effective_admin_token
    control_plane_southbound_token          = local.effective_control_plane_southbound_token
    node_agent_bootstrap_token              = local.effective_node_agent_bootstrap_token
    trust_auth_proxy_headers                = var.trust_auth_proxy_headers
    cloud_plane_config_seed_json            = local.cloud_plane_config_seed_json
  })
  user_data_replace_on_change    = var.tencent.user_data_replace_on_change
  eip_name                       = var.tencent.eip_name
  eip_type                       = var.tencent.eip_type
  eip_internet_charge_type       = var.tencent.eip_internet_charge_type
  eip_internet_max_bandwidth_out = var.tencent.eip_internet_max_bandwidth_out
}
