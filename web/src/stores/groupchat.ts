import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import { groupChatApi, type Room, type Message, type Member } from '../api/groupchat'

export const useGroupChatStore = defineStore('groupChat', () => {
  // State
  const rooms = ref<Room[]>([])
  const currentRoom = ref<Room | null>(null)
  const messages = ref<Message[]>([])
  const members = ref<Member[]>([])
  const isLoading = ref(false)
  const error = ref<string | null>(null)
  const typingUsers = ref<Set<string>>(new Set())
  const socket = ref<WebSocket | null>(null)

  // Computed
  const onlineMembers = computed(() => members.value.filter(m => m.is_online))
  const sortedMessages = computed(() => 
    [...messages.value].sort((a, b) => new Date(a.timestamp).getTime() - new Date(b.timestamp).getTime())
  )

  // Actions
  async function fetchRooms() {
    isLoading.value = true
    error.value = null
    try {
      rooms.value = await groupChatApi.listRooms()
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to fetch rooms'
    } finally {
      isLoading.value = false
    }
  }

  async function createRoom(name: string, description: string) {
    isLoading.value = true
    error.value = null
    try {
      const room = await groupChatApi.createRoom({ name, description })
      rooms.value.push(room)
      return room
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to create room'
      throw e
    } finally {
      isLoading.value = false
    }
  }

  async function joinRoom(code: string) {
    isLoading.value = true
    error.value = null
    try {
      const room = await groupChatApi.joinRoom(code)
      if (!rooms.value.find(r => r.id === room.id)) {
        rooms.value.push(room)
      }
      return room
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to join room'
      throw e
    } finally {
      isLoading.value = false
    }
  }

  async function leaveRoom(roomId: string) {
    try {
      await groupChatApi.leaveRoom(roomId)
      rooms.value = rooms.value.filter(r => r.id !== roomId)
      if (currentRoom.value?.id === roomId) {
        currentRoom.value = null
        messages.value = []
        members.value = []
      }
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to leave room'
      throw e
    }
  }

  async function deleteRoom(roomId: string) {
    try {
      await groupChatApi.deleteRoom(roomId)
      rooms.value = rooms.value.filter(r => r.id !== roomId)
      if (currentRoom.value?.id === roomId) {
        currentRoom.value = null
        messages.value = []
        members.value = []
      }
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to delete room'
      throw e
    }
  }

  async function selectRoom(roomId: string) {
    isLoading.value = true
    error.value = null
    try {
      currentRoom.value = rooms.value.find(r => r.id === roomId) || null
      if (currentRoom.value) {
        await Promise.all([
          fetchMessages(roomId),
          fetchMembers(roomId)
        ])
        connectWebSocket(roomId)
      }
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to select room'
    } finally {
      isLoading.value = false
    }
  }

  async function fetchMessages(roomId: string) {
    try {
      messages.value = await groupChatApi.getMessages(roomId)
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to fetch messages'
    }
  }

  async function fetchMembers(roomId: string) {
    try {
      members.value = await groupChatApi.getMembers(roomId)
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to fetch members'
    }
  }

  async function sendMessage(content: string) {
    if (!currentRoom.value) return
    try {
      const message = await groupChatApi.sendMessage(currentRoom.value.id, content)
      messages.value.push(message)
      return message
    } catch (e) {
      error.value = e instanceof Error ? e.message : 'Failed to send message'
      throw e
    }
  }

  function connectWebSocket(roomId: string) {
    disconnectWebSocket()
    
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:'
    const wsUrl = `${protocol}//${window.location.host}/ws/groupchat/${roomId}`
    
    socket.value = new WebSocket(wsUrl)
    
    socket.value.onopen = () => {
      console.log('GroupChat WebSocket connected')
    }
    
    socket.value.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data)
        handleWebSocketMessage(data)
      } catch (e) {
        console.error('Failed to parse WebSocket message:', e)
      }
    }
    
    socket.value.onclose = () => {
      console.log('GroupChat WebSocket disconnected')
    }
    
    socket.value.onerror = (error) => {
      console.error('WebSocket error:', error)
    }
  }

  function handleWebSocketMessage(data: any) {
    switch (data.type) {
      case 'message':
        messages.value.push(data.message)
        break
      case 'typing':
        if (data.isTyping) {
          typingUsers.value.add(data.username)
        } else {
          typingUsers.value.delete(data.username)
        }
        break
      case 'member_joined':
        if (!members.value.find(m => m.id === data.member.id)) {
          members.value.push(data.member)
        }
        break
      case 'member_left':
        members.value = members.value.filter(m => m.id !== data.userId)
        break
      case 'member_online':
        const member = members.value.find(m => m.id === data.userId)
        if (member) member.is_online = true
        break
      case 'member_offline':
        const offlineMember = members.value.find(m => m.id === data.userId)
        if (offlineMember) offlineMember.is_online = false
        break
    }
  }

  function disconnectWebSocket() {
    if (socket.value) {
      socket.value.close()
      socket.value = null
    }
  }

  function sendTyping(isTyping: boolean) {
    if (socket.value && socket.value.readyState === WebSocket.OPEN) {
      socket.value.send(JSON.stringify({ type: 'typing', isTyping }))
    }
  }

  // Cleanup
  function cleanup() {
    disconnectWebSocket()
    rooms.value = []
    currentRoom.value = null
    messages.value = []
    members.value = []
    typingUsers.value.clear()
  }

  return {
    // State
    rooms,
    currentRoom,
    messages,
    members,
    isLoading,
    error,
    typingUsers,
    
    // Computed
    onlineMembers,
    sortedMessages,
    
    // Actions
    fetchRooms,
    createRoom,
    joinRoom,
    leaveRoom,
    deleteRoom,
    selectRoom,
    fetchMessages,
    fetchMembers,
    sendMessage,
    connectWebSocket,
    disconnectWebSocket,
    sendTyping,
    cleanup
  }
})
