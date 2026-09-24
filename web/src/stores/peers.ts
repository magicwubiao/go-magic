import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as peersApi from '@/api/peers'
import type { PeerDMResult, PeerInfo } from '@/api/peers'

/**
 * Cross-machine peers (bot_mode relay). Peers are managed through the modal
 * entry on the Bot Mode page, so this store owns the whole slice: the peer
 * table plus this machine's identity.
 */
export const usePeersStore = defineStore('peers', () => {
  const peers = ref<PeerInfo[]>([])
  const instanceId = ref('')
  const magicHome = ref('')
  const loading = ref(false)

  async function loadPeers(): Promise<void> {
    loading.value = true
    try {
      const data = await peersApi.getPeers()
      instanceId.value = data.instance_id || ''
      magicHome.value = data.magic_home || ''
      peers.value = data.peers || []
    } catch {
      peers.value = []
    } finally {
      loading.value = false
    }
  }

  async function addPeer(payload: { name: string; base_url: string; token?: string }): Promise<PeerInfo> {
    const peer = await peersApi.addPeer(payload)
    await loadPeers()
    return peer
  }

  async function removePeer(name: string): Promise<void> {
    await peersApi.deletePeer(name)
    await loadPeers()
  }

  /** Relay a DM to a bot on a remote instance (blocking: drives a full turn). */
  function sendDM(name: string, bot: string, text: string): Promise<PeerDMResult> {
    return peersApi.sendPeerDM(name, bot, text)
  }

  return {
    peers,
    instanceId,
    magicHome,
    loading,
    loadPeers,
    addPeer,
    removePeer,
    sendDM,
  }
})
