import { request } from './client'

export interface GatewayStatus {
  status: string
  platforms: Record<string, PlatformStatus>
}

export interface PlatformStatus {
  name: string
  connected: boolean
  platform: string
  last_activity?: string
}

export async function getGatewayStatus(): Promise<GatewayStatus> {
  return request('/status')
}

export async function restartGateway(): Promise<{ ok: boolean }> {
  return request('/gateway/restart', { method: 'POST' })
}

export async function getPlatforms(): Promise<PlatformStatus[]> {
  const status = await getGatewayStatus()
  return Object.values(status.platforms || {})
}
