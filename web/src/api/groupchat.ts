// Group Chat API
import request from '../request'

export interface Room {
  id: string
  name: string
  description: string
  created_at: string
  invite_code: string
  member_count: number
  is_active: boolean
}

export interface Message {
  id: string
  room_id: string
  user_id: string
  username: string
  content: string
  timestamp: string
  type: 'text' | 'agent' | 'system'
  agent_id?: string
}

export interface Member {
  id: string
  username: string
  room_id: string
  joined_at: string
  is_online: boolean
}

export const groupChatApi = {
  // Rooms
  listRooms: () => request.get<Room[]>('/api/groupchat/rooms'),
  
  createRoom: (data: { name: string; description: string }) => 
    request.post<Room>('/api/groupchat/rooms', data),
  
  getRoom: (id: string) => request.get<Room>(`/api/groupchat/rooms/${id}`),
  
  updateRoom: (id: string, data: Partial<Room>) => 
    request.put<Room>(`/api/groupchat/rooms/${id}`, data),
  
  deleteRoom: (id: string) => request.delete<void>(`/api/groupchat/rooms/${id}`),
  
  generateInviteCode: (id: string) => 
    request.post<{ code: string }>(`/api/groupchat/rooms/${id}/invite`),
  
  joinRoom: (code: string) => 
    request.post<Room>('/api/groupchat/rooms/join', { code }),
  
  leaveRoom: (id: string) => 
    request.post<void>(`/api/groupchat/rooms/${id}/leave`),

  // Messages
  getMessages: (roomId: string, params?: { before?: string; limit?: number }) =>
    request.get<Message[]>(`/api/groupchat/rooms/${roomId}/messages`, { params }),
  
  sendMessage: (roomId: string, content: string) =>
    request.post<Message>(`/api/groupchat/rooms/${roomId}/messages`, { content }),

  // Members
  getMembers: (roomId: string) =>
    request.get<Member[]>(`/api/groupchat/rooms/${roomId}/members`),
  
  removeMember: (roomId: string, userId: string) =>
    request.delete<void>(`/api/groupchat/rooms/${roomId}/members/${userId}`),

  // Agents
  listAgents: () => request.get<{ id: string; name: string }[]>('/api/groupchat/agents'),
  
  addAgent: (roomId: string, agentId: string) =>
    request.post<void>(`/api/groupchat/rooms/${roomId}/agents`, { agent_id: agentId }),
  
  removeAgent: (roomId: string, agentId: string) =>
    request.delete<void>(`/api/groupchat/rooms/${roomId}/agents/${agentId}`),
}
