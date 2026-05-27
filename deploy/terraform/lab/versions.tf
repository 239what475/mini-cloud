terraform {
  required_version = ">= 1.14.0"

  required_providers {
    alicloud = {
      source  = "aliyun/alicloud"
      version = "= 1.274.0"
    }
    random = {
      source  = "hashicorp/random"
      version = "= 3.7.2"
    }
    tencentcloud = {
      source  = "tencentcloudstack/tencentcloud"
      version = "= 1.82.84"
    }
  }
}

provider "alicloud" {
  region = local.selected_provider == "aliyun" && trimspace(var.aliyun.region_id) != "" ? var.aliyun.region_id : "cn-beijing"

  // Terraform 会先初始化所有 provider 配置。
  // 这里给未选中的云填占位 AK/SK，避免它在本次 apply 中因为缺少真实凭证而提前失败。
  access_key = local.selected_provider == "aliyun" ? null : "unused"
  secret_key = local.selected_provider == "aliyun" ? null : "unused"
}

provider "tencentcloud" {
  region = local.selected_provider == "tencent" && trimspace(var.tencent.region_id) != "" ? var.tencent.region_id : "ap-beijing"

  // 同理，未选中的 provider 只需要完成初始化，不会真的去调用 API。
  secret_id  = local.selected_provider == "tencent" ? null : "unused"
  secret_key = local.selected_provider == "tencent" ? null : "unused"
}
