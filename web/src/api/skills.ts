import { request } from './client'

export interface Skill {
  id: string
  name: string
  description: string
  category: string
  tags: string[]
  enabled: boolean
  source: 'builtin' | 'local' | 'global' | 'registry' | 'auto' | string
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

export async function getSkillCategories(): Promise<string[]> {
  return request('/skills/categories')
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