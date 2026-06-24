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

export async function startGateway(): Promise<{ ok: boolean }> {
  return request('/gateway/start', { method: 'POST' })
}

export async function stopGateway(): Promise<{ ok: boolean }> {
  return request('/gateway/stop', { method: 'POST' })
}

export async function getPlatforms(): Promise<PlatformStatus[]> {
  const status = await getGatewayStatus()
  return []
}

export interface QRResponse {
  platform: string
  status: string
  qr_code?: string
  qr_data?: string
  message?: string
  expires_in?: number
}

export async function fetchGatewayQR(platform: string): Promise<QRResponse> {
  return request(`/gateway/qr?platform=${platform}`)
}
