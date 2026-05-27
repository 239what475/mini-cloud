# 12 V1 Hardening And Docs Polish

这一章不是再给 `mini-cloud v1` 加新功能，
而是专门做一次：

- 收尾加固
- 文档统一
- 小毛刺修正

如果说 `11`
解决的是：

- “这条主链有没有稳定测试”

那 `12`
解决的就是：

- “这条主链现在看起来是不是已经像一个能收尾的 `v1`”

## 这一章先记一个判断

`v1` 到了这里，
最重要的已经不是继续堆功能，
而是把下面三件事收干净：

1. API 形状要稳定
2. 本地验证入口要统一
3. 文档和脚本要能直接指导下一次实验

这也是为什么 `12`
没有再去做：

- 新资源模型
- 新调度能力
- 新网关能力

而是回头修这些看起来“小”，
但实际上非常影响体验的点。

## 这一章实际修了什么

### 1. 把项目级脚本统一收进 `scripts/`

现在 `mini-cloud` 的项目级脚本统一放在：

- `projects/mini-cloud/scripts/`

包括：

- `scripts/check.sh`
- `scripts/test-integration.sh`
- `scripts/smoke.sh`

这样做的目的很直接：

- 根目录不再继续堆脚本
- 验证入口更集中
- 后面如果继续加辅助脚本，位置也更清晰

### 2. `check.sh` 现在能更稳地跑在受限环境里

这一章专门修了一个很容易让人误判的问题：

- `staticcheck`

在某些受限环境里会因为：

- `~/.cache/go-build`
- `~/.cache/staticcheck`

不可写而报错。

为了让这一层检查更稳定，
现在 `scripts/check.sh`
默认会在没有外部覆盖时把缓存目录放到：

- `/tmp`

下的可写位置。

也就是说，
这一章之后：

- `scripts/check.sh`

不再那么依赖“当前 shell 环境刚好配置得合适”。

### 3. API 里的空集合现在统一返回 `[]` / `{}`

这是这一章最重要的一批接口加固。

在这次修正之前，
`v1` 的部分响应里会出现这种情况：

- 空数组字段返回 `null`
- 空对象字段返回 `null`

例如：

- app 的 `env`
- release 的 `command`
- release 的 `args`
- release 的 `env`
- app detail 里的：
  - `domains`
  - `releases`
  - `deploymentTransitions`
- 各种列表接口里的：
  - `items`

这会带来两个问题：

1. API 形状不稳定
   - 同一个字段有时是数组
   - 有时却是 `null`
2. 前端和脚本都更难写
   - 因为读到之前必须先额外判断空值

这一章之后，
这类字段统一变成：

- 空数组用 `[]`
- 空对象用 `{}`

也就是说，
现在可以更稳定地把这些字段当成真正的集合类型处理。

### 4. `cmd/agent` 的测试不再依赖本地监听端口

这一章还修了一个测试层的小毛刺：

之前新增的 agent 健康检查测试，
会真的在本地起一个：

- `httptest.NewServer`

这在正常开发机上通常没问题，
但在某些受限环境里，
测试进程未必有权限自己监听端口。

所以现在把这组测试改成了：

- 注入假的 HTTP getter

这样好处是：

- 测试仍然验证同一段逻辑
- 但不再依赖本地开端口

这更适合作为：

- 日常 `check`
- 受限环境里的快速回归

## 这一章补了哪些回归保证

这一章除了修代码，
还专门补了一组集成测试，
去钉住新的 API 形状约束。

现在会明确验证：

- 空列表接口返回 `items: []`
- app 的空 `env` 返回 `{}`
- release 的空 `command` / `args` 返回 `[]`
- app detail 在还没有 deployment / domain / release 历史时，
  - 也不会再把这些集合字段返回成 `null`

也就是说，
这一章不是“手工看起来顺眼一点”，
而是已经把这些行为写进测试里了。

## 一个很容易踩坑的执行细节

这一章还顺手把一个实验层面的坑写明确了：

- `scripts/test-integration.sh`
- `scripts/smoke.sh`

这两个脚本：

- 都会自己起本地 `docker compose`
- 都会使用同一个：
  - `mini-cloud-postgres`
  - `compose_default`

所以它们不能并行跑。

如果你同时启动这两个脚本，
很容易看到这种冲突：

- 容器名已经存在
- compose network 已存在

所以正确做法是：

1. 先跑完一个
2. 等它自动清理
3. 再跑下一个

## 这一章之后，本地验证入口怎么理解

到 `12`
为止，
本地验证入口已经可以很明确地分成三层：

### 第一层：轻量检查

```bash
./scripts/check.sh
```

适合：

- 日常改代码后快速回归

### 第二层：真实数据库集成测试

```bash
./scripts/test-integration.sh
```

适合：

- 验证 migrations
- 验证 store / API 组合

### 第三层：关键主链 smoke

```bash
./scripts/smoke.sh
```

适合：

- 验证从注册节点到 app running 的关键闭环

## 我这次实际重新验证了什么

这次 `12`
我实际重新跑了：

- `bash ./scripts/check.sh`
- `bash ./scripts/test-integration.sh`
- `bash ./scripts/smoke.sh`

其中 smoke 输出里最值得注意的一点是：

修完 API 形状以后，
这些字段已经稳定变成了：

- app 的 `env = {}`
- release 的 `command = []`
- release 的 `args = []`
- release 的 `env = {}`
- app detail 的 `domains = []`

而不是之前那种：

- `null`

这说明这次加固不只是测试通过，
而是已经体现在真实主链输出上了。

## 到这里，`v1` 的状态又前进了一步

做完这一章以后，
`v1` 更像一个真正准备收尾的第一版了。

因为现在它已经同时具备了：

- 更稳定的本地检查入口
- 更统一的脚本组织
- 更稳定的 API 形状
- 更少的前端/脚本空值毛刺
- 更清晰的实验执行说明

所以最后一章 `13`
就可以更聚焦地做：

- `v1` 边界复盘
- `v2` 扩展方向说明

## 本章检查点

- commit:
  - `ed55eca1b12747f4804bce1ebd821100dc927853`
- 状态：
  - `v1` 的脚本入口、API 空集合形状和实验说明已经完成一轮统一加固
