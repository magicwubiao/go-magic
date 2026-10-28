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

/** One bot advertised by a remote peer's /api/relay/v1/bots listing. */
export interface PeerBotInfo {
  name: string
  title?: string
  description?: string
  avatar?: string
  status?: string
}

export interface PeerBotsResult {
  ok: boolean
  peer: string
  bots: PeerBotInfo[]
}

/** Reachability probe result: what the dashboard's online dot is built from. */
export interface PeerPingResult {
  ok: boolean
  peer: string
  /** Bots the remote advertises (0 is valid: Bot Mode may be off). */
  bot_count: number
  latency_ms: number
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

/**
 * Fix a peer's address or rotate its relay secret in place.
 *
 * An empty token keeps the stored secret — the API never returns the token, so
 * requiring it here would force a re-entry just to correct a port number. The
 * peer name is immutable (it keys the local conversation history).
 */
export async function updatePeer(
  name: string,
  payload: { base_url: string; token?: string },
): Promise<PeerInfo> {
  return request(`/peers/${encodeURIComponent(name)}`, {
    method: 'PUT',
    body: JSON.stringify(payload),
  })
}

/**
 * Ask whether a peer answers right now. The server bounds the probe at 6s and
 * surfaces the remote's own failure ("invalid relay token", DNS error) so the
 * UI can distinguish a credential problem from an unreachable host.
 */
export async function pingPeer(name: string): Promise<PeerPingResult> {
  return request(`/peers/${encodeURIComponent(name)}/ping`, { retries: 0, timeoutMs: 20000 })
}

export async function deletePeer(name: string): Promise<{ deleted: string }> {
  return request(`/peers/${encodeURIComponent(name)}`, { method: 'DELETE' })
}

/**
 * Fetch the bot catalog a remote peer exposes, for the DM modal's bot picker.
 * Retries are disabled: a failed listing should surface immediately (usually a
 * token/network problem) instead of retrying behind the user's back.
 */
export async function listPeerBots(name: string): Promise<PeerBotsResult> {
  return request(`/peers/${encodeURIComponent(name)}/bots`, { retries: 0, timeoutMs: 35000 })
}

/**
 * A relayed DM waits for the remote bot's whole turn. The server's own budget
 * is 6 minutes, so the browser must sit slightly longer instead of aborting
 * first (it used to die at the 30s default, making every long turn look failed).
 */
const PEER_DM_TIMEOUT_MS = 6 * 60 * 1000 + 30 * 1000

/**
 * Relay a DM to a bot on a remote instance and wait for its reply.
 *
 * Retries are disabled on purpose: the request drives a full agent turn on the
 * remote machine, so the default "retry on 5xx/timeout" policy would run that
 * turn two or three times and send the target bot the same message repeatedly.
 *
 * `signal` lets the dashboard stop waiting; the remote turn keeps running, the
 * browser just stops caring about the response.
 */
export async function sendPeerDM(
  name: string,
  bot: string,
  message: string,
  signal?: AbortSignal,
): Promise<PeerDMResult> {
  return request(`/peers/${encodeURIComponent(name)}/dm`, {
    method: 'POST',
    body: JSON.stringify({ bot, message }),
    retries: 0,
    timeoutMs: PEER_DM_TIMEOUT_MS,
    signal,
  })
}
