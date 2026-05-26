<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>{{ t('kanban.title') }}</h2>
      <n-button type="primary" @click="openAddTask">{{ t('kanban.newTask') }}</n-button>
    </n-space>

    <n-spin v-if="kanbanStore.loading" />
    <div v-else class="kanban-container">
      <!-- Upper Row: Triage / To Do / Ready -->
      <div class="kanban-board">
        <div v-for="col in kanbanStore.upperColumns" :key="col.key" class="kanban-column">
          <div class="column-header">
            <n-text strong>{{ t(col.titleKey) }}</n-text>
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
                  <n-button size="tiny" quaternary @click.stop="openEditTask(task)">{{ t('kanban.edit') }}</n-button>
                  <n-button size="tiny" quaternary type="error" @click.stop="removeTask(task.id)">{{ t('kanban.delete') }}</n-button>
                </n-space>
              </template>
            </n-card>
          </div>
        </div>
      </div>

      <!-- Lower Row: Running / Blocked / Done -->
      <div class="kanban-board">
        <div v-for="col in kanbanStore.lowerColumns" :key="col.key" class="kanban-column" :class="{ 'blocked-column': col.key === 'blocked' }">
          <div class="column-header">
            <n-text strong>{{ t(col.titleKey) }}</n-text>
            <n-tag size="small" round :type="col.key === 'blocked' ? 'error' : 'default'">{{ col.tasks.length }}</n-tag>
          </div>
          <div class="column-body">
            <n-card
              v-for="task in col.tasks"
              :key="task.id"
              size="small"
              hoverable
              style="margin-bottom: 8px; cursor: pointer;"
              @click="col.key !== 'blocked' && moveTaskForward(task)"
            >
              <n-text strong>{{ task.title }}</n-text>
              <br />
              <n-text depth="3" style="font-size: 12px;">{{ task.description?.slice(0, 80) }}</n-text>
              <template #action>
                <n-space>
                  <n-tag :type="priorityType(task.priority)" size="tiny">{{ task.priority }}</n-tag>
                  <n-button v-if="col.key === 'blocked'" size="tiny" type="primary" @click.stop="unblockTask(task.id)">{{ t('kanban.unblock') }}</n-button>
                  <n-button size="tiny" quaternary @click.stop="openEditTask(task)">{{ t('kanban.edit') }}</n-button>
                  <n-button size="tiny" quaternary type="error" @click.stop="removeTask(task.id)">{{ t('kanban.delete') }}</n-button>
                </n-space>
              </template>
            </n-card>
          </div>
        </div>
      </div>
    </div>

    <!-- Add/Edit Task Modal -->
    <n-modal v-model:show="showTaskModal" :title="editingTask ? t('kanban.editTask') : t('kanban.newTask')">
      <n-card style="width: 450px;">
        <n-form>
          <n-form-item :label="t('kanban.formTitle')">
            <n-input v-model:value="taskForm.title" :placeholder="t('kanban.taskTitle')" />
          </n-form-item>
          <n-form-item :label="t('kanban.formDescription')">
            <n-input v-model:value="taskForm.description" type="textarea" :rows="4" />
          </n-form-item>
          <n-form-item :label="t('kanban.formPriority')">
            <n-select v-model:value="taskForm.priority" :options="priorityOptions" />
          </n-form-item>
          <n-form-item :label="t('kanban.formStatus')" v-if="editingTask">
            <n-select v-model:value="taskForm.status" :options="statusOptions" />
          </n-form-item>
        </n-form>
        <template #footer>
          <n-space justify="end">
            <n-button @click="showTaskModal = false">{{ t('common.cancel') }}</n-button>
            <n-button type="primary" @click="saveTask">{{ editingTask ? t('common.save') : t('kanban.create') }}</n-button>
          </n-space>
        </template>
      </n-card>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useKanbanStore } from '@/stores/kanban'
import type { KanbanTask } from '@/api/kanban'

const { t } = useI18n()

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

const priorityOptions = computed(() => [
  { label: t('kanban.priorityOptions.low'), value: 'low' },
  { label: t('kanban.priorityOptions.medium'), value: 'medium' },
  { label: t('kanban.priorityOptions.high'), value: 'high' },
  { label: t('kanban.priorityOptions.critical'), value: 'critical' },
])

const statusOptions = computed(() => [
  { label: t('kanban.statusOptions.triage'), value: 'triage' },
  { label: t('kanban.statusOptions.todo'), value: 'todo' },
  { label: t('kanban.statusOptions.ready'), value: 'ready' },
  { label: t('kanban.statusOptions.running'), value: 'running' },
  { label: t('kanban.statusOptions.blocked'), value: 'blocked' },
  { label: t('kanban.statusOptions.done'), value: 'done' },
  { label: t('kanban.statusOptions.archived'), value: 'archived' },
])

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
    message.warning(t('kanban.enterTitle'))
    return
  }

  if (editingTask.value) {
    await kanbanStore.updateTask(editingTask.value.id, { ...taskForm } as Partial<KanbanTask>)
    message.success(t('kanban.taskUpdated'))
  } else {
    await kanbanStore.addTask({ ...taskForm } as Partial<KanbanTask>)
    message.success(t('kanban.taskCreated'))
  }
  showTaskModal.value = false
}

async function moveTaskForward(task: KanbanTask) {
  const currentIdx = statusFlow.indexOf(task.status)
  if (currentIdx < statusFlow.length - 1) {
    const nextStatus = statusFlow[currentIdx + 1]
    await kanbanStore.moveTask(task.id, nextStatus)
    message.success(`${t('kanban.movedTo')} ${nextStatus}`)
  }
}

async function unblockTask(id: string) {
  await kanbanStore.moveTask(id, 'todo')
  message.success(t('kanban.taskUnblocked'))
}

async function removeTask(id: string) {
  await kanbanStore.removeTask(id)
  message.success(t('kanban.taskDeleted'))
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
  flex: 1;
  min-width: 200px;
  background: #fafafa;
  border-radius: 8px;
  border: 1px solid #e8e8e8;
}

.blocked-column {
  border-color: #ff4d4f;
  background: #fff2f0;
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
