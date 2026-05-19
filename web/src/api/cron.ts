import { request } from './client'

export interface CronJob {
  id: string
  name: string
  schedule: string
  command: string
  enabled: boolean
  last_run?: string
  next_run?: string
  status: 'active' | 'paused' | 'error'
}

export async function getCronJobs(): Promise<CronJob[]> {
  return request('/cron/jobs')
}

export async function createCronJob(job: Partial<CronJob>): Promise<CronJob> {
  return request('/cron/jobs', { method: 'POST', body: JSON.stringify(job) })
}

export async function updateCronJob(id: string, updates: Partial<CronJob>): Promise<CronJob> {
  return request(`/cron/jobs/${id}`, { method: 'PUT', body: JSON.stringify(updates) })
}

export async function deleteCronJob(id: string): Promise<void> {
  return request(`/cron/jobs/${id}`, { method: 'DELETE' })
}

export async function triggerCronJob(id: string): Promise<void> {
  return request(`/cron/jobs/${id}/trigger`, { method: 'POST' })
}

export async function pauseCronJob(id: string): Promise<void> {
  return request(`/cron/jobs/${id}/pause`, { method: 'POST' })
}

export async function resumeCronJob(id: string): Promise<void> {
  return request(`/cron/jobs/${id}/resume`, { method: 'POST' })
}
