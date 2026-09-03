import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as skillsApi from '@/api/skills'
import type { Skill } from '@/api/skills'

export const useSkillsStore = defineStore('skills', () => {
  const skills = ref<Skill[]>([])
  const loading = ref(false)

  async function loadSkills() {
    loading.value = true
    try {
      skills.value = await skillsApi.getSkills()
    } finally {
      loading.value = false
    }
  }

  async function toggleSkill(name: string, enabled: boolean) {
    await skillsApi.toggleSkill(name, enabled)
    const skill = skills.value.find(s => s.name === name || s.id === name)
    if (skill) skill.enabled = enabled
  }

  async function installSkill(url: string) {
    await skillsApi.installSkill(url)
    await loadSkills()
  }

  async function updateSkill(id: string, data: Partial<Skill>) {
    await skillsApi.updateSkill(id, data)
    await loadSkills()
  }

  async function searchHubSkills(keyword?: string) {
    return skillsApi.searchHubSkills(keyword)
  }

  async function installHubSkill(source: string, sourceID: string) {
    const result = await skillsApi.installHubSkill(source, sourceID)
    if (result && result.ok) {
      await loadSkills()
    }
    return result
  }

  function updateSkillStatus(id: string, status: string) {
    const idx = skills.value.findIndex(s => s.id === id)
    if (idx >= 0) {
      skills.value[idx] = { ...skills.value[idx], status }
    }
  }

  return {
    skills,
    loading,
    loadSkills,
    toggleSkill,
    installSkill,
    updateSkill,
    updateSkillStatus,
    searchHubSkills,
    installHubSkill,
  }
})