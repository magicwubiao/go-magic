const API_BASE = import.meta.env.VITE_API_BASE || 'http://localhost:8642'

export const api = {
  // Sessions
  async listSessions() {
    const res = await fetch(`${API_BASE}/api/sessions`)
    return res.json()
  },

  async createSession() {
    const res = await fetch(`${API_BASE}/api/sessions`, { method: 'POST' })
    return res.json()
  },

  async getSession(id) {
    const res = await fetch(`${API_BASE}/api/sessions/${id}`)
    return res.json()
  },

  async deleteSession(id) {
    await fetch(`${API_BASE}/api/sessions/${id}`, { method: 'DELETE' })
  },

  // Chat
  async sendMessage(sessionId, message) {
    const res = await fetch(`${API_BASE}/api/sessions/${sessionId}/messages`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message })
    })
    return res.json()
  },

  // Streaming chat
  async sendMessageStream(sessionId, message, onChunk) {
    const res = await fetch(`${API_BASE}/api/sessions/${sessionId}/messages/stream`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ message }),
      headers: { 'Accept': 'text/event-stream' }
    })
    const reader = res.body.getReader()
    const decoder = new TextDecoder()

    while (true) {
      const { done, value } = await reader.read()
      if (done) break
      onChunk(decoder.decode(value))
    }
  },

  // Tools
  async listTools() {
    const res = await fetch(`${API_BASE}/api/tools`)
    return res.json()
  },

  async listToolsets() {
    const res = await fetch(`${API_BASE}/api/toolsets`)
    return res.json()
  },

  // Skills
  async listSkills() {
    const res = await fetch(`${API_BASE}/api/skills`)
    return res.json()
  },

  // Config
  async getConfig() {
    const res = await fetch(`${API_BASE}/api/config`)
    return res.json()
  },

  async updateConfig(config) {
    const res = await fetch(`${API_BASE}/api/config`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(config)
    })
    return res.json()
  },

  // Health
  async health() {
    const res = await fetch(`${API_BASE}/api/health`)
    return res.json()
  },

  // Logs
  async getLogs(level = 'info') {
    const res = await fetch(`${API_BASE}/api/logs?level=${level}`)
    return res.json()
  }
}
