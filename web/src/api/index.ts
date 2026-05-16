import axios from 'axios'

const api = axios.create({
  baseURL: '/api',
  timeout: 30000,
})

// Add auth token if available
api.interceptors.request.use((config) => {
  const token = localStorage.getItem('auth_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Session API
export const sessionApi = {
  list: () => api.get('/sessions'),
  get: (id: string) => api.get(`/sessions/${id}`),
  create: (data: { name?: string; model?: string }) => api.post('/sessions', data),
  delete: (id: string) => api.delete(`/sessions/${id}`),
  reset: (id: string) => api.post(`/sessions/${id}/reset`),
  search: (query: string) => api.get('/sessions/search', { params: { q: query } }),
}

// Toolset API
export const toolsetApi = {
  list: () => api.get('/toolsets'),
  get: (name: string) => api.get(`/toolsets/${name}`),
  enable: (name: string) => api.post(`/toolsets/${name}/enable`),
  disable: (name: string) => api.post(`/toolsets/${name}/disable`),
}

// Skill API
export const skillApi = {
  list: () => api.get('/skills'),
  get: (name: string) => api.get(`/skills/${name}`),
  create: (data: { name: string; content: string; category?: string }) => api.post('/skills', data),
  update: (name: string, data: { content: string }) => api.put(`/skills/${name}`, data),
  delete: (name: string) => api.delete(`/skills/${name}`),
  browse: () => api.get('/skills/browse'),
  install: (source: string) => api.post(`/skills/install`, { source }),
}

// Cron API
export const cronApi = {
  list: () => api.get('/cron'),
  get: (id: string) => api.get(`/cron/${id}`),
  create: (data: { name: string; schedule: string; command: string }) => api.post('/cron', data),
  update: (id: string, data: any) => api.put(`/cron/${id}`, data),
  delete: (id: string) => api.delete(`/cron/${id}`),
  pause: (id: string) => api.post(`/cron/${id}/pause`),
  resume: (id: string) => api.post(`/cron/${id}/resume`),
  run: (id: string) => api.post(`/cron/${id}/run`),
}

// Platform API
export const platformApi = {
  list: () => api.get('/platforms'),
  get: (name: string) => api.get(`/platforms/${name}`),
  update: (name: string, data: any) => api.put(`/platforms/${name}`, data),
  enable: (name: string) => api.post(`/platforms/${name}/enable`),
  disable: (name: string) => api.post(`/platforms/${name}/disable`),
}

// Analytics API
export const analyticsApi = {
  summary: () => api.get('/analytics/summary'),
  tokens: (days?: number) => api.get('/analytics/tokens', { params: { days } }),
  models: () => api.get('/analytics/models'),
}

// Settings API
export const settingsApi = {
  get: () => api.get('/settings'),
  update: (data: any) => api.put('/settings', data),
  profiles: {
    list: () => api.get('/settings/profiles'),
    create: (name: string) => api.post('/settings/profiles', { name }),
    switch: (name: string) => api.post(`/settings/profiles/${name}/switch`),
    delete: (name: string) => api.delete(`/settings/profiles/${name}`),
  },
}

// Profile API
export const profileApi = {
  list: () => api.get('/profiles'),
  get: (name: string) => api.get(`/profiles/${name}`),
  create: (name: string) => api.post('/profiles', { name }),
  switch: (name: string) => api.post(`/profiles/${name}/switch`),
  delete: (name: string) => api.delete(`/profiles/${name}`),
}

// Config API
export const configApi = {
  get: () => api.get('/config'),
  save: (data: { section: string; data: any }) => api.put('/config', data),
}

// Log API
export const logApi = {
  list: (params?: { file?: string; level?: string; search?: string; limit?: number }) =>
    api.get('/logs', { params }),
  tail: (params?: { file?: string; level?: string; lines?: number }) =>
    api.get('/logs/tail', { params }),
}

// Health API
export const healthApi = {
  check: () => api.get('/health'),
}

export default api
