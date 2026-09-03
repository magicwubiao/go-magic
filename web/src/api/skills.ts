import { request } from './client'

export interface Skill {
  id: string
  name: string
  description: string
  tags: string[]
  enabled: boolean
  source: 'builtin' | 'local' | 'global' | 'registry' | 'auto' | string
  status?: 'pending' | 'approved' | 'archived' | 'rejected' | string  // auto-skill lifecycle status
}

export interface SkillStatistics {
  skill_name: string
  total_invocations: number
  success_rate: number
  avg_quality: number
  trend: string
}

export interface SkillVersion {
  version: string
  description: string
  created_at: string
  is_current: boolean
  quality_score: number
}

export interface EvolutionRecord {
  id: string
  generation: number
  reason: string
  status: string
  timestamp: string
}

export async function getSkills(): Promise<Skill[]> {
  return request('/skills')
}

export async function toggleSkill(name: string, enabled: boolean): Promise<void> {
  await request('/skills/toggle', {
    method: 'PUT',
    body: JSON.stringify({ name, enabled }),
  })
}

export async function installSkill(url: string): Promise<void> {
  return request('/skills/install', {
    method: 'POST',
    body: JSON.stringify({ url }),
  })
}

export async function updateSkill(id: string, data: Partial<Skill>): Promise<void> {
  return request(`/skills/${id}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

export async function uploadSkill(file: File, name?: string, relativePath?: string): Promise<{ ok: boolean; name: string }> {
  const formData = new FormData()
  formData.append('file', file)
  if (name) {
    formData.append('name', name)
  }
  if (relativePath) {
    formData.append('path', relativePath)
  }
  
  return request('/skills/upload', {
    method: 'POST',
    body: formData,
  })
}

export async function searchSkills(query: string): Promise<Skill[]> {
  return request(`/dashboard/skills/search?q=${encodeURIComponent(query)}`)
}

export async function deleteSkill(id: string): Promise<{ ok: boolean; id: string; deleted: boolean }> {
  return request(`/skills/${encodeURIComponent(id)}`, {
    method: 'DELETE',
  })
}

// New Cortex-based APIs
export async function getSkillStatistics(): Promise<SkillStatistics[]> {
  return request('/skills/statistics')
}

export async function getSkillVersions(skillName: string): Promise<SkillVersion[]> {
  return request(`/skills/${encodeURIComponent(skillName)}/versions`)
}

export async function getSkillEvolutionHistory(skillName: string): Promise<EvolutionRecord[]> {
  return request(`/skills/${encodeURIComponent(skillName)}/evolution`)
}

// Hub / Skill Market APIs
export interface HubSkill {
  name: string
  description: string
  tags: string[]
  source: string
  source_id: string
  url: string
  author: string
  stars: number
  installs: number
  verified: boolean
}

export async function searchHubSkills(keyword?: string): Promise<HubSkill[]> {
  const params = keyword ? `?q=${encodeURIComponent(keyword)}` : ''
  return request<HubSkill[]>(`/skills/hub/search${params}`)
}

export async function installHubSkill(source: string, sourceID: string): Promise<{ ok: boolean; error?: string }> {
  return request<{ ok: boolean; error?: string }>('/skills/hub/install', {
    method: 'POST',
    body: JSON.stringify({ source, sourceID }),
  })
}

// Auto-skill lifecycle management
export async function performAutoSkillAction(name: string, action: 'approve' | 'reject' | 'archive' | 'restore' | 'delete'): Promise<{ ok: boolean; name: string; action: string; message?: string }> {
  return request('/skills/auto/action', {
    method: 'POST',
    body: JSON.stringify({ name, action }),
  })
}