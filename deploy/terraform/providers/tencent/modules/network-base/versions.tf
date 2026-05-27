terraform {
  required_version = ">= 1.14.0"

  required_providers {
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
