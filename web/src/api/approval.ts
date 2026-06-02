import { request } from './client'

const API_BASE = '/approval'

export interface ApprovalStatus {
  enabled: boolean
  strategy: string
  pending_count: number
}

export interface ApprovalHistoryRecord {
  id: string
  command: string
  risk_level: string
  decision: string
  strategy: string
  duration_ms: number
  timestamp: string
}

export interface ApprovalStats {
  total_requests: number
  auto_approved: number
  user_approved: number
  user_denied: number
  trusted_patterns: number
  denied_patterns: number
  avg_response_time_ms: number
}

export interface TrustedPattern {
  pattern: string
  count: number
  last_seen: string
}

export interface DeniedPattern {
  pattern: string
  count: number
  last_seen: string
}

export interface ApprovalSettings {
  strategy: string
  trust_threshold: number
  enable_learning: boolean
  cli_confirm: boolean
  whitelist: string[]
}

export interface PendingApproval {
  id: string
  command: string
  risk_level: string
  context: string
  created_at: string
}

export async function getApprovalStatus(): Promise<ApprovalStatus> {
  return request(`${API_BASE}/status`)
}

export async function getApprovalHistory(limit: number = 100, offset: number = 0): Promise<ApprovalHistoryRecord[]> {
  return request(`${API_BASE}/history?limit=${limit}&offset=${offset}`)
}

export async function getApprovalStats(): Promise<ApprovalStats> {
  return request(`${API_BASE}/stats`)
}

export async function getTrustedPatterns(): Promise<TrustedPattern[]> {
  return request(`${API_BASE}/patterns/trusted`)
}

export async function getDeniedPatterns(): Promise<DeniedPattern[]> {
  return request(`${API_BASE}/patterns/denied`)
}

export async function getPendingApprovals(): Promise<PendingApproval[]> {
  return request(`${API_BASE}/pending`)
}

export async function resolvePendingApproval(id: string, approved: boolean): Promise<void> {
  return request(`${API_BASE}/pending/${id}/resolve`, {
    method: 'POST',
    body: JSON.stringify({ approved }),
  })
}

export async function removeTrustedPattern(pattern: string): Promise<void> {
  return request(`${API_BASE}/patterns/trusted`, {
    method: 'DELETE',
    body: JSON.stringify({ pattern }),
  })
}

export async function clearDeniedPattern(pattern: string): Promise<void> {
  return request(`${API_BASE}/patterns/denied`, {
    method: 'DELETE',
    body: JSON.stringify({ pattern }),
  })
}

export async function addWhitelist(pattern: string): Promise<void> {
  return request(`${API_BASE}/whitelist`, {
    method: 'POST',
    body: JSON.stringify({ pattern }),
  })
}

export async function removeWhitelist(pattern: string): Promise<void> {
  return request(`${API_BASE}/whitelist`, {
    method: 'DELETE',
    body: JSON.stringify({ pattern }),
  })
}

export async function setStrategy(strategy: string): Promise<void> {
  return request(`${API_BASE}/settings`, {
    method: 'PUT',
    body: JSON.stringify({ strategy }),
  })
}

export async function saveSettings(settings: ApprovalSettings): Promise<void> {
  return request(`${API_BASE}/settings`, {
    method: 'PUT',
    body: JSON.stringify(settings),
  })
}

export async function clearHistory(olderThanHours: number): Promise<void> {
  return request(`${API_BASE}/clear-history`, {
    method: 'POST',
    body: JSON.stringify({ older_than_hours: olderThanHours }),
  })
}