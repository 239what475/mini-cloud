terraform {
  required_version = ">= 1.14.0"

  required_providers {
    alicloud = {
      source  = "aliyun/alicloud"
      version = "= 1.274.0"
    }
    tencentcloud = {
      source  = "tencentcloudstack/tencentcloud"
      version = "= 1.82.84"
    }
  }
}

provider "alicloud" {
  region  = local.selected_provider == "aliyun" && trimspace(var.aliyun.region_id) != "" ? var.aliyun.region_id : "cn-beijing"
  profile = local.selected_provider == "aliyun" ? var.aliyun_profile : null

  access_key = local.selected_provider == "aliyun" ? null : "unused"
  secret_key = local.selected_provider == "aliyun" ? null : "unused"
}

provider "tencentcloud" {
  region = local.selected_provider == "tencent" && trimspace(var.tencent.region_id) != "" ? var.tencent.region_id : "ap-beijing"

  // 同理，未选中的 provider 只需要完成初始化，不会真的去调用 API。
  secret_id  = local.selected_provider == "tencent" ? null : "unused"
  secret_key = local.selected_provider == "tencent" ? null : "unused"
}
