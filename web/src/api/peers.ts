import { request } from './client'

/** One registered remote go-magic instance. The relay token is never returned
 *  by the API — only whether one is configured. */
export interface PeerInfo {
  name: string
  base_url: string
  has_token: boolean
  created_at: number
}

export interface PeersOverview {
  /** This machine's stable identity, shown to remote operators. */
  instance_id: string
  magic_home: string
  peers: PeerInfo[]
}

export interface PeerDMResult {
  ok: boolean
  peer: string
  bot: string
  reply: string
}

export async function getPeers(): Promise<PeersOverview> {
  return request('/peers')
}

export async function addPeer(payload: {
  name: string
  base_url: string
  token?: string
}): Promise<PeerInfo> {
  return request('/peers', { method: 'POST', body: JSON.stringify(payload) })
}

export async function deletePeer(name: string): Promise<{ deleted: string }> {
  return request(`/peers/${encodeURIComponent(name)}`, { method: 'DELETE' })
}

/**
 * Relay a DM to a bot on a remote instance and wait for its reply.
 *
 * Retries are disabled on purpose: the request drives a full agent turn on the
 * remote machine, so the default "retry on 5xx/timeout" policy would run that
 * turn two or three times and send the target bot the same message repeatedly.
 */
export async function sendPeerDM(
  name: string,
  bot: string,
  message: string,
): Promise<PeerDMResult> {
  return request(`/peers/${encodeURIComponent(name)}/dm`, {
    method: 'POST',
    body: JSON.stringify({ bot, message }),
    retries: 0,
  })
}
