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
    // 先清空旧消息并标记 loading，避免切换房间时短暂显示上一个房间的消息
    messages.value = []
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
    const roomId = activeRoomId.value
    // 本地先 push 占位消息，避免发送期间白屏
    const localId = 'local_' + Date.now()
    messages.value.push({
      id: localId,
      room_id: roomId,
      sender: 'User',
      role: 'user',
      content,
      timestamp: Date.now(),
    })
    try {
      const msg = await groupchatApi.sendRoomMessage(roomId, content)
      // 用服务端返回的真实消息替换本地占位
      const idx = messages.value.findIndex(m => m.id === localId)
      if (idx >= 0) {
        messages.value[idx] = msg
      } else {
        messages.value.push(msg)
      }
    } catch {
      // 发送失败：移除占位消息
      messages.value = messages.value.filter(m => m.id !== localId)
      throw new Error('send failed')
    }
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

  async function updateRoomName(name: string) {
    if (!activeRoomId.value) return
    try {
      await groupchatApi.updateRoom(activeRoomId.value, { name })
      const room = rooms.value.find(r => r.id === activeRoomId.value)
      if (room) room.name = name
    } catch (e) {
      console.error('Failed to update room name:', e)
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
    updateRoomName,
    generateInviteCode,
    deleteRoom,
  }
})
