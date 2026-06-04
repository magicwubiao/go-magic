import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as skillsApi from '@/api/skills'
import type { Skill } from '@/api/skills'

export const useSkillsStore = defineStore('skills', () => {
  const skills = ref<Skill[]>([])
  const categories = ref<string[]>([])
  const loading = ref(false)

  async function loadSkills() {
    loading.value = true
    try {
      skills.value = await skillsApi.getSkills()
    } finally {
      loading.value = false
    }
  }

  async function loadCategories() {
    categories.value = await skillsApi.getSkillCategories()
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
    await skillsApi.installHubSkill(source, sourceID)
    await loadSkills()
  }

  return {
    skills,
    categories,
    loading,
    loadSkills,
    loadCategories,
    toggleSkill,
    installSkill,
    updateSkill,
    searchHubSkills,
    installHubSkill,
  }
})
