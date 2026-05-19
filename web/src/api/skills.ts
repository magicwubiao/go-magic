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

export async function getSkill(id: string): Promise<Skill> {
  return request(`/skills/${id}`)
}

export async function searchSkills(query: string): Promise<Skill[]> {
  return request(`/dashboard/skills/search?q=${encodeURIComponent(query)}`)
}
