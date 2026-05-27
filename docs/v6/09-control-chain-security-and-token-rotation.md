# 09 Control Chain Security And Token Rotation

这一章不做整个平台的
`PKI / mTLS`
重构。

这一章先做一件更贴近当前实现、
也更容易闭环的事：

- 把平台内部已经存在的几条
  `token`
  控制链
  正式收成一套清楚、
  可轮转、
  可观察的信任模型

## 为什么这一章先不直接做证书体系

如果这一章直接把内部控制链改成：

- `CA`
- `CSR`
- 证书签发
- 证书续期
- 吊销
- `mTLS`

那 `09`
就不再是：

- 安全收口章

而会变成：

- 身份基础设施重构章

这会把当前已经能跑的控制链，
一下子拖进一整套新的基础设施。

对现在这版
`mini-cloud`
来说，
更务实的做法是：

- 先把已有
  `token`
  链做干净
- 把边界、
  作用域、
  轮转语义、
  最后使用时间、
  失效路径
  先收清楚

等这套模型完全稳定以后，
再单独开后续章节做：

- 内部 `PKI`
- `mTLS`

## 这一章要解决什么问题

到 `08`
结束时，
平台已经有多条可工作的身份链，
但成熟度并不一致：

1. `control-plane`
   northbound
   有：
   - `admin token`
   - `project token`
2. `control-plane -> cloud-plane`
   已经有一条可用的 southbound
   `token`
   链
3. `cloud-plane -> node-agent`
   已经有：
   - `bootstrap token`
   - `session token`
     两段式
4. front door
   adapter
   仍然是一个过于宽松的同步口

也就是说，
当前真正的问题不是：

- 完全没有安全模型

而是：

- 模型已经存在，
  但不同链路的边界不一致、
  轮转能力不一致、
  最小鉴权强度也不一致

这一章就是要把这些差异收掉。

## 这一章明确收什么能力

这一章结束后，
平台内部控制链至少要具备下面这些能力：

1. `control-plane`
   northbound
   `token`
   语义更清楚
   - `admin token`
     仍然是平台级管理口
   - `project token`
     仍然是项目级自动化入口
   - `whoami / lastUsedAt / revoke`
     这些能力继续保持一致
2. `control-plane -> cloud-plane`
   southbound
   `token`
   语义正式化
   - 这一段当前仍然用
     `token`
   - 不在这一章强行升级到
     证书体系
   - register /
     re-register
     本身就要成为一条明确的
     轮转入口
3. `cloud-plane -> node-agent`
   信任链正式化
   - `bootstrap token`
     只允许注册
   - `session token`
     只允许心跳、
     领任务、
     回报执行结果
   - `session token`
     需要有明确的失效时间
   - 失效后，
     `agent`
     要能自动清空本地状态并重新注册
4. front door
   adapter
   最小鉴权
   - 不再允许知道地址就能直接推送快照
   - 至少要有独立的
     southbound
     `token`
5. 文档里把这几段链路的边界写清楚
   - 谁发 token
   - 谁持有 token
   - token
     作用域是什么
   - 失效后谁来恢复

## 这一章明确不做什么

这一章明确不做：

- 内部 `CA`
- 证书签发
- 证书续期
- `mTLS`
- `Vault / KMS`
- provider
  凭证体系重构
- `secret set / registry credential`
  的完整生命周期治理
- 更复杂的人类身份系统统一

这些内容并不是不重要，
而是它们一旦认真展开，
就会让 `09`
超出“控制链 token 收口”这个边界。

## 这一章采用的正式信任模型

这一章结束后，
平台内部的最小控制链，
应该这样理解：

1. 人类或自动化客户端
   调 `control-plane`
   northbound API
   - 用：
     - `admin token`
     - `project token`
2. `control-plane`
   调 `cloud-plane`
   southbound API
   - 用：
     - plane
       级 southbound
       `token`
3. `node-agent`
   首次接入
   `cloud-plane`
   - 用：
     - `bootstrap token`
4. `node-agent`
   注册完成后继续工作
   - 用：
     - `session token`
5. `control-plane`
   推送 front door
   路由快照
   - 用：
     - front door
       adapter
       独立
       `token`

也就是说，
这一章不是要把平台变成：

- 全证书

而是先把它变成：

- 每一跳都有明确主体、
  明确 token、
  明确作用域

## 这一章的实现重点

代码上，
这章优先做下面几件事：

1. `front door adapter`
   增加最小 bearer token
   鉴权
2. `node-agent session token`
   增加失效时间，
   让旧会话不能永久使用
3. `agent`
   继续沿用当前已经存在的：
   - 收到
     `401 / 403 / 404`
     后清空本地状态
   - 下一个周期重新注册
     这条恢复路径
4. 文档和术语明确说明：
   `control-plane -> cloud-plane`
   这段当前仍然是
   token
   模型，
   并且对外可见的
   API 字段、
   env 名、
   Terraform 变量与输出
   统一使用：
   - plane
     级
     `southbound token`
   不再混用
   `bootstrap token`
   叫法，
   后续如果要做证书，
   应该单独成章

## 这一章完成后平台该怎么理解

做到这里以后，
operator
应该这样理解当前平台的安全边界：

- 这不是最终形态的零信任平台
- 但它已经不再是：
  - 几个松散 token
    拼起来的实验系统
- 而是：
  - 一套边界清楚的
    控制链 token
    模型
  - 并且这些 token
    已经具备最基本的：
    - 作用域
    - 最后使用时间
    - 轮转或重注册入口
    - 失效恢复路径

## 检查点

- 待本章提交时回填 commit hash。
