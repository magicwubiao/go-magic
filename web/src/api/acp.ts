import { request } from './client'

/**
 * ACP（Agent Client Protocol）外部 agent 的管理接口。
 *
 * 配置落在 magic home 下的 acp.json（不是 config.json），由服务端
 * internal/acp 自己读写；这里只负责 CRUD 与"真的连一次"。
 */

export interface ACPAgent {
  name: string
  command: string
  args?: string[]
  /** KEY=VALUE 列表；值以 *** 回显（纯 ${VAR} 引用不掩码） */
  env?: string[]
  cwd?: string
  /** 会话工作目录，也是 agent 可访问文件的根 */
  workspace?: string
  additional_dirs?: string[]
  description?: string
  timeout_seconds?: number
  /** 允许 agent 通过 fs/write_text_file 改文件 */
  allow_write: boolean
  /** 自动批准的工具种类；"all" = 全放行 */
  auto_approve?: string[]
  enabled: boolean
  connected: boolean
}

export interface ACPAgentList {
  config_path: string
  agents: ACPAgent[]
  /** 三个 acp_* 工具当前是否对模型可见（没有启用的 agent 时为 false） */
  tools_available: boolean
}

export interface ACPCapabilities {
  load_session: boolean
  image: boolean
  audio: boolean
  embedded_context: boolean
  mcp_http: boolean
  mcp_sse: boolean
}

export interface ACPTestResult {
  ok: boolean
  agent: string
  elapsed_ms: number
  protocol_version: number
  session_id: string
  roots: string[]
  agent_info: { name?: string; title?: string; version?: string }
  auth_methods: string[]
  capabilities: ACPCapabilities
}

/** 保存时提交的载荷（name 与路径不同 = 改名） */
export type ACPAgentPayload = Omit<ACPAgent, 'connected'>

export async function listACPAgents(): Promise<ACPAgentList> {
  return request<ACPAgentList>('/acp/agents')
}

export async function createACPAgent(agent: ACPAgentPayload): Promise<{ ok: boolean; name: string }> {
  return request<{ ok: boolean; name: string }>('/acp/agents', {
    method: 'POST',
    body: JSON.stringify(agent),
  })
}

export async function updateACPAgent(name: string, agent: ACPAgentPayload): Promise<{ ok: boolean; name: string }> {
  return request<{ ok: boolean; name: string }>(`/acp/agents/${encodeURIComponent(name)}`, {
    method: 'PUT',
    body: JSON.stringify(agent),
  })
}

export async function deleteACPAgent(name: string): Promise<void> {
  return request<void>(`/acp/agents/${encodeURIComponent(name)}`, { method: 'DELETE' })
}

/**
 * 建连并返回协商结果。
 *
 * 这里真的会拉子进程（initialize + session/new），冷启动常见几秒到几十秒
 * ——npx 首次安装适配器包时更久，所以超时给到 100s（服务端上限 90s，
 * 留 10s 给网络往返与 JSON 编码）。
 */
export async function testACPAgent(name: string): Promise<ACPTestResult> {
  return request<ACPTestResult>(`/acp/agents/${encodeURIComponent(name)}/test`, {
    method: 'POST',
    timeoutMs: 100000,
  })
}

export async function resetACPAgentSession(name: string): Promise<void> {
  return request<void>(`/acp/agents/${encodeURIComponent(name)}/reset`, {
    method: 'POST',
    timeoutMs: 100000,
  })
}

/** 关闭缓存连接（保留配置）；下次调用会重新拉起。 */
export async function disconnectACPAgent(name: string): Promise<void> {
  return request<void>(`/acp/agents/${encodeURIComponent(name)}/disconnect`, { method: 'POST' })
}
