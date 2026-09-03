import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as roomsApi from '@/api/rooms'
import type { Room, RoomMessage, RoomSendResult } from '@/api/rooms'

export const useRoomsStore = defineStore('rooms', () => {
  const rooms = ref<Room[]>([])
  const activeRoomId = ref<string | null>(null)
  const messages = ref<RoomMessage[]>([])
  const loading = ref(false)
  const sending = ref(false)

  // AbortController for the in-flight blocking round so the user can cancel.
  let sendAbort: AbortController | null = null

  async function loadRooms(): Promise<void> {
    loading.value = true
    try {
      rooms.value = await roomsApi.getRooms()
    } catch {
      rooms.value = []
    } finally {
      loading.value = false
    }
  }

  async function createRoom(data: Partial<Room>): Promise<Room> {
    const room = await roomsApi.createRoom(data)
    rooms.value.push(room)
    return room
  }

  async function selectRoom(id: string): Promise<void> {
    activeRoomId.value = id
    // Clear previous room messages first so switching never flashes stale chat.
    messages.value = []
    loading.value = true
    try {
      const [msgs] = await Promise.all([roomsApi.getRoomMessages(id)])
      messages.value = msgs
    } catch {
      messages.value = []
    } finally {
      loading.value = false
    }
  }

  function getActiveRoom(): Room | null {
    if (!activeRoomId.value) return null
    return rooms.value.find(r => r.id === activeRoomId.value) || null
  }

  /**
   * Deliver a message to the room. The backend blocks until the coordinated
   * round finishes, so the user message is optimistically appended locally and
   * replaced/kept by the authoritative results afterwards.
   */
  async function sendMessage(message: string, target?: string): Promise<RoomSendResult | null> {
    if (!activeRoomId.value || sending.value) return null
    const roomId = activeRoomId.value
    sending.value = true
    sendAbort = new AbortController()

    const localId = 'local_' + Date.now()
    messages.value.push({ id: localId, from: '@user', content: message, timestamp: Date.now() })
    try {
      const res = await roomsApi.sendRoomMessage(roomId, message, target, sendAbort.signal)
      messages.value = messages.value.filter(m => m.id !== localId)
      for (const m of res.messages) {
        if (!messages.value.some(x => x.id === m.id)) {
          messages.value.push(m)
        }
      }
      return res
    } catch (e) {
      // On failure, replace the optimistic bubble with a visible error marker.
      messages.value = messages.value.map(m =>
        m.id === localId ? { ...m, content: `⚠️ ${m.content}` } : m
      )
      throw e
    } finally {
      sendAbort = null
      sending.value = false
    }
  }

  function cancelSend(): void {
    sendAbort?.abort()
  }

  async function refreshMessages(): Promise<void> {
    if (!activeRoomId.value) return
    try {
      messages.value = await roomsApi.getRoomMessages(activeRoomId.value)
    } catch {
      /* keep current messages */
    }
  }

  async function updateRoom(roomId: string, data: Partial<Room>): Promise<Room> {
    const updated = await roomsApi.updateRoom(roomId, data)
    const idx = rooms.value.findIndex(r => r.id === roomId)
    if (idx >= 0) rooms.value[idx] = updated
    return updated
  }

  async function deleteRoom(roomId: string): Promise<void> {
    await roomsApi.deleteRoom(roomId)
    rooms.value = rooms.value.filter(r => r.id !== roomId)
    if (activeRoomId.value === roomId) {
      activeRoomId.value = null
      messages.value = []
    }
  }

  return {
    rooms,
    activeRoomId,
    messages,
    loading,
    sending,
    loadRooms,
    createRoom,
    selectRoom,
    getActiveRoom,
    sendMessage,
    cancelSend,
    refreshMessages,
    updateRoom,
    deleteRoom,
  }
})
