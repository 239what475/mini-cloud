# 18. 文件投影与最小持久目录挂载

到了
`v6/17`
为止，
`mini-cloud`
已经能用 northbound
`gRPC`
和 `TUI`
去创建长期服务，
但服务运行时输入仍然主要只有：

- `env`
- `configSetID`
- `secretSetID`
- `registryCredentialID`

这对很多简单服务已经够用，
但对真实开源应用还不够。

像
`CLIProxyAPI`
这种应用，
更自然的输入方式往往不是：

- 一堆环境变量

而是：

- 一个配置文件
- 一些密钥文件
- 一个可写、
  且重启后还要保留的认证目录

如果平台不能把项目里的配置 /
密文配置投影成容器内文件，
也不能给服务提供一个最小可写目录，
那就只能：

- 手工改镜像
- 手工进容器写文件
- 手工在宿主机上准备目录
- 或者把本来应该是文件 /
  目录的数据硬塞进 env

这都会让平台模型变脏。

所以这一章现在做两件事：

- 增加最小、干净的
  projected files
  能力
- 增加最小、干净的
  persistent dirs
  能力

明确不在这一章里继续扩成：

- 通用 volume
  系统
- `PVC / CSI`
  式存储产品线
- `hostPath`
  透传
- 跨节点自动迁移卷
- 多副本共享可写卷

## 这一章解决什么问题

这一章收的是下面两条链：

### 1. 只读文件输入

- `service spec`
  可以声明：
  - 容器内目标文件路径
  - 来源资源类型
  - 来源资源 id
  - 来源 key
- `control-plane`
  能校验这些引用是否合法
- 跨 plane
  同步时，
  能把本地资源 id
  换成远端资源 id
- `cloud-plane`
  能把 projected files
  纳入 revision
  快照
- `execution`
  claim
  时，
  能把文件内容真正物化出来
- `agent / runtime`
  能把这些内容以只读文件挂进容器

### 2. 最小可写目录输入

- `service spec`
  可以声明：
  - 一个或多个需要持久化的容器目录
- `control-plane`
  只保存期望态，
  不暴露宿主机路径
- `cloud-plane`
  把目录声明纳入 service /
  revision /
  execution
- `agent / runtime`
  在节点本地准备持久目录，
  并把它作为可写 bind mount
  挂入容器

也就是说，
这一章要补的是：

- 文件型运行时输入
- 最小持久目录输入

而不是：

- 新的存储产品线

## 为什么不是直接做 volume

现在平台真正缺的不是：

- 一个抽象很大的 volume DSL

而是：

- 一个简单可靠的“把资源里的 key
  变成容器只读文件”的能力
- 一个简单可靠的“给单副本长期服务一个节点本地持久目录”的能力

如果现在直接引入通用 volume
系统，
会立刻把很多尚未决定清楚的东西一起带进来：

- 生命周期
- 回收语义
- 可写性
- 配额
- 宿主机路径暴露
- 安全边界
- 跨节点重挂载
- 多副本一致性

这会把本章的目标拉散。

所以这里明确只做：

- projected file spec
- revision-scoped
  文件快照
- agent
  本机临时目录物化
- 只读 bind mount
- persistent dir
  最小声明
- 节点本地持久目录
- 可写 bind mount

## 这一章的设计

### 1. northbound service spec

`service`
新增两类运行时输入：

- `projectedFiles[]`
- `persistentDirs[]`

其中：

- `projectedFiles[]`
  用来描述只读文件
- `persistentDirs[]`
  用来描述可写目录

#### `projectedFiles[]`

每一项只保留四个字段：

- `mountPath`
- `sourceKind`
  - `config_set`
  - `secret_set`
- `sourceID`
- `sourceKey`

这意味着：

- 文件内容不直接出现在 northbound
  请求里
- 平台继续复用项目级 config /
  secret
  资源

#### `persistentDirs[]`

第一版每一项也只保留最小字段：

- `name`
- `mountPath`

它不包含：

- 宿主机路径
- 存储类
- 容量大小
- 访问模式矩阵

因为第一版的真实语义只有一句话：

- 在某个 node
  上为这个 service
  持久保留一个目录，
  并把它可写挂进容器

### 2. control-plane

`control-plane`
做三件事：

1. 校验 projected file
   引用
   - `sourceID`
     必须存在
   - `sourceKey`
     必须存在
2. 校验 persistent dir
   声明是否合法
   - `name`
     不能为空，
     且只能是小写字母 /
     数字 /
     `-`
   - `mountPath`
     必须是绝对目录路径
   - 不能和
     projected file
     路径双向重叠
   - 不允许
     persistent dir
     之间做父子嵌套
3. 跨 plane
   同步时重写文件资源引用
   - 把本地资源 id
     换成远端绑定后的资源 id
4. 对已经进入过 release
   状态的
   `persistentDirs`
   service，
   如果这次更新会导致新 revision，
   则在写入前直接拒绝

它不做：

- 生成宿主机临时文件
- 生成宿主机持久目录
- 拼 bind mount
- 直接保存文件内容

### 3. cloud-plane

`cloud-plane`
把两类输入都纳入：

- `service`
- `revision`
- `execution`

这里的关键语义是：

- `revision`
  保存的是 projected files /
  persistent dirs
  的声明快照
- `execution`
  claim
  时：
  - 对 projected files
    物化真正文件内容
  - 对 persistent dirs
    物化节点本地目录挂载信息

这样可以保持和现有：

- config set
  revision
  语义
- secret set
  execution
  语义

一致。

### 4. agent / runtime

node agent
收到的不是资源 id，
而是已经可执行的输入：

- projected file
  - `mountPath`
  - `content`
  - `mode`
  - `sensitive`
- persistent dir
  - `name`
  - `mountPath`
  - 节点本地 source path

然后：

- 对 projected files
  - 在本机创建临时目录
  - 把文件写进去
  - 按文件路径做只读 bind mount
  - 容器停止后清理临时目录
- 对 persistent dirs
  - 在节点本地固定根目录下创建目录
  - 作为可写 bind mount
    挂进容器
  - 容器停止后不删除目录
  - service
    删除后当前版本也不自动清理目录

这样 runtime
不需要知道：

- `config_set`
- `secret_set`
- project
  resource binding
- 哪个 service
  原来怎样存的

它只关心：

- 该挂什么文件
- 该挂什么目录

### 5. 第一版限制

这章最重要的是把边界写死。

第一版
`persistentDirs`
明确只支持：

- 单副本服务
- 单 node
  本地目录
- 单 plane
  原地保留
- `ReadWriteOnce`
  语义

明确不支持：

- 多副本共享写
- 跨 node
  自动迁移目录
- node
  坏掉后自动漂移并恢复数据
- 已经产生
  current /
  candidate revision
  后，
  再对带
  `persistentDirs`
  的 service
  做 revision-changing update
  并触发 candidate rollout

这里的拒绝语义是：

- `control-plane`
  入口前置拒绝
- `cloud-plane`
  仍保留同样的硬限制
  作为最终防线

也就是说，
如果某个带
`persistentDirs`
的 service
已经产生过 release，
那后续不会再自动迁移到：

- 别的 plane
- 别的 node

如果原 plane /
node
不可用，
第一版更诚实的做法是：

- 明确进入需要人工介入的状态
- 对已经进入过
  revision /
  rollout
  状态的
  `persistentDirs`
  service，
  如果修改会导致新 revision，
  直接拒绝这次变更
- 不偷偷在另一台机器上创建一个新的空目录

而不是假装已经支持透明迁移。

## TUI 的最小输入方式

这一章先把 `TUI`
创建服务表单继续往前推进两小步。

新增字段：

- `projectedFiles`
- `persistentDirs`

其中：

- `projectedFiles`
  继续保持当前格式：

```text
/etc/cliproxy/config.yaml|config_set|cfg_demo|config.yaml;/etc/cliproxy/auth/token|secret_set|sec_demo|token
```

- `persistentDirs`
  第一版可以先收成：

```text
auth|/root/.cli-proxy-api
```

也就是：

- 一条规则两段
- 用 `|`
  分隔字段
- 多条规则用 `;`
  分隔
- `name`
  必须是小写字母 /
  数字 /
  `-`
- `mountPath`
  不能是 `/`
- 不允许和
  projected files
  路径重叠
- 不允许多个
  persistent dirs
  做父子嵌套

这一版仍然没有在
`TUI`
里继续补：

- config / secret
  资源创建
- env
  编辑
- command / args
- registry credential
  管理

所以这章的目标不是把
`TUI`
做成完整控制台，
而是先让它能把
`CLIProxyAPI`
所需的两类运行时输入一路送进 northbound
接口。

## 这一章完成后的目标

这章收完以后，
平台应该能正式承载这种服务：

- 一个只读配置文件
- 一个只读密钥文件
- 一个可写持久目录

对
`CLIProxyAPI`
来说就是：

- `/etc/cliproxy/config.yaml`
  - 走 `projectedFiles`
- `/root/.cli-proxy-api`
  - 走 `persistentDirs`

这个目录的真实宿主机路径
由平台内部生成，
当前形态是节点本地：

- `/var/lib/mini-cloud/persistent-dirs/<serviceID>/<dirName>`

它不是：

- 跨 node
  漂移卷
- 自动清理卷

## 检查点

- 本章代码已经把：
  projected files
  接进 northbound /
  control-plane /
  cloud-plane /
  nodeagent /
  runtime
- 本章代码已经把：
  config / secret
  key
  以只读文件形式挂进容器
- 本章代码已经把：
  projected files
  纳入 revision-scoped
  运行时输入语义
- 本章代码已经把：
  最小 persistent dirs
  接进 service /
  revision /
  execution /
  runtime
- 本章代码已经把：
  `TUI`
  create service
  表单补上
  `projectedFiles`
  和
  `persistentDirs`
  输入
- 本章代码已经把：
  持久目录能力限制在：
  - 单副本
  - 单 node
  - 节点本地目录
  - 已有 release 时，
    不允许 revision-changing update
