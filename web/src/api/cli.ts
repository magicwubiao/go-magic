import { request } from './client'

/**
 * CLI 连接器：把只有命令行接口的外部程序（gh / kubectl / ffmpeg / 内部脚本）
 * 声明成连接器，每个子命令展开成 cli_<connector>_<tool> 工具。
 *
 * 与 MCP 的差别是"无状态"：没有连接/断开，每次调用拉起一个新进程。
 */

export interface CLIParam {
  name: string
  /** string | integer | number | boolean，缺省 string */
  type?: string
  description?: string
  required?: boolean
  default?: unknown
  enum?: string[]
}

export interface CLIToolSpec {
  /** 工具短名，注册名是 cli_<connector>_<name> */
  name: string
  description: string
  /**
   * 参数模板，一行一个。支持：
   *   - {{name}}        值替换，整行只有它且值为空时该参数省略
   *   - {{?name}}--flag 条件开关，name 为真时才带上 --flag
   */
  args?: string[]
  /** 把哪个参数写进 stdin */
  stdin_param?: string
  params?: CLIParam[]
  timeout_seconds?: number
  /** 声明只读：不修改任何状态，可免人工确认 */
  read_only?: boolean
}

export interface CLIConnector {
  /** 可执行文件名或绝对路径 */
  command: string
  /** 每个工具都会带上的公共参数 */
  args?: string[]
  description?: string
  working_dir?: string
  /** KEY=VALUE，值在接口返回里一律是 *** */
  env?: string[]
  timeout_seconds?: number
  enabled?: boolean
  profiles?: string[]
  /** 强制每个工具都走人工确认（优先级高于工具自己的 read_only） */
  require_approval?: boolean
  /** OAuth 设备码：由本程序取 token 并注入 token_env，执行前自动刷新 */
  oauth?: CLIOAuthSpec
  tools: CLIToolSpec[]
}

/** 连接器级 OAuth（RFC 8628 设备码）。client_secret 回显为 ***。 */
export interface CLIOAuthSpec {
  device_authorization_endpoint: string
  token_endpoint: string
  client_id: string
  client_secret?: string
  scopes?: string[]
  /** 执行时注入 access token 的环境变量名 */
  token_env: string
  audience?: string
}

export interface CLIConnectorView extends CLIConnector {
  name: string
  /** 展开后的工具注册名 */
  tool_names: string[]
  /** 当前分身下是否生效 */
  active: boolean
  /** 声明了设备码配置 */
  oauth_configured?: boolean
  /** authorized / awaiting_authorization / none / expired / failed */
  oauth_state?: string
  oauth_user_code?: string
  oauth_verification_uri?: string
}

export interface CLITestResult {
  ok: boolean
  error?: string
  elapsed_ms: number
  timeout_sec?: number
  result?: {
    command?: string
    exit_code?: number
    stdout?: string
    stderr?: string
    duration_ms?: number
    truncated?: boolean
    truncated_hint?: string
    hint?: string
  }
}

/** 目录条目需要的环境变量（安装时由用户填）。 */
export interface CLICatalogRequirement {
  key: string
  label: string
  secret?: boolean
  required?: boolean
  placeholder?: string
  help_url?: string
}

/** 内置目录中的一个 CLI 工具（带本机安装状态）。 */
export interface CLICatalogEntry {
  id: string
  name: string
  description: string
  category: string
  /** 建议的连接器名，也是工具前缀 */
  default_name: string
  connector: {
    command: string
    description?: string
    tools: CLIToolSpec[]
  }
  requires?: CLICatalogRequirement[]
  docs_url?: string
  tags?: string[]
  /** 命令在本机 PATH 上找得到 */
  installed: boolean
  install_path?: string
  /** 版本探测的首行输出（如 "gh version 2.40.0"） */
  version?: string
  /** 当前平台的安装命令（仅展示，服务端不执行） */
  install_commands: string[]
  /** 当前平台的登录命令（仅展示，服务端不执行） */
  login_commands?: string[]
  /**
   * 可由服务端代跑的登录参数（不含命令本身）。
   * 有值 = 详情页会出现"一键登录"，用户不用打开终端。
   */
  login_exec?: string[]
  /** 就绪检查结论：undefined 表示该条目没有检查项或尚未检查 */
  ready?: boolean
  /** 就绪检查的一句话说明（当前 context / 失败原因） */
  ready_message?: string
  /** 同名连接器已存在 */
  added: boolean
}

export interface CLICatalogCategory {
  id: string
}

/** 列出内置 CLI 目录（含本机安装检测结果）。 */
export async function listCLICatalog(): Promise<{
  entries: CLICatalogEntry[]
  categories: CLICatalogCategory[]
}> {
  return request('/cli/catalog')
}

/**
 * 对一个目录条目做完整复检（版本 + 登录/服务状态）。
 * 只传目录 ID：命令与参数全部来自内置目录。
 */
export async function checkCLICatalogEntry(id: string): Promise<CLICatalogEntry> {
  return request('/cli/catalog/check', {
    method: 'POST',
    body: JSON.stringify({ id }),
  })
}

/**
 * 一键添加目录条目：服务端校验命令存在、写入 config.json、注册工具。
 * env 只接受目录声明过的键；疑似明文凭据默认拒绝，需显式 allowPlaintext。
 * 返回里带上装完的自检结果，前端可以直接展示"可用/不可用 + 原因"。
 */
export async function installCLICatalogEntry(
  id: string,
  name: string,
  env: Record<string, string>,
  allowPlaintext = false,
): Promise<{ success: boolean; name: string; ready?: boolean; ready_message?: string }> {
  return request('/cli/catalog/install', {
    method: 'POST',
    body: JSON.stringify({ id, name, env, allow_plaintext: allowPlaintext }),
  })
}

/** 命令行解析结果：连接器命令 + 工具草稿。 */
export interface CLIParseResult {
  command: string
  tool: CLIToolSpec
  warnings?: string[]
}

/**
 * 把一条完整命令行解析成工具草稿（后端无状态，不落盘）。
 * 这是「快速导入」的后端：用户粘贴 gh pr list --repo o/r --limit 20 即得表单草稿。
 */
export async function parseCLICommand(input: string): Promise<CLIParseResult> {
  return request<CLIParseResult>('/cli/parse', {
    method: 'POST',
    body: JSON.stringify({ input }),
  })
}

export async function listCLIConnectors(): Promise<CLIConnectorView[]> {
  return request<CLIConnectorView[]>('/cli/connectors')
}

export async function getCLIConnector(name: string): Promise<CLIConnectorView> {
  return request<CLIConnectorView>(`/cli/connectors/${encodeURIComponent(name)}`)
}

export async function saveCLIConnector(name: string, connector: CLIConnector): Promise<void> {
  return request<void>(`/cli/connectors/${encodeURIComponent(name)}`, {
    method: 'PUT',
    body: JSON.stringify(connector),
  })
}

export async function removeCLIConnector(name: string): Promise<void> {
  return request<void>(`/cli/connectors/${encodeURIComponent(name)}`, {
    method: 'DELETE',
  })
}

/**
 * 试跑一次（不保存）。后端会真的拉起子进程，返回 stdout/exit_code。
 * 超时按连接器的 timeout_seconds 计算，因此这里给足请求时间。
 *
 * connector.name 会被一起带上：已保存的连接器，env 回显的是 ***，
 * 后端据此把 *** 还原成磁盘真值再执行（否则拿 *** 当 token 跑必然 401）。
 */
export async function testCLIConnector(
  connector: CLIConnector & { name?: string },
  tool: string,
  params: Record<string, unknown>,
): Promise<CLITestResult> {
  const timeoutMs = Math.max(30_000, ((connector.timeout_seconds || 60) + 10) * 1000)
  return request<CLITestResult>('/cli/test', {
    method: 'POST',
    body: JSON.stringify({ name: connector.name ?? '', connector, tool, params }),
    timeoutMs,
  })
}

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

/** 把用户在弹窗里输入的内容转发给子进程 stdin（回答 "Press Enter" 之类的提问）。 */
export async function sendCLILoginInput(sessionId: string, text: string): Promise<void> {
  return request<void>(`/cli/catalog/login/${encodeURIComponent(sessionId)}/input`, {
    method: 'POST',
    body: JSON.stringify({ text }),
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

/** 发起设备码授权；已授权时 already_authorized = true。 */
export async function startCLIConnectorOAuth(name: string): Promise<{
  ok: boolean
  status: CLIOAuthStatus
  already_authorized?: boolean
  device?: CLIDeviceCode
}> {
  return request(`/cli/connectors/${encodeURIComponent(name)}/oauth/start`, { method: 'POST' })
}

export async function getCLIConnectorOAuth(name: string): Promise<{
  ok: boolean
  status: CLIOAuthStatus
  configured: boolean
}> {
  return request(`/cli/connectors/${encodeURIComponent(name)}/oauth/status`)
}

export async function signOutCLIConnectorOAuth(name: string): Promise<void> {
  return request<void>(`/cli/connectors/${encodeURIComponent(name)}/oauth/signout`, { method: 'POST' })
}

// === 插件内连接器的 OAuth（同一套设备码流程，token 键带插件命名空间） ===

export async function startPluginCLIOAuth(plugin: string, name: string): Promise<{
  ok: boolean
  status: CLIOAuthStatus
  already_authorized?: boolean
  device?: CLIDeviceCode
}> {
  return request(
    `/agent-plugins/${encodeURIComponent(plugin)}/cli/${encodeURIComponent(name)}/oauth/start`,
    { method: 'POST' },
  )
}

export async function getPluginCLIOAuth(plugin: string, name: string): Promise<{
  ok: boolean
  status: CLIOAuthStatus
  configured: boolean
}> {
  return request(
    `/agent-plugins/${encodeURIComponent(plugin)}/cli/${encodeURIComponent(name)}/oauth/status`,
  )
}

export async function signOutPluginCLIOAuth(plugin: string, name: string): Promise<void> {
  return request<void>(
    `/agent-plugins/${encodeURIComponent(plugin)}/cli/${encodeURIComponent(name)}/oauth/signout`,
    { method: 'POST' },
  )
}
