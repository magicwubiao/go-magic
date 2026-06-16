import { request } from './client'

export interface DailyUsage {
  date: string
  sessions: number
  messages: number
  input_tokens: number
  output_tokens: number
  total_tokens: number
  cost: number
  models: Record<string, {
    sessions: number
    messages: number
    input_tokens: number
    output_tokens: number
    total_tokens: number
    cost: number
  }>
}

export interface TodayStats {
  sessions: number
  messages: number
  input_tokens: number
  output_tokens: number
  total_tokens: number
  cost: number
  avg_response_time: number
  top_models: Array<{
    model: string
    messages: number
    tokens: number
  }>
}

export interface MonthlyUsage {
  month: string
  total_sessions: number
  total_messages: number
  total_tokens: number
  total_cost: number
}

export interface UsageInsight {
  total_sessions: number
  total_messages: number
  total_input_tokens: number
  total_output_tokens: number
  total_cost: number
  avg_cost_per_session: number
  avg_cost_per_message: number
  avg_tokens_per_message: number
  most_used_model: string
  most_active_hour: number
  most_active_day: string
}

export async function getUsageToday(): Promise<TodayStats> {
  return request('/usage/today')
}

export async function getUsageDaily(days?: number): Promise<DailyUsage[]> {
  return request(`/usage/daily${days ? `?days=${days}` : ''}`)
}

export async function getUsageMonthly(): Promise<MonthlyUsage[]> {
  return request('/usage/monthly')
}

export async function getUsageInsights(): Promise<UsageInsight> {
  return request('/usage/insights')
}
