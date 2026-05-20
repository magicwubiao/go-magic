import { request } from './client'

export interface Skill {
  id: string
  name: string
  description: string
  category: string
  tags: string[]
  enabled: boolean
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

export async function searchSkills(query: string): Promise<Skill[]> {
  return request(`/dashboard/skills/search?q=${encodeURIComponent(query)}`)
}
