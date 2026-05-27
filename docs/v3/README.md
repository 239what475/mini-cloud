# Mini Cloud v3

`v3`
是 `mini-cloud` 从：

- 阿里云单 provider 的自安装平台

继续往前走到：

- `Terraform bootstrap + SDK runtime`
- 第二家 provider 接入

的这一条主线。

## 当前规则

- `docs/v3/ROADMAP.md`
  - 负责 `v3` 的总规划
- `docs/v3/01-aliyun-terraform-platform-module.md`
  - 负责 `v3/01` 的具体实现说明
- `docs/v3/02-aliyun-cloud-init-control-plane-install.md`
  - 负责 `v3/02` 的具体实现说明
- `docs/v3/03-aliyun-app-triggered-scale-out.md`
  - 负责 `v3/03` 的具体实现说明
- `docs/v3/04-aliyun-runtime-retry-and-reclaim.md`
  - 负责 `v3/04` 的具体实现说明
- `docs/v3/05-single-provider-boundary-refactor.md`
  - 负责 `v3/05` 的边界收口设计与实现说明
- `docs/v3/06-real-aliyun-end-to-end.md`
  - 负责 `v3/06` 的真实阿里云端到端验收说明
- `docs/v3/07-tencent-terraform-platform-module.md`
  - 负责 `v3/07` 的腾讯云 Terraform 平台底座设计与实现说明
- `docs/v3/08-tencent-control-plane-install-and-runtime-scale-out.md`
  - 负责 `v3/08` 的腾讯云 control-plane 安装与运行时扩容说明
- `docs/v3/09-real-tencent-end-to-end.md`
  - 负责 `v3/09` 的真实腾讯云端到端验收说明
- `docs/v3/10-unified-terraform-bootstrap-entry.md`
  - 负责 `v3/10` 的统一 Terraform bootstrap 入口说明
- `docs/v3/11-mini-cloud-terraform-entry.md`
  - 负责 `v3/11` 的 mini-cloud 平台资源 API 合同与 Terraform 入口设计
- `docs/v3/12-v3-tests-and-hardening.md`
  - 负责 `v3/12` 的测试分层与回归加固说明
- `docs/v3/13-mini-cloud-terraform-provider.md`
  - 负责 `v3/13` 的真正 Terraform provider 实现与使用说明
- `docs/v3/14-v3-final-review.md`
  - 负责 `v3/14` 的最终回看与 `v4` 起点说明
- 每一章结束时仍然单独做一个 `commit`
  - 作为章节检查点

## 当前章节

- `01-aliyun-terraform-platform-module`
- `02-aliyun-cloud-init-control-plane-install`
- `03-aliyun-app-triggered-scale-out`
- `04-aliyun-runtime-retry-and-reclaim`
- `05-single-provider-boundary-refactor`
- `06-real-aliyun-end-to-end`
- `07-tencent-terraform-platform-module`
- `08-tencent-control-plane-install-and-runtime-scale-out`
- `09-real-tencent-end-to-end`
- `10-unified-terraform-bootstrap-entry`
- `11-mini-cloud-terraform-entry`
- `12-v3-tests-and-hardening`
- `13-mini-cloud-terraform-provider`
- `14-v3-final-review`

当前做到 `14`，
`v3` 已经完成：

- 阿里云单 provider 真实端到端
- 腾讯云单 provider 真实端到端
- 统一 Terraform bootstrap 入口
- mini-cloud 平台资源 API 收口
- mini-cloud Terraform provider 第一版
- 本地检查 / 集成 / smoke 与真实云黑盒的分层验证

阿里云单 provider 这条线上已经同时具备：

- `Terraform`
  创建和销毁平台底座
- control-plane 运行时通过阿里云 `SDK`
  自动申请 worker
- runtime worker inventory / reclaim / platform teardown
- provider / service / Terraform contract
  已经做过一次正式收口
- `terraform destroy`
  先触发平台自清理，
  再销毁 control-plane 和底座
- 一套独立的真实阿里云黑盒验收入口
  - `cd projects/mini-cloud/tests && go run .`
  - 自动跑完整：
    - `apply -> 安装 -> API 验证 -> scale-out -> teardown -> destroy`

腾讯云这条线上现在已经完成：

- `07-tencent-terraform-platform-module`
- `08-tencent-control-plane-install-and-runtime-scale-out`
- `09-real-tencent-end-to-end`

从 `10`
开始，
`v3`
会把 bootstrap 侧的公开入口进一步统一成：

- `deploy/terraform/lab`

底层 provider module 仍然分开保留：

- `deploy/terraform/providers/aliyun/modules/*`
- `deploy/terraform/providers/tencent/modules/*`

也就是说，
平台底座的推荐正式入口会变成：

- `terraform init / plan / apply / destroy`
- `provider_name = "aliyun" | "tencent"`

而不是再额外包一层：

- `Go CLI -> terraform shell`

从 `11`
开始，
`v3`
会继续把平台资源入口本身收成：

- `project`
- `app`

这两类真正适合 Terraform 管理的声明式资源，
而不是直接把：

- `deployment`

当成 Terraform 主资源。

真实阿里云验收的具体说明见：

- [projects/mini-cloud/docs/v3/06-real-aliyun-end-to-end.md](./06-real-aliyun-end-to-end.md)

腾讯云 control-plane 与 runtime scale-out 的实现说明见：

- [projects/mini-cloud/docs/v3/08-tencent-control-plane-install-and-runtime-scale-out.md](./08-tencent-control-plane-install-and-runtime-scale-out.md)

真实腾讯云端到端验收的具体说明见：

- [projects/mini-cloud/docs/v3/09-real-tencent-end-to-end.md](./09-real-tencent-end-to-end.md)

统一 Terraform bootstrap 入口的具体说明见：

- [projects/mini-cloud/docs/v3/10-unified-terraform-bootstrap-entry.md](./10-unified-terraform-bootstrap-entry.md)

mini-cloud 平台资源 API 合同与 Terraform 入口设计见：

- [projects/mini-cloud/docs/v3/11-mini-cloud-terraform-entry.md](./11-mini-cloud-terraform-entry.md)

`v3` 的测试分层与加固说明见：

- [projects/mini-cloud/docs/v3/12-v3-tests-and-hardening.md](./12-v3-tests-and-hardening.md)

`v3` 的 mini-cloud Terraform provider 实现说明见：

- [projects/mini-cloud/docs/v3/13-mini-cloud-terraform-provider.md](./13-mini-cloud-terraform-provider.md)

`v3` 的最终回看见：

- [projects/mini-cloud/docs/v3/14-v3-final-review.md](./14-v3-final-review.md)

后续章节仍以：

- `projects/mini-cloud/docs/v3/ROADMAP.md`

为准。
