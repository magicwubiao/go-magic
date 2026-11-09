import { request } from './client'

/** 局域网访问信息（/api/access/info）。 */
export interface AccessInfo {
  /** 局域网访问根地址，如 http://192.168.1.10:5000；未检测到网卡时为空串。 */
  url: string
  /** 是否检测到了非回环 IPv4 地址。 */
  has_lan: boolean
  /** "扫码免密登录"开关当前状态。 */
  pairing_enabled: boolean
}

/** 一次性配对码签发结果（/api/access/pair）。 */
export interface PairResponse {
  ok: boolean
  /** 携带配对码的完整登录 URL，直接编码进二维码。 */
  url: string
  /** 配对码有效期（秒）。 */
  expires_in: number
}

/** 配对码兑换结果（/api/access/pair/redeem）。 */
export interface PairRedeemResponse {
  ok: boolean
  /** 兑换出的正式会话 token。 */
  token: string
}

export async function getAccessInfo(): Promise<AccessInfo> {
  return request('/access/info')
}

export async function setPairing(enabled: boolean): Promise<{ ok: boolean }> {
  return request('/access/pairing', {
    method: 'POST',
    body: JSON.stringify({ enabled }),
  })
}

export async function createPairToken(): Promise<PairResponse> {
  return request('/access/pair', {
    method: 'POST',
  })
}

/**
 * 用配对码兑换会话。免登录通道，必须 skipAuth：调用方此时还没有任何凭据。
 * 失败时服务端回 400（无效/过期），不触发 request 层的 401 全局登出语义。
 */
export async function redeemPairToken(token: string): Promise<PairRedeemResponse> {
  return request('/access/pair/redeem', {
    method: 'POST',
    body: JSON.stringify({ token }),
    skipAuth: true,
    // 配对码兑换是一次性动作，失败重试没有意义（码已作废）。
    retries: 0,
  })
}
