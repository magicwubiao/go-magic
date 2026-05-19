<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>Kanban Board</h2>
      <n-button type="primary" @click="showAddTask = true">+ New Task</n-button>
    </n-space>

    <n-spin v-if="kanbanStore.loading" />
    <div v-else class="kanban-board">
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
            @click="moveTask(task)"
          >
            <n-text strong>{{ task.title }}</n-text>
            <br />
            <n-text depth="3" style="font-size: 12px;">{{ task.description?.slice(0, 80) }}</n-text>
            <template #action>
              <n-space>
                <n-tag :type="priorityType(task.priority)" size="tiny">{{ task.priority }}</n-tag>
                <n-button size="tiny" quaternary @click.stop="removeTask(task.id)">✕</n-button>
              </n-space>
            </template>
          </n-card>
        </div>
      </div>
    </div>

    <!-- Add Task Modal -->
    <n-modal v-model:show="showAddTask" title="New Task">
      <n-card style="width: 450px;">
        <n-form>
          <n-form-item label="Title">
            <n-input v-model:value="newTask.title" placeholder="Task title" />
          </n-form-item>
          <n-form-item label="Description">
            <n-input v-model:value="newTask.description" type="textarea" />
          </n-form-item>
          <n-form-item label="Priority">
            <n-select v-model:value="newTask.priority" :options="priorityOptions" />
          </n-form-item>
        </n-form>
        <template #footer>
          <n-space justify="end">
            <n-button @click="showAddTask = false">Cancel</n-button>
            <n-button type="primary" @click="addTask">Create</n-button>
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

const message = useMessage()
const kanbanStore = useKanbanStore()
const showAddTask = ref(false)

const newTask = reactive({ title: '', description: '', priority: 'medium' as string })
const priorityOptions = [
  { label: 'Low', value: 'low' },
  { label: 'Medium', value: 'medium' },
  { label: 'High', value: 'high' },
  { label: 'Critical', value: 'critical' },
]

const priorityType = (p: string) => {
  const map: Record<string, string> = { low: 'default', medium: 'info', high: 'warning', critical: 'error' }
  return (map[p] || 'default') as any
}

const statusFlow = ['triage', 'todo', 'ready', 'running', 'done']

async function addTask() {
  if (!newTask.title) return
  await kanbanStore.addTask({ ...newTask, status: 'triage' })
  newTask.title = ''
  newTask.description = ''
  showAddTask.value = false
  message.success('Task created')
}

async function moveTask(task: any) {
  const currentIdx = statusFlow.indexOf(task.status)
  if (currentIdx < statusFlow.length - 1) {
    const nextStatus = statusFlow[currentIdx + 1]
    await kanbanStore.moveTask(task.id, nextStatus)
    message.success(`Moved to ${nextStatus}`)
  }
}

async function removeTask(id: string) {
  await kanbanStore.removeTask(id)
  message.success('Task deleted')
}

onMounted(() => kanbanStore.loadBoard())
</script>

<style scoped>
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
