<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>Kanban Board</h2>
      <n-button type="primary" @click="openAddTask">+ New Task</n-button>
    </n-space>

    <n-spin v-if="kanbanStore.loading" />
    <div v-else class="kanban-container">
      <!-- Normal Flow Columns -->
      <div class="kanban-board">
        <div v-for="col in kanbanStore.columns" :key="col.key" class="kanban-column">
          <div class="column-header">
            <n-text strong>{{ col.title }}</n-text>
            <n-tag size="small" round>{{ col.tasks.length }}</n-tag>
          </div>
          <div class="column-body">
            <n-card
              v-for="task in col.tasks"
              :key="task.id"
              size="small"
              hoverable
              style="margin-bottom: 8px; cursor: pointer;"
              @click="moveTaskForward(task)"
            >
              <n-text strong>{{ task.title }}</n-text>
              <br />
              <n-text depth="3" style="font-size: 12px;">{{ task.description?.slice(0, 80) }}</n-text>
              <template #action>
                <n-space>
                  <n-tag :type="priorityType(task.priority)" size="tiny">{{ task.priority }}</n-tag>
                  <n-button size="tiny" quaternary @click.stop="openEditTask(task)">Edit</n-button>
                  <n-button size="tiny" quaternary type="error" @click.stop="removeTask(task.id)">✕</n-button>
                </n-space>
              </template>
            </n-card>
          </div>
        </div>
      </div>

      <!-- Blocked Column (Separate) -->
      <div v-if="kanbanStore.blockedTasks.length > 0" class="blocked-section">
        <n-divider>Blocked</n-divider>
        <div class="kanban-column blocked">
          <div class="column-header">
            <n-text strong>Blocked</n-text>
            <n-tag size="small" round type="error">{{ kanbanStore.blockedTasks.length }}</n-tag>
          </div>
          <div class="column-body">
            <n-card
              v-for="task in kanbanStore.blockedTasks"
              :key="task.id"
              size="small"
              hoverable
              style="margin-bottom: 8px;"
            >
              <n-text strong>{{ task.title }}</n-text>
              <template #action>
                <n-space>
                  <n-tag :type="priorityType(task.priority)" size="tiny">{{ task.priority }}</n-tag>
                  <n-button size="tiny" type="primary" @click="unblockTask(task.id)">Unblock</n-button>
                  <n-button size="tiny" quaternary @click.stop="openEditTask(task)">Edit</n-button>
                  <n-button size="tiny" quaternary type="error" @click.stop="removeTask(task.id)">✕</n-button>
                </n-space>
              </template>
            </n-card>
          </div>
        </div>
      </div>
    </div>

    <!-- Add/Edit Task Modal -->
    <n-modal v-model:show="showTaskModal" :title="editingTask ? 'Edit Task' : 'New Task'">
      <n-card style="width: 450px;">
        <n-form>
          <n-form-item label="Title">
            <n-input v-model:value="taskForm.title" placeholder="Task title" />
          </n-form-item>
          <n-form-item label="Description">
            <n-input v-model:value="taskForm.description" type="textarea" :rows="4" />
          </n-form-item>
          <n-form-item label="Priority">
            <n-select v-model:value="taskForm.priority" :options="priorityOptions" />
          </n-form-item>
          <n-form-item label="Status" v-if="editingTask">
            <n-select v-model:value="taskForm.status" :options="statusOptions" />
          </n-form-item>
        </n-form>
        <template #footer>
          <n-space justify="end">
            <n-button @click="showTaskModal = false">Cancel</n-button>
            <n-button type="primary" @click="saveTask">{{ editingTask ? 'Save' : 'Create' }}</n-button>
          </n-space>
        </template>
      </n-card>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useKanbanStore } from '@/stores/kanban'
import type { KanbanTask } from '@/api/kanban'

const message = useMessage()
const kanbanStore = useKanbanStore()
const showTaskModal = ref(false)
const editingTask = ref<KanbanTask | null>(null)

const taskForm = reactive({
  title: '',
  description: '',
  priority: 'medium' as string,
  status: 'triage' as string,
})

const priorityOptions = [
  { label: 'Low', value: 'low' },
  { label: 'Medium', value: 'medium' },
  { label: 'High', value: 'high' },
  { label: 'Critical', value: 'critical' },
]

const statusOptions = [
  { label: 'Triage', value: 'triage' },
  { label: 'To Do', value: 'todo' },
  { label: 'Ready', value: 'ready' },
  { label: 'Running', value: 'running' },
  { label: 'Blocked', value: 'blocked' },
  { label: 'Done', value: 'done' },
  { label: 'Archived', value: 'archived' },
]

const priorityType = (p: string) => {
  const map: Record<string, string> = { low: 'default', medium: 'info', high: 'warning', critical: 'error' }
  return (map[p] || 'default') as any
}

// Normal flow: triage -> todo -> ready -> running -> done -> archived
const statusFlow = ['triage', 'todo', 'ready', 'running', 'done', 'archived']

function openAddTask() {
  editingTask.value = null
  taskForm.title = ''
  taskForm.description = ''
  taskForm.priority = 'medium'
  taskForm.status = 'triage'
  showTaskModal.value = true
}

function openEditTask(task: KanbanTask) {
  editingTask.value = task
  taskForm.title = task.title
  taskForm.description = task.description
  taskForm.priority = task.priority
  taskForm.status = task.status
  showTaskModal.value = true
}

async function saveTask() {
  if (!taskForm.title) {
    message.warning('Please enter a title')
    return
  }

  if (editingTask.value) {
    await kanbanStore.updateTask(editingTask.value.id, { ...taskForm } as Partial<KanbanTask>)
    message.success('Task updated')
  } else {
    await kanbanStore.addTask({ ...taskForm } as Partial<KanbanTask>)
    message.success('Task created')
  }
  showTaskModal.value = false
}

async function moveTaskForward(task: KanbanTask) {
  const currentIdx = statusFlow.indexOf(task.status)
  if (currentIdx < statusFlow.length - 1) {
    const nextStatus = statusFlow[currentIdx + 1]
    await kanbanStore.moveTask(task.id, nextStatus)
    message.success(`Moved to ${nextStatus}`)
  }
}

async function unblockTask(id: string) {
  await kanbanStore.moveTask(id, 'todo')
  message.success('Task unblocked and moved to To Do')
}

async function removeTask(id: string) {
  await kanbanStore.removeTask(id)
  message.success('Task deleted')
}

onMounted(() => kanbanStore.loadBoard())
</script>

<style scoped>
.kanban-container {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.kanban-board {
  display: flex;
  gap: 12px;
  overflow-x: auto;
  padding-bottom: 16px;
}

.kanban-column {
  min-width: 220px;
  max-width: 220px;
  background: #fafafa;
  border-radius: 8px;
  border: 1px solid #e8e8e8;
}

.kanban-column.blocked {
  border-color: #ff4d4f;
  background: #fff2f0;
}

.blocked-section {
  margin-top: 8px;
}

.column-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 12px;
  border-bottom: 1px solid #e8e8e8;
}

.column-body {
  padding: 8px;
  min-height: 100px;
}
</style>
