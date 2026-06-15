terraform {
  required_version = ">= 1.14.0"

  required_providers {
    alicloud = {
      source  = "aliyun/alicloud"
      version = "= 1.274.0"
    }
  }
}

provider "alicloud" {
  region  = var.aliyun.region_id
  profile = var.aliyun_profile
}
