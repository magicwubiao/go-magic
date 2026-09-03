import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as kanbanApi from '@/api/kanban'
import type { KanbanTask } from '@/api/kanban'

export const useKanbanStore = defineStore('kanban', () => {
  const tasks = ref<KanbanTask[]>([])
  const loading = ref(false)
  const error = ref<string | null>(null)

  // Upper row: Triage / To Do / Ready
  const upperColumns = computed(() => [
    { key: 'triage', titleKey: 'kanban.statusOptions.triage', tasks: (tasks.value ?? []).filter(t => t.status === 'triage') },
    { key: 'todo', titleKey: 'kanban.statusOptions.todo', tasks: (tasks.value ?? []).filter(t => t.status === 'todo') },
    { key: 'ready', titleKey: 'kanban.statusOptions.ready', tasks: (tasks.value ?? []).filter(t => t.status === 'ready') },
  ])

  // Lower row: Running / Blocked / Done
  const lowerColumns = computed(() => [
    { key: 'running', titleKey: 'kanban.statusOptions.running', tasks: (tasks.value ?? []).filter(t => t.status === 'running') },
    { key: 'blocked', titleKey: 'kanban.statusOptions.blocked', tasks: (tasks.value ?? []).filter(t => t.status === 'blocked') },
    { key: 'done', titleKey: 'kanban.statusOptions.done', tasks: (tasks.value ?? []).filter(t => t.status === 'done' || t.status === 'archived') },
  ])

  // Stats computed from tasks
  const stats = computed(() => {
    const all = tasks.value ?? []
    return {
      total: all.length,
      completed: all.filter(t => t.status === 'done' || t.status === 'archived').length,
      in_progress: all.filter(t => t.status === 'running').length,
      pending: all.filter(t => ['triage', 'todo', 'ready'].includes(t.status)).length,
    }
  })

  async function loadBoard() {
    loading.value = true
    error.value = null
    try {
      const board = await kanbanApi.getBoard()
      tasks.value = board.tasks || []
    } catch (e) {
      tasks.value = []
      error.value = e instanceof Error ? e.message : 'Failed to load board'
    } finally {
      loading.value = false
    }
  }

  async function addTask(task: Partial<KanbanTask>) {
    const newTask = await kanbanApi.createTask(task)
    tasks.value.push(newTask)
    return newTask
  }

  async function updateTask(id: string, updates: Partial<KanbanTask>) {
    const updated = await kanbanApi.updateTask(id, updates)
    const idx = tasks.value.findIndex(t => t.id === id)
    if (idx >= 0) tasks.value[idx] = updated
    return updated
  }

  async function moveTask(id: string, status: string) {
    const updated = await kanbanApi.moveTask(id, status)
    const idx = tasks.value.findIndex(t => t.id === id)
    if (idx >= 0) tasks.value[idx] = updated
  }

  async function removeTask(id: string) {
    await kanbanApi.deleteTask(id)
    tasks.value = tasks.value.filter(t => t.id !== id)
  }

  async function splitTask(id: string) {
    try {
      const subtasks = await kanbanApi.splitTask(id)
      tasks.value.push(...subtasks)
      return subtasks
    } catch (e) {
      const errMsg = e instanceof Error ? e.message : 'Failed to split task'
      // Ignore abort errors
      if (errMsg.includes('aborted') || errMsg.includes('abort')) {
        return []
      }
      throw new Error(errMsg)
    }
  }

  return { tasks, upperColumns, lowerColumns, loading, error, stats, loadBoard, addTask, updateTask, moveTask, removeTask, splitTask }
})