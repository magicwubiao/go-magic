// 右侧栏 Web 终端 API：每个终端会话在服务端只保留一个持久 cwd，
// exec 用一次性 shell 进程执行并回传合并输出（无 PTY，Windows 兼容）。
import { request } from './client'

export interface TerminalSession {
  id: string
  cwd: string
}

export interface TerminalExecResult {
  output: string
  exit_code: number
  cwd: string
  duration_ms: number
}

export function createTerminal(sessionId?: string): Promise<TerminalSession> {
  return request<TerminalSession>('/terminal', {
    method: 'POST',
    body: JSON.stringify({ session_id: sessionId || '' }),
  })
}

export function getTerminal(id: string): Promise<TerminalSession> {
  return request<TerminalSession>(`/terminal?id=${encodeURIComponent(id)}`, { method: 'GET' })
}

export function execTerminal(id: string, command: string): Promise<TerminalExecResult> {
  return request<TerminalExecResult>('/terminal/exec', {
    method: 'POST',
    body: JSON.stringify({ id, command }),
    retries: 0,
  })
}

export function closeTerminal(id: string): Promise<{ ok: boolean }> {
  return request<{ ok: boolean }>(`/terminal?id=${encodeURIComponent(id)}`, { method: 'DELETE' })
}
