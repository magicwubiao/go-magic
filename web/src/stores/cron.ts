import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as cronApi from '@/api/cron'
import type { CronJob } from '@/api/cron'

export interface CronError {
  message: string
  code?: string
}

export const useCronStore = defineStore('cron', () => {
  const jobs = ref<CronJob[]>([])
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

  async function createJob(job: Partial<CronJob>): Promise<CronJob | null> {
    try {
      error.value = null
      const newJob = await cronApi.createCronJob(job)
      jobs.value.push(newJob)
      return newJob
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to create job: ' + errMsg }
      return null
    }
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
      await cronApi.pauseCronJob(id)
      const job = jobs.value.find(j => j.id === id)
      if (job) job.status = 'paused'
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
      await cronApi.resumeCronJob(id)
      const job = jobs.value.find(j => j.id === id)
      if (job) job.status = 'active'
      return true
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to resume job: ' + errMsg }
      return false
    }
  }

  return { jobs, loading, error, loadJobs, createJob, deleteJob, triggerJob, pauseJob, resumeJob }
})
