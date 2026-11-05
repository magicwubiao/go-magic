import { request } from './client'

/**
 * CLI 连接器的**运行期**接口：委托登录 + 插件内连接器的 OAuth 设备码授权。
 *
 * 连接器本身的增删改没有 HTTP 面了：CLI 应用统一由「插件」页的目录卡片
 * 装成 Agent Plugin（见 api/agentplugins），或来自配置文件里的 cli 段。
 * 这里只剩两件"必须跟进程打交道"的事：代跑别人的 login 命令、以及替
 * 只认环境变量的 CLI 申请 token。
 */

// === 委托登录（服务端代跑 CLI 自己的 login 命令） ===

export interface CLILoginSession {
  id: string
  catalog_id: string
  /** running | done | failed | cancelled */
  state: string
  /** 实际执行的命令行（展示给用户看，来源是内置目录） */
  command: string
  /** 子进程累计输出（含交互式提问） */
  output: string
  /** 从输出里抓到的授权页地址 */
  auth_url?: string
  /** 从输出里抓到的设备码 / 一次性短码 */
  user_code?: string
  /** failed 时的原因 */
  error?: string
  /** 就绪检查的一句话结论（登录成功后通常就是账号名） */
  check_message?: string
  truncated?: boolean
  started_at: string
  finished_at?: string
}

/** 开始（或复用）一次委托登录；只传目录 id。 */
export async function startCLILogin(id: string): Promise<{ ok: boolean; reused?: boolean; session: CLILoginSession }> {
  return request<{ ok: boolean; reused?: boolean; session: CLILoginSession }>('/cli/catalog/login', {
    method: 'POST',
    body: JSON.stringify({ id }),
  })
}

export async function getCLILoginSession(sessionId: string): Promise<{ ok: boolean; session: CLILoginSession }> {
  return request<{ ok: boolean; session: CLILoginSession }>(
    `/cli/catalog/login/${encodeURIComponent(sessionId)}`,
  )
}

/** 把用户在弹窗里输入的内容转发给子进程 stdin（回答 "Press Enter" 之类的提问）。
 *  secret=true 表示密码/令牌类敏感输入：服务端不镜像进输出缓冲，只写占位掩码。 */
export async function sendCLILoginInput(sessionId: string, text: string, secret = false): Promise<void> {
  return request<void>(`/cli/catalog/login/${encodeURIComponent(sessionId)}/input`, {
    method: 'POST',
    body: JSON.stringify({ text, secret }),
  })
}

export async function cancelCLILogin(sessionId: string): Promise<void> {
  return request<void>(`/cli/catalog/login/${encodeURIComponent(sessionId)}/cancel`, {
    method: 'POST',
  })
}

// === 连接器级 OAuth（设备码流程） ===

export interface CLIOAuthStatus {
  state: string
  error?: string
  user_code?: string
  verification_uri?: string
  redirect_uri?: string
}

export interface CLIDeviceCode {
  user_code: string
  verification_uri: string
  verification_uri_complete?: string
  expires_in?: number
  interval?: number
}

// === 插件内连接器的 OAuth（token 键带插件命名空间） ===

export async function startPluginCLIOAuth(plugin: string, name: string): Promise<{
  ok: boolean
  status: CLIOAuthStatus
  already_authorized?: boolean
  device?: CLIDeviceCode
}> {
  return request(
    `/plugins/${encodeURIComponent(plugin)}/cli/${encodeURIComponent(name)}/oauth/start`,
    { method: 'POST' },
  )
}

export async function getPluginCLIOAuth(plugin: string, name: string): Promise<{
  ok: boolean
  status: CLIOAuthStatus
  configured: boolean
}> {
  return request(
    `/plugins/${encodeURIComponent(plugin)}/cli/${encodeURIComponent(name)}/oauth/status`,
  )
}

export async function signOutPluginCLIOAuth(plugin: string, name: string): Promise<void> {
  return request<void>(
    `/plugins/${encodeURIComponent(plugin)}/cli/${encodeURIComponent(name)}/oauth/signout`,
    { method: 'POST' },
  )
}
