import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as cronApi from '@/api/cron'
import type { CronJob, ExecutionLog } from '@/api/cron'
import { getLocale } from '@/locales'

export interface CronError {
  message: string
  code?: string
}

export const useCronStore = defineStore('cron', () => {
  const jobs = ref<CronJob[]>([])
  const logs = ref<ExecutionLog[]>([])
  const loading = ref(false)
  const error = ref<CronError | null>(null)

  // silent=true 用于执行状态轮询：不显示 loading、不打断界面
  async function loadJobs(silent = false): Promise<void> {
    if (!silent) loading.value = true
    if (!silent) error.value = null
    try {
      // schedule_display 跟随界面语言（后端按 locale 参数翻译描述）
      const data = await cronApi.getCronJobs(getLocale())
      jobs.value = data ?? []
    } catch (e) {
      if (!silent) {
        const errMsg = e instanceof Error ? e.message : 'Unknown error'
        error.value = { message: 'Failed to load jobs: ' + errMsg }
      }
      if (!silent) jobs.value = []
    } finally {
      if (!silent) loading.value = false
    }
  }

  async function createJob(job: Partial<CronJob>): Promise<CronJob> {
    error.value = null
    const newJob = await cronApi.createCronJob(job)
    jobs.value.push(newJob)
    return newJob
  }

  async function updateJob(id: string, updates: Partial<CronJob>): Promise<CronJob> {
    error.value = null
    const updated = await cronApi.updateCronJob(id, updates)
    const idx = jobs.value.findIndex(j => j.id === id)
    if (idx >= 0) jobs.value[idx] = updated
    return updated
  }

  async function deleteJob(id: string): Promise<boolean> {
    try {
      error.value = null
      await cronApi.deleteCronJob(id)
      jobs.value = (jobs.value ?? []).filter(j => j.id !== id)
      return true
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to delete job: ' + errMsg }
      return false
    }
  }

  async function triggerJob(id: string): Promise<boolean> {
    try {
      error.value = null
      await cronApi.triggerCronJob(id)
      return true
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to trigger job: ' + errMsg }
      return false
    }
  }

  async function pauseJob(id: string): Promise<boolean> {
    try {
      error.value = null
      const updated = await cronApi.pauseCronJob(id)
      const idx = jobs.value.findIndex(j => j.id === id)
      if (idx >= 0) jobs.value[idx] = updated
      return true
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to pause job: ' + errMsg }
      return false
    }
  }

  async function resumeJob(id: string): Promise<boolean> {
    try {
      error.value = null
      const updated = await cronApi.resumeCronJob(id)
      const idx = jobs.value.findIndex(j => j.id === id)
      if (idx >= 0) jobs.value[idx] = updated
      return true
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to resume job: ' + errMsg }
      return false
    }
  }

  async function loadLogs(jobId: string, limit = 20): Promise<void> {
    try {
      const data = await cronApi.getCronLogs(jobId, limit)
      logs.value = data ?? []
    } catch (e) {
      logs.value = []
    }
  }

  return {
    jobs, logs, loading, error,
    loadJobs, createJob, updateJob, deleteJob,
    triggerJob, pauseJob, resumeJob, loadLogs,
  }
})
