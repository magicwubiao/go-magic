import { request } from './client'

export interface ChatRoom {
  id: string
  name: string
  description: string
  members: string[]
  agent_ids: string[]
  created_at: number
}

export interface ChatMessage {
  id: string
  room_id: string
  sender: string
  role: 'user' | 'agent' | 'system'
  content: string
  timestamp: number
}

export interface RoomMember {
  id: string
  user_id: string
  name: string
  online: boolean
  joined_at: number
  last_seen?: number
}

export interface RoomAgent {
  id: string
  agent_id: string
  name: string
  profile: string
  description: string
  system_prompt?: string
  temperature?: number
  tools?: string
  invited: boolean
}

export async function getRooms(): Promise<ChatRoom[]> {
  return request('/groupchat/rooms')
}

export async function createRoom(room: Partial<ChatRoom>): Promise<ChatRoom> {
  return request('/groupchat/rooms', { method: 'POST', body: JSON.stringify(room) })
}

export async function getRoomMessages(roomId: string): Promise<ChatMessage[]> {
  return request(`/groupchat/rooms/${roomId}/messages`)
}

export async function sendRoomMessage(roomId: string, content: string): Promise<ChatMessage> {
  return request(`/groupchat/rooms/${roomId}/messages`, {
    method: 'POST',
    body: JSON.stringify({ content }),
  })
}

// Members
export async function getRoomMembers(roomId: string): Promise<RoomMember[]> {
  return request(`/groupchat/rooms/${roomId}/members`)
}

export async function addRoomMember(roomId: string, userId: string, name: string): Promise<RoomMember> {
  return request(`/groupchat/rooms/${roomId}/members`, {
    method: 'POST',
    body: JSON.stringify({ user_id: userId, name }),
  })
}

export async function removeRoomMember(roomId: string, memberId: string): Promise<void> {
  return request(`/groupchat/rooms/${roomId}/members/${memberId}`, { method: 'DELETE' })
}

// Agents
export async function getRoomAgents(roomId: string): Promise<RoomAgent[]> {
  return request(`/groupchat/rooms/${roomId}/agents`)
}

export async function addRoomAgent(roomId: string, agent: Partial<RoomAgent>): Promise<RoomAgent> {
  return request(`/groupchat/rooms/${roomId}/agents`, {
    method: 'POST',
    body: JSON.stringify(agent),
  })
}

export async function removeRoomAgent(roomId: string, agentId: string): Promise<void> {
  return request(`/groupchat/rooms/${roomId}/agents/${agentId}`, { method: 'DELETE' })
}

export async function updateRoomAgent(roomId: string, agentId: string, data: Partial<RoomAgent>): Promise<RoomAgent> {
  return request(`/groupchat/rooms/${roomId}/agents/${agentId}`, {
    method: 'PUT',
    body: JSON.stringify(data),
  })
}

// Invite
export async function generateInviteCode(roomId: string): Promise<{ invite_code: string }> {
  return request(`/groupchat/rooms/${roomId}/invite`, { method: 'POST' })
}

// Delete room
export async function deleteRoom(roomId: string): Promise<void> {
  return request(`/groupchat/rooms/${roomId}`, { method: 'DELETE' })
}

export async function updateRoom(roomId: string, data: { name: string }): Promise<void> {
  return request(`/groupchat/rooms/${roomId}`, { method: 'PUT', body: JSON.stringify(data) })
}
