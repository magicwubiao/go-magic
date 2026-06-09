import { request } from './client'

export interface DailyStats {
  date: string
  total_requests: number
  total_input_tokens: number
  total_output_tokens: number
  total_cost: number
  by_model: Record<string, ModelStats>
}

export interface ModelStats {
  requests: number
  input_tokens: number
  output_tokens: number
  cost: number
}

export interface MonthlyBudget {
  month: string
  limit: number
  current: number
  alert_threshold: number
}

export interface Insights {
  total_requests: number
  total_tokens: number
  total_cost: number
  avg_tokens_per_request: number
  avg_cost_per_request: number
  top_models: Array<{ name: string; requests: number; cost: number }>
  top_providers: Array<{ name: string; requests: number; cost: number }>
  daily_trend: Array<{ date: string; tokens: number; cost: number }>
  peak_hours: Array<{ hour: number; requests: number }>
  cost_breakdown: Record<string, number>
  recommendations: string[]
}

export function getUsageToday(): Promise<DailyStats> {
  return request<DailyStats>('/usage/today')
}

export function getUsageDaily(days = 30): Promise<DailyStats[]> {
  return request<DailyStats[]>(`/usage/daily?days=${days}`)
}

export function getUsageWeekly(): Promise<DailyStats[]> {
  return request<DailyStats[]>('/usage/weekly')
}

export function getUsageMonthly(): Promise<DailyStats[]> {
  return request<DailyStats[]>('/usage/monthly')
}

export function getUsageInsights(): Promise<Insights> {
  return request<Insights>('/usage/insights')
}

export function getUsageBudget(): Promise<MonthlyBudget> {
  return request<MonthlyBudget>('/usage/budget')
}

export function setUsageBudget(budget: MonthlyBudget): Promise<{ status: string }> {
  return request<{ status: string }>('/usage/budget', {
    method: 'PUT',
    body: JSON.stringify(budget),
  })
}
