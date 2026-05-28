import { request } from './client'

export interface SystemInfo {
  version: string
  platform: string
  arch: string
  go_version: string
}

export interface SystemVersion {
  version: string
  commit: string
  build_date: string
  platform: string
  arch: string
}

export interface VersionCheckResult {
  current_version: string
  latest_version: string
  has_update: boolean
  release_name: string
  release_notes: string
  published_at: string
  html_url: string
  download_url: string
  asset_size: number
  prerelease: boolean
}

export async function getSystemInfo(): Promise<SystemInfo> {
  return request('/system/info')
}

export async function getSystemVersion(): Promise<SystemVersion> {
  return request('/system/version')
}

export async function checkForUpdates(): Promise<VersionCheckResult> {
  return request('/system/version/check')
}

export interface SystemStats {
  sessions: number
  messages: number
  uptime: number
  memory_usage: number
  goroutines: number
}

export async function getSystemStats(): Promise<SystemStats> {
  return request('/system/stats')
}

export async function getHealth(): Promise<{ status: string }> {
  return request('/system/health')
}
