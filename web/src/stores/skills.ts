import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as skillsApi from '@/api/skills'
import type { Skill } from '@/api/skills'

export const useSkillsStore = defineStore('skills', () => {
  const skills = ref<Skill[]>([])
  const categories = ref<string[]>([])
  const loading = ref(false)
  const searchQuery = ref('')

  const filteredSkills = computed(() => {
    if (!searchQuery.value) return skills.value
    const q = searchQuery.value.toLowerCase()
    return skills.value.filter(s =>
      s.name.toLowerCase().includes(q) ||
      s.description.toLowerCase().includes(q)
    )
  })

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

  async function search(query: string) {
    searchQuery.value = query
    if (query) {
      loading.value = true
      try {
        skills.value = await skillsApi.searchSkills(query)
      } finally {
        loading.value = false
      }
    } else {
      await loadSkills()
    }
  }

  return {
    skills,
    categories,
    loading,
    searchQuery,
    filteredSkills,
    loadSkills,
    loadCategories,
    search,
  }
})
