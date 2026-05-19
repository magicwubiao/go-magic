import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as systemApi from '@/api/system'
import type { SystemInfo, SystemStats } from '@/api/system'

export const useSystemStore = defineStore('system', () => {
  const info = ref<SystemInfo | null>(null)
  const stats = ref<SystemStats | null>(null)
  const health = ref<{ status: string } | null>(null)
  const loading = ref(false)

  async function loadInfo() {
    info.value = await systemApi.getSystemInfo()
  }

  async function loadStats() {
    stats.value = await systemApi.getSystemStats()
  }

  async function checkHealth() {
    health.value = await systemApi.getHealth()
  }

  async function loadAll() {
    loading.value = true
    try {
      await Promise.all([loadInfo(), loadStats(), checkHealth()])
    } finally {
      loading.value = false
    }
  }

  return {
    info,
    stats,
    health,
    loading,
    loadInfo,
    loadStats,
    checkHealth,
    loadAll,
  }
})
