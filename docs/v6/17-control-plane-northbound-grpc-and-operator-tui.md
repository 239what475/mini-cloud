# 17 control-plane northbound gRPC 与 operator TUI

回到本版路线图：

- [ROADMAP.md](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/docs/v6/ROADMAP.md)

## 这一章要解决什么

到
`v6/16`
为止，
`mini-cloud`
已经有了：

- `control-plane`
- 多个真实 `cloud-plane`
- 完整的长期服务主线
- 真实观测栈

但 operator
入口仍然停在：

- `control-plane`
  的手写 `HTTP`
  管理接口
- 一套很薄的
  `React + TypeScript`
  控制台

这对当前项目有两个问题：

- 主实现语言是 `Go`，
  再维护一套 `TS`
  operator UI
  收益很低
- 如果后面想做一个真正高密度、
  常驻终端可用的 operator
  工具，
  继续围着浏览器页面做会越来越别扭

所以这一章的目标很明确：

- `control-plane`
  operator northbound
  先收成
  `proto + gRPC`
- 再新增：
  - `cmd/tui`
  - `Go + Bubble Tea`
  - 作为新的 operator
    主入口

## 这一章的设计结论

### 1. TUI 不是替代 northbound API，而是新的 operator adapter

这一章不会让：

- `Bubble Tea`
  直接操作 store
- `cmd/tui`
  旁路 `control-plane`
  业务层

相反，
它只做一件事：

- 通过正式 northbound
  契约调用
  `control-plane`

也就是说，
新的层次应该是：

- `control-plane`
  内部服务 /
  store /
  controller
- `control-plane`
  northbound gRPC
- `cmd/tui`
  作为 operator
  交互入口

### 2. gRPC 是主入口，grpc-gateway 继续保留

这一章之后，
operator
主链以：

- `proto + gRPC`

为准。

但 `grpc-gateway`
不应该立刻删掉，
因为它仍然有价值：

- curl
  调试
- 教学展示
- shell
  脚本
- Terraform /
  外部自动化
  集成

所以这里的收口原则是：

- `gRPC`
  是主 northbound
  契约
- `grpc-gateway`
  是 HTTP
  适配层
- `TUI`
  是终端交互适配层

### 3. 第一版 TUI 只覆盖 operator 核心闭环

这一章第一版不会试图把所有页面都搬过去，
而是只做最核心的几个区域：

- `Overview`
  - 平台总览
  - plane /
    service /
    deployment
    状态
- `Planes`
  - plane
    列表
  - 详情
  - 最近同步状态
  - `latestRuntimeConfig`
- `Projects`
  - 项目列表
  - 项目详情
- `Services`
  - 服务列表
  - 服务详情
  - rollout
    基础动作

第一版不做：

- 花哨视觉设计
- 鼠标优先体验
- 复杂组件系统
- 全量表单覆盖
- 多窗口布局实验

### 4. control-plane 当前缺的是 northbound gRPC，不是 TUI 组件

当前仓库里：

- `cloud-plane`
  已经有：
  - `proto`
  - `gRPC`
  - `grpc-gateway`
- `control-plane`
  对 operator
  还主要是：
  - 手写 `HTTP`
    handler

所以这一章真正的第一步不是：

- 先写 Bubble Tea
  页面

而是：

- 先把 `control-plane`
  的 operator
  northbound
  收成正式 `proto + gRPC`

不然新的 TUI
  只会再绑死在一套 ad-hoc
  `REST`
  语义上。

### 5. `web/` 不再作为后续主入口继续演进

这一章之后，
`web/`
不再是后续主线投入方向。

处理原则是：

- 当前能跑的内容可以暂时保留
- 但不再继续扩新能力
- 新能力优先落到：
  - `control-plane`
    gRPC
  - `cmd/tui`

等 `TUI`
完成核心闭环后，
再决定是否彻底删除
`web/`
代码。

## 这一章的实现顺序

### 1. 先定义 control-plane operator proto

第一步先把最核心的 operator
对象收成 northbound proto：

- overview
- plane
- project
- service

并明确：

- 读接口
- 基础写接口
- rollout
  动作接口

### 2. 再实现 control-plane gRPC server

第二步才是在
`control-plane`
进程里真正提供：

- gRPC
- 必要的 `grpc-gateway`

同时保证：

- 不旁路现有 service /
  controller
- 语义和 store /
  controller
  保持一致

### 3. 最后落 `cmd/tui`

在 northbound gRPC
立稳之后，
再新增：

- [cmd/tui](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/cmd/tui)

第一版只做：

- overview
- plane list/detail
- project list/detail
- service list/detail
- create service
- rollout
  基础动作

## 这一章明确不做什么

这一章不做：

- 用户侧门户
- 浏览器控制台重设计
- 复杂主题系统
- 彻底移除所有 HTTP
  接口
- 域名 /
  TLS /
  CDN
  能力

## 这一章结束后的判断标准

如果这一章做对了，
应该能比较明确地回答：

1. operator
   主入口是不是已经从浏览器转到终端
2. `control-plane`
   有没有正式 northbound gRPC
3. 新的 TUI
   是否已经能完成核心 operator
   工作流
4. 后续新能力应该优先落到哪里：
   - `web`
   - 还是 `gRPC + TUI`

## 检查点

- 本章会先把：
  `control-plane`
  operator northbound
  收成正式 `gRPC`
- 本章会新增：
  [cmd/tui](/home/what/myproject/swe-tools-learn-etcd/projects/mini-cloud/cmd/tui)
  作为新的 operator
  主入口

## 当前使用方式

本章完成后，
本地可以直接启动：

```bash
cd projects/mini-cloud
go run ./cmd/tui \
  -addr http://127.0.0.1:8080 \
  -token "$MINICLOUD_ADMIN_TOKEN"
```

如果想让
`Services`
页默认落到某个项目，
可以再带上：

```bash
-project prj_xxxxxxxx
```

首版按键保持最小集合：

- `tab`
  切页
- `r`
  刷新
- `j` / `k`
  上下移动
- `s`
  在
  `Planes`
  页触发一次
  `sync`
- `enter`
  在
  `Projects`
  页切换当前
  service scope
- `n`
  在
  `Services`
  页进入
  create service
  表单
- `p`
  在
  `Services`
  页暂停 rollout
- `o`
  在
  `Services`
  页推进 rollout
- `a`
  在
  `Services`
  页中止 rollout
- `q`
  退出

当前首版表单只覆盖部署闭环所需的最小字段：

- `name`
- `displayName`
- `provider`
- `region`
- `replicas`
- `instanceClass`
- `exposure`
- `image`
- `defaultPort`
- `readinessPath`

这一版明确先不在
`TUI`
里做：

- service update
- env / config / secret / registry credential
- command / args
- front-door route
- delete service
