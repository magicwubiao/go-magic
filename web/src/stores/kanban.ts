import { defineStore } from 'pinia'
import { ref, computed } from 'vue'
import * as kanbanApi from '@/api/kanban'
import type { KanbanTask } from '@/api/kanban'
import { useI18n } from 'vue-i18n'

export const useKanbanStore = defineStore('kanban', () => {
  const tasks = ref<KanbanTask[]>([])
  const loading = ref(false)

  // Upper row: Triage / To Do / Ready
  const upperColumns = computed(() => [
    { key: 'triage', titleKey: 'kanban.statusOptions.triage', tasks: tasks.value.filter(t => t.status === 'triage') },
    { key: 'todo', titleKey: 'kanban.statusOptions.todo', tasks: tasks.value.filter(t => t.status === 'todo') },
    { key: 'ready', titleKey: 'kanban.statusOptions.ready', tasks: tasks.value.filter(t => t.status === 'ready') },
  ])

  // Lower row: Running / Blocked / Done
  const lowerColumns = computed(() => [
    { key: 'running', titleKey: 'kanban.statusOptions.running', tasks: tasks.value.filter(t => t.status === 'running') },
    { key: 'blocked', titleKey: 'kanban.statusOptions.blocked', tasks: tasks.value.filter(t => t.status === 'blocked') },
    { key: 'done', titleKey: 'kanban.statusOptions.done', tasks: tasks.value.filter(t => t.status === 'done' || t.status === 'archived') },
  ])

  async function loadBoard() {
    loading.value = true
    try {
      const board = await kanbanApi.getBoard()
      tasks.value = board.tasks || []
    } catch {
      tasks.value = []
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

  return { tasks, upperColumns, lowerColumns, loading, loadBoard, addTask, updateTask, moveTask, removeTask }
})
