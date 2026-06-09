# Mini Cloud 总路线图

`ROADMAP.md`
现在只保留：

- 项目总方向
- 各版本主线的状态
- 跳转到分版本路线图的导航

更细的版本规划已经拆到：

- [projects/mini-cloud/docs/v1/ROADMAP.md](./v1/ROADMAP.md)
- [projects/mini-cloud/docs/v2/ROADMAP.md](./v2/ROADMAP.md)
- [projects/mini-cloud/docs/v3/ROADMAP.md](./v3/ROADMAP.md)
- [projects/mini-cloud/docs/v4/ROADMAP.md](./v4/ROADMAP.md)
- [projects/mini-cloud/docs/v5/ROADMAP.md](./v5/ROADMAP.md)
- [projects/mini-cloud/docs/v6/ROADMAP.md](./v6/ROADMAP.md)
- [projects/mini-cloud/docs/v7/README.md](./v7/README.md)

这样做的原因很直接：

- 总路线图负责回答：
  - 这个项目整体要往哪走
- 分版本路线图负责回答：
  - 这一版具体做什么
  - 不做什么
  - 章节怎么排
  - 应该怎么验证
- `v7`
  是源码重构阶段，
  不先写完整路线图，
  只保留轻量重构原则和检查点

## 项目定位

`mini-cloud` 想解决的问题是：

- 你手里有分布在不同 provider、不同 region 的少量机器
- 你想把这些异构节点抽象成统一资源池
- 再在上面做一个最小可用的平台控制面

它更像：

- 一个小型多地域应用平台

而不是：

- 一个完整公有云

## 当前结构约定

- `docs/ROADMAP.md`
  - 总路线图
- `docs/v1/ROADMAP.md`
  - `v1` 详细路线图
- `docs/v2/ROADMAP.md`
  - `v2` 详细路线图
- `docs/v3/ROADMAP.md`
  - `v3` 详细路线图
- `docs/v4/ROADMAP.md`
  - `v4` 详细路线图
- `docs/v5/ROADMAP.md`
  - `v5` 详细路线图
- `docs/v6/ROADMAP.md`
  - `v6` 详细路线图
- `docs/v7/README.md`
  - `v7` 源码重构原则和检查点
- `docs/v1/`
  - `v1` 教程文档
- `docs/v2/`
  - `v2` 教程文档
- `docs/v3/`
  - `v3` 教程文档
- `docs/v4/`
  - `v4` 教程文档
- `docs/v5/`
  - `v5` 教程文档
- `docs/v6/`
  - `v6` 教程文档
- `docs/v7/`
  - `v7` 源码重构记录

也就是说，
后面如果继续做：

- `v7`
  或更后面的版本

默认仍然保持版本目录结构。

但如果某个版本像
`v7`
一样是重构阶段，
可以只保留：

- `docs/v7/README.md`

## 当前版本线

### `v1`

`v1`
解决的是：

- 平台骨架能不能成立

这一版重点做了：

- control-plane 骨架
- node agent
- scheduler
- app / revision / deployment 状态机
- runtime 执行
- 基础入口
- 基础观测
- 基础配额和成本视图
- 测试和收尾加固

详细规划见：

- [projects/mini-cloud/docs/v1/ROADMAP.md](./v1/ROADMAP.md)

### `v2`

`v2`
解决的是：

- 平台能不能先把自己装到阿里云上
- 再在真实环境里部署、升级、重装、恢复和销毁

这一版重点做了：

- 阿里云单 provider `bootstrap`
- 平台自有基础设施自动创建
- control-plane 自动安装
- 真实环境运维
- 完整模拟环境 example
- 入口、`TLS`、运维与恢复

详细规划见：

- [projects/mini-cloud/docs/v2/ROADMAP.md](./v2/ROADMAP.md)

### `v3`

`v3`
当前的设计重点是：

- 前半段先把阿里云这条线做完整
- 平台自有基础设施改成由 `Terraform` 创建
- 平台运行时 worker 改成由 provider `SDK` 动态创建和回收
- 一套 control-plane 只绑定一个 provider
- 后半段再把腾讯云作为第二家 provider 接进来

当前阿里云这条线已经进一步落到了：

- `Terraform module / stack`
  作为正式 bootstrap 入口
- provider `SDK`
  作为运行时 worker 生命周期入口
- 单独的真实阿里云端到端黑盒验收
  - `cd projects/mini-cloud/tests && go run .`

这里要特别注意，
`v3`
不是：

- 一个 control-plane 同时管理多家云

而是：

- 同一套代码
- 同一套 Terraform 组织方式
- 部署前通过 provider 对应的 root env 选定后端
- 部署后这套 control-plane 只管理这一家云

详细规划见：

- [projects/mini-cloud/docs/v3/ROADMAP.md](./v3/ROADMAP.md)

### `v4`

`v4`
当前的设计重点是：

- 不把多云问题重新塞回单个 control-plane
- 保持“每套 plane 只绑定一个 provider”的边界
- 在上面增加一个 fleet 管理层
- 统一查看多套 plane 的：
  - health
  - capacity
  - app inventory
  - operation history
- 强化日志、指标、告警、SLO、审计和事故处理
- 再补一层更务实的高可用能力：
  - 备份
  - 恢复
  - standby
  - 演练

这里要特别注意，
`v4`
不是：

- 一个 control-plane 同时管理阿里云和腾讯云运行时资源

而是：

- 多套独立 control-plane
- 每套 control-plane 继续各自绑定单一 provider
- 上面再加一个统一入口和观测运维面

详细规划见：

- [projects/mini-cloud/docs/v4/ROADMAP.md](./v4/ROADMAP.md)

### `v5`

`v5`
当前的设计重点是：

- 把平台收成一个只支持长期在线服务的多云 `CaaS`
- 每个云作为一个：
  - `service cell`
- 每个 `service cell`
  至少包含：
    - `cloud-plane`
    - 开源 `gateway`
    - 小 `node`
    - 按需扩出来的更多 `node`
- `cloud-plane`
  的 northbound 管理接口收成：
  - `gRPC + grpc-gateway`
- fleet `control-plane`
  的 northbound 接口在这一版继续保留：
  - `HTTP JSON`
- 应用入口收成：
  - `Caddy`
- 应用观测入口收成：
  - `OTel Collector`
- 重点补齐：
  - `service`
    资源模型
  - 服务暴露
  - 发布与回滚
  - 扩缩容
  - 应用侧观测
  - 项目级约束与成本视图
- 最终收成：
  - 一个完整可搭建的平台结果
  - 包含：
    - 外部 `front door`
      前门
    - 双云固定入口机
    - 双云 `service cell`

这里要特别注意，
`v5`
不是：

- 一个继续扩 workload 类型的实验
- 双 `control-plane`
  对等同步
- 正式多活 `control-plane`
- `job / cron / oneoff`
- 有状态服务主线
- 第三家 provider

而是：

- 在现有多云底座上，
  把长期在线服务这条主链做厚、做完整、做得更像一个真的可演示 `CaaS`

详细规划见：

- [projects/mini-cloud/docs/v5/ROADMAP.md](./v5/ROADMAP.md)

### `v6`

`v6`
当前的设计重点是：

- 不再继续扩 workload 类型和 provider
- 把 `mini-cloud`
  从“已经能跑通的多云 `CaaS`”
  往“更可运营、更可收敛、更可恢复的平台”
  推进一步
- 重点补齐：
  - controller / reconcile
    收敛
  - 服务拓扑和更安全的发布
  - front door
    健康路由与跨 cell
    故障切换
  - fleet `control-plane`
    的生产化部署与务实高可用
  - 身份、安全和轮转
  - `day-2`
    运维与 operator 级可观测性

这里要特别注意，
`v6`
不是：

- 新 workload 产品线
- 第三家 provider
- `service mesh`
- 支付、对象存储、数据库等新平台产品

而是：

- 围绕现有多云 `CaaS`
  主线做一次真正的生产化硬化

详细规划见：

- [projects/mini-cloud/docs/v6/ROADMAP.md](./v6/ROADMAP.md)

### `v7`

`v7`
当前的设计重点是：

- 不继续新增产品功能
- 不新增 provider
- 专门重构源码结构和工程边界
- 把前面版本演进留下的历史命名、旧边界和冗余实现清理掉

这里要特别注意，
`v7`
不是：

- 一条新产品主线
- 一份完整章节式路线图
- 为旧 API 保留兼容的迁移版本

而是：

- 一个按源码边界逐步推进的重构阶段

检查点记录见：

- [projects/mini-cloud/docs/v7/README.md](./v7/README.md)

## `v7` 之后的方向

如果 `v7`
把：

- 多云 `CaaS`
  主线
- 应用模型
- 运行时能力
- 生产化运维能力
- 源码结构和工程边界

都做稳了，
那后面更自然的方向才会是：

- 更复杂的全局流量与入口能力
- 更正式的多 `control-plane` 高可用
- 更复杂的租户、配额和成本系统
- 第三家 provider

## 建议入口

1. `projects/mini-cloud/README.md`
2. `projects/mini-cloud/docs/ROADMAP.md`
3. `projects/mini-cloud/docs/v1/ROADMAP.md`
4. `projects/mini-cloud/docs/v2/ROADMAP.md`
5. `projects/mini-cloud/docs/v3/ROADMAP.md`
6. `projects/mini-cloud/docs/v4/ROADMAP.md`
7. `projects/mini-cloud/docs/v5/ROADMAP.md`
8. `projects/mini-cloud/docs/v6/ROADMAP.md`
9. `projects/mini-cloud/docs/v7/README.md`
10. `projects/mini-cloud/docs/v1/README.md`
