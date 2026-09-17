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

// 排队中的用户消息（本地镜像）。服务端队列才是权威，这里只负责渲染：
// status='queued' 表示等待执行，'running' 表示已被服务端认领正在执行。
// turnId 是服务端分配的队列项 ID，用于把 queued / stream_started / done
// 三类事件关联到同一条消息。
export interface QueuedMessage {
  turnId: string
  content: string
  status: 'queued' | 'running'
  createdAt: number
  /** 服务端给出的排队位置，1 起；0 表示尚未确认 */
  position: number
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
  // 服务端队列状态。用户在一个回合进行中继续发消息不再被丢弃：消息进入
  // 服务端 per-session 串行队列，依次执行。queued 是本地镜像（用于渲染
  // 排队气泡），queueDepth 用于状态提示，activeTurnId 标识"当前正在执行
  // 的是哪一条"（空串表示这条是本地发起但尚未被服务端认领）。
  queued: QueuedMessage[]
  activeTurnId: string
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
  // 排队消息的本地附件清单（key = 排队项 id）。仅用于排队气泡上的缩略图
  // 展示——服务端落库时也会带上附件引用，刷新后由会话消息重建。
  const queuedAttachments = new Map<string, sessionsApi.UploadedFile[]>()
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

  // 当前会话排队等待执行的消息（不含正在执行的）。
  const queuedMessages = computed((): QueuedMessage[] => {
    const state = activeSessionState.value
    return state?.queued || []
  })

  // 排队数量，供状态栏提示与"停止并清空"确认文案使用。
  const queueDepth = computed(() => queuedMessages.value.length)

  // 是否有回合在跑或有消息排队：决定输入框能否发送。
  // 与改造前不同，现在"回合进行中"不再禁止发送（消息会排队），
  // 因此这里只用于提示，不再作为发送按钮的禁用依据。
  const busy = computed(() => streaming.value || queueDepth.value > 0)

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
        queued: [],
        activeTurnId: '',
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

  // 返回是否成功：调用方需要据此给用户反馈（曾经无条件 catch + console.error，
  // 界面上既不报错也不提示，用户无法判断"到底删没删"）。
  async function deleteSession(id: string, deleteFiles: boolean = false): Promise<boolean> {
    let ok = true
    try {
      await sessionsApi.deleteSession(id, deleteFiles)
    } catch (e) {
      ok = false
      console.error('Failed to delete session:', e)
    }
    // 请求失败也照常做本地清理：会话可能早已不存在（重复删除/别的标签页删过），
    // 让条目继续挂在侧栏比"看起来删不掉"更糟。
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
    return ok
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
    state.activeTurnId = ''
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
        const running = await sessionsApi.getSessionRunning(sessionId)
        // 服务端队列是权威：把本地排队镜像对齐过去。这样刷新页面、或在
        // 另一台设备上操作后，本端也能看到"还有几条在等"。
        syncQueuedFromServer(sessionId, running.queued, running.active_id)

        if (!running.running && running.queue_depth === 0) {
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

  // 本地待确认的排队占位：服务端 queued 事件到达前的临时状态，
  // 用 localId 关联，收到 queued 事件后替换成带 turnId 的正式项。
  let localQueuedCounter = 0

  // sendMessage 提交一条用户消息。
  //
  // 与改造前最重要的差别：不再因为"当前回合正在跑"而拒绝发送。消息交给
  // 服务端 per-session 队列，同一个会话永远只有一个回合在跑，后续消息排队
  // 依次执行——因此这里既不再有 `if (streaming) return` 的静默丢弃，也不需要
  // 在回合进行中强行拆掉现有 SSE 连接。
  //
  // 三种情形：
  //   1. 空闲（无流、无排队）→ 开新流提交，进入流式渲染；
  //   2. 有回合在跑 → 复用现有连接提交（服务端回 queued 事件），
  //      前端只追加一条"排队中"气泡；
  //   3. 客户端处于断线恢复轮询 → 停止轮询并重新建立连接后提交。
  // syncQueuedFromServer 用服务端队列快照对齐本地排队镜像。
  //
  // 两个方向都要处理：
  //   - 服务端有、本地没有（刷新页面 / 另一台设备提交 / 本地占位丢失）→ 补上；
  //   - 本地有、服务端没有（已被执行 / 被取消丢弃）→ 移除。
  // 正在执行的那一条由 active_id 标识，会在服务端 snapshot 的 items 之外，
  // 因此不能出现在排队列表里（它已被转入流式渲染）。
  function syncQueuedFromServer(sessionId: string, serverQueued: sessionsApi.QueuedTurnInfo[], activeId: string): void {
    const state = sessionStates.value[sessionId]
    if (!state) return

    const serverIds = new Set(serverQueued.map(q => q.id))

    // 1) 移除服务端已不存在的本地项（本地占位 local_* 除外：它还没被认领，
    //    服务端可能尚未处理到；但若队列已空且没有回合在跑，也要清掉）。
    state.queued = state.queued.filter(q => {
      if (q.turnId.startsWith('local_')) {
        // 本地占位：服务端队列为空且无活跃回合 → 说明提交失败或已执行完
        return !(serverQueued.length === 0 && !activeId)
      }
      if (serverIds.has(q.turnId)) return true
      queuedAttachments.delete(q.turnId)
      return false
    })

    // 2) 补齐服务端有而本地没有的项
    for (const sq of serverQueued) {
      if (sq.id === activeId) continue
      const existing = state.queued.find(q => q.turnId === sq.id)
      if (existing) {
        existing.position = sq.position
        continue
      }
      state.queued.push({
        turnId: sq.id,
        content: sq.content,
        status: 'queued',
        createdAt: sq.created_at * 1000 || Date.now(),
        position: sq.position,
      })
    }

    // 3) 按服务端顺序重排，保证"下一条执行的"排在最前
    const order = new Map(serverQueued.map(q => [q.id, q.position]))
    state.queued.sort((a, b) => {
      const pa = order.get(a.turnId) ?? Number.MAX_SAFE_INTEGER
      const pb = order.get(b.turnId) ?? Number.MAX_SAFE_INTEGER
      return pa - pb
    })

    if (activeId) state.activeTurnId = activeId
  }

  async function sendMessage(content: string, images?: string[], files?: sessionsApi.UploadedFile[], imageUrls?: string[], imageNames?: string[], attachments?: sessionsApi.UploadedFile[]): Promise<void> {
    if (!activeSessionId.value) {
      const session = await createSession()
      if (!session) return
    }

    const sessionId = activeSessionId.value!
    const state = getOrCreateSessionState(sessionId)

    // 本地乐观插入排队项，立刻可见（避免"点了发送但界面没反应"）。
    // turnId 先占位（local_ 前缀），服务端 queued 事件回来后替换成真实 ID。
    const localId = `local_${++localQueuedCounter}`
    state.queued.push({
      turnId: localId,
      content,
      status: 'queued',
      createdAt: Date.now(),
      position: 0,
    })
    // 附件跟随排队项展示，保证本地不丢缩略图（服务端落库时同样带上引用）。
    const pendingFiles = attachments && attachments.length ? attachments : files
    if (pendingFiles?.length) queuedAttachments.set(localId, pendingFiles)

    if (!state.streaming) {
      // 空闲提交：清空上一轮流式残留，让本回合从干净状态开始。
      state.streamContent = ''
      state.streamBuffer = ''
      state.toolCalls = []
      state.streamingSegments = []
      state.lastStreamSegEnd = 0
      state.taskProgress = null
      state.pendingApprovals = []
      state.pendingClarifications = []
    }
    error.value = null

    // 新请求马上会建立/复用连接，断线恢复轮询必须停掉，否则两者互相干扰
    stopStreamRecovery(sessionId)
    if (sessionFlushTimers.value[sessionId]) {
      clearTimeout(sessionFlushTimers.value[sessionId]!)
      sessionFlushTimers.value = { ...sessionFlushTimers.value, [sessionId]: null }
    }

    // 已经不 streaming 却还挂着旧连接（上一轮结束后未关闭 / 附着遗留）：
    // 关掉它并重新建流，避免在陈旧连接上等待。
    if (!state.streaming && sessionEventSources.value[sessionId]) {
      sessionEventSources.value[sessionId]!.close()
      sessionEventSources.value = { ...sessionEventSources.value, [sessionId]: null }
    }

    try {
      if (sessionEventSources.value[sessionId]) {
        // 回合进行中：复用现有连接入队。服务端会在这条流上回 queued 事件，
        // 当前回合的渲染因此不会被打断。
        await sessionsApi.submitMessage(sessionId, content, images, files, imageUrls, imageNames)
      } else {
        const eventSource = sessionsApi.streamChat(sessionId, content, images, files, imageUrls, imageNames)
        sessionEventSources.value = { ...sessionEventSources.value, [sessionId]: eventSource }
        attachStreamHandlers(sessionId, eventSource, localId)
      }
    } catch (e) {
      // 提交失败：撤掉本地占位，避免留下"永远排队中"的幽灵气泡
      state.queued = state.queued.filter(q => q.turnId !== localId)
      queuedAttachments.delete(localId)
      const errMsg = e instanceof Error ? e.message : 'Unknown error'
      if (errMsg.includes('aborted') || errMsg.includes('abort')) {
        return
      }
      error.value = { message: 'Failed to send message: ' + errMsg }
    }
  }

  // attachStreamHandlers 把 SSE 事件处理逻辑挂到一条流上。抽成独立函数是因为
  // 现在有多条入口需要挂同一套处理（新提交、续接已有回合、断线重连）。
  function attachStreamHandlers(sessionId: string, eventSource: sessionsApi.ChatStream, localId?: string): void {
    const state = getOrCreateSessionState(sessionId)
    let myLocalId = localId || ''

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

          // queued：服务端已把消息放进队列，回传队列项 ID 与位置。
          // 用服务端 ID 替换本地占位，后续 stream_started/done 才能对齐。
          if (data.type === 'queued') {
            const turnId = String(data.id || '')
            const item = myLocalId
              ? state.queued.find(q => q.turnId === myLocalId)
              : undefined
            if (item && turnId) {
              // 附着模式下服务端可能重发已存在的排队项：同 ID 不重复插入
              const clash = state.queued.some(q => q.turnId === turnId)
              item.turnId = turnId
              item.position = data.position || 0
              if (myLocalId) {
                const att = queuedAttachments.get(myLocalId)
                if (att) {
                  queuedAttachments.delete(myLocalId)
                  queuedAttachments.set(turnId, att)
                }
              }
              if (clash) {
                state.queued = state.queued.filter(q => q !== item)
              }
              myLocalId = turnId
            } else if (turnId && !state.queued.some(q => q.turnId === turnId)) {
              // 恢复场景：本地没有对应占位（例如刷新页面后重新附着），
              // 按服务端快照补一条排队气泡，保证用户看得见"还有几条在等"。
              state.queued.push({
                turnId,
                content: data.content || '',
                status: 'queued',
                createdAt: (data.created_at || 0) * 1000 || Date.now(),
                position: data.position || 0,
              })
            }
            return
          }

          // stream_started：某个回合开始执行。started=false 是附着连接的
          // "此刻没有回合在跑"应答（不是错误，也不结束连接）。
          if (data.type === 'stream_started') {
            if (data.started === false) {
              return
            }
            const turnId = String(data.id || '')
            // 本条流对应的排队项转入"执行中"；其余项的排队位置前移。
            const mine = turnId
              ? state.queued.find(q => q.turnId === turnId)
              : state.queued.find(q => q.status === 'queued')
            if (mine) {
              mine.status = 'running'
              state.activeTurnId = mine.turnId
              state.queued = state.queued.filter(q => q !== mine)
              // 排队项转入流式渲染：把它从"排队气泡"移出，内容由 delta 重建。
              startStreamingFromQueued(sessionId, mine)
            } else {
              state.activeTurnId = turnId
              state.streaming = true
            }
            return
          }

          // queue_changed：别的连接（另一台设备 / 另一个标签页）增删改了
          // 队列。用服务端快照对齐本地排队镜像，保证多端一致。
          if (data.type === 'queue_changed') {
            syncQueuedFromServer(sessionId, data.queued || [], String(data.active_id || ''))
            return
          }

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

          // 澄清已终结（回合被取消/等待超时/用户在其他端关闭）：后端已删除
          // pending，前端若不移除卡片，答复/关闭均会 404，变成永久死卡片。
          if (data.type === 'clarify_closed') {
            if (data.id) {
              markClarifyExpired(sessionId, data.id)
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

            // 先恢复按钮状态，让用户可以立即操作。注意：排队队列非空时
            // 连接必须保留——服务端 worker 会立刻开始执行下一条排队消息并
            // 在同一条流上继续推 stream_started/delta，拆掉连接就看不到它。
            const hasMoreQueued = state.queued.length > 0
            state.streaming = false
            state.taskProgress = null
            state.activeTurnId = ''

            if (!hasMoreQueued && sessionEventSources.value[sessionId]) {
              sessionEventSources.value[sessionId]!.close()
              sessionEventSources.value = { ...sessionEventSources.value, [sessionId]: null }
            }

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
              // 清空流式残留，为下一条排队消息腾出干净的渲染状态
              state.streamContent = ''
              state.streamBuffer = ''
              state.toolCalls = []
              state.streamingSegments = []
              state.lastStreamSegEnd = 0
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
  }

  // startStreamingFromQueued 把一条排队消息切换为流式渲染状态：
  // 清空上一轮的流式残留，让 delta 从零开始累积。
  function startStreamingFromQueued(sessionId: string, item: QueuedMessage): void {
    const state = sessionStates.value[sessionId]
    if (!state) return
    state.streaming = true
    state.streamContent = ''
    state.streamBuffer = ''
    state.toolCalls = []
    state.streamingSegments = []
    state.lastStreamSegEnd = 0
    state.taskProgress = null
    state.pendingApprovals = []
    state.pendingClarifications = []
    // 排队气泡转为流式渲染：把它从队列里移除（内容会由 delta 重建），
    // 但保留 activeTurnId 关联，done 时才能对应上。
    state.activeTurnId = item.turnId
  }

  // removeQueuedMessage 删除一条尚未执行的排队消息（本地 + 服务端）。
  //
  // 删除是"用户点名这一条"，与停止（清空全部 + 杀当前回合）不同：这里
  // 只动这一条，正在执行的回合继续跑。服务端返回 removed=false 表示该条
  // 已经被 worker 认领（来不及删），此时同样从本地队列移除——它已经转入
  // 流式渲染，留着排队气泡就是重复显示。
  async function removeQueuedMessage(sessionId: string, turnId: string): Promise<boolean> {
    const state = sessionStates.value[sessionId]
    if (!state) return false

    // 本地占位（服务端还没认领）不需要请求：直接从本地删除即可，
    // 但为避免"幽灵提交"，仍调用服务端接口做 best-effort 清理。
    const isLocal = turnId.startsWith('local_')
    try {
      if (!isLocal) await sessionsApi.removeQueuedTurn(sessionId, turnId)
    } catch {
      // 网络失败不阻塞本地删除：服务端队列会在下次 /running 对账时收敛
    }

    state.queued = state.queued.filter(q => q.turnId !== turnId)
    queuedAttachments.delete(turnId)
    return true
  }

  // editQueuedMessage 把一条排队消息的内容从队列撤下并返回，供输入框回填。
  //
  // 语义是"改完再发"：撤下这一条（服务端 + 本地），把内容交给调用方填进
  // 输入框，用户改完点发送就是一条全新的排队消息。若服务端已开始执行
  // （started=true），撤回失败并返回 null，调用方应提示用户改用停止。
  async function editQueuedMessage(sessionId: string, turnId: string): Promise<{ content: string; attachments: sessionsApi.UploadedFile[] } | null> {
    const state = sessionStates.value[sessionId]
    if (!state) return null
    const item = state.queued.find(q => q.turnId === turnId)
    if (!item) return null

    const content = item.content
    const attachments = queuedAttachments.get(turnId) || []

    if (!turnId.startsWith('local_')) {
      try {
        const res = await sessionsApi.removeQueuedTurn(sessionId, turnId)
        if (!res.removed) {
          // 已经被认领开始执行：不能编辑。本地也把它移出排队（已转流式渲染）。
          state.queued = state.queued.filter(q => q.turnId !== turnId)
          queuedAttachments.delete(turnId)
          return null
        }
      } catch {
        // 网络失败：本地照常撤下，服务端会在下次 /running 对账时收敛
      }
    }

    state.queued = state.queued.filter(q => q.turnId !== turnId)
    queuedAttachments.delete(turnId)
    return { content, attachments }
  }

  function stopGeneration(): void {
    if (!activeSessionId.value) return

    const sessionId = activeSessionId.value
    const state = getOrCreateSessionState(sessionId)

    // 回合已与连接解耦：abort 本地流不再能取消服务端执行，
    // 必须显式调用取消端点（best-effort，失败不阻塞本地清理）。
    // 服务端语义是"停止这一切"：正在执行的回合被取消，排队消息一并丢弃。
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
    state.activeTurnId = ''
    // 清空排队消息：与服务端 cancelAll 保持一致，否则刷新后这些消息
    // 会从服务端队列里"复活"并开始执行，与用户的停止意图相反。
    for (const q of state.queued) {
      queuedAttachments.delete(q.turnId)
    }
    state.queued = []

    for (const tc of state.toolCalls) {
      if (tc.status === 'running') {
        tc.status = 'error'
        tc.success = false
      }
    }

    // 回合被用户终止：挂起的澄清/提问随回合一起取消（后端 pending 已删），
    // 标记过期移除，否则残留死卡片，答复/关闭均 404。
    for (const c of state.pendingClarifications) {
      if (c.status === 'pending') {
        markClarifyExpired(sessionId, c.id)
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
      const errMsg = e instanceof Error ? e.message : String(e)
      if (/HTTP 404|not found/i.test(errMsg)) {
        // 后端已不存在该澄清（回合被取消/等待超时）：按过期处理，静默移除
        // 卡片而不是报错卡死。
        card.status = 'pending'
        markClarifyExpired(sessionId, clarifyId)
        return
      }
      card.status = 'pending'
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
      if (/HTTP 404|not found/i.test(errMsg)) {
        // 后端已不存在该澄清（回合被取消/等待超时）：按过期处理，静默移除
        // 卡片而不是报错卡死。
        markClarifyExpired(sessionId, clarifyId)
        return
      }
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
    busy,
    queuedMessages,
    queueDepth,
    queuedAttachments,
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
    removeQueuedMessage,
    editQueuedMessage,
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