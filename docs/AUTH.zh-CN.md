# go-magic 鉴权与 URL 票据模型

> 适用版本：v0.5.9 及以后
> 对应实现：`internal/server/auth.go`、`internal/server/fs_ticket.go`、`internal/server/fs_serve.go`
> 英文版：[AUTH.en.md](AUTH.en.md)

本文说明 Dashboard / HTTP API 的鉴权模型，以及「必须把凭据放进 URL」的场景（`<img src>`、`<a href>`、`EventSource`、iframe 子资源）现在应该怎么写。

---

## 1. 一页速览

1. **登录凭据只走请求头**：`Authorization: Bearer <token>` 或 `X-Magic-Session-Token: <token>`。
2. **`?token=<登录凭据>` 已彻底删除**，全站任何接口都不再接受（含 `/api/fs/*`、`/api/uploads/*`、`/api/events`）。
3. 浏览器**发不出请求头**的请求，改用**票据**：先带请求头调 `POST /api/fs/sign` 换票，再把返回的地址交给 `<img>` / `<a>` / `EventSource`。
4. 票据是**自包含、按用途限定、带硬性过期时间**的签名串，只解锁「某一个动作 + 某一个文件/目录」，泄漏面从「整套 API」收敛为「一个文件、一段时间」。
5. 例外仅两处，且都不是登录凭据：对外分享链接 `/api/fs/shared/<token>`（`POST /api/fs/share` 自签的 TTL 凭据）、跨机器 relay `/api/relay/v1/dm`（令牌在请求体内独立校验）。

---

## 2. 凭据分层

| 凭据 | 形态 | 存放 | 用途 | 能否进 URL |
|------|------|------|------|-----------|
| 登录密码 | 明文，用户输入 | 仅用户记忆 | 换取会话令牌 | 否 |
| `authToken` | 密码的 bcrypt 哈希 | `<magic_home>/.auth_token` | 静态凭据；票据签名密钥的来源 | 否 |
| 会话令牌 | 服务端签发的随机串 | 前端 `localStorage`（登录响应） | 日常 API 认证 | 否 |
| 票据（ticket） | `base64url(v1␀scope␀path␀opts␀exp).base64url(hmac[:16])` | URL 的**路径段**或查询参数 `sig` | 替代 `?token=`，只解锁单一动作 | 是（设计如此） |
| 分享令牌 | `/api/fs/share` 签发的随机串 | URL 路径 | 给站外的人看一个文件/目录 | 是（独立体系） |

要点：

* 票据与登录凭据在密码学上**分离**：签名密钥由 `authToken` 单向派生（`sha256("go-magic/fs-ticket/v1\0" + authToken)`），拿到票据无法反推 `authToken`，因此也无法冒充登录。
* 因为密钥派生自 `authToken`，**重置密码 / 重置认证后所有旧票据立即失效**。
* 未完成初始化（`authToken` 为空）时拒绝签发任何票据，否则密钥会退化成公开常量。

---

## 3. 请求头认证（常规 API）

```bash
# 推荐：先用密码登录，拿会话令牌（支持登出与过期）
curl -s -X POST http://localhost:5000/api/auth/login \
  -H 'Content-Type: application/json' \
  -d '{"password":"your-password","remember":true}'
# => {"ok":true,"token":"..."}

TOKEN=...   # 上面的返回值

curl -s -H "Authorization: Bearer $TOKEN" \
  'http://localhost:5000/api/fs/read?path=README.md'
```

也接受静态凭据（与会话令牌等效，无登出/过期语义，适合本机脚本）：

```bash
# authToken 就是 <magic_home>/.auth_token 文件内容（bcrypt 哈希本身）
curl -s -H "Authorization: Bearer $(cat ~/.magic/.auth_token)" \
  'http://localhost:5000/api/fs/list?path=.'
# 等价写法
curl -s -H "X-Magic-Session-Token: $(cat ~/.magic/.auth_token)" \
  'http://localhost:5000/api/fs/list?path=.'
```

不接受 Cookie 承载登录态；`?token=` 一律 401。

---

## 4. 票据模型

### 4.1 结构

```
base64url("v1" ␀ scope ␀ path ␀ opts ␀ exp) . base64url(hmac_sha256(key, payload)[:16])
```

* `scope` 决定**只能做什么**，消费端严格匹配，作用域之间不可互相顶替。
* `path` 在**签发时**就由服务端解析成绝对路径（工作区/上传根边界已在签发前校验），消费端只验签名，不再接受任何客户端传入的路径。
* `opts` 目前仅 zip 使用：含 `h` 表示归档包含隐藏文件。
* `exp` 是硬性过期时间戳。
* 校验失败（缺段、签名不符、版本不认识、已过期、路径越界）**统一返回同一个错误**，不区分原因，避免成为攻击者的探测信号。

### 4.2 作用域与有效期

| scope | 解锁的动作 | `path` 语义 | 消费端点 | TTL |
|-------|-----------|-------------|----------|-----|
| `read` | 单文件内联读取（`<img src>`、新标签页打开） | 会话工作区内的绝对路径（文件） | `GET /api/fs/ticket/<sig>` | 1 小时 |
| `download` | 单文件附件下载（`<a href>`） | 同上（文件） | `GET /api/fs/ticket/<sig>` | 1 小时 |
| `zip` | 文件/目录打包下载 | 同上（可为目录） | `GET /api/fs/ticket/<sig>` | 1 小时 |
| `uploads` | 会话上传的附件 | **上传根内的相对路径**（如 `sess-1/abc.png`） | `GET /api/fs/ticket/<sig>` | 1 小时 |
| `serve` | 静态网页预览（托管一个目录） | 工作区内的绝对路径（目录，或文件所在目录） | `GET /api/fs/serve/<sig>/<subpath>` | 24 小时 |
| `events` | 全局 SSE 事件流 | 无 | `GET /api/events?sig=<sig>` | 24 小时 |

一次性动作票据只给 1 小时（客户端在真正发起请求前才去换）；`serve` 要被 iframe 内的相对子资源反复继承、`events` 要扛过 EventSource 的自动重连，因此按天给。

### 4.3 为什么 serve 的凭据必须写在**路径**里

`/api/fs/serve/<sig>/<subpath>`，而不是 query：

1. `index.html` 里的相对引用（`assets/style.css`）按 RFC 3986 §5.3 只做**路径合并**，query 会被整段丢弃——凭据挂在 query 上必然全部丢失，每个 css/js/图片都会 401。
2. iframe 内的子资源请求（`img`/`link`/`script`）带不上 `Authorization` 头。
3. 预览 iframe 刻意使用 `sandbox="allow-scripts"`（不含 `allow-same-origin`），此时 cookie 同样不会随子资源发送——而这正是阻断被预览页面读取 `localStorage.auth_token` 的隔离手段。

路径段是唯一「能被相对引用自动继承、又不依赖任何浏览器凭据」的位置。

### 4.4 票据不是一次性

同一张票据在 TTL 内可重复使用（`<img>` 可能被重绘、视频要靠 Range 请求拖拽、EventSource 会重连）。因此：

* 不要把票据写进日志/工单/公开文档；
* 不要缓存超过响应里给出的 `expires_in`；
* 需要更细粒度的收窄，就换更小范围的 `path`（票据只授权签发载荷里那一个文件/目录）。

---

## 5. 换取票据：`POST /api/fs/sign`

* 认证：**必须**带请求头凭据（`requireAuth`），这是登录凭据唯一出现的地方。
* 请求体（JSON，最大 64 KiB）：

| 字段 | 类型 | 说明 |
|------|------|------|
| `scope` | string | `read` / `download` / `zip` / `uploads` / `serve` / `events`，缺省 `serve`（向后兼容老前端） |
| `path` | string | `events` 不需要；`uploads` 用上传根内相对路径；其余用会话工作区路径 |
| `session_id` | string | 可选，工作区边界判定用（`uploads` 不需要） |
| `hidden` | bool | 仅 `zip`：归档是否包含隐藏文件 |

* 响应：`{"url":"...","expires_in":3600}`（`serve` 为 `{"url":"/api/fs/serve/<sig>/<entry>","expires_in":86400}`）。

```bash
# 换一张 read 票据，再用它给 <img src>
URL=$(curl -s -X POST http://localhost:5000/api/fs/sign \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"scope":"read","path":"/home/me/proj/cover.png"}' | jq -r .url)

curl -s -o cover.png "http://localhost:5000$URL"

# SSE：换 events 票据后交给 EventSource
curl -s -X POST http://localhost:5000/api/fs/sign \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"scope":"events"}'
# => {"url":"/api/events?sig=...","expires_in":86400}
```

前端 JS 等价写法（`web/src/api/sessions.ts` 已封装为 `signFSTicket()`）：

```js
const res = await fetch('/api/fs/sign', {
  method: 'POST',
  headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
  body: JSON.stringify({ scope: 'read', path }),
})
const { url } = await res.json()   // 交给 <img src={url}>
```

Python：

```python
import requests
s = requests.Session()
s.headers['Authorization'] = f'Bearer {token}'
ticket = s.post(f'{base}/api/fs/sign', json={'scope': 'download', 'path': p}).json()['url']
data = s.get(f'{base}{ticket}').content      # 或直接丢给 <a href>
```

### 5.1 常见错误码

| 状态 | 场景 |
|------|------|
| 401 `authentication required...` | `/api/fs/sign`、`/api/events` 没带有效凭据（或服务端尚未初始化） |
| 400 `invalid request body` / `unknown ticket scope: x` | 请求体非法 / scope 拼错 |
| 400 `path is a directory` | `read` / `download` 指向目录 |
| 400 `directory has no index.html to preview` | `serve` 指向的目录没有 `index.html`（提前报错，避免 iframe 收到一片空白） |
| 403 `invalid or expired ticket` | 票据被改、过期、或作用域串用（如把 `read` 票据塞进 `/api/fs/serve/`） |
| 404 `resource not found` / `attachment not found` | 签发目标不存在 |

---

## 6. 受保护资源速查

| 动作 | 端点 | 认证方式 |
|------|------|----------|
| 列目录、读文本、写、删、改名、上传 | `GET/POST /api/fs/{list,read,write,delete,rename,upload}` | 请求头 |
| 下载单个文件（脚本/curl） | `GET /api/fs/download?path=...` | 请求头 |
| 打包下载（脚本/curl） | `GET /api/fs/zip?path=...&hidden=1` | 请求头 |
| 下载/内联展示（浏览器标签、`<img>`、`<a>`） | `GET /api/fs/ticket/<sig>` | 票据（`read`/`download`/`zip`/`uploads`） |
| 静态网页预览 | `GET /api/fs/serve/<sig>/<subpath>` | 票据（`serve`） |
| 会话上传附件 | `GET /api/uploads/<session>/<file>` | 请求头（`<img>` 场景用 `uploads` 票据） |
| 实时事件流 | `GET /api/events` | 请求头，或 `?sig=` 的 `events` 票据 |
| 对外分享链接 | `GET /api/fs/shared/<token>` | 分享令牌本身（无关登录态，TTL 约束） |
| 跨机器 relay | `POST /api/relay/v1/dm` | 请求体内的 relay 令牌 |

---

## 7. 迁移指南（`?token=` 删除之后）

### 7.1 旧写法 → 新写法

| 旧写法 | 新写法 |
|--------|--------|
| `GET /api/fs/read?path=p&token=<authToken>` | `GET /api/fs/read?path=p` + `Authorization: Bearer <token>` |
| `<img src="/api/fs/read?path=p&token=...">` | `POST /api/fs/sign {"scope":"read","path":p}` → `<img src={url}>` |
| `<a href="/api/fs/download?path=p&token=...">` | `scope:"download"` |
| `/api/fs/zip?path=p&hidden=1&token=...` | `scope:"zip"` + `hidden:true` |
| `/api/uploads/...?token=...` | `scope:"uploads"` + 上传根内相对路径 |
| `/api/fs/serve?path=dir&token=...` | `scope:"serve"` → `/api/fs/serve/<sig>/<entry>` |
| `new EventSource('/api/events?token=...')` | `scope:"events"` → `new EventSource('/api/events?sig=...')` |

### 7.2 兼容性边界（哪些旧东西仍然可以工作）

* **历史会话里落库的附件引用**可能仍带 `?token=`（旧上传响应留下的）。Web 端在读取时会剥离该查询参数（`web/src/api/sessions.ts` 中的 `stripLegacyUrlToken`），因此老会话照常显示，**不需要数据迁移**。
* **CLI / TUI** 直接读写本地文件系统，不经过这些 HTTP 端点，不受影响。
* **桌面端（go-magic-desktop）**启动的是同一个 Go 二进制并内嵌同一套 Web UI，自动跟随新模型，无需改动。
* **跨机器 relay**（`bot_mode.relay_token`）与会话令牌是两套独立校验：`/api/relay/v1/dm` 只校验请求体里的共享密钥（常量时间比较），未配置密钥时仅接受本机回环（127.0.0.1）请求；peer 表的增删查（`/api/peers`）属于管理接口，与其他 `/api/*` 一样要求会话令牌。
* **对外分享链接** `/api/fs/shared/<token>` 里的 token 由 `POST /api/fs/share` 签发，是「给站外的人看一个文件」的独立凭据，不是登录凭据，保持不变（默认带 TTL，目录列举会跳过隐藏文件）。
* 需要**带请求头**的场景（脚本、`curl`、服务端集成）行为完全不变：`/api/fs/read`、`/api/fs/download`、`/api/fs/zip`、`/api/uploads/`、`/api/events` 都照旧接受请求头凭据。

### 7.3 只有这些场景会被破坏

任何**外部脚本 / 第三方集成**如果把登录凭据拼进 URL（`?token=`），现在会收到 401：

```
{"error":"authentication required, please setup via /api/auth/setup first"}
```

按 7.1 的对照表改成「脚本用请求头、浏览器用票据」即可。

---

## 8. 排障

| 现象 | 原因 / 处理 |
|------|------------|
| 图片 403 `invalid or expired ticket` | 票据过期（`read` 只有 1 小时）→ 重新换票；或票据被截断/改写 |
| 预览 403 `invalid or expired preview link` | `serve` 票据过期（24 小时）或签名被改；重新调 `/api/fs/sign` |
| 预览页面里 css/js 全 401 | 凭据被放在 query 而不是路径；必须用 `/api/fs/serve/<sig>/<subpath>` 形态，且不要让反代改写路径 |
| 某个文件 403、其它正常的 `read` 票据不生效 | 作用域串用：`read` 票据只能走 `/api/fs/ticket/<sig>`，`serve` 票据只能走 `/api/fs/serve/` |
| SSE 断线后无法自动重连 | 反代丢掉/改写了 `?sig=` 查询参数，或票据在重连前过期；正向代理需透传 query |
| 重置密码后所有预览/下载链接失效 | 预期行为：签名密钥派生自 `authToken` |
| 签发直接 500 `ticket signing unavailable` | 服务端尚未完成初始化（`authToken` 为空），先访问 `/api/auth/setup` |
| 想临时绕过票据 | 不要加回 `?token=`；改用请求头认证（脚本场景），或签发一张范围更小的票据 |
