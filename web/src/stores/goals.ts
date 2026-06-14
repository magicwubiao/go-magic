import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as goalsApi from '@/api/goals'
import type { Goal, GoalAnalysis } from '@/api/goals'

export const useGoalsStore = defineStore('goals', () => {
  const goals = ref<Goal[]>([])
  const currentGoal = ref<Goal | null>(null)
  const loading = ref(false)

  const activeGoals = computed(() => (goals.value ?? []).filter(g => g.status === 'active'))
  const completedGoals = computed(() => (goals.value ?? []).filter(g => g.status === 'completed'))
  const abandonedGoals = computed(() => (goals.value ?? []).filter(g => g.status === 'abandoned'))

  async function loadGoals(status?: string) {
    loading.value = true
    try {
      const data = await goalsApi.getGoals(status)
      goals.value = data ?? []
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

  async function abandonGoal(id: string) {
    return updateGoal(id, { status: 'abandoned' })
  }

  async function reactivateGoal(id: string) {
    return updateGoal(id, { status: 'active' })
  }

  async function linkSession(goalId: string, sessionId: string) {
    await goalsApi.linkSession(goalId, sessionId)
    await loadGoal(goalId)
  }

  async function unlinkSession(goalId: string, sessionId: string) {
    await goalsApi.unlinkSession(goalId, sessionId)
    await loadGoal(goalId)
  }

  async function decomposeGoal(goalId: string) {
    const result = await goalsApi.decomposeGoal(goalId)
    // Refresh goals and board to reflect new tasks
    await loadGoals()
    return result
  }

  async function analyzeGoal(goalId: string, conversation: string): Promise<GoalAnalysis> {
    return goalsApi.analyzeGoal(goalId, conversation)
  }

  return {
    goals,
    currentGoal,
    loading,
    activeGoals,
    completedGoals,
    abandonedGoals,
    loadGoals,
    loadGoal,
    loadCurrentGoal,
    createGoal,
    updateGoal,
    deleteGoal,
    completeGoal,
    abandonGoal,
    reactivateGoal,
    linkSession,
    unlinkSession,
    decomposeGoal,
    analyzeGoal,
  }
})