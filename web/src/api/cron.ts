import { request } from './client'

export interface CronJob {
  id: string
  name: string
  description: string
  schedule: string
  schedule_display: string
  prompt: string
  script: string
  no_agent: boolean
  skills: string[]
  enabled: boolean
  state: 'active' | 'inactive' | 'running'
  last_status: string
  last_error: string
  last_run_at?: string
  next_run_at?: string
  run_count: number
}

export interface ExecutionLog {
  id: string
  job_id: string
  job_name: string
  started_at: string
  finished_at?: string
  status: 'success' | 'failed' | 'running'
  output?: string
  error?: string
  duration?: string
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

export async function pauseCronJob(id: string): Promise<CronJob> {
  return request(`/cron/jobs/${id}/pause`, { method: 'POST' })
}

export async function resumeCronJob(id: string): Promise<CronJob> {
  return request(`/cron/jobs/${id}/resume`, { method: 'POST' })
}

export async function getCronLogs(jobId: string, limit?: number): Promise<ExecutionLog[]> {
  const params = limit ? `?limit=${limit}` : ''
  return request(`/cron/jobs/${jobId}/logs${params}`)
}
