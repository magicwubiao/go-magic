import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as todosApi from '@/api/todos'
import type { TodoItem, TodoListResponse } from '@/api/todos'
import { signFSTicket } from '@/api/sessions'
import { useChatStore } from './chat'

let globalEventSource: EventSource | null = null
let globalEventSourceRefCount = 0

// resolveSessionId 返回"本次 listTodos 应该过滤的 session_id 值"。
// 显式传的优先；否则取当前 chatStore.activeSessionId。
// 返回空串代表「仅看未归属任何会话的全局 todo」，不再是"不过滤"。
// 关键契约：loadTodos 调用 todosApi.listTodos 时必须 ALWAYS 带 session_id key（哪怕是 ""），
// 后端才会启用严格的"按值过滤"语义，避免首页空会话时把所有会话 todo 混在一起。
function resolveSessionId(explicit?: string): string {
  if (explicit !== undefined && explicit !== null) return explicit
  try {
    const chat = useChatStore()
    return chat.activeSessionId || ''
  } catch {
    return ''
  }
}

const EVENTS_RESIGN_DELAY_MS = 10_000

function ensureGlobalEventSource(onTodoUpdate: () => void): void {
  globalEventSourceRefCount++
  if (globalEventSource) return
  // 票据是异步换来的，所以这里不能直接 new EventSource。
  void openGlobalEventSource(onTodoUpdate)
}

// openGlobalEventSource 换一张 events 票据并建立 SSE 连接。
//
// EventSource 无法自定义请求头，凭据只能走 URL——这正是这条流过去唯一必须
// 依赖 ?token=<登录凭据> 的原因。现在换成一张 scope=events 的签名票据：它只
// 解锁这条只读事件流、对其它 API 无效，且带过期时间；换取票据用带
// Authorization 头的 fetch，登录凭据不再出现在 URL 里。
async function openGlobalEventSource(onTodoUpdate: () => void): Promise<void> {
  // 期间可能已被 release（或已被另一次 ensure 打开）
  if (globalEventSourceRefCount <= 0 || globalEventSource) return

  let url: string
  try {
    url = await signFSTicket({ scope: 'events' })
  } catch {
    // 换不到票据（离线、未登录、服务重启）：按与断线重连相同的节奏重试
    scheduleGlobalEventSourceReopen(onTodoUpdate)
    return
  }

  // await 期间状态可能已变：重新检查，避免留下一个无人引用的连接
  if (globalEventSourceRefCount <= 0 || globalEventSource) return

  const es = new EventSource(url)
  es.onmessage = (event: MessageEvent) => {
    try {
      const data = JSON.parse(event.data)
      if (data.type === 'todo_update') {
        onTodoUpdate()
      }
    } catch {
      // ignore malformed events
    }
  }
  es.onerror = () => {
    // 浏览器 EventSource 在网络抖动时会自动重连，但它复用**同一个 URL**：
    // 若票据已过期，重连只会一遍遍拿到 403，永远好不了。所以 CLOSED 时清掉
    // 实例、重新签一张票据再连，而不是原样重连那串旧票据。
    if (es.readyState === EventSource.CLOSED) {
      if (globalEventSource === es) {
        globalEventSource = null
      }
      scheduleGlobalEventSourceReopen(onTodoUpdate)
    }
  }
  globalEventSource = es
}

// scheduleGlobalEventSourceReopen 延迟重建连接。
//
// 刻意不调用 ensureGlobalEventSource：那会再自增一次引用计数，而这里并没有
// 新增订阅者。原实现正是在这条路径上反复自增，使计数只涨不跌——连接被关掉
// 之后计数还大于 0，后续 release 永远归不了零。
function scheduleGlobalEventSourceReopen(onTodoUpdate: () => void): void {
  setTimeout(() => {
    if (globalEventSource || globalEventSourceRefCount <= 0) return
    void openGlobalEventSource(onTodoUpdate)
  }, EVENTS_RESIGN_DELAY_MS)
}

function releaseGlobalEventSource(): void {
  globalEventSourceRefCount = Math.max(0, globalEventSourceRefCount - 1)
  if (globalEventSourceRefCount === 0 && globalEventSource) {
    globalEventSource.close()
    globalEventSource = null
  }
}

export const useTodosStore = defineStore('todos', () => {
  const todos = ref<TodoItem[]>([])
  const loading = ref(false)
  const responseMeta = ref<{ total: number; pending_count: number; in_progress_count: number; completed_count: number }>({
    total: 0,
    pending_count: 0,
    in_progress_count: 0,
    completed_count: 0,
  })
  const liveSubscribed = ref(false)

  const sortedTodos = computed(() => {
    const list = [...(todos.value ?? [])]
    // status 顺序：pending=0 / in_progress=0（未完成靠前），completed=2，cancelled=3
    const statusRank: Record<string, number> = {
      pending: 0,
      in_progress: 0,
      completed: 2,
      cancelled: 3,
    }
    const priorityRank: Record<string, number> = { high: 0, medium: 1, low: 2 }
    list.sort((a, b) => {
      const s = (statusRank[a.status] ?? 99) - (statusRank[b.status] ?? 99)
      if (s !== 0) return s
      const p = (priorityRank[a.priority] ?? 99) - (priorityRank[b.priority] ?? 99)
      if (p !== 0) return p
      // created_asc：旧的在前，新的在后，与后端默认排序一致（created_at 为 Unix 秒时间戳，数值升序）
      return (a.created_at || 0) - (b.created_at || 0)
    })
    return list
  })

  const activeTodos = computed(() => sortedTodos.value)
  const pendingTodos = computed(() => (todos.value ?? []).filter(t => t.status === 'pending'))
  const inProgressTodos = computed(() => (todos.value ?? []).filter(t => t.status === 'in_progress'))
  const completedTodos = computed(() => [] as TodoItem[])

  async function loadTodos(params?: {
    filter_status?: string
    filter_priority?: string
    sort?: string
    session_id?: string
  }) {
    loading.value = true
    try {
      const effectiveSessionId = resolveSessionId(params?.session_id)
      const apiParams = params ? { ...params } : {} as any
      // 必须 ALWAYS 带 session_id（空串=显式要全局 bucket），
      // 不能省略；否则后端会把它解释为"完全未传会话 key → 返回全量"，导致跨会话混。
      apiParams.session_id = effectiveSessionId
      const data = (await todosApi.listTodos(apiParams)) as TodoListResponse
      // 双路刷新（SSE / chatStore.onTodoChange）竞争时，按 id 去重，避免 UI 上出现重复卡片
      const seen = new Set<string>()
      const unique: TodoItem[] = []
      for (const t of (data?.todos as TodoItem[]) ?? []) {
        if (!t || !t.id || seen.has(t.id)) continue
        seen.add(t.id)
        unique.push(t)
      }
      todos.value = unique
      if (data) {
        responseMeta.value = {
          total: data.total ?? 0,
          pending_count: data.pending_count ?? 0,
          in_progress_count: data.in_progress_count ?? 0,
          completed_count: data.completed_count ?? 0,
        }
      }
    } catch (e) {
      todos.value = []
    } finally {
      loading.value = false
    }
  }

  function ensureLiveSubscription() {
    if (liveSubscribed.value) return
    liveSubscribed.value = true
    // 仅依赖 SSE 实时推送刷新，不做定时轮询
    ensureGlobalEventSource(() => {
      void loadTodos()
    })
  }

  function releaseLiveSubscription() {
    if (!liveSubscribed.value) return
    liveSubscribed.value = false
    releaseGlobalEventSource()
  }

  async function createTodo(data: { title: string; description?: string; priority?: string }) {
    const res = await todosApi.createTodo(data)
    await loadTodos()
    return res
  }

  async function updateTodo(id: string, updates: Partial<TodoItem>) {
    const res = await todosApi.updateTodo(id, updates)
    const idx = todos.value.findIndex(t => t.id === id)
    if (idx >= 0 && res?.todo) {
      todos.value[idx] = res.todo as TodoItem
    } else {
      await loadTodos()
    }
    return res
  }

  async function deleteTodo(id: string) {
    await todosApi.deleteTodo(id)
    todos.value = todos.value.filter(t => t.id !== id)
  }

  async function toggleComplete(todo: TodoItem) {
    if (todo.status === 'completed') {
      return updateTodo(todo.id, { status: 'pending' })
    } else {
      return updateTodo(todo.id, { status: 'completed' })
    }
  }

  return {
    todos,
    loading,
    responseMeta,
    sortedTodos,
    activeTodos,
    pendingTodos,
    inProgressTodos,
    completedTodos,
    liveSubscribed,
    loadTodos,
    ensureLiveSubscription,
    releaseLiveSubscription,
    createTodo,
    updateTodo,
    deleteTodo,
    toggleComplete,
  }
})