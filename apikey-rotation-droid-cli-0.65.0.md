# Droid CLI 0.65.0：请求鉴权与 `FACTORY_API_KEY` 轮换（给 Electron 集成用）

本文基于本仓库的反编译源码 `/Users/clive/dev/ddc/source/0.65.0/droid.js`（版本 **0.65.0**）整理，目标是：

1. 讲清楚 Droid CLI 是如何发 HTTP 请求、如何附带鉴权（`Authorization`）。
2. 讲清楚 `FACTORY_API_KEY` 是否会随“所有请求”携带，以及携带范围。
3. 给出一个在 **Electron 应用**中实现“多把 key 均匀使用”的可落地方案（不改 CLI 二进制）。

---

## 1. 关键结论（TL;DR）

1. Droid CLI 对 Factory API 的请求，走统一封装 `zL()`，内部用 `fetch()` 发请求。`zL()` 会根据 `baseUrl` 决定是否注入鉴权/追踪等 header。  
   代码位置：`/Users/clive/dev/ddc/source/0.65.0/droid.js:131`

2. CLI 的鉴权 header 由 `_U()` 生成，逻辑是：
   - 优先使用“登录态 access token”（`Dz()` 返回的 `access_token`），写入 `Authorization: Bearer <access_token>`
   - 如果没有 token，则使用环境变量 `FACTORY_API_KEY`，写入 `Authorization: Bearer <FACTORY_API_KEY>`
   - 如果两者都没有，会抛错（无法鉴权）
   代码位置：`/Users/clive/dev/ddc/source/0.65.0/droid.js:284`

3. 因为 `zL()` 是“统一入口”，所以 **不只有跑模型**会带上 `Authorization`：诸如 org/session/日志上报等，只要请求落在 `FACTORY_API_BASE_URL` 下并通过 `zL(..., lI())` 发出，也会带鉴权。  
   示例：  
   - `/api/cli/org`：`/Users/clive/dev/ddc/source/0.65.0/droid.js:7825`  
   - `/api/sessions/${id}/droid-status`：`/Users/clive/dev/ddc/source/0.65.0/droid.js:470`

4. 0.65.0 没有“官方 hook 点”让你拦截所有 HTTP 请求并改 header；想做 key 轮换，推荐 **反向代理方案**：  
   - 启一个本地 HTTP 代理（反代到 `https://api.factory.ai`）
   - 通过 `FACTORY_API_BASE_URL=http://127.0.0.1:<port>` 让 CLI 把请求发到本地代理
   - 代理对部分路径（推荐仅 `/api/llm/`）把 `Authorization` 替换成轮询选择的 key

仓库已提供一个可直接复用的代理脚本：  
`/Users/clive/dev/ddc/scripts/factory-api-proxy-rotate-keys.mjs`

---

## 2. CLI 请求与鉴权注入：源码链路

### 2.1 `zL()`：统一请求封装

在 0.65.0 里，`zL()` 的行为要点（简化表述）：

1. 获取 `apiConfig`（入参传入或 `JN()` 返回的全局 config）。
2. 如果传入的是相对路径（以 `/` 开头）并且 `apiConfig.baseUrl` 存在，会拼成绝对 URL：`baseUrl + path`。
3. 如果目标 URL 被认为属于 `baseUrl`（同域/同 base），则：
   - 调用 `apiConfig.getHeaders()` 获取鉴权/客户端标识 header
   - 注入 tracing header（`D0.injectContext(q)`）
   - 合并你传入的 `init.headers`
4. 调用 `fetch(url, init)` 发出请求；若 `!response.ok` 会读取 `text()` 并抛出错误（`lg`）。

代码位置：`/Users/clive/dev/ddc/source/0.65.0/droid.js:131`

> 这意味着：对 Factory API（baseUrl）的大多数请求，都有一个共同的“加 header”路径，非常适合在网络层做统一改写（比如代理）。

### 2.2 `lI()`：Factory API 的 config（baseUrl + getHeaders）

`lI()` 返回：

- `baseUrl: I0().apiBaseUrl`
- `getHeaders: _U`

代码位置：`/Users/clive/dev/ddc/source/0.65.0/droid.js:284`

`I0().apiBaseUrl` 的来源：

- 默认：`P_A="https://api.factory.ai"`（与 `M_A="https://app.factory.ai"` 一起硬编码）
  代码位置：`/Users/clive/dev/ddc/source/0.65.0/droid.js:109`
- 允许通过环境变量覆盖：`process.env.FACTORY_API_BASE_URL || P_A`
  代码位置：`/Users/clive/dev/ddc/source/0.65.0/droid.js:131`

> 这给了我们“无侵入 hook”入口：在 Electron 启动 CLI 子进程时注入 `FACTORY_API_BASE_URL` 指向本地代理即可。

### 2.3 `_U()`：鉴权 header 的生成（token 优先，其次 API key）

`_U()` 的逻辑要点（简化表述）：

1. `await Dz()`：如果拿到 `access_token`，则返回：
   - `Authorization: Bearer <access_token>`
   - 以及额外的客户端标识 header（比如 `X-Factory-Client: cli`）
2. 否则读取 `process.env.FACTORY_API_KEY?.trim()`：如果存在，则返回：
   - `Authorization: Bearer <FACTORY_API_KEY>`
   - 同样合并客户端标识 header
3. 否则抛错（缺少鉴权）

代码位置：`/Users/clive/dev/ddc/source/0.65.0/droid.js:284`

#### 2.3.1 对轮换策略的直接影响

- 如果你希望“轮换 `FACTORY_API_KEY`”，需要明确 CLI 实际发出的 `Authorization` 是谁：
  - **已登录**：CLI 默认发 `Bearer <access_token>`（不是 `FACTORY_API_KEY`）
  - **未登录**：CLI 才会发 `Bearer <FACTORY_API_KEY>`

如果你采用“代理覆盖 Authorization”的方案，那么无论 CLI 原本带的是 token 还是 key，代理都可以在需要的路径上覆盖为你选定的 key。

---

## 3. `FACTORY_API_KEY` 会附带在哪些请求上？

不要按“接口类型（模型/非模型）”理解，而应按“是否属于 Factory API baseUrl 且走 `zL()`”理解：

### 3.1 会带鉴权的典型请求

下面这些都在源码里直接可见使用 `zL(..., lI())`：

- 获取 org：`/api/cli/org`  
  代码位置：`/Users/clive/dev/ddc/source/0.65.0/droid.js:7825`

- session 状态同步：`/api/sessions/${id}/droid-status`  
  代码位置：`/Users/clive/dev/ddc/source/0.65.0/droid.js:470`

- 上报失败请求日志（LLM error logger）：`POST /api/llm/failed-requests`  
  代码位置：`/Users/clive/dev/ddc/source/0.65.0/droid.js:5749`（同段落内的 `fetch(.../api/llm/failed-requests...)`）

因此，“只跑模型才带 key”这个假设不成立：**只要是 Factory API（baseUrl）且通过 `zL()`，都会带上 `Authorization`。**

### 3.2 不会带 `FACTORY_API_KEY` 的情况

以下请求不属于 Factory API baseUrl（或不走 `zL()`），通常不会带 `FACTORY_API_KEY`：

- 指向第三方域名的请求（例如 WorkOS 的 JWKS、下载资源等）
- BYOK（自定义模型）请求第三方 base_url 时，带的是你在 `custom_models` 里配置的第三方 key（不是 Factory key）

---

## 4. 轮换多个 API key：可选方案对比

你有很多 `FACTORY_API_KEY`，想“均匀用量”。常见有三种实现层级：

### 方案 A：按“进程启动”轮换（最简单、粒度最粗）

思路：每次启动 CLI 子进程时，从 key 池挑一个，设置 `FACTORY_API_KEY=<selected>`。

优点：
- 实现最简单
- 不会在同一个 session 内出现“身份跳变”

缺点：
- 如果一次运行会发很多模型请求，仍会集中在单一 key 上
- 均匀性取决于你启动次数与运行时长

适用：你只需要大致均衡，而不是严格按请求/按 token 均衡。

### 方案 B：本地反向代理按请求轮换（推荐，可控）

思路：
1. Electron 主进程启动一个本地 HTTP server（反代到 `https://api.factory.ai`）。
2. CLI 子进程设置 `FACTORY_API_BASE_URL=http://127.0.0.1:<port>`，让所有 Factory API 请求先到本地代理。
3. 代理根据请求路径决定是否轮换，并覆盖 `Authorization`。

优点：
- 不改 CLI 二进制
- 可做到“按请求轮询”甚至“按路径轮询”（只轮换模型相关接口）
- Electron 集成自然：本地 server 直接跑在主进程

缺点/风险：
- 如果对“身份相关接口”（session/org 等）也轮换，可能出现 401/403 或 session 归属混乱

**建议默认只对 `/api/llm/` 前缀做轮换**，其它请求保持原样。

本仓库提供的参考实现：  
`/Users/clive/dev/ddc/scripts/factory-api-proxy-rotate-keys.mjs`

### 方案 C：直接修改/patch CLI（不推荐）

思路：修改 bundle（`droid.js`）里 `_U()` 或 `zL()`。

缺点：
- 维护成本高（每次升级都要重新 patch）
- 风险大（不透明、容易破坏签名/自更新/行为）

除非你明确要做 fork，否则不建议。

---

## 5. 推荐实现：Electron 内置反代 + 轮询换 key

### 5.1 代理需要做什么？

最小可用代理行为：

1. 接收来自 CLI 的请求（HTTP/1.1）。
2. 转发到上游 `https://api.factory.ai`（或你的目标 baseUrl）。
3. 复制请求方法、路径、query、body、headers（注意 hop-by-hop header、content-length）。
4. 在需要轮换的请求上，设置（覆盖）：
   - `Authorization: Bearer <selectedKey>`
5. 把上游响应状态码、响应头、响应体原样回写给 CLI。

### 5.2 为什么建议“只轮换 /api/llm/”？

因为 `/api/cli/org`、`/api/sessions/...` 等接口可能与用户/org/session 的权限绑定。  
如果你把它们也轮换，会导致：

- 同一个 session 的后续写入可能被另一个 key 拒绝（权限不同）
- CLI 认为自己“已认证/未认证”的状态在多个 key 间跳变

因此建议：

- LLM 相关（例如 `/api/llm/...`）做轮换，达到“均匀消耗”
- 身份/会话相关请求保持一致（token 或固定 key）

### 5.3 本仓库提供的脚本（可直接搬进 Electron 主进程）

脚本：`/Users/clive/dev/ddc/scripts/factory-api-proxy-rotate-keys.mjs`

默认行为：
- 监听：`127.0.0.1:8787`
- 上游：`https://api.factory.ai`
- **仅当 path 以 `/api/llm/` 开头**时轮询选择 key，并覆盖 `Authorization`

用法示例：
```bash
# 启动代理（key 列表用逗号或换行分隔）
FACTORY_API_KEYS="fk_a,fk_b,fk_c" node /Users/clive/dev/ddc/scripts/factory-api-proxy-rotate-keys.mjs

# 启动 droid，并让它走本地代理
FACTORY_API_BASE_URL="http://127.0.0.1:8787" droid
```

可选环境变量：
- `LISTEN_HOST` / `LISTEN_PORT`
- `FACTORY_API_BASE_URL_TARGET`（上游）
- `ROTATE_PATH_PREFIX`（默认 `/api/llm/`）
- `ROTATE_ALL=true`（对所有路径轮换，慎用）
- `DEBUG_PROXY=true`（仅打印模式/方法/路径；**不要打印 key**）

### 5.4 Electron 集成建议（主进程）

推荐做法：

1. 主进程启动代理 server（可直接复用脚本逻辑或拷贝成模块）。
2. 监听端口建议用 `0` 让系统分配空闲端口，然后把实际端口注入 CLI env：
   - `FACTORY_API_BASE_URL=http://127.0.0.1:<dynamicPort>`
3. CLI 子进程仍然需要能通过 `_U()` 生成某种 `Authorization`，否则会直接报“缺少鉴权”：
   - 最简单：给 `FACTORY_API_KEY` 赋一个“兜底 key”（比如 key 池第 1 个）
   - 然后由代理对 `/api/llm/` 覆盖成轮询 key；非 `/api/llm/` 则沿用 CLI 传来的 `Authorization`

> 如果你希望“非 /api/llm/ 的请求也不固定在某一个 key 上”，就要非常确认这些 key 是否具备完全一致的权限与身份归属，并且要接受潜在的不稳定。

### 5.5 想要“更均匀”（跨重启保持轮询位置）

如果你希望在 Electron 重启后仍然保持均匀分布，可以在主进程持久化一个计数器（`rrIndex`）：

- 每次选 key：`key = keys[rrIndex % keys.length]; rrIndex++`
- 定期把 `rrIndex` 写入本地文件或 Electron store

注意：
- 多进程/多窗口并发时，需要加锁或用单一主进程仲裁，避免多个代理各自轮询导致分布偏差。

---

## 6. 合规与风险提示（务必在产品设计里写清楚）

1. 仅在你对这些 key 的使用具有明确授权的前提下轮换使用。
2. 不建议把 key 写入日志/崩溃上报/遥测。
3. 如果 key 代表不同用户/组织身份，按请求轮换会导致权限错误甚至数据归属混乱；至少应限制为 `/api/llm/` 这类无状态/弱状态接口。

---

## 7. 附：可直接引用的源码点（快速定位）

- `zL()`：`/Users/clive/dev/ddc/source/0.65.0/droid.js:131`
- `_U()` 与 `lI()`：`/Users/clive/dev/ddc/source/0.65.0/droid.js:284`
- 默认 baseUrl 常量：`/Users/clive/dev/ddc/source/0.65.0/droid.js:109`
- baseUrl env 覆盖：`/Users/clive/dev/ddc/source/0.65.0/droid.js:131`
- `zL("/api/cli/org", ..., lI())`：`/Users/clive/dev/ddc/source/0.65.0/droid.js:7825`
- `zL(\`/api/sessions/${id}/droid-status\`, ..., lI())`：`/Users/clive/dev/ddc/source/0.65.0/droid.js:470`

