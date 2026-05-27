# 20. 后台控制器错误边界与降级健康面

到了
`v6/19`
为止，
`mini-cloud`
已经把真实域名、
TLS
和 CDN
接了进来，
但这里也暴露出一个非常具体的问题：

- 后台控制器的一次运行时错误，
  可能直接把整个
  `cloud-plane`
  进程拉挂

这在生产语义上是不对的。

像 gateway
reload
这种事情，
本质上是一个后台 reconcile
控制器在做：

- 读取当前期望态
- 渲染配置
- 尝试应用
- 失败后继续重试

它不是：

- 一个必须把整个进程退出掉的启动期 fatal

如果这一层没有错误边界，
那么后面即使：

- 域名配置是对的
- 服务本身是健康的
- 数据库仍然可用

也可能因为一次：

- 端口占用
- 权限不足
- gateway reload
  失败
- 渲染异常

直接把整个
`cloud-plane`
干掉。

所以这一章专门做一件事：

- 把后台控制器错误
  从“进程级致命”
  改成“默认降级、可观测、持续重试”

同时保留一个显式 strict
开关，
让测试或调试时仍然可以选择 fail-fast。

## 这一章解决什么问题

这一章收的是一条很明确的错误语义链：

### 1. 启动期错误仍然是 fatal

下面这些错误继续直接让进程启动失败：

- 进程配置缺失
- 平台配置非法
- 数据库打不开
- migration
  失败
- gateway engine
  的静态配置本身非法

这些错误说明：

- 平台根本没准备好启动

所以应该继续 fail-fast。

### 2. 后台控制器运行期错误不再拉挂进程

像下面这类错误，
不再默认升级成进程退出：

- gateway snapshot
  加载失败
- gateway render
  失败
- Caddy reload
  失败
- 后台对账的一次瞬时错误

这些错误应该变成：

- 写日志
- 更新 controller
  运行状态
- 对外暴露 degraded
  健康面
- 下一轮继续重试

### 3. 健康面要真实反映后台控制器是否异常

如果 gateway
控制器已经连续失败，
那平台的健康面不能还继续只返回：

- 数据库正常
- 整体 `ok`

所以这一章会把 controller
状态接到：

- `/api/healthz`
- plane snapshot

让 operator
能看到：

- 平台进程虽然还活着
- 但 gateway
  已经 degraded

## 这一章的设计

### 1. 错误边界按“运行位置”划分

这里不再做一套大的：

- debug
- release

全局错误语义模型。

这一章只做更直接的边界划分：

- 启动期错误
  - fatal
- 前台请求错误
  - 返回调用方
- 后台控制器错误
  - 默认日志 + 状态 + 重试

也就是说，
错误怎么处理，
优先看它发生在：

- 启动路径
- 前台请求路径
- 后台 loop

而不是先看一个抽象的全局模式。

### 2. 后台控制器增加最小运行状态

第一版不引入持久化，
只在内存里维护一份最小状态：

- `status`
  - `ok`
  - `degraded`
- `lastAttemptAt`
- `lastSuccessAt`
- `lastError`
- `consecutiveFailures`

这里故意不扩成：

- 通用 controller
  event
  系统
- 历史错误表
- 持久化状态机

因为这一章要收的是：

- 错误边界
- 健康面

不是新的事件平台。

### 3. strict 开关只影响后台控制器

这一章新增一个显式开关：

- `MINICLOUD_STRICT_BACKGROUND_ERRORS`

默认值是：

- `false`

默认语义：

- 后台 controller
  出错只降级，不退出进程

当它打开时：

- 后台 controller
  保持 fail-fast

也就是说，
strict
不是为了让生产继续崩，
而是为了：

- 集成测试
- 调试问题
- 主动验证错误路径

### 4. healthz 与 plane snapshot 都要接入 controller 状态

这一章之后：

- `/api/healthz`
  不再只看 DB
- plane snapshot
  的 `health.service`
  也会把 gateway degraded
  算进去

这样：

- HTTP 健康探针
- plane southbound
  观察视图

两边看到的总体健康结论才一致。

## 这一章不做什么

这一章明确不做：

- 通用后台任务框架
- 全平台统一 controller
  基类
- controller
  状态持久化
- 复杂 debug /
  release
  双模式
- 新的事件总线

这一章只收：

- gateway
  这条后台控制器错误边界

先把最容易把进程直接拉挂的这一层改对。

## 这一章完成后的结果

做完以后，
`mini-cloud`
在这条链上应该具备下面这些行为：

1. 启动期 fatal
   继续保持 fail-fast
2. gateway
   后台 apply
   失败时，
   `cloud-plane`
   进程不会直接退出
3. 失败会被记录为：
   - 日志
   - degraded
     健康状态
   - 连续失败计数
4. 下一轮 reconcile
   会继续自动尝试
5. `/api/healthz`
   与 plane snapshot
   能看见这次降级
6. 打开 strict
   时，
   仍然可以保留 fail-fast
   语义

## 检查点

这一章完成时，
至少要有下面这些结果：

- `gateway reconciler`
  默认不会因为一次后台错误退出
- `/api/healthz`
  能返回 gateway
  降级信息
- plane snapshot
  会把 gateway degraded
  反映到 `health.service`
- 有单元测试覆盖：
  - 非 strict
    模式继续运行
  - strict
    模式直接返回错误
- 有集成测试覆盖：
  - healthz
    降级
  - gateway
    loop
    仍继续存活
