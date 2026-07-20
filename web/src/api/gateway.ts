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
  // GatewayStatus 类型当前不包含 platforms/connectedPlatforms 字段，
  // 后端 /gateway/status 响应未返回已连接平台列表。
  // 待后端支持后，此处改为从 status 中提取 platforms 字段。
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
