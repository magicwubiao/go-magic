export interface Session {
  id: string
  name: string
  model?: string
  created_at: string
  updated_at: string
  message_count: number
  platform?: string
}

export interface Message {
  id: string
  role: 'user' | 'assistant' | 'system' | 'tool'
  content: string
  timestamp: string
  tool_calls?: ToolCall[]
  tool_results?: ToolResult[]
}

export interface ToolCall {
  id: string
  name: string
  arguments: Record<string, any>
}

export interface ToolResult {
  tool_call_id: string
  content: string
  success: boolean
}

export interface Toolset {
  name: string
  description: string
  tools: string[]
  enabled: boolean
  tags?: string[]
}

export interface Skill {
  name: string
  description: string
  category: string
  tags: string[]
  content?: string
  version?: string
  author?: string
}

export interface CronJob {
  id: string
  name: string
  schedule: string
  command: string
  active: boolean
  last_run?: string
  next_run?: string
  created_at: string
}

export interface Platform {
  name: string
  display_name: string
  enabled: boolean
  configured: boolean
  settings: Record<string, any>
}

export interface Analytics {
  total_tokens: number
  input_tokens: number
  output_tokens: number
  session_count: number
  estimated_cost: number
  cache_hit_rate: number
  model_usage: Record<string, number>
  daily_trend: DailyData[]
}

export interface DailyData {
  date: string
  tokens: number
  sessions: number
  cost: number
}

export interface Profile {
  name: string
  home_dir: string
  active: boolean
}

export interface Settings {
  display: DisplaySettings
  agent: AgentSettings
  memory: MemorySettings
  privacy: PrivacySettings
}

export interface DisplaySettings {
  streaming: boolean
  compact_mode: boolean
  reasoning: boolean
  cost_display: boolean
}

export interface AgentSettings {
  max_turns: number
  timeout: number
  tool_enforcement: boolean
}

export interface MemorySettings {
  enabled: boolean
  max_chars: number
}

export interface PrivacySettings {
  pii_redaction: boolean
}

export interface LogEntry {
  timestamp: string
  level: 'debug' | 'info' | 'warn' | 'error'
  message: string
  source?: string
}
