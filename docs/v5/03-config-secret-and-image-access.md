# v5/03 Config Secret And Image Access

`v5/02`
已经把：

- `service`
  主对象
- northbound management API
- `gRPC + grpc-gateway`
  单轨

收下来了。

但那时的 `service spec`
还只是一个“能描述服务壳子”的对象，
离真正把容器跑起来还差三类输入：

1. 非敏感配置
2. 敏感配置
3. 私有镜像拉取凭据

所以这一章要做的事很明确：

- 把 `service spec`
  里和运行输入直接相关的部分补齐
- 把项目级共享配置资源收出来
- 让平台第一次能比较自然地承接：
  - 公共配置
  - 密文配置
  - 私有镜像拉取

这一章不做：

- 内建 `Harbor`
- 自己托管镜像仓库
- 镜像构建、推送、复制、扫描
- 外部 `secret manager`
  集成
- 云厂商专有仓库模型
  下沉进平台主资源

也就是说：

- 阿里云 `ACR`
- 腾讯云 `TCR`

都只作为实验时可对接的私有仓库，
而不是平台自己的资源类型。

## 这一章之后，service spec 增加了什么

当前 `service.spec`
在原来字段基础上，
补了三类引用和一类内联输入：

- `env`
- `configSetID`
- `secretSetID`
- `registryCredentialID`

它们各自的职责要分清：

### `env`

`env`
是跟单个服务规格直接绑在一起的内联键值。

它适合放：

- 端口
- 模式开关
- 少量服务专属配置

它不适合承担：

- 同一项目下多个服务共享的大块公共配置
- 密文配置

### `configSetID`

`configSet`
是项目级的非敏感配置集合。

它的目的不是替代 `env`，
而是补一层“可复用的公共配置”：

- 一个项目里可以先创建多个 `config set`
- 一个服务通过 `configSetID`
  引用其中一个
- 这样同项目下多个服务可以共用同一组普通配置

当前模型里，
`configSet`
是项目资源，
不是服务子资源。

### `secretSetID`

`secretSet`
是项目级的敏感键值集合。

它和 `configSet`
最大的区别是：

- `secretSet`
  的值不会在读取接口里回显
- 接口只返回：
  - `id`
  - `projectID`
  - `name`
  - `keys`

也就是说：

- 平台允许你声明“这个服务要注入哪一组密文键”
- 但不会把密文值再读出来

### `registryCredentialID`

`registryCredential`
表示一组私有镜像拉取凭据。

它也是项目级资源，
当前最小字段是：

- `name`
- `server`
- `username`
- `password`

读取时只返回：

- `passwordConfigured`

而不会回显密码本身。

它的职责也必须说清：

- 它只用于节点拉镜像
- 不会被自动注入容器环境变量

## 当前资源模型长什么样

这一章之后，
项目级运行输入资源有三类：

1. `configSet`
2. `secretSet`
3. `registryCredential`

对应的 `HTTP/JSON`
接口是：

- `GET /api/v1/projects/{projectID}/config-sets`
- `POST /api/v1/projects/{projectID}/config-sets`
- `GET /api/v1/projects/{projectID}/secret-sets`
- `POST /api/v1/projects/{projectID}/secret-sets`
- `GET /api/v1/projects/{projectID}/registry-credentials`
- `POST /api/v1/projects/{projectID}/registry-credentials`

这些接口的来源和 `v5/02`
保持一致：

- `proto`
- `gRPC`
- `grpc-gateway`

而不是再补一套手写 REST。

## 为什么 config 和 secret 不能只剩一个对象

如果平台里只有：

- `secretSet`
- `registryCredential`

那“服务运行输入”这一半会天然偏向：

- 敏感值
- 私有镜像

但真实服务还需要大量：

- 非敏感
- 可复用
- 项目级共享

的普通配置。

所以这一章必须同时补：

- `configSet`

否则模型会变成：

- `service.env`
  承担服务内联小配置
- `secretSet`
  承担密文
- 但“共享普通配置”没有正式位置

这会让 `env`
很快失控。

## 当前生效规则是什么

这一章最关键的不是把对象加上去，
而是把合并语义讲清楚。

当前实现里，
有两次合并：

### 第一次：创建 revision 快照时

当服务创建或更新触发新 `revision`
时，
平台会把：

- `service.spec.env`
- `configSet.values`

合并后写进：

- `revision.env`

同时把当前引用的：

- `configSetID`
- `secretSetID`
- `registryCredentialID`

也一起冻结在 `revision`
上。

当前顺序是：

- 先放 `service env`
- 再放 `config set`

所以如果同一个键同时出现，
当前结果是：

- `configSet`
  覆盖 inline `env`

### 第二次：worker 领取 work item 时

执行工作项下发给节点时，
平台会使用当前这份 `revision`
自己的运行输入快照：

- `revision env`
- `revision.secretSetID`
  对应的 `secret set`

做一次合并，
生成真正传给容器的：

- `work item env`

当前顺序是：

- `revision env`
- `secret set`

所以冲突时的最终优先级是：

1. `secretSet`
2. `configSet`
3. inline `env`

这里要注意两点：

1. 这是一套当前实现语义
   - 重点是保证旧 `revision`
     回滚时仍然吃到自己那一版的输入
2. `registryCredential`
   不参与这个环境变量合并
   - 它只进镜像拉取凭据

## 为什么 registry credential 要单独建模

私有镜像访问看起来也像“秘密”，
但它和应用自己的密文配置不是一回事。

差别在于：

1. 使用时机不同
   - `registryCredential`
     在拉镜像前就要用
   - `secretSet`
     是容器运行时输入
2. 注入位置不同
   - `registryCredential`
     进的是镜像拉取逻辑
   - `secretSet`
     进的是容器环境变量
3. 生命周期不同
   - 一个服务可能换镜像仓库
   - 也可能完全不动应用密文

所以这一章把它单独收成项目资源，
而不是混进 `secretSet`。

## ACR 和 TCR 在这一章里怎么理解

这一章不把：

- `ACR`
- `TCR`

建模成平台资源。

这一章的定位只是：

- 平台接受一组通用私有仓库凭据
- 这组凭据可以拿去拉：
  - 阿里云 `ACR`
  - 腾讯云 `TCR`
  - 其他兼容 Docker Registry
    的私有仓库

所以对平台来说，
真正重要的字段只有：

- `server`
- `username`
- `password`

而不是：

- 某个云厂商自己的仓库实例 ID
- 命名空间模型
- 仓库生命周期 API

这些东西以后如果要做，
应该是：

- provider adapter
  或
- 仓库集成层

的话题，
不是这章的主模型。

## Web 这一章补了什么

这一章对应的 `Web UI`
也同步补了项目级输入管理：

- 创建 `config set`
- 创建 `secret set`
- 创建 `registry credential`
- 在创建 / 编辑 `service`
  时引用这些资源

同时，
`service` 表单里保留：

- `env`
  文本编辑

这样这一章结束后，
用户第一次可以在一个完整表单里同时声明：

- 服务镜像
- 端口和探针
- 普通环境变量
- 公共配置引用
- 密文引用
- 私有镜像拉取凭据

## 这一章的边界

到这里，
`service`
已经不再只是：

- 镜像
- 副本
- 端口

这种“空壳定义”。

它第一次具备了真正运行一个长期在线容器服务所需的最小输入闭环：

- 规格
- 普通配置
- 密文配置
- 私有镜像访问

但这还不等于服务已经能被公网访问。

下一章要补的是：

- 域名
- `TLS`
- 暴露语义
- 托管 `Caddy`
  网关

也就是把“能跑起来”
推进到：

- “能被外部访问到”

## 本章检查点

如果这一章做对了，
你现在应该已经能比较自然地回答下面这些问题：

1. 为什么 `service spec`
   里不能只剩：
   - 镜像
   - 端口
   - 副本
2. 为什么普通配置、密文配置、私有镜像凭据
   不能混成一个对象
3. 为什么：
   - `configSet`
   - `secretSet`
   - `registryCredential`
   都应该是项目级资源
4. 为什么 `registryCredential`
   只参与拉镜像，
   不进入容器环境变量
5. 为什么 `revision`
   必须冻结自己的运行输入引用，
   不能在回滚时再偷看当前 `service`
6. 当前冲突优先级为什么是：
   - `secretSet`
   - `configSet`
   - inline `env`
7. 为什么这一章不把：
   - `ACR`
   - `TCR`
   直接建模成平台资源

如果这些问题都已经能稳定回答，
那 `v5/03`
的运行输入边界就算真正立住了。

对应提交：

- `acbffa12be74351178ca568571b9e6eb14616628`
