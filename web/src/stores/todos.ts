import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as todosApi from '@/api/todos'
import type { TodoItem, TodoListResponse } from '@/api/todos'
import { getAuthToken } from '@/api/client'
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

function ensureGlobalEventSource(onTodoUpdate: () => void): void {
  globalEventSourceRefCount++
  if (globalEventSource) return

  const token = getAuthToken()
  let url = '/api/events'
  if (token) {
    url += '?token=' + encodeURIComponent(token)
  }

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
    // 浏览器 EventSource 会自动在网络错误时重连，但如果是 401 等会停止
    // 这里兜底：如果 readyState === CLOSED，10s 后重建
    if (es.readyState === EventSource.CLOSED) {
      setTimeout(() => {
        if (globalEventSource === es) {
          globalEventSource = null
          globalEventSourceRefCount = Math.max(0, globalEventSourceRefCount)
          if (globalEventSourceRefCount > 0) ensureGlobalEventSource(onTodoUpdate)
        }
      }, 10000)
    }
  }
  globalEventSource = es
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