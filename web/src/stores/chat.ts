import { defineStore } from 'pinia'
import { ref, computed, reactive, nextTick } from 'vue'
import type { Session, Message, FileOp } from '@/api/sessions'
import * as sessionsApi from '@/api/sessions'
import * as commandsApi from '@/api/commands'
import * as approvalApi from '@/api/approval'
import * as clarifyApi from '@/api/clarify'
import { i18n } from '@/locales'

export interface ChatError {
  message: string
  code?: string
}

export interface ToolCallEvent {
  id: string
  name: string
  args: string
  args_text?: string
  success?: boolean
  duration?: string
  content?: string
  status: 'running' | 'completed' | 'error'
  file_ops?: FileOp[]
  todo_changed?: boolean
}

export type { ToolCallEvent as ToolCallSnapshot }
export type { FileOp }
export type { StreamSegment as TimelineSegment }
export {}

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

// 澄清卡片状态。pending=等待用户选择/补充；answering=已提交正在请求后端；
// answered=已答复（短暂终态后移除）；expired=等待超时（AI 已继续或收尾）。
export type ClarificationCardStatus = 'pending' | 'answering' | 'answered' | 'expired'

export interface PendingClarificationCard {
  id: string
  question: string
  options: string[]
  context: string
  multiSelect: boolean
  header: string
  createdAt: number // unix seconds
  expiresAt: number // unix seconds
  status: ClarificationCardStatus
  answerSummary?: string // 本端已提交的答复描述（终态展示）
}

// 流式渲染 timeline：按"发生顺序"记录 text 段切点和 tool 段，
// 使思考文本与工具执行在对话中能够互相穿插，而不是工具一律堆在文本最后。
// - text 段：end = 该段结束时 streamContent 的累计字符长度
//   （相邻 text 段可以合并显示，end 只用于切片）
// - tool 段：toolCallId = 对应 ToolCallEvent.id
export interface StreamSegText { id: string; kind: 'text'; end: number }
export interface StreamSegTool { id: string; kind: 'tool'; toolCallId: string }
export type StreamSegment = StreamSegText | StreamSegTool

interface SessionState {
  messages: Message[]
  streaming: boolean
  streamContent: string
  streamBuffer: string
  toolCalls: ToolCallEvent[]
  taskProgress: TaskProgress | null
  pendingApprovals: PendingApprovalCard[]
  pendingClarifications: PendingClarificationCard[]
  streamingSegments: StreamSegment[]
  // 上次追加 text 段时 streamContent 的末尾字符长度
  // （下次 push text 段时 end 必须大于它，否则不产生新段）
  lastStreamSegEnd: number
}

function $t(key: string, params?: Record<string, unknown>): string {
  return params === undefined ? i18n.global.t(key) : i18n.global.t(key, params)
}

export const useChatStore = defineStore('chat', () => {
  const sessions = ref<Session[]>([])
  const activeSessionId = ref<string | null>(null)
  const error = ref<ChatError | null>(null)
  const sessionsLoading = ref(false)
  const sessionsHasMore = ref(true)
  const sessionsOffset = ref(0)
  const SESSIONS_LIMIT = 20

  // 事件总线：用于跨 store 通知（例如 tool_result 中 todo_changed=true 时触发待办刷新）
  type TodoChangeListener = () => void
  const todoChangeListeners: TodoChangeListener[] = []
  function onTodoChange(listener: TodoChangeListener) {
    todoChangeListeners.push(listener)
    return () => {
      const i = todoChangeListeners.indexOf(listener)
      if (i >= 0) todoChangeListeners.splice(i, 1)
    }
  }
  function emitTodoChanged() {
    todoChangeListeners.forEach(l => {
      try { l() } catch (e) { console.error(e) }
    })
  }

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
  const sessionEventSources = ref<Record<string, sessionsApi.ChatStream | null>>({})
  const sessionFlushTimers = ref<Record<string, ReturnType<typeof setTimeout> | null>>({})
  // 流断线恢复轮询定时器（移动端切后台杀连接后使用）
  const sessionRecoveryTimers = ref<Record<string, ReturnType<typeof setTimeout> | null>>({})
  
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

  const streamingSegments = computed((): StreamSegment[] => {
    const state = activeSessionState.value
    return state?.streamingSegments || []
  })

  // 如果当前 streamContent 有新内容还没进入 timeline，
  // 就追加一个 text 段（只记录 end 偏移，切片在渲染时做）。
  function pushTextSegmentIfNeeded(sessionId: string): void {
    const state = sessionStates.value[sessionId]
    if (!state) return
    const end = state.streamContent.length
    // 只有真的有新增字符，且跟上次位置不一样时才加段。
    // 同时如果 timeline 末尾已经是 text 段，直接覆盖其 end 即可（避免产生无意义的多段）。
    if (end <= state.lastStreamSegEnd) return
    const segs = state.streamingSegments
    const last = segs[segs.length - 1]
    if (last && last.kind === 'text') {
      last.end = end
    } else {
      segs.push({ id: `seg_t_${Date.now()}_${end}`, kind: 'text', end })
    }
    state.lastStreamSegEnd = end
  }

  function pushToolSegment(sessionId: string, toolCallId: string): void {
    const state = sessionStates.value[sessionId]
    if (!state) return
    // 在插入 tool 段之前，先把当下已经 flush 完的文本切到 timeline 里，
    // 保证"先出文字，再出该文字之后触发的工具"这个顺序正确。
    pushTextSegmentIfNeeded(sessionId)
    state.streamingSegments.push({
      id: `seg_tc_${toolCallId}_${Date.now()}`,
      kind: 'tool',
      toolCallId,
    })
  }

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

  const pendingClarifications = computed(() => {
    const state = activeSessionState.value
    return state?.pendingClarifications || []
  })

  const activePendingClarifications = computed(() => {
    return pendingClarifications.value.filter(c => c.status === 'pending')
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
        pendingClarifications: [],
        streamingSegments: [],
        lastStreamSegEnd: 0,
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
      // hasMore 必须按原始页长判断：一页里混有非 web 会话（bot/tui/平台网关
      // 等）时 filtered 数会小于页大小，若据此判定会提前截断、老会话永远
      // 加载不进侧栏（搜索全量补齐却能搜到，表现为"搜到的比侧栏多"）。
      if (result.sessions.length < SESSIONS_LIMIT) {
        sessionsHasMore.value = false
      }
      // Do NOT auto-select the first session on refresh — land on a
      // blank new-chat state. A session is created automatically when
      // the user sends their first message.
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
      // 新建会话等操作会让后端按时间排序的位置漂移，数字 offset 可能取到
      // 已加载的会话；按 id 去重兜底，避免列表出现重复条目
      const known = new Set(sessions.value.map(s => s.id))
      const newSessions = result.sessions.filter(s => (!s.source || s.source === 'web') && !known.has(s.id))
      if (newSessions.length > 0) {
        sessions.value = [...sessions.value, ...newSessions]
      }
      // 同 loadSessions：按原始页长判断 hasMore，避免非 web 会话混页导致提前截断
      if (result.sessions.length < SESSIONS_LIMIT) {
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

  // 把不在当前列表中的会话并入顶部（去重）。会话侧栏搜索命中"尚未加载的
  // 历史会话"时，先并入列表再 select，保证 activeSession 等派生状态正常。
  function mergeSessions(list: Session[]): void {
    const known = new Set(sessions.value.map(s => s.id))
    const fresh = list.filter(s => !known.has(s.id))
    if (fresh.length > 0) sessions.value = [...fresh, ...sessions.value]
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
    // 恢复待答复的澄清卡片（AI 需求不明确时的提问）。
    restorePendingClarifies(id)
  }

  async function deleteSession(id: string, deleteFiles: boolean = false): Promise<void> {
    try {
      await sessionsApi.deleteSession(id, deleteFiles)
      sessions.value = sessions.value.filter(s => s.id !== id)
      
      if (sessionFlushTimers.value[id]) {
        clearTimeout(sessionFlushTimers.value[id]!)
      }
      stopStreamRecovery(id)
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

// 直接透传后端错误，调用方负责提示。后端在 work_dir 已被用户设置后会拒绝修改。
  async function updateSessionWorkDir(id: string, workDir: string): Promise<void> {
    await sessionsApi.updateSessionWorkDir(id, workDir)
    const session = sessions.value.find(s => s.id === id)
    if (session) {
      session.work_dir = workDir
      // 仅在设置非空目录时锁定；清空时解除用户设置标记
      session.work_dir_user_set = workDir !== ''
    }
  }

  // 判断某个会话当前是否正在执行（流式进行中 / 断线恢复轮询中）。
  function isSessionRunning(id: string): boolean {
    const state = sessionStates.value[id]
    return !!(state && state.streaming)
  }

  function flushStreamBuffer(sessionId: string): void {
    const state = sessionStates.value[sessionId]
    if (!state) return

    if (state.streamBuffer) {
      state.streamContent += state.streamBuffer
      state.streamBuffer = ''
    }
    sessionFlushTimers.value = { ...sessionFlushTimers.value, [sessionId]: null }
    pushTextSegmentIfNeeded(sessionId)
  }

  // ========== 流断线恢复（移动端切后台/锁屏场景） ==========
  // 手机浏览器切后台会杀掉 SSE 连接，但服务端回合与连接已解耦、
  // 会继续执行并落库。onerror 不再直接判死，而是轮询 /running：
  // running=false 且服务端出现 assistant 消息 → 用服务端最终消息收尾。

  const RECOVERY_POLL_INTERVAL = 3000
  const RECOVERY_TIMEOUT = 10 * 60 * 1000
  const RECOVERY_EMPTY_CONFIRMATIONS = 3

  function stopStreamRecovery(sessionId: string): void {
    if (sessionRecoveryTimers.value[sessionId]) {
      clearTimeout(sessionRecoveryTimers.value[sessionId]!)
      sessionRecoveryTimers.value = { ...sessionRecoveryTimers.value, [sessionId]: null }
    }
  }

  function clearStreamingState(sessionId: string): void {
    const state = sessionStates.value[sessionId]
    if (!state) return
    state.streaming = false
    state.taskProgress = null
    state.streamContent = ''
    state.streamBuffer = ''
    state.toolCalls = []
    state.streamingSegments = []
    state.lastStreamSegEnd = 0
  }

  // 回合确实中断且服务端无完整结果时的兜底：固化已收到的部分内容。
  function finalizeInterruptedStream(sessionId: string): void {
    const state = sessionStates.value[sessionId]
    if (!state) return
    stopStreamRecovery(sessionId)
    const partialContent = state.streamContent
    const hadPartial = !!partialContent || state.toolCalls.length > 0
    pushTextSegmentIfNeeded(sessionId)
    const partialToolCalls = [...state.toolCalls]
    const partialTimeline = [...state.streamingSegments]
    clearStreamingState(sessionId)
    if (hadPartial) {
      state.messages.push({
        id: Date.now().toString(),
        role: 'assistant' as const,
        content: partialContent + '\n\n*[Connection interrupted, partial response saved]*',
        timestamp: new Date().toISOString(),
        session_id: sessionId,
        tool_calls_snapshot: partialToolCalls as unknown[],
        streaming_timeline_snapshot: partialTimeline as unknown[],
      })
      loadSessions()
    } else if (!state.messages.some(m => m.role === 'assistant' && m.session_id === sessionId)) {
      error.value = { message: 'Connection lost' }
    }
  }

  function startStreamRecovery(sessionId: string): void {
    const state = sessionStates.value[sessionId]
    if (!state || !state.streaming) return
    if (sessionRecoveryTimers.value[sessionId]) return

    const startedAt = Date.now()
    let emptyFinishes = 0

    const tick = async (): Promise<void> => {
      sessionRecoveryTimers.value = { ...sessionRecoveryTimers.value, [sessionId]: null }
      const st = sessionStates.value[sessionId]
      if (!st || !st.streaming) return // 用户已点停止 / 新回合已开始
      if (sessionEventSources.value[sessionId]) return // 流已重连

      try {
        const { running } = await sessionsApi.getSessionRunning(sessionId)
        if (!running) {
          const res = await sessionsApi.getSession(sessionId)
          const serverMsgs = res.messages || []
          const last = serverMsgs[serverMsgs.length - 1]
          if (last && last.role === 'assistant') {
            // 回合已在服务端完成并落库：用服务端最终消息替换内存态
            st.messages = serverMsgs
            clearStreamingState(sessionId)
            loadSessions()
            return
          }
          // running=false 但没有 assistant 落库（回合被取消/无输出）：
          // 连续确认几次后按中断兜底，避免无限等待
          emptyFinishes++
          if (emptyFinishes >= RECOVERY_EMPTY_CONFIRMATIONS) {
            finalizeInterruptedStream(sessionId)
            return
          }
        } else {
          emptyFinishes = 0
        }
      } catch {
        // 网络暂不可达（弱网/离线），继续重试
      }

      if (Date.now() - startedAt > RECOVERY_TIMEOUT) {
        finalizeInterruptedStream(sessionId)
        return
      }
      sessionRecoveryTimers.value = {
        ...sessionRecoveryTimers.value,
        [sessionId]: setTimeout(tick, RECOVERY_POLL_INTERVAL),
      }
    }

    sessionRecoveryTimers.value = {
      ...sessionRecoveryTimers.value,
      [sessionId]: setTimeout(tick, RECOVERY_POLL_INTERVAL),
    }
  }

  async function sendMessage(content: string, images?: string[], files?: sessionsApi.UploadedFile[], imageUrls?: string[], imageNames?: string[], attachments?: sessionsApi.UploadedFile[]): Promise<void> {
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
      // 气泡里展示的附件清单：必须包含图片。图片走 images 多模态通道，不在
      // files 里；只挂 files 的话这条消息一旦从服务端重载，图片就只剩 [文件]。
      // attachments 是发送前的完整附件列表，仅用于本地渲染，不上行。
      files: attachments && attachments.length ? attachments : files,
    })

    state.streaming = true
    state.streamContent = ''
    state.streamBuffer = ''
    state.toolCalls = []
    state.streamingSegments = []
    state.lastStreamSegEnd = 0
    state.taskProgress = null
    // 新一轮对话开始时清空上一轮的审批卡片（此时 streaming=false 已保证无 pending 项）
    state.pendingApprovals = []
    state.pendingClarifications = []
    error.value = null

    // 取消上一轮流断线后的恢复轮询，避免其与新一轮流互相干扰
    stopStreamRecovery(sessionId)
    if (sessionFlushTimers.value[sessionId]) {
      clearTimeout(sessionFlushTimers.value[sessionId]!)
      sessionFlushTimers.value = { ...sessionFlushTimers.value, [sessionId]: null }
    }
    if (sessionEventSources.value[sessionId]) {
      sessionEventSources.value[sessionId]!.close()
      sessionEventSources.value = { ...sessionEventSources.value, [sessionId]: null }
    }

    try {
      const eventSource = sessionsApi.streamChat(sessionId, content, images, files, imageUrls, imageNames)
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
            pushTextSegmentIfNeeded(sessionId)
            const finalToolCalls = [...state.toolCalls]
            const finalTimeline = [...state.streamingSegments]
            state.messages.push({
              id: Date.now().toString(),
              role: 'assistant' as const,
              content: state.streamContent,
              timestamp: new Date().toISOString(),
              session_id: sessionId,
              tool_calls_snapshot: finalToolCalls as unknown[],
              streaming_timeline_snapshot: finalTimeline as unknown[],
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
            const argsVal = data.args ?? ''
            const argsText = typeof argsVal === 'string' ? argsVal : JSON.stringify(argsVal)
            state.toolCalls.push({
              id,
              name: data.name,
              args: argsText,
              args_text: data.args_text || argsText,
              status: 'running',
              file_ops: data.file_ops || [],
            })
            pushToolSegment(sessionId, id)
            return
          }

          if (data.type === 'tool_result') {
            const tc = state.toolCalls.find(t => t.name === data.name && t.status === 'running')
            const fileOpsFromResult = data.file_ops || []
            if (tc) {
              tc.success = data.success
              tc.duration = data.duration
              tc.content = data.content
              tc.status = data.success ? 'completed' : 'error'
              // 合并 tool_start 的 file_ops 和 tool_result 的 file_ops
              const mergedOps = [...(tc.file_ops || []), ...fileOpsFromResult]
              const seen = new Set<string>()
              tc.file_ops = mergedOps.filter(op => {
                const k = `${op.action}|${op.path}`
                if (seen.has(k)) return false
                seen.add(k)
                return true
              })
              tc.todo_changed = !!data.todo_changed
            } else {
              state.toolCalls.push({
                id: `tc_${++toolCallIdCounter}`,
                name: data.name,
                args: '',
                success: data.success,
                duration: data.duration,
                content: data.content,
                status: data.success ? 'completed' : 'error',
                file_ops: fileOpsFromResult,
                todo_changed: !!data.todo_changed,
              })
            }
            if (data.todo_changed) {
              emitTodoChanged()
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

          // 澄清请求：AI 需求不明确时调用 clarify 工具，后端推送本事件，
          // 前端渲染澄清卡片（选项按钮 + 追加说明），用户答复后回合恢复。
          if (data.type === 'clarify_required') {
            const card: PendingClarificationCard = {
              id: data.id || '',
              question: data.question || '',
              options: Array.isArray(data.options) ? data.options : [],
              context: data.context || '',
              multiSelect: !!data.multi_select,
              header: data.header || '',
              createdAt: data.created_at || Math.floor(Date.now() / 1000),
              expiresAt: data.expires_at || 0,
              status: 'pending',
            }
            if (!card.id) return
            const exists = state.pendingClarifications.some(c => c.id === card.id)
            if (!exists) {
              state.pendingClarifications.push(card)
            }
            return
          }

          if (data.error) {
            state.streaming = false
            state.taskProgress = null

            if (sessionFlushTimers.value[sessionId]) {
              clearTimeout(sessionFlushTimers.value[sessionId]!)
            }
            flushStreamBuffer(sessionId)
            if (sessionEventSources.value[sessionId]) {
              sessionEventSources.value[sessionId]!.close()
              sessionEventSources.value = { ...sessionEventSources.value, [sessionId]: null }
            }

            // fix: 错误发生时不能丢弃已流式显示的内容。与 onerror 分支一致，
            // 把出错前已生成的文本/工具调用/时间线固化为一条 assistant 消息，
            // 否则用户会看到"已执行的对话凭空消失"。
            pushTextSegmentIfNeeded(sessionId)
            const errToolCalls = [...state.toolCalls]
            const errTimeline = [...state.streamingSegments]
            const errContent = state.streamContent
            const executedSomething = !!errContent || errToolCalls.length > 0

            nextTick(() => {
              // 后端在无文本输出时会落库 partial（已执行工具摘要），前端刷新后可见；
              // 这里同步固化当前内存中的内容，保证不刷新也不丢失。
              if (executedSomething) {
                let msgContent = errContent || ''
                if (!msgContent && errToolCalls.length > 0) {
                  const steps = errToolCalls.map(tc => {
                    const mark = tc.status === 'error' ? '✗' : '✓'
                    return `- ${mark} ${tc.name}`
                  }).join('\n')
                  msgContent = `⚠️ 对话在此轮执行中途出错（${data.error}），以下为出错前已完成的操作：\n\n${steps}`
                } else {
                  msgContent += `\n\n⚠️ 对话在此轮执行中途出错（${data.error}）`
                }
                state.messages.push({
                  id: Date.now().toString(),
                  role: 'assistant' as const,
                  content: msgContent,
                  timestamp: new Date().toISOString(),
                  session_id: sessionId,
                  tool_calls_snapshot: errToolCalls as unknown[],
                  streaming_timeline_snapshot: errTimeline as unknown[],
                })
              }
              state.streamContent = ''
              state.streamBuffer = ''
              state.toolCalls = []
              state.streamingSegments = []
              state.lastStreamSegEnd = 0
              loadSessions()
            })

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
            pushTextSegmentIfNeeded(sessionId)
            const finalToolCalls = [...state.toolCalls]
            const finalTimeline = [...state.streamingSegments]
            // 后端 done 事件携带本轮"变更的文件"（快照 + diff），直接写入消息：
            // 无需等刷新，内存态消息的 file_ops 即为最终列表。
            const finalFileOps = (data.file_ops as unknown as sessionsApi.FileOp[] | undefined) || undefined
            nextTick(() => {
              state.messages.push({
                id: Date.now().toString(),
                role: 'assistant' as const,
                content: finalContent,
                timestamp: new Date().toISOString(),
                session_id: sessionId,
                file_ops: finalFileOps,
                tool_calls_snapshot: finalToolCalls as unknown[],
                streaming_timeline_snapshot: finalTimeline as unknown[],
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

        // 移动端切后台/锁屏会杀掉连接，但服务端回合与连接已解耦、
        // 会继续执行并落库。不再立即判死，启动恢复轮询；
        // 恢复失败或超时才按中断固化（finalizeInterruptedStream）。
        startStreamRecovery(sessionId)
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

    // 回合已与连接解耦：abort 本地流不再能取消服务端执行，
    // 必须显式调用取消端点（best-effort，失败不阻塞本地清理）
    sessionsApi.cancelGeneration(sessionId).catch(() => {})
    stopStreamRecovery(sessionId)

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

  // 澄清卡片倒计时结束（等待超时）：AI 已自行继续/收尾，标记终态并移除。
  function markClarifyExpired(sessionId: string, clarifyId: string): void {
    const state = sessionStates.value[sessionId]
    if (!state) return
    const card = state.pendingClarifications.find(c => c.id === clarifyId)
    if (!card || card.status !== 'pending') return
    card.status = 'expired'
    setTimeout(() => {
      const st = sessionStates.value[sessionId]
      if (!st) return
      const idx = st.pendingClarifications.findIndex(c => c.id === clarifyId)
      if (idx >= 0) {
        st.pendingClarifications.splice(idx, 1)
      }
    }, 2000)
  }

  // 从后端恢复待答复澄清（页面刷新 / SSE 断连后 fallback），按 id 去重。
  async function restorePendingClarifies(sessionId: string): Promise<void> {
    const state = sessionStates.value[sessionId]
    if (!state) return
    if (state.pendingClarifications.some(c => c.status === 'pending')) return
    try {
      const items = await clarifyApi.getPendingClarifies(sessionId)
      const now = Math.floor(Date.now() / 1000)
      for (const p of items) {
        const exists = state.pendingClarifications.some(c => c.id === p.id)
        if (exists) continue
        state.pendingClarifications.push({
          id: p.id,
          question: p.question,
          options: Array.isArray(p.options) ? p.options : [],
          context: p.context || '',
          multiSelect: !!p.multi_select,
          header: p.header || '',
          createdAt: p.created_at ? Math.floor(new Date(p.created_at).getTime() / 1000) : now,
          expiresAt: p.expires_at ? Math.floor(new Date(p.expires_at).getTime() / 1000) : 0,
          status: 'pending',
        })
      }
    } catch {
      // 静默失败，不影响会话加载
    }
  }

  // 提交澄清答复：choices 为所选选项（单选 1 项/多选多项），note 为追加说明。
  // 答复成功后后端唤醒挂起的 clarify 工具，AI 在本回合内继续原任务。
  async function answerChatClarify(
    sessionId: string,
    clarifyId: string,
    choices: string[],
    note: string = '',
  ): Promise<void> {
    const state = sessionStates.value[sessionId]
    if (!state) return
    const card = state.pendingClarifications.find(c => c.id === clarifyId)
    if (!card) return
    if (card.status !== 'pending') return

    card.status = 'answering'
    try {
      await clarifyApi.answerClarify(clarifyId, { choices, note })
      card.status = 'answered'
      card.answerSummary = [choices.join(', '), note].filter(Boolean).join(' · ')
      // 短暂展示终态后自动移除
      setTimeout(() => {
        const st = sessionStates.value[sessionId]
        if (!st) return
        const idx = st.pendingClarifications.findIndex(c => c.id === clarifyId)
        if (idx >= 0) {
          st.pendingClarifications.splice(idx, 1)
        }
      }, 1500)
    } catch (e) {
      card.status = 'pending'
      const errMsg = e instanceof Error ? e.message : String(e)
      error.value = { message: `${$t('chat.clarifyAnswerFailed')}: ${errMsg}` }
      throw e
    }
  }

  // 关闭澄清卡片（用户点 ✕）：通知服务端取消等待，成功后本地移除卡片。
  // 挂起的 clarify 工具会收到"用户已关闭"错误，模型自行决定继续或收尾。
  async function dismissChatClarify(sessionId: string, clarifyId: string): Promise<void> {
    const state = sessionStates.value[sessionId]
    if (!state) return
    const card = state.pendingClarifications.find(c => c.id === clarifyId)
    if (!card || card.status !== 'pending') return
    try {
      await clarifyApi.dismissClarify(clarifyId)
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : String(e)
      error.value = { message: `${$t('chat.clarifyDismissFailed')}: ${errMsg}` }
      throw e
    }
    const st = sessionStates.value[sessionId]
    if (!st) return
    const idx = st.pendingClarifications.findIndex(c => c.id === clarifyId)
    if (idx >= 0) {
      st.pendingClarifications.splice(idx, 1)
    }
  }

  function cleanup(): void {
    for (const sessionId of Object.keys(sessionStates.value)) {
      if (sessionFlushTimers.value[sessionId]) {
        clearTimeout(sessionFlushTimers.value[sessionId]!)
      }
      stopStreamRecovery(sessionId)
      if (sessionEventSources.value[sessionId]) {
        sessionEventSources.value[sessionId]!.close()
      }
    }
    sessionStates.value = {}
    sessionEventSources.value = {}
    sessionFlushTimers.value = {}
    sessionRecoveryTimers.value = {}
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
    streamingSegments,
    activeToolCalls,
    taskProgress,
    isLongTask,
    pendingApprovals,
    activePendingApprovals,
    pendingClarifications,
    activePendingClarifications,
    sessionsLoading,
    sessionsHasMore,
    loadSessions,
    loadMoreSessions,
    mergeSessions,
    createSession,
    selectSession,
    deleteSession,
    renameSession,
    updateSessionWorkDir,
    isSessionRunning,
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
    answerChatClarify,
    dismissChatClarify,
    markClarifyExpired,
    restorePendingClarifies,
    onTodoChange,
    emitTodoChanged,
  }
})