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
