import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as groupchatApi from '@/api/groupchat'
import type { ChatRoom, ChatMessage } from '@/api/groupchat'

export const useGroupChatStore = defineStore('groupchat', () => {
  const rooms = ref<ChatRoom[]>([])
  const activeRoomId = ref<string | null>(null)
  const messages = ref<ChatMessage[]>([])
  const loading = ref(false)

  async function loadRooms() {
    loading.value = true
    try {
      rooms.value = await groupchatApi.getRooms()
    } catch {
      rooms.value = []
    } finally {
      loading.value = false
    }
  }

  async function createRoom(room: Partial<ChatRoom>) {
    const newRoom = await groupchatApi.createRoom(room)
    rooms.value.push(newRoom)
    return newRoom
  }

  async function selectRoom(id: string) {
    activeRoomId.value = id
    loading.value = true
    try {
      messages.value = await groupchatApi.getRoomMessages(id)
    } catch {
      messages.value = []
    } finally {
      loading.value = false
    }
  }

  async function sendMessage(content: string) {
    if (!activeRoomId.value) return
    const msg = await groupchatApi.sendRoomMessage(activeRoomId.value, content)
    messages.value.push(msg)
  }

  return { rooms, activeRoomId, messages, loading, loadRooms, createRoom, selectRoom, sendMessage }
})
