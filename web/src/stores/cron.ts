import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as cronApi from '@/api/cron'
import type { CronJob, ExecutionLog } from '@/api/cron'

export interface CronError {
  message: string
  code?: string
}

export const useCronStore = defineStore('cron', () => {
  const jobs = ref<CronJob[]>([])
  const logs = ref<ExecutionLog[]>([])
  const loading = ref(false)
  const error = ref<CronError | null>(null)

  async function loadJobs(): Promise<void> {
    loading.value = true
    error.value = null
    try {
      jobs.value = await cronApi.getCronJobs()
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to load jobs: ' + errMsg }
      jobs.value = []
    } finally {
      loading.value = false
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
      jobs.value = jobs.value.filter(j => j.id !== id)
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
      logs.value = await cronApi.getCronLogs(jobId, limit)
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
