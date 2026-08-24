import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as todosApi from '@/api/todos'
import type { TodoItem, TodoListResponse } from '@/api/todos'
import { getAuthToken } from '@/api/client'
import { useChatStore } from './chat'

let globalEventSource: EventSource | null = null
let globalEventSourceRefCount = 0
let globalPollingTimer: ReturnType<typeof setInterval> | null = null
let globalPollingRefCount = 0
const GLOBAL_POLLING_INTERVAL = 30 * 1000

function resolveSessionId(explicit?: string): string | undefined {
  if (explicit) return explicit
  try {
    const chat = useChatStore()
    return chat.activeSessionId || undefined
  } catch {
    return undefined
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

function ensureGlobalPolling(onTick: () => void): void {
  globalPollingRefCount++
  if (globalPollingTimer) return
  globalPollingTimer = setInterval(() => onTick(), GLOBAL_POLLING_INTERVAL)
}

function releaseGlobalPolling(): void {
  globalPollingRefCount = Math.max(0, globalPollingRefCount - 1)
  if (globalPollingRefCount === 0 && globalPollingTimer) {
    clearInterval(globalPollingTimer)
    globalPollingTimer = null
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
    const statusRank: any = { pending: 0, in_progress: 0, completed: 2 }
    const priorityRank: any = { high: 0, medium: 1, low: 2 }
    list.sort((a, b) => {
      const s = (statusRank[a.status] ?? 99) - (statusRank[b.status] ?? 99)
      if (s !== 0) return s
      const p = (priorityRank[a.priority] ?? 99) - (priorityRank[b.priority] ?? 99)
      if (p !== 0) return p
      return (b.created_at || '').localeCompare(a.created_at || '')
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
      if (effectiveSessionId) {
        apiParams.session_id = effectiveSessionId
      }
      const data = (await todosApi.listTodos(apiParams)) as TodoListResponse
      todos.value = (data?.todos as TodoItem[]) ?? []
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
    ensureGlobalEventSource(() => {
      void loadTodos()
    })
    ensureGlobalPolling(() => {
      void loadTodos()
    })
  }

  function releaseLiveSubscription() {
    if (!liveSubscribed.value) return
    liveSubscribed.value = false
    releaseGlobalEventSource()
    releaseGlobalPolling()
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