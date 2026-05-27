# 13 Mini Cloud Terraform Provider

这一章把 Terraform provider 收口成真正适合用户侧使用的形态：

- provider 配置只保留：
  - `base_url`
  - `token`
- provider 只管理：
  - `minicloud_service`
- provider 不再直接管理：
  - `project`

也就是说，
这一版的 Terraform 不再假设你拿着平台管理员令牌来“造项目”，
而是假设：

1. 你已经有自己的 `project`
2. 你已经为这个 `project` 签发了一个长期使用的 `project api token`
3. Terraform provider 用这个 token 先调用：
   - `GET /api/v1/auth/whoami`
4. 再自动解析出它绑定的唯一 `project`
5. 之后所有 `service` 的创建、更新、删除都落到这个 `project` 下

这样边界会更干净：

- 平台侧负责：
  - 建 project
  - 发 token
- 用户侧 Terraform 负责：
  - 管自己项目里的 service

## 上一章检查点

- `v3/12`
  - `3409e99d42185ed9dfdb3e58fd1b7d21308c99bd`

## 这一章的目标

这一章完成以后：

- `mini-cloud`
  会提供一个真正可被 Terraform 直接加载的 provider 插件
- provider 启动时会自动做一次 `whoami`
- 只有：
  - `project api token`
  才能被接受
- `service`
  不再要求手填：
  - `project_id`

这里明确不保留旧模型：

- `admin_token`
- `minicloud_project`
- `minicloud_service.project_id`

## 代码放在哪

### 1. provider 进程入口

- [main.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/cmd/terraform-provider-mini-cloud/main.go)

它负责把 Terraform provider 插件进程真正跑起来。

### 2. provider 实现

- [provider.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/terraformprovider/provider.go)
- [client.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/terraformprovider/client.go)
- [service_resource.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/terraformprovider/service_resource.go)

这里的职责也比较直接：

- `provider.go`
  - 定义 provider 配置
  - 只注册 `minicloud_service`
- `client.go`
  - 负责和 northbound HTTP API 通信
  - 在配置阶段先调用 `whoami`
  - 校验 token 是否真的是 `project api token`
- `service_resource.go`
  - 负责 `minicloud_service`

## 为什么这里不用 human user token

这一章故意不把 Terraform provider 设计成“吃人类登录态 token”，
而是要求使用：

- `project api token`

原因很简单：

- Terraform 是自动化执行者，不是人类交互会话
- `project api token`
  本身就天然是：
  - 项目级作用域
  - 长期稳定
  - 适合自动化
- `whoami`
  已经会返回：
  - `principal.kind = service_account`
  - `principal.serviceAccount.scope = project`
  - `principal.serviceAccount.source = project_token`
  - `principal.serviceAccount.projectID`

所以 provider 不需要再查项目列表，
也不需要再猜“当前 token 想操作哪个项目”。

## token 从哪里来

这章没有把“签发 token”塞进 provider 里，
因为那属于平台 bootstrap 或项目管理动作。

这里仍然沿用已有接口：

```text
POST /api/v1/projects/{projectID}/actions/issue-token
```

也就是说，
推荐流程是：

1. 平台管理员或项目 owner 先建好 project
2. 给这个 project 签发一个长期使用的 Terraform token
3. 用户把这个 token 放进 Terraform provider 配置里

## provider 现在怎么用

本地开发时，
先把 provider 二进制编出来：

```bash
cd projects/mini-cloud
go build -o /tmp/mini-cloud-tf/terraform-provider-mini-cloud ./cmd/terraform-provider-mini-cloud
cp /tmp/mini-cloud-tf/terraform-provider-mini-cloud /tmp/mini-cloud-tf/terraform-provider-mini-cloud_v0.0.0
```

然后给 Terraform 一份本地开发用的 CLI 配置：

```hcl
provider_installation {
  dev_overrides {
    "local/mini-cloud" = "/tmp/mini-cloud-tf"
  }
  direct {
    exclude = ["local/mini-cloud"]
  }
}
```

再写一份最小 Terraform 配置：

```hcl
terraform {
  required_providers {
    minicloud = {
      source = "local/mini-cloud"
    }
  }
}

provider "minicloud" {
  base_url = "http://127.0.0.1:18080"
  token    = "replace-with-your-project-token"
}

resource "minicloud_service" "hello" {
  name              = "hello"
  display_name      = "Hello"
  region            = "cn-beijing"
  replicas          = 1
  instance_class    = "small"
  exposure          = "public"
  image             = "nginx:1.27-alpine"
  command           = ["nginx"]
  args              = ["-g", "daemon off;"]
  default_port      = 8080
  readiness_path = "/healthz"

  env = {
    LOG_LEVEL = "info"
  }
}
```

然后显式指定这份 CLI 配置去执行：

```bash
TF_CLI_CONFIG_FILE=/path/to/terraform.tfrc terraform apply
```

这里要注意：

- 在 `dev_overrides`
  模式下，
  不要先跑 `terraform init`
- 直接跑：
  - `terraform apply`
  - `terraform plan`
  - `terraform import`
  - `terraform destroy`
  就可以了

## `minicloud_service` 支持什么

这一版 `minicloud_service` 已经支持：

- create
- read
- update
- delete
- import

这一版里，
下面这些字段按资源身份处理：

- `name`

也就是说，
改它会触发 replace。

其他常用运行规格，
例如：

- `display_name`
- `region`
- `replicas`
- `instance_class`
- `exposure`
- `image`
- `command`
- `args`
- `default_port`
- `readiness_path`
- `env`
- `config_set_id`
- `secret_set_id`
- `registry_credential_id`

都会走正常 update。

同时，
provider 也会回读一部分运行态字段：

- `domain_hosts`
- `current_release_id`
- `current_release_label`
- `candidate_release_id`
- `candidate_release_label`
- `rollout_phase`
- `rollout_message`
- `status_phase`
- `healthy`
- `status_message`

## provider 会拒绝哪些 token

为了避免作用域不清，
provider 当前只接受：

- `project api token`

所以这些 token 会被直接拒绝：

- `break-glass admin token`
- `platform-scoped service account token`
- 其他来源伪装成 `scope=project` 的 service account token
- `human user` 身份令牌
- 解析后没有 `project_id` 的异常 token

也就是说，
provider 的定位现在非常明确：

- 它不是平台管理入口
- 它只是某个 project 的 service 管理器

## 这一章怎么验证

这一章没有只停在：

- `go build`

而是补了一套真正调用 Terraform CLI 的黑盒测试：

- [provider_cli_test.go](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/internal/terraformprovider/provider_cli_test.go)

这套测试现在验证的是三类场景：

1. `project token -> service create/update/destroy`
2. `project token -> service import/read/destroy`
3. 非法 token 类型会在 provider 配置阶段直接失败

这里虽然还是用 fake API server，
但 Terraform 侧走的是：

- 真 provider 二进制
- 真 Terraform CLI

所以它验证的已经是比较接近真实使用方式的闭环。
