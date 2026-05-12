import axios from 'axios'

const api = axios.create({
  baseURL: '',
  timeout: 60000,
})

// Session types
export interface Session {
  id: string
  name: string
  model?: string
  created_at: string
  updated_at: string
  message_count?: number
  messages?: Message[]
}

export interface Message {
  id: string
  role: 'user' | 'assistant' | 'system'
  content: string
  timestamp: string
  tools?: ToolCall[]
}

export interface ToolCall {
  name: string
  arguments?: string
  result?: string
  status?: string
}

// Stats
export interface Stats {
  total_sessions: number
  total_messages: number
  uptime_seconds: number
  toolset_count: number
  skill_count: number
}

// Toolset types
export interface Toolset {
  name: string
  description: string
  enabled: boolean
  tools: Tool[]
}

export interface Tool {
  name: string
  description: string
  toolset?: string
}

// Skill types
export interface Skill {
  name: string
  description: string
  category: string
  enabled: boolean
}

// Config types
export interface Config {
  provider: string
  model: string
  temperature: number
  max_tokens: number
  theme: string
  language: string
  streaming: boolean
  system_prompt: string
}

// Log types
export interface Log {
  id: string
  time: string
  timestamp: number
  level: string
  message: string
}

// Platform types
export interface Platform {
  name: string
  status: string
  message: string
}

// API methods
export const apiService = {
  // Health
  health: () => api.get('/api/health'),
  
  // Stats
  getStats: () => api.get<Stats>('/api/stats'),
  
  // Sessions
  sessions: {
    list: () => api.get<Session[]>('/api/sessions'),
    create: (data: { name?: string; model?: string }) => api.post<Session>('/api/sessions', data),
    get: (id: string) => api.get<Session>(`/api/sessions/${id}`),
    update: (id: string, data: { name?: string }) => api.put<Session>(`/api/sessions/${id}`, data),
    delete: (id: string) => api.delete(`/api/sessions/${id}`),
    getMessages: (id: string) => api.get<Message[]>(`/api/sessions/${id}/messages`),
    addMessage: (id: string, message: { content: string; role: string }) => 
      api.post<Message>(`/api/sessions/${id}/messages`, message),
  },
  
  // Chat
  chat: {
    send: (data: { message: string; session_id?: string; model?: string }) => 
      api.post('/api/chat', data),
    stream: (data: { message: string; session_id?: string; model?: string }) => {
      return api.post('/api/chat/stream', data, {
        responseType: 'stream',
      })
    },
  },
  
  // Toolsets
  toolsets: {
    list: () => api.get<Toolset[]>('/api/toolsets'),
    get: (name: string) => api.get<Toolset>(`/api/toolsets/${name}`),
    toggle: (name: string, enabled: boolean) => 
      api.post(`/api/toolsets/${name}/toggle`, { enabled }),
  },
  
  // Tools
  tools: {
    list: () => api.get<Tool[]>('/api/tools'),
  },
  
  // Skills
  skills: {
    list: () => api.get<Skill[]>('/api/skills'),
    create: (data: { name: string; description: string; category: string }) =>
      api.post<Skill>('/api/skills', data),
    get: (name: string) => api.get<Skill>(`/api/skills/${name}`),
    delete: (name: string) => api.delete(`/api/skills/${name}`),
  },
  
  // Config
  config: {
    get: () => api.get<Config>('/api/config'),
    update: (data: Partial<Config>) => api.put<Config>('/api/config', data),
  },
  
  // Logs
  logs: {
    list: (level?: string) => api.get<Log[]>('/api/logs', { params: { level } }),
    clear: () => api.delete('/api/logs'),
  },
  
  // Platforms
  platforms: {
    list: () => api.get<Platform[]>('/api/platforms'),
  },
}

export default apiService
