# 08 Gateway, TLS, and Domain Flow

这一章不是去硬做一个完整证书平台。

它要先把 `v2`
入口层里最容易混淆的三件事拆开：

1. 平台自己的域名是什么
2. 应用默认对外域名怎么生成
3. `TLS`
   到底由谁终止，gateway 又应该据此做什么

所以这一章的目标很明确：

- 把“平台域名”和“应用域名”正式分层
- 给应用自动分配一个可预测的默认域名
- 让 gateway 理解：
  - “上游已经终止了 `TLS`，我这里只看到 HTTP 转发请求”

## 这一章先记一句话

当前 `v2/08`
不是在 control-plane 里直接签证书。

它做的是：

- 平台通过配置知道自己的：
  - `PlatformDomain`
  - `AppBaseDomain`
- 外层正式入口
  - 例如：
    - `SLB`
    - `Nginx`
    - `Caddy`
  - 负责真正监听：
    - `443`
  - 负责真正终止 `TLS`
- `mini-cloud` 内部 gateway
  - 根据：
    - `Host`
    - `X-Forwarded-Proto`
  - 决定：
    - 是否做 HTTPS 重定向
    - 是否继续反代到 app backend

也就是说，
这章是在把：

- “域名分层”
- “入口流量语义”

先做正式。

## 这章新增了哪些配置

control-plane 现在多了三项入口配置：

### `PlatformDomain`

平台自己的域名。

例如：

- `console.lab.example.com`

它应该留给：

- 平台 UI
- 平台 API
- 平台运维入口

所以应用不应该再把这个 host 绑走。

### `AppBaseDomain`

应用托管域名的基准后缀。

例如：

- `apps.lab.example.com`

有了它以后，
平台就可以自动生成默认应用域名。

### `GatewayRedirectHTTPToHTTPS`

这个开关表达的是：

- 如果 gateway 收到的是明文 HTTP 视角请求
- 而平台又已经进入“正式 HTTPS 对外”的阶段
- 那就把应用流量重定向到：
  - `https://...`

注意这里的重点是：

- gateway 自己未必直接监听 TLS
- 它也可能只是通过：
  - `X-Forwarded-Proto`
  - 知道上游终止前到底是不是 HTTPS

## 应用默认域名现在怎么生成

如果配置了：

- `AppBaseDomain`

那么 app 创建成功后，
平台会自动给它加上一条默认域名绑定：

```text
{app}.{project}.{appBaseDomain}
```

例如：

- project:
  - `edge-team`
- app:
  - `hello`
- `AppBaseDomain`:
  - `apps.lab.example.com`

那自动生成的 host 就是：

```text
hello.edge-team.apps.lab.example.com
```

这样做好处很直接：

- 默认域名是稳定、可预测的
- 不需要每次手工先绑一个域名
- 也天然把不同 project 隔开了

## 为什么平台域名和应用域名要分开

如果不分开，
很快就会出现一个很难处理的问题：

- 某个 app 直接把：
  - `console.lab.example.com`
  - 这种 host 绑定走了

那平台自己的：

- UI
- API
- 运维入口

就会和应用流量抢同一个 host。

所以现在这章明确加了一个边界：

- `PlatformDomain`
  - 是保留给平台自己的
- app domain binding
  - 不能绑定到这个 host

## gateway 现在怎么理解 TLS

这一章没有让 control-plane 自己直接签证书。

而是明确采用一个更现实、也更常见的分层：

```text
公网客户端
  ->
外层 TLS 入口
  负责 443 / 证书 / TLS 终止
  ->
mini-cloud gateway
  看到 Host + X-Forwarded-Proto
  ->
app backend
```

所以 gateway 现在会看：

- `Host`
- `X-Forwarded-Proto`
- `r.TLS`

然后决定当前请求在平台视角里是：

- `http`
  还是
- `https`

如果：

- `GatewayRedirectHTTPToHTTPS = true`
- 且当前请求视角仍然是：
  - `http`

那对于已经绑定到 app 的 host，
gateway 就会先返回：

- `308 Permanent Redirect`

把它跳到：

- `https://{host}{uri}`

## 为什么这一步很重要

因为一旦平台开始正式对外，
你通常不希望应用还是继续以：

- `http://app.example.com`

这种入口长期暴露。

但当前 `mini-cloud`
内部的 gateway 又不一定就是最终 TLS 终止点。

所以最合适的做法是：

- 先让平台的 gateway 理解：
  - “上游已经终止过 TLS”
- 再由它统一决定：
  - 是否重定向到 HTTPS

这比“每个后端应用自己决定要不要跳 HTTPS”
要干净得多。

## 这章现在多了一个可观察接口

现在平台提供：

```bash
GET /api/v1/platform/gateway-config
```

返回最小 gateway 公开配置，
例如：

- `platformDomain`
- `appBaseDomain`
- `managedAppDomainPattern`
- `redirectHTTPToHTTPS`

这个接口的价值是：

- 让你能直接看到当前入口层到底按什么规则工作
- 文档、前端和运维脚本都可以直接读这一层配置

## 这章最值得看的代码落点

### 配置入口

- `internal/config/config.go`

### 自动默认域名

- `internal/ingress/ingress.go`
- `internal/httpapi/app_handler.go`

### gateway 的平台配置接口和 HTTPS 语义

- `internal/httpapi/router.go`
- `internal/httpapi/gateway_handler.go`

### bootstrap 配置模板

- `internal/bootstrap/config.go`
- `deploy/bootstrap/bootstrap-config.json.example`

## 本地最小实验顺序

这一章最值得跑的不是“真证书”，
而是下面这条闭环：

1. 配好：
   - `PlatformDomain`
   - `AppBaseDomain`
   - `GatewayRedirectHTTPToHTTPS`
2. 创建一个 project 和 app
3. 观察 app 自动拿到默认域名
4. 验证平台域名不能被 app 绑走
5. 对绑定域名发一个：
   - `X-Forwarded-Proto: http`
   请求
   - 看它是否跳到了 `https://...`
6. 再发一个：
   - `X-Forwarded-Proto: https`
   请求
   - 看 gateway 是否真的把流量转到 backend

### 1. 启动时带上入口配置

例如：

```bash
export MINICLOUD_PLATFORM_DOMAIN=console.lab.example.com
export MINICLOUD_APP_BASE_DOMAIN=apps.lab.example.com
export MINICLOUD_GATEWAY_REDIRECT_HTTP_TO_HTTPS=true

go run ./cmd/control-plane
```

### 2. 看当前 gateway 公开配置

```bash
curl -fsS http://127.0.0.1:8080/api/v1/platform/gateway-config | jq
```

你应该能看到：

- `platformDomain`
- `appBaseDomain`
- `managedAppDomainPattern`
- `redirectHTTPToHTTPS`

### 3. 创建 project 和 app

创建完 app 以后，
再看详情：

```bash
curl -fsS http://127.0.0.1:8080/api/v1/apps/<app-id> | jq
```

这时 `domains`
里应该已经自动出现一条：

- `{app}.{project}.{appBaseDomain}`

### 4. 试着绑定平台域名

```bash
curl -fsS -X POST http://127.0.0.1:8080/api/v1/apps/<app-id>/domains \
  -H 'Content-Type: application/json' \
  -d '{
    "host": "console.lab.example.com"
  }' | jq
```

这里应该被拒绝，
因为这个 host 是保留给平台自己的。

### 5. 验证 HTTP 到 HTTPS 的跳转

```bash
curl -i \
  -H 'Host: hello.edge-team.apps.lab.example.com' \
  -H 'X-Forwarded-Proto: http' \
  http://127.0.0.1:8080/hello
```

你应该看到：

- `308 Permanent Redirect`

以及：

- `Location: https://hello.edge-team.apps.lab.example.com/hello`

### 6. 验证已经是 HTTPS 视角时会继续反代

```bash
curl -i \
  -H 'Host: hello.edge-team.apps.lab.example.com' \
  -H 'X-Forwarded-Proto: https' \
  http://127.0.0.1:8080/hello
```

这时 gateway 会继续查路由并反代到 app backend。

## 这一章的现实边界

这一章仍然没有做这些事：

- ACME / Let's Encrypt 自动签发
- 证书文件下发和轮换
- 多 gateway 实例之间的证书共享
- 通配符证书管理
- DNS 自动解析下发
- 更重的外层 gateway 编排

所以这一章最准确的理解是：

- 入口层配置已经从“纯教学网关”升级成了“有正式域名分层和 HTTPS 语义的最小版本”
- 但真正的证书生命周期管理
  - 仍然留给后续章节或外层组件

## 这一章跑了哪些检查

当前已经跑过：

```bash
go test ./...
./scripts/test-integration.sh
./scripts/check.sh
```

其中新增的集成测试重点覆盖了：

- `AppBaseDomain`
  - 会自动生成默认 app host
- `PlatformDomain`
  - 不能被 app 绑定
- `GET /api/v1/platform/gateway-config`
  - 会正确暴露当前入口层配置
- `GatewayRedirectHTTPToHTTPS`
  - 在 `X-Forwarded-Proto: http` 时会返回 `308`
- gateway 反代
  - 在 `X-Forwarded-Proto: https` 时会继续把请求转到 backend

## 本章检查点

- 提交：
  - `151e98c7509a303ad49067e82914619fcb79611b`
- 状态：
  - 平台域名和应用域名
    - 已经正式分层
  - app 创建
    - 已经能自动拿到默认托管域名
  - 平台保留域名
    - 已经不会被 app 绑走
  - gateway
    - 已经理解“上游已终止 TLS”的 HTTPS 语义
  - gateway 配置
    - 已经能通过平台 API 直接读出来
