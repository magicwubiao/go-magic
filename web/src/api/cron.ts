import { request } from './client'

export interface CronJob {
  id: string
  name: string
  description: string
  schedule: string
  schedule_display: string
  prompt: string
  script: string
  working_dir?: string
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
  work_dir?: string
}

export async function getCronJobs(locale?: string): Promise<CronJob[]> {
  const qs = locale ? `?locale=${encodeURIComponent(locale)}` : ''
  return request(`/cron/jobs${qs}`)
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

export interface NextRunsResult {
  valid: boolean
  error?: string
  next_runs?: string[]
  display?: string
}

// 校验 cron 表达式并返回未来 N 次执行时间（后端真实解析，替代前端的
// "只数字段个数"式假校验）
export async function getCronNextRuns(schedule: string, count = 5, locale?: string): Promise<NextRunsResult> {
  const params = new URLSearchParams({ schedule, count: String(count) })
  if (locale) params.set('locale', locale)
  return request(`/cron/next-runs?${params.toString()}`)
}

export interface CronParseResult {
  schedule: string
  name?: string
  prompt?: string
  schedule_display?: string
  next_runs?: string[]
}

// 自然语言解析："每天早上8点提醒我喝水" -> { schedule, name, prompt }
export async function parseCronNatural(text: string, locale?: string): Promise<CronParseResult> {
  const qs = locale ? `?locale=${encodeURIComponent(locale)}` : ''
  return request(`/cron/parse${qs}`, { method: 'POST', body: JSON.stringify({ text }) })
}
