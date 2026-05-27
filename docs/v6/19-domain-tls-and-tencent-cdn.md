# 19. 域名、TLS 与腾讯云 CDN

到了
`v6/18`
为止，
平台已经能把真实服务输入收进：

- `env`
- `projectedFiles`
- `persistentDirs`

但用户流量入口这条链，
还没有正式收完。

这一章原本尝试过：

- 腾讯云 CDN
  ->
  统一 `front-door`
  ->
  各 plane

但在这一章的设计复盘之后，
最终定下来的正式方向不是这条链，
而是：

- 腾讯云 CDN
  直接按规则
  回源到对应 plane
  的入口机

也就是说，
这一章最终要收的是：

- 外部域名
- CDN
- plane 入口机
- plane 内网关

这四层之间的正式关系。

这里要明确一点：

- `v6/07`
  和
  `v6/09`
  记录的是
  已经完成并冻结的
  历史实现版本
- 这一章从设计层面，
  正式覆盖那套外部入口模型
- 从这一章开始，
  后续章节一律按新的入口设计推进

也就是说：

- 旧实现章节保留，
  但不再代表后续主线
- 新主线从这一章开始，
  改成：
  - CDN
    直接回源到各 plane
    入口机

## 这一章只做什么

这一章明确只做：

- 明确域名角色
- 明确 TLS
  终止边界
- 明确腾讯云 CDN
  作为边缘流量入口
  的职责
- 明确各 plane
  入口机作为真实源站
  的职责
- 明确服务通过子域名
  对外暴露
  的正式设计
- 明确
  `cliproxyapi`
  作为本章验收任务

这一章明确不做：

- 多云全局负载均衡
- 自动故障切流
- 智能流量调度
- 证书自动申请
- 复杂的 CDN
  高级策略编排

## 这一章的正式链路

这一章把正式链路定成：

- 用户
  ->
  腾讯云 CDN
  ->
  目标 plane
  的 `publicOrigin`
  ->
  plane 内 `Caddy`
  ->
  service backend

这意味着：

- CDN
  不再把流量先送到
  一个统一入口机
- 每个 plane
  的入口机
  才是这个云里的真实公网源站
- CDN
  负责把：
  - 哪个服务子域名
  回源到哪个 plane
- plane
  入口机负责把：
  - 这个外部域名
  再转给本 plane
  里的真实 service

## 为什么这一章改成 CDN 直达 plane

原因很直接：

- 统一 `front-door`
  会多一跳
- 用户请求会先跨云到统一入口机，
  再跨云回到真正承载服务的 plane
- 对我们这个个人项目来说，
  这层额外跳转
  带来的复杂度
  和流量路径
  都不划算

而按子域名暴露服务，
对当前项目更合适：

- 当前 plane gateway
  本来就更接近
  按 `Host`
  暴露服务
- 很多服务天然适合跑在：
  - `/`
  而不是
  - `/cpi`
    这种子路径
- CDN
  侧按域名接入和回源，
  配置也更直接

对这个项目来说，
虽然它有规则数量上限，
但这是可以接受的：

- 这是个人项目
- 服务数量不会很多
- 当前更重要的是把流量链做薄，
  而不是一开始就追求大规模能力

所以这一章直接选：

- CDN 作为边缘分流层
- plane 入口机作为真实源站

## 当前仓库里 `front-door` 的定位

这一章前一轮实现里，
仓库里一度有过：

- `pkg/frontdoorproxy`
- `cmd/front-door`
- `deploy/front-door/`

这一整套资产。

它们对应的是旧的
统一 `front-door`
原型，
现在已经从主树移除。

但从这一章开始生效的
新主线设计看：

- 它不是主链
- 也不是必须组件
- 也不再进入当前仓库
  的正式代码 /
  release
  产物

所以这一章的语义是：

- `front-door`
  对应的是前一版
  已实现过的模型
- CDN
  直达各 plane
  是从这一章开始
  覆盖旧模型的
  新设计主线

后续章节如果继续沿着
CDN
直达 plane
这条路线推进，
那真正需要增强的，
是：

- plane 入口机
  的外部路由能力

而不是继续把
统一 `front-door`
做厚。

## 域名角色

这一章把域名明确拆成三类。

### 1. 管理域名

例如：

- `console.example.com`

它只给：

- `control-plane`
- `operator`
- `TUI`

使用。

它不走 CDN。

### 2. 服务外部子域名

例如：

- `cliproxyapi.example.com`
- `grafana.example.com`

它们是用户真正访问的业务入口。

它们接到：

- 腾讯云 CDN

上。

### 3. plane origin 域名

例如：

- `tx-plane-origin.example.com`
- `ali-plane-origin.example.com`

它们分别对应：

- 腾讯云 plane
  的 `publicOrigin`
- 阿里云 plane
  的 `publicOrigin`

它们主要给：

- 腾讯云 CDN
  回源
- operator
  绕过 CDN
  调试

使用。

## 域名到底要不要绑到入口机

这一章把这个问题直接定死：

- 业务域名
  例如
  `cliproxyapi.example.com`
  不应该直接解析到入口机
- 它应该解析到
  腾讯云 CDN
  分配的
  `CNAME`

也就是说：

- 用户只访问业务域名
- 业务域名先接 CDN

而每个 plane
  的入口机，
则可以按需绑定自己的
origin
域名，
例如：

- `tx-plane-origin.example.com`
- `ali-plane-origin.example.com`

这两个域名
或它们对应的公网 IP，
才是 CDN
真正回源时使用的目标。

## TLS 边界

这一章把 TLS
边界写成：

- 客户端
  到
  腾讯云 CDN
  - CDN
    边缘证书
- CDN
  到
  plane origin
  - 这一版允许
    HTTP
    或 HTTPS
    二选一
- operator
  到
  `control-plane`
  - 走独立管理域名
  - 不混进 CDN

这意味着：

- 用户侧 TLS
  先收在 CDN
- plane
  入口机
  是否继续用 HTTPS，
  取决于后续运维选择
- 这一章先把流量分层和源站关系收干净，
  不在这里一次性塞完
  所有证书自动化

## 腾讯云 CDN 自动化边界

这一章里，
腾讯云 CDN
负责：

- 外部域名接入
- 边缘 TLS
- 子域名级回源配置
- 必要时的缓存刷新

对
`mini-cloud`
来说，
可以自动化的最小闭环是：

- 新增加速域名
- 更新主源站
- 更新加速域名的源站配置
- 查询域名状态
- 查询 CDN
  分配的
  `CNAME`
- 在 DNSPod
  上确保业务子域名
  `CNAME`
  指向 CDN
  分配的地址

这一章当前已经落地的
控制面自动化边界是：

- operator /
  TUI
  调用
  `BindServiceDomain`
- control-plane
  先把域名绑到目标 plane
- 然后直接调用
  腾讯云 CDN
  和 DNSPod
  做幂等发布

也就是说，
对当前主线来说，
“绑定 service 域名”
不再只是 plane 内部动作，
而是正式入口发布动作。

对应到腾讯云官方 API，
这一章值得关注的是：

- `AddCdnDomain`
- `UpdateDomainConfig`
- `DescribeDomains`
- `StartCdnDomain`
- `CreateRecord`
- `ModifyRecord`
- `DescribeRecordList`

如果后面还要自动管证书，
再额外补：

- `UpdateDomainHttps`

如果域名本身托管在
腾讯云 DNSPod，
并希望直接在腾讯云侧
一键配置
`CNAME`，
还需要域名解析写权限。

最简单的做法是：

- 对应 CDN
  域名的写权限
- `QcloudDNSPodFullAccess`

## 这一章对腾讯云 CDN 的正式使用方式

这一章不再要求：

- CDN
  回源到统一
  `front-door`

而是要求：

- 每个服务使用自己的外部子域名
- 这些子域名
  直接经 CDN
  回源到对应 plane
  的入口机

例如：

- `cliproxyapi.example.com`
  回源到
  腾讯云 plane
- `grafana.example.com`
  回源到
  阿里云 plane

这意味着腾讯云 CDN
在这一章里，
承担的是：

- 边缘入口
- 域名级流量分流

而 plane
入口机承担的是：

- 本 plane
  内真正的外部路由

## 这一章对 plane gateway 的要求

既然统一 `front-door`
不再是正式主链，
那这一章正式要求
plane 入口机上的网关具备：

- 接收外部业务域名的请求
- 按：
  - 外部域名
  做本 plane
  内转发
- 把请求送到
  对应 service
  的当前后端

也就是说，
这一章最终依赖的是：

- plane gateway
  能直接理解外部入口语义

而不是：

- 先由统一
  `front-door`
  把外部语义翻译一遍

## 真实验证矩阵

这一章的真实验证不追求一把梭，
而是分层验证。

### 1. 管理域名

- `control-plane`
  管理域名
  可达
- `operator`
  和 `TUI`
  能正常连

### 2. plane origin

- 每个 plane
  的 `publicOrigin`
  可达
- 绕过 CDN
  直接打 plane
  origin
  时，
  能做调试验证

### 3. CDN

- 服务子域名
  经过 CDN
  可达
- 不同服务子域名
  能被腾讯云 CDN
  回源到不同 plane
  的入口机
- 发布 /
  回滚后，
  origin
  直连结果正确
- 需要时手动刷新 URL
  或目录后，
  CDN 访问结果正确

## 本章验收任务

这一章的明确验收任务，
定成：

- 在平台里创建一个
  `cliproxyapi`
  service
- 把它放到某个目标 plane
- 给它分配一个
  外部子域名
- 让外部访问：
  - `https://cliproxyapi.example.com/`
  时，
  能直接经 CDN
  回源到这个 plane
  的入口机
- 再由这个 plane
  的网关
  把流量转给
  `cliproxyapi`
  容器

### 1. 创建 `cliproxyapi` service

- service
  本身要成功调度
- service
  `exposure`
  必须是：
  - `public`
- plane
  侧要已经有：
  - `publicOrigin`

### 2. 配置腾讯云 CDN 域名

- `cliproxyapi.example.com`
  已经接到 CDN
- 它的源站
  直接指向
  `cliproxyapi`
  所在 plane
  的入口机

### 3. 验收通过条件

- `cliproxyapi`
  service
  在目标 plane
  上运行正常
- `https://cliproxyapi.example.com/`
  经过 CDN
  访问时，
  能稳定到达
  对应 plane
  的入口机
- 再由该 plane
  的网关
  正确转到
  `cliproxyapi`
  后端
- 整个链路不需要再经过
  统一 `front-door`
  入口机

## 这一章完成后的平台边界

这一章收完以后，
平台应该明确形成下面这条正式链：

- 管理面
  有独立域名
- 用户流量
  有统一外部域名
- 腾讯云 CDN
  负责边缘入口
  和子域名级分流
- 各 plane
  入口机
  负责本云真实源站能力
- plane 内网关
  负责把请求
  转给对应 service

这也意味着：

- CDN
  不只是边缘 TLS
  终止点
- 它还是这一章里的
  第一级流量分流层
- 统一 `front-door`
  不再是正式主链

还有一个真实部署前提
必须明确：

- plane gateway
  既然要直接承接：
  - `80`
  - `443`
- 那么长期运行的
  `cloud-plane`
  进程就必须具备
  低端口绑定能力

在当前仓库里，
正式收口方式就是：

- `systemd`
  单元显式授予
  `CAP_NET_BIND_SERVICE`

否则第一次为
public service
重载 gateway
之后，
`cloud-plane`
会因为：

- `listen tcp :443: bind: permission denied`

直接退出，
从而把这一章的
域名发布链打断。

## 真实验收结果

这一章已经完成了
真实环境里的三层验证。

下面所有真实环境描述，
都统一用占位域名表示，
例如：

- `service-a.example.com`
- `tx-origin.example.com`

不在文档里记录真实生产域名。

### 1. plane southbound 域名链路

真实验收里，
最后补上的不是
CDN 规则本身，
而是
`control-plane`
到
`cloud-plane`
的域名 southbound
链路。

最终正式实现是：

- `control-plane`
  使用
  plane southbound token
- 调用
  `PlaneService`
  新增的：
  - `ListPlaneServiceDomainBindings`
  - `CreatePlaneServiceDomainBinding`
- 不再借用
  northbound
  `ServiceService`

这一步的意义是：

- 域名绑定
  继续留在
  control-plane
  -> plane
  这条受管链路里
- 不再混用
  用户面
  northbound API
  的鉴权边界

### 2. origin 直连验证

真实环境里，
`service-a`
已经成功跑在
腾讯云 plane
上，
并且：

- operator
  `GetServiceIngress`
  返回 `200`
- service
  当前已有：
  - 平台托管域名
  - 自定义域名
- 直接请求：
  - `tx-origin.example.com`
    并携带
    `Host: service-a.example.com`
  已经返回
  `200`

这说明下面这条链已经打通：

- 自定义域名
  ->
  plane gateway
  ->
  `cliproxyapi`
  backend

也就是说，
平台内部的
域名绑定 /
网关路由 /
源站转发
已经成立。

### 3. 腾讯云 CDN 实际阻塞点

真实环境里，
当平台尝试把：

- `service-a.example.com`

发布到腾讯云 CDN
时，
腾讯云返回：

- `ResourceUnavailable.CdnHostNoIcp`

这不是
`mini-cloud`
代码错误，
而是腾讯云 CDN
对域名备案的
真实前置要求。

也就是说，
当前这章的真实状态是：

- plane 内域名绑定
  成功
- origin
  直连验证
  成功
- CDN 自动发布代码
  已经打通到真实 API
- 但最终公网
  CDN 域名上线
  还需要域名先完成备案

## 备案与访问限制

这一章在真实环境里，
额外暴露出一个必须单独记下来的前置条件：

- 域名解析
  和
  中国内地可正式对外提供
  Web/CDN
  服务，
  不是一回事

### 1. 备案前可以做什么

备案完成前，
仍然可以做：

- 配置 DNS
  记录
- 做 plane 内部
  域名绑定
- 用
  `origin`
  域名
  或公网 IP
  做 operator
  侧调试
- 验证：
  - plane gateway
  - service backend
  - southbound
    域名发布
    链路

也就是说，
平台内部的：

- 绑定
- 发布
- 回源配置
- 网关转发

这些逻辑，
都可以先行验证。

### 2. 备案前不能把它当正式公网入口

如果目标是：

- 腾讯云中国内地 CDN
- 或中国内地源站
  通过正式业务域名
  对外提供访问

那么备案前，
不应把这条链当成正式公网验收通过。

这一章在真实环境里
已经确认：

- 腾讯云中国内地 CDN
  对未满足备案条件的域名，
  会直接拒绝创建加速域名
- 即使不走 CDN，
  中国内地源站域名访问
  也不应在备案前
  作为正式上线形态

所以这一章的真实状态
要严格区分：

- 平台实现完成
- 平台公网正式验收
  受备案前置条件阻塞

### 3. 备案资源和源站 IP 不是同一个概念

这一章还要明确一个很容易混淆的问题：

- 备案依托资源
  不等于
  最终业务源站 IP

备案关注的是：

- 这个域名通过哪家
  中国内地云服务商
  提供接入能力

而不是：

- 文档里是不是把所有
  回源 IP
  都逐个登记进去

对当前主线来说，
更准确的理解是：

- 腾讯云中国内地 CDN
  这一侧，
  要满足腾讯云可识别的备案条件
- 如果后面某个域名
  还要直接通过阿里云
  中国内地源站
  对外提供正式访问，
  那阿里云接入备案
  也要单独评估

### 4. 多云情况下的实际顺序

对当前项目最实际的顺序是：

1. 先在一个接入商侧
   完成首次备案
2. 让腾讯云中国内地 CDN
   具备可接入条件
3. 如果后面同一个域名
   还要直接使用另一家
   中国内地云厂商
   的正式入口，
   再补对应的
   接入备案

也就是说，
对我们这种多云架构，
备案不会阻塞：

- 平台内部开发
- `origin`
  调试
- 自动化代码实现

但会阻塞：

- 最终正式公网域名
  上线验收

### 4. 真实部署里的一个实际注意点

真实环境替换
`control-plane`
和
`cloud-plane`
二进制时，
不能只把文件覆盖到：

- `/opt/mini-cloud/bin/`

因为 systemd
长期运行单元实际用的是：

- `/opt/mini-cloud/control-plane/bin/control-plane`
- `/opt/mini-cloud/cloud-plane/bin/cloud-plane`

并且替换运行中的
二进制时，
要走：

- 先写临时文件
- 停服务
- 原子替换
- 再启动

否则会遇到：

- `Text file busy`

## 检查点

- 本章已经明确：
  `v6/19`
  开始覆盖此前的
  统一 `front-door`
  入口设计
- `v6/07`
  和
  `v6/09`
  作为冻结的历史实现章节保留，
  但不再代表后续主线
- 本章最终正式设计已经改成：
  腾讯云 CDN
  直接回源到各 plane
  入口机
- 本章文档已经把：
  - 管理域名
  - CDN 外部域名
  - plane origin
  三类域名角色
  写清
- 本章文档已经把：
  - 腾讯云 CDN
    的自动化边界
  - DNSPod
    的最小自动化闭环
  - 最小 CAM 权限
  - CDN 直达 plane
    的责任划分
  写清
- 本章实现已经支持：
  `BindServiceDomain`
  触发
  腾讯云 CDN +
  DNSPod
  的幂等发布
- 本章真实环境已经验证：
  `PlaneService`
  southbound
  域名绑定链路
  可以成功工作
- 本章真实环境已经验证：
  `service-a.example.com`
  作为
  plane 自定义域名
  已经可以在
  origin
  侧返回
  正确内容
- 本章真实环境已经确认：
  腾讯云 CDN
  最终公网发布
  当前会被
  `ResourceUnavailable.CdnHostNoIcp`
  挡住，
  需要域名先备案
- 本章文档已经把：
  `cliproxyapi`
  通过
  `cliproxyapi.example.com`
  对外暴露
  作为本章验收任务
  写清
- 本章文档已经明确：
  仓库里已有的
  `front-door`
  资产保留为原型 /
  调试路径，
  但不再作为这一章
  的正式生产主链
