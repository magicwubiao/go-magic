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
  tools: CLIToolSpec[]
}

export interface CLIConnectorView extends CLIConnector {
  name: string
  /** 展开后的工具注册名 */
  tool_names: string[]
  /** 当前分身下是否生效 */
  active: boolean
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
