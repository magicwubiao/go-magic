import { defineStore } from 'pinia'
import { ref, computed, reactive, nextTick } from 'vue'
import type { Session, Message } from '@/api/sessions'
import * as sessionsApi from '@/api/sessions'
import * as commandsApi from '@/api/commands'
import * as approvalApi from '@/api/approval'
import { i18n } from '@/locales'

export interface ChatError {
  message: string
  code?: string
}

export interface ToolCallEvent {
  id: string
  name: string
  args: string
  success?: boolean
  duration?: string
  content?: string
  status: 'running' | 'completed' | 'error'
}

export interface TaskProgress {
  phase: string
  detail: string
  percent: number
  iteration: number
  maxIterations: number
  tokensUsed: number
  tokensRemaining: number
}

// 对话流内嵌审批卡片状态。pending=等待用户决策，
// approving/denying=已点击按钮正在请求后端，approved/denied/expired=终态。
export type ApprovalCardStatus =
  | 'pending'
  | 'approving'
  | 'denying'
  | 'approved'
  | 'denied'
  | 'expired'

export interface PendingApprovalCard {
  id: string
  command: string
  riskLevel: string
  reason: string
  context: string
  workDir: string
  createdAt: number // unix seconds
  expiresAt: number // unix seconds
  status: ApprovalCardStatus
  resolveReason?: string
}

interface SessionState {
  messages: Message[]
  streaming: boolean
  streamContent: string
  streamBuffer: string
  toolCalls: ToolCallEvent[]
  taskProgress: TaskProgress | null
  pendingApprovals: PendingApprovalCard[]
}

function $t(key: string, params?: Record<string, string | number>): string {
  return i18n.global.t(key, params)
}

export const useChatStore = defineStore('chat', () => {
  const sessions = ref<Session[]>([])
  const activeSessionId = ref<string | null>(null)
  const error = ref<ChatError | null>(null)
  const sessionsLoading = ref(false)
  const sessionsHasMore = ref(true)
  const sessionsOffset = ref(0)
  const SESSIONS_LIMIT = 20

  const builtinCommands: Array<Omit<commandsApi.Command, 'description'>> = [
    { name: 'help', usage: '/help', aliases: [], category: 'general' },
    { name: 'new', usage: '/new', aliases: [], category: 'session' },
    { name: 'clear', usage: '/clear', aliases: [], category: 'session' },
    { name: 'undo', usage: '/undo', aliases: [], category: 'session' },
  ]

  const commands = computed<commandsApi.Command[]>(() => {
    return builtinCommands.map(cmd => ({
      ...cmd,
      description: $t(`chat.commands.${cmd.name}`),
    }))
  })
  
  const sessionStates = ref<Record<string, SessionState>>({})
  const sessionEventSources = ref<Record<string, EventSource | null>>({})
  const sessionFlushTimers = ref<Record<string, ReturnType<typeof setTimeout> | null>>({})
  
  let toolCallIdCounter = 0

  const STREAM_FLUSH_INTERVAL = 80

  const activeSession = computed(() =>
    sessions.value.find(s => s.id === activeSessionId.value)
  )

  const currentWorkDir = computed(() => activeSession.value?.work_dir || '')

  const currentWorkDirUserSet = computed(() => activeSession.value?.work_dir_user_set || false)

  const activeSessionState = computed(() => {
    if (!activeSessionId.value) return null
    return sessionStates.value[activeSessionId.value] || null
  })

  const messages = computed(() => {
    const state = activeSessionState.value
    return state?.messages || []
  })

  const streaming = computed(() => {
    const state = activeSessionState.value
    return state?.streaming || false
  })

  const streamContent = computed(() => {
    const state = activeSessionState.value
    return state?.streamContent || ''
  })

  const toolCalls = computed(() => {
    const state = activeSessionState.value
    return state?.toolCalls || []
  })

  const activeToolCalls = computed(() => {
    return toolCalls.value.filter(tc => tc.status === 'running')
  })

  const taskProgress = computed(() => {
    const state = activeSessionState.value
    return state?.taskProgress || null
  })

  const isLongTask = computed(() => {
    if (!taskProgress.value) return false
    const tp = taskProgress.value
    return tp.maxIterations > 20 || tp.percent > 0
  })

  const pendingApprovals = computed(() => {
    const state = activeSessionState.value
    return state?.pendingApprovals || []
  })

  // 当前会话中仍在等待用户决策的审批（用于阻塞提示与卡片渲染）
  const activePendingApprovals = computed(() => {
    return pendingApprovals.value.filter(p => p.status === 'pending')
  })

  function getOrCreateSessionState(sessionId: string): SessionState {
    let state = sessionStates.value[sessionId]
    if (!state) {
      state = reactive({
        messages: [],
        streaming: false,
        streamContent: '',
        streamBuffer: '',
        toolCalls: [],
        taskProgress: null,
        pendingApprovals: [],
      })
      sessionStates.value = { ...sessionStates.value, [sessionId]: state }
    }
    return state
  }

  async function loadSessions(): Promise<void> {
    sessionsOffset.value = 0
    sessionsHasMore.value = true
    try {
      const result = await sessionsApi.getSessions(SESSIONS_LIMIT, 0)
      const filtered = result.sessions.filter(s => !s.source || s.source === 'web')
      sessions.value = filtered
      if (filtered.length < SESSIONS_LIMIT) {
        sessionsHasMore.value = false
      }
      if (!activeSessionId.value && sessions.value.length > 0) {
        const first = sessions.value[0]
        activeSessionId.value = first.id
        getOrCreateSessionState(first.id)
      }
    } catch (e) {
      console.error('Failed to load sessions:', e)
      sessions.value = []
    }
  }

  async function loadMoreSessions(): Promise<boolean> {
    if (sessionsLoading.value) return false
    if (!sessionsHasMore.value) return false

    sessionsLoading.value = true
    sessionsOffset.value += SESSIONS_LIMIT
    try {
      const result = await sessionsApi.getSessions(SESSIONS_LIMIT, sessionsOffset.value)
      const newSessions = result.sessions.filter(s => !s.source || s.source === 'web')
      if (newSessions.length > 0) {
        sessions.value = [...sessions.value, ...newSessions]
      }
      if (newSessions.length < SESSIONS_LIMIT) {
        sessionsHasMore.value = false
      }
      return newSessions.length > 0
    } catch (e) {
      console.error('Failed to load more sessions:', e)
      sessionsHasMore.value = false
      return false
    } finally {
      sessionsLoading.value = false
    }
  }

  // Server commands integration not yet implemented, using builtin commands only

  function isCommand(input: string): boolean {
    return input.trim().startsWith('/')
  }

  async function executeCommand(input: string): Promise<void> {
    const trimmed = input.trim()
    if (!trimmed) return

    const parts = trimmed.split(/\s+/)
    const cmd = parts[0].toLowerCase()

    if (!activeSessionId.value && cmd !== '/new') {
      await createSession()
      if (!activeSessionId.value) return
    }

    let result: string | null = null

    switch (cmd) {
      case '/help': {
        const lines = commands.value.map(c => `  ${c.usage.padEnd(20)} ${c.description}`)
        result = `**${$t('chat.commands.available')}**：\n\n${lines.join('\n')}\n\n${$t('chat.commands.hint')}`
        break
      }
      case '/new': {
        await createSession()
        result = $t('chat.commands.created')
        break
      }
      case '/clear': {
        if (activeSessionId.value) {
          const state = sessionStates.value[activeSessionId.value]
          if (state) state.messages = []
        }
        result = $t('chat.commands.cleared')
        break
      }
      case '/undo': {
        if (activeSessionId.value) {
          const state = sessionStates.value[activeSessionId.value]
          if (state && state.messages.length > 0) {
            state.messages = state.messages.slice(0, -1)
            result = $t('chat.commands.undone')
          } else {
            result = $t('chat.commands.noUndo')
          }
        } else {
          result = $t('chat.commands.noSession')
        }
        break
      }
      default: {
        result = $t('chat.commands.unknown', { cmd })
      }
    }

    if (result && activeSessionId.value) {
      addSystemMessage(result)
    }
  }

  function addSystemMessage(content: string): void {
    if (!activeSessionId.value) return
    
    const sessionId = activeSessionId.value
    const state = getOrCreateSessionState(sessionId)
    state.messages.push({
      id: Date.now().toString(),
      role: 'system',
      content,
      timestamp: new Date().toISOString(),
      session_id: sessionId,
    })
  }

  function autocompleteCommand(input: string): string[] {
    if (!input.startsWith('/')) return []

    const partial = input.toLowerCase().slice(1)
    const suggestions: string[] = []
    const cmdList = commands.value

    for (const cmd of cmdList) {
      if (cmd.name.toLowerCase().startsWith(partial)) {
        suggestions.push('/' + cmd.name)
      }
      for (const alias of cmd.aliases || []) {
        if (alias.toLowerCase().startsWith(partial)) {
          suggestions.push(alias)
        }
      }
    }

    return [...new Set(suggestions)].slice(0, 10)
  }

  async function createSession(workDir?: string): Promise<Session | null> {
    try {
      error.value = null
      const session = await sessionsApi.createSession(workDir)
      sessions.value = [session, ...sessions.value]
      activeSessionId.value = session.id
      getOrCreateSessionState(session.id)
      return session
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      error.value = { message: 'Failed to create session: ' + errMsg }
      console.error('Failed to create session:', e)
      return null
    }
  }

  async function selectSession(id: string): Promise<void> {
    activeSessionId.value = id
    const state = getOrCreateSessionState(id)

    if (state.messages.length === 0) {
      try {
        const res = await sessionsApi.getSession(id)
        state.messages = res.messages || []
      } catch (e) {
        console.error('Failed to load session messages:', e)
        state.messages = []
      }
    }

    // 恢复待审批：页面刷新或 SSE 断连后，从后端拉取当前会话的 pending 审批，
    // 确保用户不会因为连接中断而错过阻塞中的审批。
    restorePendingApprovals(id)
  }

  async function deleteSession(id: string, deleteFiles: boolean = false): Promise<void> {
    try {
      await sessionsApi.deleteSession(id, deleteFiles)
      sessions.value = sessions.value.filter(s => s.id !== id)
      
      if (sessionFlushTimers.value[id]) {
        clearTimeout(sessionFlushTimers.value[id]!)
      }
      if (sessionEventSources.value[id]) {
        sessionEventSources.value[id]!.close()
      }
      
      const newStates = { ...sessionStates.value }
      delete newStates[id]
      sessionStates.value = newStates
      
      const newEventSources = { ...sessionEventSources.value }
      delete newEventSources[id]
      sessionEventSources.value = newEventSources
      
      if (activeSessionId.value === id) {
        activeSessionId.value = null
      }
    } catch (e) {
      console.error('Failed to delete session:', e)
    }
  }

  async function renameSession(id: string, name: string): Promise<void> {
    try {
      await sessionsApi.renameSession(id, name)
      const session = sessions.value.find(s => s.id === id)
      if (session) {
        session.title = name
      }
    } catch (e) {
      console.error('Failed to rename session:', e)
    }
  }

  async function updateSessionWorkDir(id: string, workDir: string): Promise<void> {
    // 直接透传后端错误，调用方负责提示。后端在 work_dir 已被用户设置后会拒绝修改。
    await sessionsApi.updateSessionWorkDir(id, workDir)
    const session = sessions.value.find(s => s.id === id)
    if (session) {
      session.work_dir = workDir
      // 仅在设置非空目录时锁定；清空时解除用户设置标记
      session.work_dir_user_set = workDir !== ''
    }
  }

  function flushStreamBuffer(sessionId: string): void {
    const state = sessionStates.value[sessionId]
    if (!state) return
    
    if (state.streamBuffer) {
      state.streamContent += state.streamBuffer
      state.streamBuffer = ''
    }
    sessionFlushTimers.value = { ...sessionFlushTimers.value, [sessionId]: null }
  }

  async function sendMessage(content: string, images?: string[], files?: sessionsApi.UploadedFile[]): Promise<void> {
    if (!activeSessionId.value) {
      const session = await createSession()
      if (!session) return
    }

    const sessionId = activeSessionId.value!
    const state = getOrCreateSessionState(sessionId)

    state.messages.push({
      id: Date.now().toString(),
      role: 'user',
      content,
      timestamp: new Date().toISOString(),
      session_id: sessionId,
      images,
      files,
    })

    state.streaming = true
    state.streamContent = ''
    state.streamBuffer = ''
    state.toolCalls = []
    state.taskProgress = null
    // 新一轮对话开始时清空上一轮的审批卡片（此时 streaming=false 已保证无 pending 项）
    state.pendingApprovals = []
    error.value = null

    if (sessionFlushTimers.value[sessionId]) {
      clearTimeout(sessionFlushTimers.value[sessionId]!)
      sessionFlushTimers.value = { ...sessionFlushTimers.value, [sessionId]: null }
    }
    if (sessionEventSources.value[sessionId]) {
      sessionEventSources.value[sessionId]!.close()
      sessionEventSources.value = { ...sessionEventSources.value, [sessionId]: null }
    }

    try {
      const eventSource = sessionsApi.streamChat(sessionId, content, images, files)
      sessionEventSources.value = { ...sessionEventSources.value, [sessionId]: eventSource }

      eventSource.onmessage = (event) => {
        // Handle legacy [DONE] signal (now backend sends {"done":true}, but keep for safety)
        if (event.data === '[DONE]') {
          if (sessionFlushTimers.value[sessionId]) {
            clearTimeout(sessionFlushTimers.value[sessionId]!)
            sessionFlushTimers.value = { ...sessionFlushTimers.value, [sessionId]: null }
          }
          flushStreamBuffer(sessionId)
          if (sessionEventSources.value[sessionId]) {
            sessionEventSources.value[sessionId]!.close()
            sessionEventSources.value = { ...sessionEventSources.value, [sessionId]: null }
          }
          state.streaming = false
          state.taskProgress = null
          if (state.streamContent) {
            state.messages.push({
              id: Date.now().toString(),
              role: 'assistant' as const,
              content: state.streamContent,
              timestamp: new Date().toISOString(),
              session_id: sessionId,
            })
            state.streamContent = ''
            loadSessions()
          }
          return
        }

        try {
          const data = JSON.parse(event.data)

          if (data.type === 'progress') {
            state.taskProgress = {
              phase: data.phase || 'executing',
              detail: data.detail || '',
              percent: data.percent || 0,
              iteration: data.iteration || 0,
              maxIterations: data.maxIterations || 0,
              tokensUsed: data.tokensUsed || 0,
              tokensRemaining: data.tokensRemaining || 0,
            }
            return
          }

          if (data.type === 'ping') {
            return
          }

          if (data.type === 'tool_start') {
            if (sessionFlushTimers.value[sessionId]) {
              clearTimeout(sessionFlushTimers.value[sessionId]!)
              sessionFlushTimers.value = { ...sessionFlushTimers.value, [sessionId]: null }
            }
            flushStreamBuffer(sessionId)
            
            const id = `tc_${++toolCallIdCounter}`
            state.toolCalls.push({
              id,
              name: data.name,
              args: data.args,
              status: 'running',
            })
            return
          }

          if (data.type === 'tool_result') {
            const tc = state.toolCalls.find(t => t.name === data.name && t.status === 'running')
            if (tc) {
              tc.success = data.success
              tc.duration = data.duration
              tc.content = data.content
              tc.status = data.success ? 'completed' : 'error'
            } else {
              state.toolCalls.push({
                id: `tc_${++toolCallIdCounter}`,
                name: data.name,
                args: '',
                success: data.success,
                duration: data.duration,
                content: data.content,
                status: data.success ? 'completed' : 'error',
              })
            }
            return
          }

          // 审批请求：后端在 ApprovalHook 创建 pending 时通过 SSE 推送，
          // 前端在对话流内渲染审批卡片，用户点击批准/拒绝后调用 resolve API。
          if (data.type === 'approval_required') {
            const card: PendingApprovalCard = {
              id: data.id || '',
              command: data.command || '',
              riskLevel: data.risk_level || data.riskLevel || 'low',
              reason: data.reason || '',
              context: data.context || '',
              workDir: data.work_dir || data.workDir || '',
              createdAt: data.created_at || Math.floor(Date.now() / 1000),
              expiresAt: data.expires_at || 0,
              status: 'pending',
            }
            // 同一 id 不重复插入（避免重复推送造成多张卡片）
            const exists = state.pendingApprovals.some(p => p.id === card.id)
            if (!exists) {
              state.pendingApprovals.push(card)
            }
            return
          }

          if (data.error) {
            state.streaming = false
            state.taskProgress = null
            
            if (sessionEventSources.value[sessionId]) {
              sessionEventSources.value[sessionId]!.close()
              sessionEventSources.value = { ...sessionEventSources.value, [sessionId]: null }
            }
            
            error.value = { message: data.error }
            console.error('Stream error:', data.error)
            return
          }

          if (data.delta) {
            state.streamBuffer += data.delta
            
            if (!sessionFlushTimers.value[sessionId]) {
              const timer = setTimeout(() => {
                flushStreamBuffer(sessionId)
              }, STREAM_FLUSH_INTERVAL)
              sessionFlushTimers.value = { ...sessionFlushTimers.value, [sessionId]: timer }
            }
          }
          
          if (data.done) {
            if (sessionFlushTimers.value[sessionId]) {
              clearTimeout(sessionFlushTimers.value[sessionId]!)
              sessionFlushTimers.value = { ...sessionFlushTimers.value, [sessionId]: null }
            }

            flushStreamBuffer(sessionId)

            if (sessionEventSources.value[sessionId]) {
              sessionEventSources.value[sessionId]!.close()
              sessionEventSources.value = { ...sessionEventSources.value, [sessionId]: null }
            }

            // 先恢复按钮状态，让用户可以立即操作
            state.streaming = false
            state.taskProgress = null

            // 用 nextTick 让按钮切换先渲染，再处理内容 push 和会话刷新
            const finalContent = state.streamContent
            nextTick(() => {
              state.messages.push({
                id: Date.now().toString(),
                role: 'assistant' as const,
                content: finalContent,
                timestamp: new Date().toISOString(),
                session_id: sessionId,
              })
              state.streamContent = ''
              loadSessions()
            })
          }
        } catch (e) {
          console.error('Failed to parse stream event:', e)
        }
      }

      eventSource.onerror = () => {
        if (sessionFlushTimers.value[sessionId]) {
          clearTimeout(sessionFlushTimers.value[sessionId]!)
          sessionFlushTimers.value = { ...sessionFlushTimers.value, [sessionId]: null }
        }
        flushStreamBuffer(sessionId)
        if (sessionEventSources.value[sessionId]) {
          sessionEventSources.value[sessionId]!.close()
          sessionEventSources.value = { ...sessionEventSources.value, [sessionId]: null }
        }

        state.streaming = false
        state.taskProgress = null

        // Save any partial content received before disconnect
        if (state.streamContent) {
          const partialContent = state.streamContent
          state.messages.push({
            id: Date.now().toString(),
            role: 'assistant' as const,
            content: partialContent + '\n\n*[Connection interrupted, partial response saved]*',
            timestamp: new Date().toISOString(),
            session_id: sessionId,
          })
          state.streamContent = ''
          loadSessions()
        } else if (!state.messages.some(m => m.role === 'assistant' && m.session_id === sessionId)) {
          error.value = { message: 'Connection lost' }
        }
      }
    } catch (e) {
      state.streaming = false
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      // Ignore abort errors (user cancelled or timeout)
      if (errMsg.includes('aborted') || errMsg.includes('abort')) {
        return
      }
      error.value = { message: 'Failed to send message: ' + errMsg }
    }
  }

  function stopGeneration(): void {
    if (!activeSessionId.value) return

    const sessionId = activeSessionId.value
    const state = getOrCreateSessionState(sessionId)

    if (sessionFlushTimers.value[sessionId]) {
      clearTimeout(sessionFlushTimers.value[sessionId]!)
      sessionFlushTimers.value = { ...sessionFlushTimers.value, [sessionId]: null }
    }
    flushStreamBuffer(sessionId)

    if (sessionEventSources.value[sessionId]) {
      sessionEventSources.value[sessionId]!.close()
      sessionEventSources.value = { ...sessionEventSources.value, [sessionId]: null }
    }

    state.streaming = false
    state.taskProgress = null

    for (const tc of state.toolCalls) {
      if (tc.status === 'running') {
        tc.status = 'error'
        tc.success = false
      }
    }

    if (state.streamContent) {
      state.messages.push({
        id: Date.now().toString(),
        role: 'assistant',
        content: state.streamContent + '\n\n*[Stopped by user]*',
        timestamp: new Date().toISOString(),
        session_id: sessionId,
      })
      state.streamContent = ''
    }
  }

  // 标记某个 pending 卡片为已过期（由组件倒计时触发）。
  // 与 resolveChatApproval 一致：短暂展示终态后自动移除卡片，避免残留。
  function markApprovalExpired(sessionId: string, approvalId: string): void {
    const state = sessionStates.value[sessionId]
    if (!state) return
    const card = state.pendingApprovals.find(p => p.id === approvalId)
    if (card && card.status === 'pending') {
      card.status = 'expired'
      card.resolveReason = 'expired'
      setTimeout(() => {
        const st = sessionStates.value[sessionId]
        if (!st) return
        const idx = st.pendingApprovals.findIndex(p => p.id === approvalId)
        if (idx >= 0) {
          st.pendingApprovals.splice(idx, 1)
        }
      }, 1500)
    }
  }

  // 从后端恢复待审批列表（页面刷新或 SSE 断连后的 fallback）。
  // 只填充当前会话已有的 pending 卡片，按 id 去重，避免与 SSE 事件重复。
  // getPendingApprovals 返回的字段不如 SSE 事件完整（缺 reason/context/workDir），
  // 但足以让用户看到命令、风险等级并完成审批操作。
  async function restorePendingApprovals(sessionId: string): Promise<void> {
    const state = sessionStates.value[sessionId]
    if (!state) return
    // 仅在没有本地 pending 时恢复，避免覆盖 SSE 实时推送的完整数据
    if (state.pendingApprovals.some(p => p.status === 'pending')) return
    try {
      const items = await approvalApi.getPendingApprovals(sessionId)
      const now = Math.floor(Date.now() / 1000)
      for (const p of items) {
        const exists = state.pendingApprovals.some(c => c.id === p.id)
        if (exists) continue
        const expiresAt = p.expiresAt
          ? Math.floor(new Date(p.expiresAt).getTime() / 1000)
          : 0
        state.pendingApprovals.push({
          id: p.id,
          command: p.command,
          riskLevel: p.riskLevel || 'low',
          reason: '',
          context: '',
          workDir: '',
          createdAt: p.createdAt ? Math.floor(new Date(p.createdAt).getTime() / 1000) : now,
          expiresAt,
          status: 'pending',
        })
      }
    } catch {
      // 静默失败，不影响会话加载
    }
  }

  // 对话流内审批：调用后端 resolve API 并更新卡片状态。
  // approved=true 走批准，false 走拒绝（可附理由）。
  async function resolveChatApproval(
    sessionId: string,
    approvalId: string,
    approved: boolean,
    reason: string = '',
  ): Promise<void> {
    const state = sessionStates.value[sessionId]
    if (!state) return
    const card = state.pendingApprovals.find(p => p.id === approvalId)
    if (!card) return
    // 防止重复点击：只处理 pending 状态的卡片
    if (card.status !== 'pending') return

    card.status = approved ? 'approving' : 'denying'
    try {
      await approvalApi.resolvePendingApproval(approvalId, approved, reason)
      card.status = approved ? 'approved' : 'denied'
      card.resolveReason = reason || (approved ? 'approved' : 'denied')
      // 短暂展示终态后自动移除卡片，避免审批完成后卡片残留
      setTimeout(() => {
        const st = sessionStates.value[sessionId]
        if (!st) return
        const idx = st.pendingApprovals.findIndex(p => p.id === approvalId)
        if (idx >= 0) {
          st.pendingApprovals.splice(idx, 1)
        }
      }, 1500)
    } catch (e) {
      // 还原为 pending，允许用户重试
      card.status = 'pending'
      const errMsg = e instanceof Error ? e.message : String(e)
      error.value = { message: `${$t('approval.pending.resolveFailed')}: ${errMsg}` }
      throw e
    }
  }

  function cleanup(): void {
    for (const sessionId of Object.keys(sessionStates.value)) {
      if (sessionFlushTimers.value[sessionId]) {
        clearTimeout(sessionFlushTimers.value[sessionId]!)
      }
      if (sessionEventSources.value[sessionId]) {
        sessionEventSources.value[sessionId]!.close()
      }
    }
    sessionStates.value = {}
    sessionEventSources.value = {}
    sessionFlushTimers.value = {}
  }

  return {
    sessions,
    activeSessionId,
    messages,
    streaming,
    streamContent,
    error,
    activeSession,
    currentWorkDir,
    currentWorkDirUserSet,
    toolCalls,
    activeToolCalls,
    taskProgress,
    isLongTask,
    pendingApprovals,
    activePendingApprovals,
    sessionsLoading,
    sessionsHasMore,
    loadSessions,
    loadMoreSessions,
    createSession,
    selectSession,
    deleteSession,
    renameSession,
    updateSessionWorkDir,
    sendMessage,
    stopGeneration,
    cleanup,
    isCommand,
    executeCommand,
    autocompleteCommand,
    addSystemMessage,
    getOrCreateSessionState,
    resolveChatApproval,
    markApprovalExpired,
    restorePendingApprovals,
  }
})