<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>{{ t('kanban.title') }}</h2>
      <n-space>
        <n-button @click="showStats = true">
          <template #icon><n-icon :component="StatsChartOutline" /></template>
          {{ t('kanban.stats') }}
        </n-button>
        <n-button type="primary" @click="openAddTask">+ {{ t('kanban.newTask') }}</n-button>
      </n-space>
    </n-space>

    <!-- Filter Bar -->
    <div class="kanban-filter-bar">
      <n-space align="center" wrap>
        <n-input
          v-model:value="filterForm.search"
          :placeholder="t('kanban.searchTasks')"
          clearable
          style="width: 200px;"
          @update:value="applyFilter"
        />
        <n-select
          v-model:value="filterForm.priority"
          :placeholder="t('kanban.priority')"
          clearable
          :options="priorityOptions"
          style="width: 120px;"
          @update:value="applyFilter"
        />
        <n-select
          v-model:value="filterForm.assignee"
          :placeholder="t('kanban.assignee')"
          clearable
          :options="assigneeOptions"
          style="width: 120px;"
          @update:value="applyFilter"
        />
        <n-select
          v-model:value="filterForm.goal_id"
          :placeholder="t('kanban.linkedGoal')"
          clearable
          :options="goalOptions"
          style="width: 150px;"
          @update:value="applyFilter"
        />
        <n-date-picker
          v-model:value="filterForm.due_before"
          :placeholder="t('kanban.dueBefore')"
          clearable
          style="width: 150px;"
          @update:value="applyFilter"
        />
        <n-button @click="resetFilter">{{ t('kanban.reset') }}</n-button>
      </n-space>
    </div>

    <n-spin :show="kanbanStore.loading">
      <div class="kanban-container">
        <!-- Upper Row: Triage / To Do / Ready -->
        <div class="kanban-board">
          <div v-for="col in filteredUpperColumns" :key="col.key" class="kanban-column">
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
                :class="{ 'task-overdue': isOverdue(task), 'task-due-soon': isDueSoon(task) }"
                style="margin-bottom: 8px; cursor: pointer;"
                @click="moveTaskForward(task)"
              >
                <n-space vertical :size="4">
                  <n-text strong>{{ task.title }}</n-text>
                  <n-text depth="3" style="font-size: 12px;">{{ task.description?.slice(0, 80) }}</n-text>
                  <n-space v-if="task.due_date" :size="4">
                    <n-tag :type="dueDateType(task)" size="tiny">
                      <template #icon><n-icon :component="CalendarOutline" /></template>
                      {{ formatDueDate(task.due_date) }}
                    </n-tag>
                  </n-space>
                  <n-space v-if="task.estimated_hours" :size="4">
                    <n-tag size="tiny" type="info">
                      <template #icon><n-icon :component="TimeOutline" /></template>
                      {{ task.estimated_hours }}h
                    </n-tag>
                  </n-space>
                </n-space>
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
          <div v-for="col in filteredLowerColumns" :key="col.key" class="kanban-column" :class="{ 'blocked-column': col.key === 'blocked' }">
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
                <n-space vertical :size="4">
                  <n-text strong>{{ task.title }}</n-text>
                  <n-text depth="3" style="font-size: 12px;">{{ task.description?.slice(0, 80) }}</n-text>
                  <n-space v-if="task.due_date" :size="4">
                    <n-tag :type="dueDateType(task)" size="tiny">
                      <template #icon><n-icon :component="CalendarOutline" /></template>
                      {{ formatDueDate(task.due_date) }}
                    </n-tag>
                  </n-space>
                </n-space>
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
    </n-spin>

    <!-- Add/Edit Task Modal -->
    <n-modal v-model:show="showTaskModal" :title="editingTask ? t('kanban.editTask') : t('kanban.newTask')">
      <n-card style="width: 500px;">
        <n-form>
          <n-form-item :label="t('kanban.formTitle')">
            <n-input v-model:value="taskForm.title" :placeholder="t('kanban.taskTitle')" />
          </n-form-item>
          <n-form-item :label="t('kanban.formDescription')">
            <n-input v-model:value="taskForm.description" type="textarea" :rows="4" />
          </n-form-item>
          <n-form-item :label="t('kanban.dueDate')">
            <n-date-picker v-model:value="taskForm.due_date" type="date" clearable style="width: 100%;" />
          </n-form-item>
          <n-form-item :label="t('kanban.estimatedHours')">
            <n-input-number v-model:value="taskForm.estimated_hours" :min="0" :step="0.5" style="width: 100%;" />
          </n-form-item>
          <n-form-item :label="t('kanban.linkedGoal')">
            <n-select v-model:value="taskForm.goal_id" :placeholder="t('kanban.selectGoal')" clearable :options="goalOptions" />
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
            <n-button v-if="editingTask" type="info" @click="splitTask">{{ t('kanban.aiSplit') }}</n-button>
            <n-button @click="showTaskModal = false">{{ t('common.cancel') }}</n-button>
            <n-button type="primary" @click="saveTask">{{ editingTask ? t('common.save') : t('kanban.create') }}</n-button>
          </n-space>
        </template>
      </n-card>
    </n-modal>

    <!-- Stats Modal -->
    <n-modal v-model:show="showStats" :title="t('kanban.statsTitle')">
      <n-card style="width: 600px;">
        <n-space vertical>
          <n-statistic :label="t('kanban.totalTasks')" :value="kanbanStore.stats?.total || 0" />
          <n-statistic :label="t('kanban.completedTasks')" :value="kanbanStore.stats?.completed || 0" />
          <n-statistic :label="t('kanban.inProgressTasks')" :value="kanbanStore.stats?.in_progress || 0" />
          <n-statistic :label="t('kanban.pendingTasks')" :value="kanbanStore.stats?.pending || 0" />
        </n-space>
      </n-card>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { CalendarOutline, TimeOutline, StatsChartOutline } from '@vicons/ionicons5'
import { useKanbanStore } from '@/stores/kanban'
import { useGoalsStore } from '@/stores/goals'
import type { KanbanTask } from '@/api/kanban'

const { t } = useI18n()

const message = useMessage()
const kanbanStore = useKanbanStore()
const goalsStore = useGoalsStore()
const showTaskModal = ref(false)
const showStats = ref(false)
const editingTask = ref<KanbanTask | null>(null)

const taskForm = reactive({
  title: '',
  description: '',
  priority: 'medium' as string,
  status: 'triage' as string,
  due_date: null as number | null,
  estimated_hours: 0,
  goal_id: '',
})

const filterForm = reactive({
  search: '',
  priority: null as string | null,
  assignee: null as string | null,
  goal_id: null as string | null,
  due_before: null as number | null,
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

const assigneeOptions = computed(() => [
  { label: t('kanban.allLabel'), value: '' },
])

const goalOptions = computed(() => {
  const options = [{ label: t('kanban.allLabel'), value: '' }]
  ;(goalsStore.goals ?? []).forEach(g => {
    options.push({ label: g.title, value: g.id })
  })
  return options
})

// Filtered columns
const filteredUpperColumns = computed(() => {
  return kanbanStore.upperColumns.map(col => ({
    ...col,
    tasks: filterTasks(col.tasks)
  }))
})

const filteredLowerColumns = computed(() => {
  return kanbanStore.lowerColumns.map(col => ({
    ...col,
    tasks: filterTasks(col.tasks)
  }))
})

function filterTasks(tasks: KanbanTask[]) {
  return tasks.filter(task => {
    if (filterForm.search && !task.title.toLowerCase().includes(filterForm.search.toLowerCase())) {
      return false
    }
    if (filterForm.priority && task.priority !== filterForm.priority) {
      return false
    }
    if (filterForm.goal_id && task.goal_id !== filterForm.goal_id) {
      return false
    }
    return true
  })
}

function applyFilter() {
  // Filter is reactive, no need to do anything
}

function resetFilter() {
  filterForm.search = ''
  filterForm.priority = null
  filterForm.assignee = null
  filterForm.goal_id = null
  filterForm.due_before = null
}

const priorityType = (p: string) => {
  const map: Record<string, string> = { low: 'default', medium: 'info', high: 'warning', critical: 'error' }
  return (map[p] || 'default') as any
}

// Due date helpers
function isOverdue(task: KanbanTask) {
  if (!task.due_date) return false
  return new Date(task.due_date).getTime() < Date.now()
}

function isDueSoon(task: KanbanTask) {
  if (!task.due_date) return false
  const due = new Date(task.due_date).getTime()
  const now = Date.now()
  const oneDay = 24 * 60 * 60 * 1000
  return due > now && due < now + oneDay * 3
}

function dueDateType(task: KanbanTask) {
  if (isOverdue(task)) return 'error'
  if (isDueSoon(task)) return 'warning'
  return 'default'
}

function formatDueDate(date: string | number) {
  const d = new Date(date)
  return d.toLocaleDateString('zh-CN', { month: 'short', day: 'numeric' })
}

// Normal flow: triage -> todo -> ready -> running -> done -> archived
const statusFlow = ['triage', 'todo', 'ready', 'running', 'done', 'archived']

function openAddTask() {
  editingTask.value = null
  taskForm.title = ''
  taskForm.description = ''
  taskForm.priority = 'medium'
  taskForm.status = 'triage'
  taskForm.due_date = null
  taskForm.estimated_hours = 0
  taskForm.goal_id = ''
  showTaskModal.value = true
}

function openEditTask(task: KanbanTask) {
  editingTask.value = task
  taskForm.title = task.title
  taskForm.description = task.description
  taskForm.priority = task.priority
  taskForm.status = task.status
  taskForm.due_date = task.due_date ? new Date(task.due_date).getTime() : null
  taskForm.estimated_hours = task.estimated_hours || 0
  taskForm.goal_id = task.goal_id || ''
  showTaskModal.value = true
}

async function saveTask() {
  if (!taskForm.title) {
    message.warning(t('kanban.enterTitle'))
    return
  }

  const data = {
    ...taskForm,
    due_date: taskForm.due_date ? new Date(taskForm.due_date).toISOString() : undefined,
  }

  if (editingTask.value) {
    await kanbanStore.updateTask(editingTask.value.id, data as Partial<KanbanTask>)
    message.success(t('kanban.taskUpdated'))
  } else {
    await kanbanStore.addTask(data as Partial<KanbanTask>)
    message.success(t('kanban.taskCreated'))
  }
  showTaskModal.value = false
}

async function splitTask() {
  if (!editingTask.value) return
  try {
    await kanbanStore.splitTask(editingTask.value.id)
    message.success(t('kanban.taskSplit'))
    showTaskModal.value = false
  } catch (e) {
    message.error(t('kanban.splitFailed'))
  }
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

onMounted(() => {
  kanbanStore.loadBoard()
  goalsStore.loadGoals()
})
</script>

<style scoped>
.kanban-filter-bar {
  margin-bottom: 16px;
  padding: 12px 16px;
  background: #fafafa;
  border: 1px solid #e8e8e8;
  border-radius: 8px;
}

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

.task-overdue {
  border-left: 3px solid #ff4d4f;
}

.task-due-soon {
  border-left: 3px solid #faad14;
}
</style>
