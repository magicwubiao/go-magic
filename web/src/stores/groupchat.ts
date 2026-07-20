import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as groupchatApi from '@/api/groupchat'
import type { ChatRoom, ChatMessage, RoomMember, RoomAgent } from '@/api/groupchat'

export const useGroupChatStore = defineStore('groupchat', () => {
  const rooms = ref<ChatRoom[]>([])
  const activeRoomId = ref<string | null>(null)
  const messages = ref<ChatMessage[]>([])
  const members = ref<RoomMember[]>([])
  const agents = ref<RoomAgent[]>([])
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
      const [msgs, mems, ags] = await Promise.all([
        groupchatApi.getRoomMessages(id),
        groupchatApi.getRoomMembers(id),
        groupchatApi.getRoomAgents(id),
      ])
      messages.value = msgs
      members.value = mems
      agents.value = ags
    } catch {
      messages.value = []
      members.value = []
      agents.value = []
    } finally {
      loading.value = false
    }
  }

  async function sendMessage(content: string) {
    if (!activeRoomId.value) return
    const msg = await groupchatApi.sendRoomMessage(activeRoomId.value, content)
    messages.value.push(msg)
  }

  // Members
  async function addMember(userId: string, name: string) {
    if (!activeRoomId.value) return
    const member = await groupchatApi.addRoomMember(activeRoomId.value, userId, name)
    members.value.push(member)
  }

  async function removeMember(memberId: string) {
    if (!activeRoomId.value) return
    try {
      await groupchatApi.removeRoomMember(activeRoomId.value, memberId)
      members.value = members.value.filter(m => m.id !== memberId)
    } catch (e) {
      console.error('Failed to remove member:', e)
      throw e
    }
  }

  // Agents
  async function addAgent(agent: Partial<RoomAgent>) {
    if (!activeRoomId.value) return
    const newAgent = await groupchatApi.addRoomAgent(activeRoomId.value, agent)
    agents.value.push(newAgent)
  }

  async function removeAgent(agentId: string) {
    if (!activeRoomId.value) return
    try {
      await groupchatApi.removeRoomAgent(activeRoomId.value, agentId)
      agents.value = agents.value.filter(a => a.id !== agentId)
    } catch (e) {
      console.error('Failed to remove agent:', e)
      throw e
    }
  }

  async function updateAgent(agentId: string, data: Partial<RoomAgent>) {
    if (!activeRoomId.value) return
    const updated = await groupchatApi.updateRoomAgent(activeRoomId.value, agentId, data)
    const idx = agents.value.findIndex(a => a.id === agentId)
    if (idx >= 0) {
      agents.value[idx] = updated
    }
  }

  // Invite
  async function generateInviteCode() {
    if (!activeRoomId.value) return ''
    const res = await groupchatApi.generateInviteCode(activeRoomId.value)
    return res.invite_code
  }

  // Delete room
  async function deleteRoom(roomId: string) {
    await groupchatApi.deleteRoom(roomId)
    rooms.value = rooms.value.filter(r => r.id !== roomId)
    if (activeRoomId.value === roomId) {
      activeRoomId.value = null
      messages.value = []
      members.value = []
      agents.value = []
    }
  }

  return {
    rooms,
    activeRoomId,
    messages,
    members,
    agents,
    loading,
    loadRooms,
    createRoom,
    selectRoom,
    sendMessage,
    addMember,
    removeMember,
    addAgent,
    removeAgent,
    updateAgent,
    generateInviteCode,
    deleteRoom,
  }
})
