terraform {
  required_version = ">= 1.14.0"

  required_providers {
    tencentcloud = {
      source  = "tencentcloudstack/tencentcloud"
      version = "= 1.82.84"
    }
  }
}
