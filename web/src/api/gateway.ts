import { request } from './client'

export interface GatewayStatus {
  running: boolean
  pid: number
  health_ok: boolean
  started?: string
  health_status?: string
  gateway_uptime_seconds?: number
  gateway_version?: string
  platforms_total?: number
  platforms_healthy?: number
  platforms?: PlatformStatus[]
}

export interface PlatformStatus {
  name: string
  connected: boolean
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
  // The backend now returns per-platform connection state from the gateway
  // health endpoint (see /api/gateway/status -> platforms[]).
  const status = await getGatewayStatus()
  return status.platforms ?? []
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

export interface PlatformActionResult {
  ok: boolean
  platform: string
  action: 'connect' | 'disconnect'
  connected?: boolean
  error?: string
}

/** Connect or disconnect a platform at runtime (gateway must be running). */
export async function platformAction(
  platform: string,
  action: 'connect' | 'disconnect',
): Promise<PlatformActionResult> {
  return request(`/gateway/platforms/${platform}/${action}`, { method: 'POST' })
}
