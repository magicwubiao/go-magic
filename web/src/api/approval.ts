import { request } from './client'

const API_BASE = '/approval'

// 前端统一使用 camelCase，字段映射在本文件内完成

export interface ApprovalHistoryRecord {
  id: string
  command: string
  normalized?: string
  riskLevel: string
  decision: string
  strategy: string
  durationMs: number
  workingDir?: string
  timestamp: string
}

export interface ApprovalHistoryResult {
  records: ApprovalHistoryRecord[]
  total: number
}

export interface ApprovalStats {
  totalRequests: number
  autoApproved: number
  userApproved: number
  userDenied: number
  riskDistribution: Record<string, number>
  topCommands: { command: string; count: number; riskLevel: string }[]
}

export interface TrustedPattern {
  pattern: string
  count: number
  lastSeen: string
  riskLevel?: string
}

export interface DeniedPattern {
  pattern: string
  count: number
  lastSeen?: string
  riskLevel?: string
}

export interface ApprovalSettings {
  strategy: string
  trustThreshold: number
  enableLearning: boolean
  cliConfirm: boolean
  whitelist: string[]
}

export interface PendingApproval {
  id: string
  command: string
  riskLevel: string
  sessionId?: string
  createdAt: string
  expiresAt?: string
}

// 任意原始记录类型，用于从后端 snake_case 到前端 camelCase 的映射
type AnyRecord = Record<string, any>

// 审批历史：后端返回 { records: [...], total: N }，记录字段为 snake_case
export async function getApprovalHistory(limit: number = 100, offset: number = 0): Promise<ApprovalHistoryResult> {
  const raw = await request<{ records?: AnyRecord[]; total?: number; items?: AnyRecord[] } | AnyRecord[]>(
    `${API_BASE}/history?limit=${limit}&offset=${offset}`,
  )
  // 兼容数组和对象两种返回格式
  const list: AnyRecord[] = Array.isArray(raw)
    ? raw
    : (raw?.records || raw?.items || [])
  const total = Array.isArray(raw) ? raw.length : (raw?.total ?? list.length)
  const records: ApprovalHistoryRecord[] = list.map((r: AnyRecord) => ({
    id: r.id || '',
    command: r.command || '',
    normalized: r.normalized,
    riskLevel: r.risk_level || r.riskLevel || 'low',
    decision: r.decision || '',
    strategy: r.strategy || '',
    durationMs: r.duration_ms ?? r.durationMs ?? 0,
    workingDir: r.working_dir || r.workingDir,
    timestamp: r.timestamp || '',
  }))
  return { records, total }
}

// 审批统计：后端返回 snake_case 字段
export async function getApprovalStats(): Promise<ApprovalStats> {
  const raw = await request<AnyRecord>(`${API_BASE}/stats`)
  return {
    totalRequests: raw.total_requests || raw.totalRequests || 0,
    autoApproved: raw.auto_approved || raw.autoApproved || 0,
    userApproved: raw.user_approved || raw.userApproved || 0,
    userDenied: raw.user_denied || raw.userDenied || 0,
    riskDistribution: raw.by_risk_level || raw.risk_distribution || raw.riskDistribution || {},
    topCommands: (raw.top_commands || raw.topCommands || []).map((c: AnyRecord) => ({
      command: c.pattern || c.command || '',
      count: c.count || 0,
      riskLevel: c.risk_level || c.riskLevel || 'low',
    })),
  }
}

// 受信任模式：后端返回 { patterns: [...], total: N }，记录字段为 snake_case
export async function getTrustedPatterns(): Promise<TrustedPattern[]> {
  const result = await request<{ patterns?: AnyRecord[]; total?: number }>(`${API_BASE}/patterns/trusted`)
  return (result?.patterns || []).map((p: AnyRecord) => ({
    pattern: p.pattern || '',
    count: p.count || 0,
    lastSeen: p.last_seen || p.lastSeen || '',
    riskLevel: p.risk_level || p.riskLevel,
  }))
}

// 被拒绝模式：后端返回 { patterns: [...], total: N }，记录字段为 snake_case
export async function getDeniedPatterns(): Promise<DeniedPattern[]> {
  const result = await request<{ patterns?: AnyRecord[]; total?: number }>(`${API_BASE}/patterns/denied`)
  return (result?.patterns || []).map((p: AnyRecord) => ({
    pattern: p.pattern || '',
    count: p.count || 0,
    lastSeen: p.last_seen || p.lastSeen,
    riskLevel: p.risk_level || p.riskLevel,
  }))
}

// 待审批：后端返回 { pending: [...], total: N }，记录字段为 snake_case
export async function getPendingApprovals(): Promise<PendingApproval[]> {
  const raw = await request<{ pending?: AnyRecord[]; total?: number }>(`${API_BASE}/pending`)
  const items = raw?.pending || []
  return items.map((p: AnyRecord) => ({
    id: p.id,
    command: p.command || '',
    riskLevel: p.risk_level || 'low',
    sessionId: p.session_id || '',
    createdAt: p.created_at || '',
    expiresAt: p.expires_at || '',
  }))
}

// 审批单个待审批项
export async function resolvePendingApproval(id: string, approved: boolean, reason: string = ''): Promise<void> {
  return request(`${API_BASE}/pending/${id}/resolve`, {
    method: 'POST',
    body: JSON.stringify({ approved, reason }),
  })
}

// 移除受信任模式
export async function removeTrustedPattern(pattern: string): Promise<void> {
  return request(`${API_BASE}/patterns/trusted`, {
    method: 'DELETE',
    body: JSON.stringify({ pattern }),
  })
}

// 清除被拒绝模式
export async function clearDeniedPattern(pattern: string): Promise<void> {
  return request(`${API_BASE}/patterns/denied`, {
    method: 'DELETE',
    body: JSON.stringify({ pattern }),
  })
}

// 获取白名单：后端返回 { patterns: [...] } 或 { whitelist: [...] }
export async function getWhitelist(): Promise<string[]> {
  const result = await request<{ patterns?: string[]; whitelist?: string[] } | string[]>(`${API_BASE}/whitelist`)
  if (Array.isArray(result)) return result
  return result?.patterns || result?.whitelist || []
}

// 添加白名单
export async function addWhitelist(pattern: string): Promise<void> {
  return request(`${API_BASE}/whitelist`, {
    method: 'POST',
    body: JSON.stringify({ pattern }),
  })
}

// 移除白名单
export async function removeWhitelist(pattern: string): Promise<void> {
  return request(`${API_BASE}/whitelist`, {
    method: 'DELETE',
    body: JSON.stringify({ pattern }),
  })
}

// 获取审批设置：后端返回 snake_case 字段
export async function getSettings(): Promise<ApprovalSettings> {
  const raw = await request<AnyRecord>(`${API_BASE}/settings`)
  return {
    strategy: raw.strategy || 'manual',
    trustThreshold: raw.trust_threshold ?? raw.trustThreshold ?? 5,
    enableLearning: raw.enable_learning ?? raw.enableLearning ?? false,
    cliConfirm: raw.cli_confirm ?? raw.cliConfirm ?? true,
    whitelist: raw.whitelist || [],
  }
}

// 保存审批设置：转换为后端 snake_case 字段
export async function saveSettings(settings: ApprovalSettings): Promise<void> {
  return request(`${API_BASE}/settings`, {
    method: 'PUT',
    body: JSON.stringify({
      strategy: settings.strategy,
      trust_threshold: settings.trustThreshold,
      enable_learning: settings.enableLearning,
      cli_confirm: settings.cliConfirm,
      whitelist: settings.whitelist,
    }),
  })
}

// 清理审批历史
export async function clearHistory(olderThanHours: number): Promise<void> {
  return request(`${API_BASE}/clear-history`, {
    method: 'POST',
    body: JSON.stringify({ older_than_hours: olderThanHours }),
  })
}
