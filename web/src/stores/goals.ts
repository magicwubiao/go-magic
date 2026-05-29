import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as goalsApi from '@/api/goals'
import type { Goal } from '@/api/goals'

export const useGoalsStore = defineStore('goals', () => {
  const goals = ref<Goal[]>([])
  const currentGoal = ref<Goal | null>(null)
  const loading = ref(false)

  const activeGoals = computed(() => goals.value.filter(g => g.status === 'active'))
  const completedGoals = computed(() => goals.value.filter(g => g.status === 'completed'))

  async function loadGoals(status?: string) {
    loading.value = true
    try {
      goals.value = await goalsApi.getGoals(status)
    } finally {
      loading.value = false
    }
  }

  async function loadGoal(id: string) {
    loading.value = true
    try {
      currentGoal.value = await goalsApi.getGoal(id)
    } finally {
      loading.value = false
    }
  }

  async function loadCurrentGoal() {
    try {
      currentGoal.value = await goalsApi.getCurrentGoal()
    } catch (e) {
      currentGoal.value = null
    }
  }

  async function createGoal(title: string, description: string) {
    const goal = await goalsApi.createGoal(title, description)
    goals.value.unshift(goal)
    return goal
  }

  async function updateGoal(id: string, updates: Partial<Goal>) {
    const updated = await goalsApi.updateGoal(id, updates)
    const idx = goals.value.findIndex(g => g.id === id)
    if (idx >= 0) goals.value[idx] = updated
    if (currentGoal.value?.id === id) currentGoal.value = updated
    return updated
  }

  async function deleteGoal(id: string) {
    await goalsApi.deleteGoal(id)
    goals.value = goals.value.filter(g => g.id !== id)
    if (currentGoal.value?.id === id) currentGoal.value = null
  }

  async function completeGoal(id: string) {
    return updateGoal(id, { status: 'completed', progress: 100 })
  }

  async function linkSession(goalId: string, sessionId: string) {
    await goalsApi.linkSession(goalId, sessionId)
    await loadGoal(goalId)
  }

  async function unlinkSession(goalId: string, sessionId: string) {
    await goalsApi.unlinkSession(goalId, sessionId)
    await loadGoal(goalId)
  }

  return {
    goals,
    currentGoal,
    loading,
    activeGoals,
    completedGoals,
    loadGoals,
    loadGoal,
    loadCurrentGoal,
    createGoal,
    updateGoal,
    deleteGoal,
    completeGoal,
    linkSession,
    unlinkSession,
  }
})
