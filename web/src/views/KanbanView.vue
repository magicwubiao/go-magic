<template>
  <div>
    <n-space justify="space-between" class="kanban-header-space" style="margin-bottom: 16px;">
      <h2>{{ t('kanban.title') }}</h2>
      <n-space>
        <n-button @click="kanbanStore.loadBoard()">
          <template #icon><n-icon :component="RefreshOutline" /></template>
        </n-button>
        <n-button @click="showStats = true">
          <template #icon><n-icon :component="StatsChartOutline" /></template>
          {{ t('kanban.stats') }}
        </n-button>
        <n-button type="primary" @click="openAddTask">+ {{ t('kanban.newTask') }}</n-button>
      </n-space>
    </n-space>

    <!-- Filter Bar -->
    <div class="kanban-filter-bar">
      <div class="kanban-filter-grid">
        <n-input
          v-model:value="filterForm.search"
          class="f-search"
          :placeholder="t('kanban.searchTasks')"
          clearable
        />
        <n-select
          v-model:value="filterForm.priority"
          class="f-sm"
          :placeholder="t('kanban.priority')"
          clearable
          :options="priorityOptions"
        />
        <n-select
          v-model:value="filterForm.assignee"
          class="f-sm"
          :placeholder="t('kanban.assignee')"
          clearable
          :options="assigneeOptions"
        />
        <n-select
          v-model:value="filterForm.goal_id"
          class="f-md"
          :placeholder="t('kanban.linkedGoal')"
          clearable
          :options="goalOptions"
        />
        <n-date-picker
          v-model:value="filterForm.due_before"
          class="f-md"
          :placeholder="t('kanban.dueBefore')"
          clearable
        />
        <n-button class="f-reset" @click="resetFilter">{{ t('kanban.reset') }}</n-button>
      </div>
    </div>

    <n-spin :show="kanbanStore.loading">
      <div class="kanban-container">
        <!-- Upper Row: Triage / To Do / Ready -->
        <div class="kanban-board">
          <div
            v-for="col in filteredUpperColumns"
            :key="col.key"
            class="kanban-column"
            :class="{ 'column-drag-over': dragOverColumn === col.key }"
            @dragover.prevent="onDragOver(col.key)"
            @dragleave="onDragLeave(col.key)"
            @drop.prevent="onDrop(col.key)"
          >
            <div class="column-header">
              <n-text strong>{{ t(col.titleKey) }}</n-text>
              <n-tag size="small" round>{{ col.tasks.length }}</n-tag>
            </div>
            <div class="column-body">
              <template v-if="col.tasks.length === 0">
                <div class="empty-column-hint">
                  <n-text depth="3">{{ t('kanban.dragHint') }}</n-text>
                </div>
              </template>
              <n-card
                v-for="task in col.tasks"
                :key="task.id"
                size="small"
                hoverable
                draggable="true"
                :class="{
                  'task-overdue': isOverdue(task),
                  'task-due-soon': isDueSoon(task),
                  'task-dragging': draggingTaskId === task.id,
                }"
                style="margin-bottom: 8px; cursor: grab;"
                @dragstart="onDragStart(task, $event)"
                @dragend="onDragEnd"
                @click="openTaskDetail(task)"
                @dblclick.stop="openEditTask(task)"
              >
                <n-space vertical :size="4">
                  <n-space justify="space-between">
                    <n-text strong>{{ task.title }}</n-text>
                    <n-tag :type="priorityType(task.priority)" size="tiny">{{ task.priority }}</n-tag>
                  </n-space>
                  <n-text depth="3" style="font-size: 12px;">{{ task.description?.slice(0, 80) }}</n-text>
                  <n-space :size="4" wrap>
                    <n-tag v-if="task.due_date" :type="dueDateType(task)" size="tiny">
                      <template #icon><n-icon :component="CalendarOutline" /></template>
                      {{ formatDueDate(task.due_date) }}
                    </n-tag>
                    <n-tag v-if="task.estimated_hours" size="tiny" type="info">
                      <template #icon><n-icon :component="TimeOutline" /></template>
                      {{ task.estimated_hours }}{{ t('kanban.hoursUnit') }}
                    </n-tag>
                    <n-tag v-if="(task.comment_count || 0) > 0" size="tiny">
                      <template #icon><n-icon :component="ChatbubbleOutline" /></template>
                      {{ task.comment_count }}
                    </n-tag>
                    <n-tag v-if="(task.child_count || 0) > 0" size="tiny" type="success">
                      <template #icon><n-icon :component="GitBranchOutline" /></template>
                      {{ task.child_count }}
                    </n-tag>
                  </n-space>
                </n-space>
                <template #action>
                  <n-space>
                    <n-button v-if="task.status === 'triage'" size="tiny" type="primary" @click.stop="runTriage(task)">
                      <template #icon><n-icon :component="SparklesOutline" /></template>
                      {{ t('kanban.aiLabel') }}
                    </n-button>
                    <n-button size="tiny" quaternary @click.stop="openEditTask(task)">{{ t('kanban.edit') }}</n-button>
                    <n-button size="tiny" quaternary type="error" @click.stop="removeTask(task)">{{ t('kanban.delete') }}</n-button>
                  </n-space>
                </template>
              </n-card>
            </div>
          </div>
        </div>

      <!-- Lower Row: Running / Blocked / Done -->
        <div class="kanban-board">
          <div
            v-for="col in filteredLowerColumns"
            :key="col.key"
            class="kanban-column"
            :class="{
              'blocked-column': col.key === 'blocked',
              'column-drag-over': dragOverColumn === col.key,
            }"
            @dragover.prevent="onDragOver(col.key)"
            @dragleave="onDragLeave(col.key)"
            @drop.prevent="onDrop(col.key)"
          >
            <div class="column-header">
              <n-text strong>{{ t(col.titleKey) }}</n-text>
              <n-tag size="small" round :type="col.key === 'blocked' ? 'error' : 'default'">{{ col.tasks.length }}</n-tag>
            </div>
            <div class="column-body">
              <template v-if="col.tasks.length === 0">
                <div class="empty-column-hint">
                  <n-text depth="3">{{ t('kanban.dragHint') }}</n-text>
                </div>
              </template>
              <n-card
                v-for="task in col.tasks"
                :key="task.id"
                size="small"
                hoverable
                draggable="true"
                :class="{ 'task-dragging': draggingTaskId === task.id }"
                style="margin-bottom: 8px; cursor: grab;"
                @dragstart="onDragStart(task, $event)"
                @dragend="onDragEnd"
                @click="openTaskDetail(task)"
                @dblclick.stop="openEditTask(task)"
              >
                <n-space vertical :size="4">
                  <n-space justify="space-between">
                    <n-text strong>{{ task.title }}</n-text>
                    <n-tag :type="priorityType(task.priority)" size="tiny">{{ task.priority }}</n-tag>
                  </n-space>
                  <n-text depth="3" style="font-size: 12px;">{{ task.description?.slice(0, 80) }}</n-text>
                  <n-space :size="4" wrap>
                    <n-tag v-if="task.due_date" :type="dueDateType(task)" size="tiny">
                      <template #icon><n-icon :component="CalendarOutline" /></template>
                      {{ formatDueDate(task.due_date) }}
                    </n-tag>
                    <n-tag v-if="(task.comment_count || 0) > 0" size="tiny">
                      <template #icon><n-icon :component="ChatbubbleOutline" /></template>
                      {{ task.comment_count }}
                    </n-tag>
                    <n-tag v-if="(task.child_count || 0) > 0" size="tiny" type="success">
                      <template #icon><n-icon :component="GitBranchOutline" /></template>
                      {{ task.child_count }}
                    </n-tag>
                  </n-space>
                </n-space>
                <template #action>
                  <n-space>
                    <n-button v-if="col.key === 'blocked'" size="tiny" type="primary" @click.stop="unblockTask(task.id)">{{ t('kanban.unblock') }}</n-button>
                    <n-button v-if="task.status === 'running'" size="tiny" type="warning" @click.stop="openBlockDialog(task)">{{ t('kanban.block') }}</n-button>
                    <n-button size="tiny" quaternary @click.stop="openEditTask(task)">{{ t('kanban.edit') }}</n-button>
                    <n-button size="tiny" quaternary type="error" @click.stop="removeTask(task)">{{ t('kanban.delete') }}</n-button>
                  </n-space>
                </template>
              </n-card>
            </div>
          </div>
        </div>
      </div>
    </n-spin>

    <!-- Add/Edit Task Modal -->
    <n-modal v-model:show="showTaskModal" preset="card" class="modal-responsive modal-scroll" style="width: 540px; max-width: 96vw;" :title="editingTask ? t('kanban.editTask') : t('kanban.newTask')">
      <n-form label-placement="top">
          <n-form-item :label="t('kanban.formTitle')">
            <n-input v-model:value="taskForm.title" :placeholder="t('kanban.taskTitle')" />
          </n-form-item>
          <n-form-item :label="t('kanban.formDescription')">
            <n-input v-model:value="taskForm.description" type="textarea" :rows="4" :placeholder="t('kanban.pleaseInput')" />
          </n-form-item>
          <n-form-item :label="t('kanban.dueDate')">
            <n-date-picker v-model:value="taskForm.due_date" type="date" clearable style="width: 100%;" :placeholder="t('kanban.selectDate')" />
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
          <n-button v-if="editingTask && editingTask.status === 'triage'" type="primary" @click="runTriageFromModal">
            <template #icon><n-icon :component="SparklesOutline" /></template>
            {{ t('kanban.aiTriage') }}
          </n-button>
          <n-button v-if="editingTask" type="info" @click="splitTask">{{ t('kanban.aiSplit') }}</n-button>
          <n-button @click="showTaskModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="saveTask">{{ editingTask ? t('common.save') : t('kanban.create') }}</n-button>
        </n-space>
      </template>
    </n-modal>

<!-- Task Detail Modal -->
    <n-modal v-model:show="showDetailModal" preset="card" class="modal-responsive modal-scroll" style="width: 620px; max-width: 96vw;" :title="detailTask?.title">
      <template v-if="detailTask">
        <n-space vertical :size="16">
          <n-space :size="12">
            <n-tag :type="priorityType(detailTask.priority)">{{ detailTask.priority }}</n-tag>
            <n-tag>{{ t('kanban.statusOptions.' + detailTask.status) }}</n-tag>
            <n-tag v-if="detailTask.due_date" :type="dueDateType(detailTask)">
              <template #icon><n-icon :component="CalendarOutline" /></template>
              {{ formatDueDate(detailTask.due_date) }}
            </n-tag>
            <n-tag v-if="detailTask.estimated_hours" type="info">
              <template #icon><n-icon :component="TimeOutline" /></template>
              {{ detailTask.estimated_hours }}{{ t('kanban.hoursUnit') }}
            </n-tag>
          </n-space>
          <n-divider />
          <n-text style="white-space: pre-wrap;">{{ detailTask.description }}</n-text>
          <div v-if="children.length > 0">
            <n-text strong style="margin-bottom: 8px; display: block;">{{ t('kanban.children') }}</n-text>
            <n-list>
              <n-list-item v-for="child in children" :key="child.id">
                <n-space>
                  <n-text>{{ child.title }}</n-text>
                  <n-tag size="tiny">{{ t('kanban.statusOptions.' + child.status) }}</n-tag>
                </n-space>
              </n-list-item>
            </n-list>
          </div>
          <div>
            <n-text strong style="margin-bottom: 8px; display: block;">{{ t('kanban.comments') }}</n-text>
            <n-list v-if="comments.length > 0">
              <n-list-item v-for="c in comments" :key="c.id">
                <n-space vertical :size="2">
                  <n-space justify="space-between">
                    <n-text strong>{{ c.author }}</n-text>
                    <n-text depth="3" style="font-size: 12px;">{{ formatTime(c.created_at) }}</n-text>
                  </n-space>
                  <n-text>{{ c.body }}</n-text>
                </n-space>
              </n-list-item>
            </n-list>
            <n-empty v-else :description="t('kanban.noComments')" style="margin: 8px 0;" />
            <n-space style="margin-top: 8px;">
              <n-input v-model:value="newComment" :placeholder="t('kanban.addComment')" style="flex: 1;" @keyup.enter="submitComment" />
              <n-button type="primary" @click="submitComment">{{ t('kanban.send') }}</n-button>
            </n-space>
          </div>
          <n-space justify="end">
            <n-button v-if="detailTask.status === 'triage'" type="primary" @click="runTriage(detailTask)">
              <template #icon><n-icon :component="SparklesOutline" /></template>
              {{ t('kanban.aiTriage') }}
            </n-button>
            <n-button v-if="detailTask.status === 'running'" type="warning" @click="openBlockDialog(detailTask)">
              {{ t('kanban.block') }}
            </n-button>
            <n-button v-if="canMoveForward(detailTask)" type="primary" @click="moveTaskForward(detailTask)">
              {{ t('kanban.moveForward') }}
            </n-button>
            <n-button @click="showDetailModal = false">{{ t('common.close') }}</n-button>
          </n-space>
        </n-space>
      </template>
    </n-modal>

    <!-- Block Reason Modal -->
    <n-modal v-model:show="showBlockModal" preset="card" class="modal-responsive" style="width: 420px; max-width: 96vw;" :title="t('kanban.blockReason')">
      <n-input v-model:value="blockReason" type="textarea" :rows="3" :placeholder="t('kanban.enterBlockReason')" />
      <template #footer>
        <n-space justify="end">
          <n-button @click="showBlockModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="warning" @click="confirmBlock">{{ t('kanban.block') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Delete Confirm Modal -->
    <n-modal v-model:show="showDeleteModal" preset="card" class="modal-responsive" style="width: 420px; max-width: 96vw;" :title="t('kanban.deleteTaskTitle')">
      <n-text>{{ t('kanban.confirmDeleteTask') }}</n-text>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showDeleteModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="error" :loading="deleting" @click="confirmDeleteTask">{{ t('common.confirm') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Error Banner -->
    <n-alert
      v-if="kanbanStore.error"
      type="error"
      show-icon
      style="margin-bottom: 16px;"
      :title="t('kanban.loadError')"
    >
      {{ kanbanStore.error }}
    </n-alert>

    <!-- Stats Modal -->
    <n-modal v-model:show="showStats" preset="card" class="modal-responsive" style="width: 620px; max-width: 96vw;" :title="t('kanban.statsTitle')">
      <n-space vertical>
          <n-grid :cols="2" :x-gap="12" :y-gap="12">
            <n-gi>
              <n-statistic :label="t('kanban.totalTasks')" :value="kanbanStore.stats.total" />
            </n-gi>
            <n-gi>
              <n-statistic :label="t('kanban.completedTasks')" :value="kanbanStore.stats.completed" />
            </n-gi>
            <n-gi>
              <n-statistic :label="t('kanban.inProgressTasks')" :value="kanbanStore.stats.in_progress" />
            </n-gi>
            <n-gi>
              <n-statistic :label="t('kanban.pendingTasks')" :value="kanbanStore.stats.pending" />
            </n-gi>
            <n-gi>
              <n-statistic :label="t('kanban.blockedTasks')" :value="(kanbanStore.tasks ?? []).filter(t => t.status === 'blocked').length" />
            </n-gi>
            <n-gi>
              <n-statistic :label="t('kanban.completionRate')" :value="kanbanStore.stats.total > 0 ? Math.round((kanbanStore.stats.completed / kanbanStore.stats.total) * 100) : 0" suffix="%" />
            </n-gi>
          </n-grid>
        </n-space>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import {
  CalendarOutline, TimeOutline, StatsChartOutline,
  ChatbubbleOutline, GitBranchOutline, SparklesOutline,
  RefreshOutline,
} from '@vicons/ionicons5'
import { useKanbanStore } from '@/stores/kanban'
import { useGoalsStore } from '@/stores/goals'
import type { KanbanTask } from '@/api/kanban'
import {
  getTaskComments, addTaskComment, getTaskChildren, triageTask, blockTask,
} from '@/api/kanban'

const { t } = useI18n()
const message = useMessage()
const kanbanStore = useKanbanStore()
const goalsStore = useGoalsStore()

const showTaskModal = ref(false)
const showStats = ref(false)
const showDetailModal = ref(false)
const showBlockModal = ref(false)
const showDeleteModal = ref(false)
const pendingDeleteTask = ref<KanbanTask | null>(null)
const deleting = ref(false)
const editingTask = ref<KanbanTask | null>(null)
const detailTask = ref<KanbanTask | null>(null)
const blockReason = ref('')
const blockTaskRef = ref<KanbanTask | null>(null)

const comments = ref<Array<{ id: string; author: string; body: string; created_at: number }>>([])
const children = ref<KanbanTask[]>([])
const newComment = ref('')

// Drag & drop state
const draggingTaskId = ref<string | null>(null)
const dragOverColumn = ref<string | null>(null)

// Auto-refresh timer
let refreshTimer: ReturnType<typeof setInterval> | null = null

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

const filteredUpperColumns = computed(() => {
  return kanbanStore.upperColumns.map(col => ({
    ...col,
    tasks: filterTasks(col.tasks),
  }))
})

const filteredLowerColumns = computed(() => {
  return kanbanStore.lowerColumns.map(col => ({
    ...col,
    tasks: filterTasks(col.tasks),
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

function isOverdue(task: KanbanTask) {
  if (!task.due_date) return false
  return new Date(task.due_date).getTime() < Date.now()
}

function isDueSoon(task: KanbanTask) {
  if (!task.due_date) return false
  const due = new Date(task.due_date).getTime()
  const now = Date.now()
  return due > now && due < now + 3 * 24 * 60 * 60 * 1000
}

function dueDateType(task: KanbanTask) {
  if (isOverdue(task)) return 'error'
  if (isDueSoon(task)) return 'warning'
  return 'default'
}

function formatDueDate(date: string | number) {
  return new Date(date).toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
}

function formatTime(ts: number) {
  return new Date(ts * 1000).toLocaleString(undefined)
}

const statusFlow = ['triage', 'todo', 'ready', 'running', 'done', 'archived']

function canMoveForward(task: KanbanTask) {
  const idx = statusFlow.indexOf(task.status)
  return idx >= 0 && idx < statusFlow.length - 1 && task.status !== 'blocked'
}

// --- Drag & Drop ---
function onDragStart(task: KanbanTask, e: DragEvent) {
  draggingTaskId.value = task.id
  if (e.dataTransfer) {
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', task.id)
  }
}

function onDragEnd() {
  draggingTaskId.value = null
  dragOverColumn.value = null
}

function onDragOver(colKey: string) {
  dragOverColumn.value = colKey
}

function onDragLeave(colKey: string) {
  if (dragOverColumn.value === colKey) {
    dragOverColumn.value = null
  }
}

async function onDrop(colKey: string) {
  dragOverColumn.value = null
  if (!draggingTaskId.value) return
  const taskId = draggingTaskId.value
  draggingTaskId.value = null
  try {
    await kanbanStore.moveTask(taskId, colKey)
    message.success(`${t('kanban.movedTo')} ${t('kanban.statusOptions.' + colKey)}`)
  } catch (e) {
    message.error(`${t('kanban.movedTo')} ${t('kanban.statusOptions.' + colKey)} ${t('common.error')}`)
  }
}

// --- Task CRUD ---
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
    message.success(`${t('kanban.movedTo')} ${t('kanban.statusOptions.' + nextStatus)}`)
    if (showDetailModal.value && detailTask.value?.id === task.id) {
      detailTask.value = { ...task, status: nextStatus }
    }
  }
}

async function unblockTask(id: string) {
  await kanbanStore.moveTask(id, 'ready')
  message.success(t('kanban.taskUnblocked'))
}

function removeTask(task: KanbanTask) {
  pendingDeleteTask.value = task
  showDeleteModal.value = true
}

async function confirmDeleteTask() {
  if (!pendingDeleteTask.value) return
  deleting.value = true
  try {
    await kanbanStore.removeTask(pendingDeleteTask.value.id)
    message.success(t('kanban.taskDeleted'))
    showDeleteModal.value = false
    pendingDeleteTask.value = null
  } catch (e) {
    message.error(t('kanban.taskDeleted'))
  } finally {
    deleting.value = false
  }
}

// --- Detail Modal ---
async function openTaskDetail(task: KanbanTask) {
  detailTask.value = task
  showDetailModal.value = true
  newComment.value = ''
  await loadDetailData(task.id)
}

async function loadDetailData(taskId: string) {
  try {
    const [cList, childList] = await Promise.all([
      getTaskComments(taskId),
      getTaskChildren(taskId),
    ])
    comments.value = cList
    children.value = childList
  } catch (e) {
    comments.value = []
    children.value = []
  }
}

async function submitComment() {
  if (!newComment.value.trim() || !detailTask.value) return
  try {
    await addTaskComment(detailTask.value.id, 'user', newComment.value.trim())
    newComment.value = ''
    await loadDetailData(detailTask.value.id)
  } catch (e) {
    message.error(t('kanban.commentFailed'))
  }
}

// --- Triage ---
async function runTriage(task: KanbanTask) {
  try {
    message.info(t('kanban.triageRunning'))
    const updated = await triageTask(task.id)
    message.success(t('kanban.triageDone'))
    await kanbanStore.loadBoard()
    if (showDetailModal.value && detailTask.value?.id === task.id) {
      detailTask.value = { ...task, ...updated }
      await loadDetailData(task.id)
    }
  } catch (e) {
    message.error(t('kanban.triageFailed'))
  }
}

async function runTriageFromModal() {
  if (!editingTask.value) return
  await runTriage(editingTask.value)
  // Refresh form with updated data
  if (editingTask.value) {
    openEditTask(editingTask.value)
  }
}

// --- Block ---
function openBlockDialog(task: KanbanTask) {
  blockTaskRef.value = task
  blockReason.value = ''
  showBlockModal.value = true
}

async function confirmBlock() {
  if (!blockTaskRef.value || !blockReason.value.trim()) return
  try {
    await blockTask(blockTaskRef.value.id, blockReason.value.trim())
    message.success(t('kanban.taskBlocked'))
    showBlockModal.value = false
    await kanbanStore.loadBoard()
    if (showDetailModal.value && detailTask.value?.id === blockTaskRef.value.id) {
      detailTask.value = { ...blockTaskRef.value, status: 'blocked' }
    }
  } catch (e) {
    message.error(t('kanban.blockFailed'))
  }
}

// --- Lifecycle ---
onMounted(() => {
  kanbanStore.loadBoard()
  goalsStore.loadGoals()
  // Auto-refresh every 30 seconds
  refreshTimer = setInterval(() => {
    kanbanStore.loadBoard()
  }, 30000)
})

onUnmounted(() => {
  if (refreshTimer) {
    clearInterval(refreshTimer)
    refreshTimer = null
  }
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
.kanban-filter-grid {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 12px;
}
.f-search { width: 220px; }
.f-sm { width: 130px; }
.f-md { width: 160px; }
.f-reset { margin-left: auto; }
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
  min-width: 220px;
  background: #fafafa;
  border-radius: 8px;
  border: 2px solid #e8e8e8;
  transition: border-color 0.2s, background-color 0.2s;
}
.kanban-column.column-drag-over {
  border-color: #1890ff;
  background-color: #e6f7ff;
}
.blocked-column {
  border-color: #ff4d4f;
  background: #fff2f0;
}
.blocked-column.column-drag-over {
  border-color: #ff4d4f;
  background-color: #ffccc7;
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
.empty-column-hint {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 80px;
  border: 2px dashed #d9d9d9;
  border-radius: 6px;
  padding: 16px;
}
.task-overdue {
  border-left: 3px solid #ff4d4f;
}
.task-due-soon {
  border-left: 3px solid #faad14;
}
.task-dragging {
  opacity: 0.5;
}

/* 移动端适配 */
@media (max-width: 768px) {
  /* 头部标题行允许换行 */
  .kanban-header-space {
    flex-wrap: wrap !important;
    row-gap: 8px !important;
  }

  /* 筛选栏:搜索占整行,其余控件两列流式铺满 */
  .kanban-filter-bar {
    padding: 10px 12px;
  }
  .kanban-filter-grid {
    gap: 8px;
  }
  .f-search {
    flex: 1 1 100% !important;
    width: auto !important;
    min-width: 0;
  }
  .f-sm,
  .f-md {
    flex: 1 1 calc(50% - 4px);
    width: auto !important;
    min-width: 0;
  }

  /* 看板改为纵向堆叠:触屏拖拽不可用,靠卡片操作按钮移动任务 */
  .kanban-board {
    display: block;
    overflow-x: visible;
    padding-bottom: 0;
  }
  .kanban-column {
    width: 100%;
    min-width: 0;
    margin-bottom: 12px;
  }
  .column-header {
    font-size: 14px;
    padding: 10px 12px;
  }
  .column-body {
    max-height: 45vh;
    overflow-y: auto;
  }

  /* 任务卡操作按钮增大触控面积 */
  .kanban-container :deep(.n-card .n-card__action .n-button) {
    height: 32px;
    padding: 0 12px;
    font-size: 13px;
  }

  /* 空列提示收敛 */
  .empty-column-hint {
    min-height: 60px;
    padding: 10px;
  }
}
</style>