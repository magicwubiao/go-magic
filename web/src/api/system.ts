import { request } from './client'

export interface SystemInfo {
  version: string
  platform: string
  arch: string
  go_version: string
}

export interface SystemStats {
  uptime: number
  memory_usage: number
  goroutines: number
}

export async function getSystemInfo(): Promise<SystemInfo> {
  return request('/system/info')
}

export async function getSystemStats(): Promise<SystemStats> {
  return request('/system/stats')
}

export async function getHealth(): Promise<{ status: string }> {
  return request('/health')
}
