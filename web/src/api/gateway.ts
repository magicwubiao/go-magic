import { request } from './client'

export interface GatewayStatus {
  running: boolean
  pid: number
  health_ok: boolean
  started?: string
}

export interface PlatformStatus {
  name: string
  connected: boolean
  platform: string
  last_activity?: string
}

export async function getGatewayStatus(): Promise<GatewayStatus> {
  return request('/gateway/status')
}

export async function restartGateway(): Promise<{ ok: boolean }> {
  return request('/gateway/restart', { method: 'POST' })
}

export async function getPlatforms(): Promise<PlatformStatus[]> {
  const status = await getGatewayStatus()
  return []
}
