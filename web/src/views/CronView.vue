<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>{{ t('cron.title') }}</h2>
      <n-space>
        <n-button :loading="cronStore.loading" @click="cronStore.loadJobs()"><template #icon><n-icon><RefreshOutline /></n-icon></template></n-button>
        <n-button type="primary" @click="openCreateModal">+ {{ t('cron.createJob') }}</n-button>
      </n-space>
    </n-space>

    <n-spin :show="cronStore.loading">
      <n-empty v-if="!cronStore.jobs.length" :description="t('cron.noJobs')" />

      <n-space vertical>
        <n-card v-for="job in cronStore.jobs" :key="job.id" size="small">
          <n-space justify="space-between" align="center">
            <n-space align="center" :size="12">
              <n-tag :type="stateType(job.state)" size="small">{{ stateLabel(job.state) }}</n-tag>
              <n-text strong style="font-size: 15px;">{{ job.name }}</n-text>
            </n-space>
            <n-space :size="4">
              <n-button v-if="job.state === 'active'" size="tiny" @click="handlePause(job.id)">{{ t('cron.pause') }}</n-button>
              <n-button v-if="job.state === 'inactive'" size="tiny" type="primary" @click="handleResume(job.id)">{{ t('cron.resume') }}</n-button>
              <n-button size="tiny" @click="handleTrigger(job.id)">{{ t('cron.runNow') }}</n-button>
              <n-button size="tiny" @click="openEditModal(job)">{{ t('common.edit') }}</n-button>
              <n-button size="tiny" @click="openLogsModal(job)">{{ t('cron.logs') }}</n-button>
              <n-button size="tiny" type="error" @click="handleDelete(job.id)">{{ t('common.delete') }}</n-button>
            </n-space>
          </n-space>

          <n-grid :cols="4" :x-gap="16" style="margin-top: 12px;">
            <n-gi>
              <n-text depth="3" style="font-size: 12px;">{{ t('cron.schedule') }}</n-text>
              <div><n-text code>{{ job.schedule }}</n-text></div>
              <div><n-text depth="3" style="font-size: 11px;">{{ job.schedule_display }}</n-text></div>
            </n-gi>
            <n-gi>
              <n-text depth="3" style="font-size: 12px;">{{ t('cron.nextRun') }}</n-text>
              <div>{{ job.next_run_at ? formatTime(job.next_run_at) : '-' }}</div>
            </n-gi>
            <n-gi>
              <n-text depth="3" style="font-size: 12px;">{{ t('cron.previousRun') }}</n-text>
              <div>{{ job.last_run_at ? formatTime(job.last_run_at) : '-' }}</div>
              <div v-if="job.last_status">
                <n-tag :type="job.last_status === 'success' ? 'success' : job.last_status === 'failed' ? 'error' : 'warning'" size="tiny">
                  {{ job.last_status }}
                </n-tag>
              </div>
            </n-gi>
            <n-gi>
              <n-text depth="3" style="font-size: 12px;">{{ t('cron.runCount') }}</n-text>
              <div>{{ job.run_count }}</div>
            </n-gi>
          </n-grid>

          <n-space v-if="job.last_error" style="margin-top: 8px;">
            <n-text type="error" style="font-size: 12px;">{{ t('cron.lastError', { error: job.last_error }) }}</n-text>
          </n-space>

          <n-space style="margin-top: 8px;">
            <n-tag v-if="job.prompt" size="tiny" type="info">{{ t('cron.agentModeLabel') }}</n-tag>
            <n-tag v-if="job.script" size="tiny" type="warning">{{ t('cron.scriptModeLabel') }}</n-tag>
            <n-tag v-if="job.working_dir" size="tiny">{{ t('cron.workingDir') }}: {{ job.working_dir }}</n-tag>
            <n-text v-if="job.prompt" depth="3" style="font-size: 11px; max-width: 400px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">
              {{ job.prompt }}
            </n-text>
            <n-text v-if="job.script && !job.prompt" depth="3" style="font-size: 11px; max-width: 400px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">
              {{ job.script }}
            </n-text>
          </n-space>
        </n-card>
      </n-space>
    </n-spin>

    <!-- Create/Edit Modal -->
    <n-modal v-model:show="showModal" :title="editingJob ? t('cron.editJob') : t('cron.createJob')" preset="card" class="modal-responsive modal-scroll" style="width: 550px; max-width: 96vw;">
      <n-form label-placement="top">
        <n-form-item :label="t('cron.jobName')" required>
          <n-input v-model:value="form.name" :placeholder="t('cron.jobName')" />
        </n-form-item>
        <n-form-item :label="t('cron.cronExpression')" required>
          <n-input v-model:value="form.schedule" :placeholder="t('cron.cronExpressionPlaceholder')" />
          <template #feedback>
            <n-text v-if="form.schedule" :type="scheduleHint.type" style="font-size: 12px;">
              {{ scheduleHint.text }}
            </n-text>
          </template>
        </n-form-item>
        <n-form-item :label="t('cron.executionMode')">
          <n-radio-group v-model:value="form.no_agent">
            <n-radio :value="false">{{ t('cron.agentMode') }}</n-radio>
            <n-radio :value="true">{{ t('cron.scriptMode') }}</n-radio>
          </n-radio-group>
        </n-form-item>
        <n-form-item v-if="!form.no_agent" label="Prompt">
          <n-input v-model:value="form.prompt" type="textarea" :rows="3" :placeholder="t('cron.promptPlaceholder')" />
        </n-form-item>
        <n-form-item v-if="form.no_agent" :label="t('cron.scriptMode')">
          <n-input v-model:value="form.script" type="textarea" :rows="3" :placeholder="t('cron.scriptPlaceholder')" />
        </n-form-item>
        <n-form-item :label="t('cron.workingDir')">
          <n-input-group>
            <n-input v-model:value="form.working_dir" :placeholder="t('cron.workingDirPlaceholder')" clearable />
            <n-button :title="t('cron.workingDirBrowse')" @click="openDirPicker">
              <template #icon><n-icon><FolderOpenOutline /></n-icon></template>
              {{ t('cron.workingDirBrowse') }}
            </n-button>
          </n-input-group>
          <template #feedback>
            <n-text depth="3" style="font-size: 12px;">{{ t('cron.workingDirHint') }}</n-text>
          </template>
        </n-form-item>
        <n-form-item :label="t('cron.commonExpressions')">
          <n-space>
            <n-button size="tiny" @click="setSchedule('0 8 * * *')">{{ t('cron.daily8am') }}</n-button>
            <n-button size="tiny" @click="setSchedule('0 8 * * 1-5')">{{ t('cron.weekday8am') }}</n-button>
            <n-button size="tiny" @click="setSchedule('0 */2 * * *')">{{ t('cron.every2hours') }}</n-button>
            <n-button size="tiny" @click="setSchedule('0 0 * * *')">{{ t('cron.dailyMidnight') }}</n-button>
            <n-button size="tiny" @click="setSchedule('0 0 1 * *')">{{ t('cron.monthly1st') }}</n-button>
          </n-space>
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="saving" @click="handleSave">{{ t('common.save') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Logs Modal -->
    <n-modal v-model:show="showLogsModal" :title="t('cron.executionLogs')" preset="card" class="modal-responsive modal-scroll" style="width: 700px; max-width: 96vw;">
      <n-empty v-if="!cronStore.logs.length" :description="t('cron.noLogs')" />
      <n-timeline v-else>
        <n-timeline-item
          v-for="log in cronStore.logs"
          :key="log.id"
          :type="log.status === 'success' ? 'success' : log.status === 'failed' ? 'error' : 'info'"
          :title="log.status === 'success' ? t('cron.success') : log.status === 'failed' ? t('cron.failed') : t('cron.running')"
          :time="formatTime(log.started_at)"
        >
          <n-text v-if="log.duration" depth="3" style="font-size: 12px;">{{ t('cron.duration', { duration: log.duration }) }}</n-text>
          <n-text v-if="log.work_dir" depth="3" style="font-size: 12px; display: block; margin-top: 4px;">{{ t('cron.workingDir') }}: {{ log.work_dir }}</n-text>
          <n-text v-if="log.output" depth="3" style="font-size: 12px; display: block; margin-top: 4px; white-space: pre-wrap;">{{ log.output }}</n-text>
          <n-text v-if="log.error" type="error" style="font-size: 12px; display: block; margin-top: 4px;">{{ log.error }}</n-text>
        </n-timeline-item>
      </n-timeline>
    </n-modal>

    <!-- Directory Picker Modal（工作目录选择） -->
    <n-modal v-model:show="showDirPicker" :title="t('cron.workingDir')" preset="card" class="modal-responsive" style="width: 520px; max-width: 96vw;">
      <div v-if="recommendedDirs.length" class="dir-picker-recommended">
        <div class="dir-picker-recommended-title">{{ t('cron.workingDirRecommended') }}</div>
        <div
          v-for="d in recommendedDirs"
          :key="d"
          class="dir-picker-recommended-item"
          :title="d"
          @click="applyRecommendedDir(d)"
        >
          <n-icon size="16"><FolderOutline /></n-icon>
          <span class="dir-picker-recommended-path">{{ d }}</span>
        </div>
      </div>

      <div class="dir-picker-breadcrumb">
        <n-text class="dir-picker-current" :title="dirCurrentPath">{{ dirCurrentPath }}</n-text>
        <n-button size="tiny" quaternary :title="t('cron.newFolder')" @click="startNewFolder">
          <template #icon><n-icon><AddOutline /></n-icon></template>
        </n-button>
      </div>

      <div v-if="showNewFolderInput" class="dir-picker-new-folder">
        <n-input
          v-model:value="newFolderName"
          size="small"
          :placeholder="t('cron.newFolder')"
          @keyup.enter="createNewFolder"
          @blur="cancelNewFolder"
          ref="newFolderInputRef"
        />
      </div>

      <div class="dir-picker-list">
        <div v-if="dirLoading" class="dir-picker-loading">
          <n-spin size="small" />
        </div>
        <div v-else-if="dirEntries.length === 0" class="dir-picker-empty">
          <n-text depth="3">{{ t('cron.workingDirEmpty') }}</n-text>
        </div>
        <div
          v-for="entry in dirEntries"
          v-else
          :key="entry.path"
          class="dir-picker-item"
          @click="navigateDir(entry.path)"
        >
          <n-icon size="16"><FolderOutline /></n-icon>
          <span>{{ entry.name }}</span>
        </div>
      </div>

      <template #footer>
        <n-space justify="end">
          <n-button @click="showDirPicker = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="applyDirCurrent" :disabled="!dirCurrentPath">
            {{ t('cron.workingDirSet') }}
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, nextTick } from 'vue'
import { RefreshOutline, FolderOpenOutline, FolderOutline, AddOutline } from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import { NButton, NCard, NEmpty, NForm, NFormItem, NGi, NGrid, NIcon, NInput, NInputGroup, NModal, NRadio, NRadioGroup, NSpace, NSpin, NTag, NText, NTimeline, NTimelineItem } from 'naive-ui'
import { useCronStore } from '@/stores/cron'
import type { CronJob } from '@/api/cron'
import { getLocale } from '@/locales'
import * as sessionsApi from '@/api/sessions'

const { t } = useI18n()
const message = useMessage()
const cronStore = useCronStore()
const showModal = ref(false)
const showLogsModal = ref(false)
const editingJob = ref<CronJob | null>(null)
const saving = ref(false)

const form = reactive({
  name: '',
  schedule: '',
  prompt: '',
  script: '',
  working_dir: '',
  no_agent: false,
})

const scheduleHint = computed(() => {
  const s = form.schedule.trim()
  if (!s) return { text: '', type: 'default' as const }
  const parts = s.split(/\s+/)
  if (parts.length < 5) return { text: t('cron.scheduleHint5fields'), type: 'error' as const }
  if (parts.length > 6) return { text: t('cron.scheduleHint6fields'), type: 'error' as const }

  // Common patterns
  if (s === '0 8 * * *') return { text: t('cron.scheduleHintDaily8'), type: 'success' as const }
  if (s === '0 9 * * 1-5') return { text: t('cron.scheduleHintWeekday9'), type: 'success' as const }
  if (s === '0 */2 * * *') return { text: t('cron.scheduleHint2hours'), type: 'success' as const }
  if (s === '0 * * * *') return { text: t('cron.scheduleHintHourly'), type: 'success' as const }
  if (s === '* * * * *') return { text: t('cron.scheduleHintMinutely'), type: 'success' as const }
  if (s === '0 0 * * *') return { text: t('cron.scheduleHintMidnight'), type: 'success' as const }
  if (s === '0 0 1 * *') return { text: t('cron.scheduleHintMonthly1st'), type: 'success' as const }
  if (s === '0 0 * * 1') return { text: t('cron.scheduleHintMonday'), type: 'success' as const }

  return { text: t('cron.scheduleValid'), type: 'success' as const }
})

function stateType(state: string) {
  const map: Record<string, string> = { active: 'success', inactive: 'default', running: 'warning' }
  return (map[state] || 'default') as any
}

function stateLabel(state: string) {
  const map: Record<string, string> = { active: t('cron.stateActive'), inactive: t('cron.stateInactive'), running: t('cron.stateRunning') }
  return map[state] || state
}

function formatTime(timeStr: string) {
  if (!timeStr) return ''
  const t = new Date(timeStr)
  if (isNaN(t.getTime())) return timeStr
  const locale = getLocale() === 'zh' ? 'zh-CN' : 'en-US'
  return t.toLocaleString(locale, { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

function setSchedule(s: string) {
  form.schedule = s
}

// ===== 工作目录选择器 =====
const showDirPicker = ref(false)
const dirCurrentPath = ref('')
const dirEntries = ref<sessionsApi.DirEntry[]>([])
const dirLoading = ref(false)
const showNewFolderInput = ref(false)
const newFolderName = ref('')
const workDirHistory = ref<string[]>([])
const newFolderInputRef = ref<{ focus: () => void } | null>(null)

function normalizeDirPath(p: string): string {
  let s = (p || '').trim().replace(/[\\/]+/g, '\\').replace(/[\\]+$/, '')
  if (/^[A-Za-z]:/.test(s)) s = s.toLowerCase()
  return s
}

// 已使用过的目录作为推荐项；排除当前已选中的工作目录
const recommendedDirs = computed(() => {
  const current = normalizeDirPath(form.working_dir || '')
  return workDirHistory.value.filter(d => normalizeDirPath(d) !== current)
})

async function loadWorkDirHistory(): Promise<void> {
  try {
    workDirHistory.value = await sessionsApi.listWorkDirHistory()
  } catch (e) {
    workDirHistory.value = []
  }
}

async function loadDirs(path?: string): Promise<boolean> {
  dirLoading.value = true
  try {
    const res = await sessionsApi.listDirs(path)
    dirCurrentPath.value = res.current
    dirEntries.value = res.dirs || []
    return true
  } catch (e) {
    dirEntries.value = []
    return false
  } finally {
    dirLoading.value = false
  }
}

function navigateDir(path: string) {
  if (!path) return
  showNewFolderInput.value = false
  newFolderName.value = ''
  loadDirs(path)
}

function startNewFolder() {
  showNewFolderInput.value = true
  newFolderName.value = ''
  nextTick(() => {
    newFolderInputRef.value?.focus()
  })
}

function cancelNewFolder() {
  setTimeout(() => {
    showNewFolderInput.value = false
    newFolderName.value = ''
  }, 150)
}

async function createNewFolder() {
  const name = newFolderName.value.trim()
  if (!name) {
    showNewFolderInput.value = false
    return
  }
  try {
    await sessionsApi.createDir(dirCurrentPath.value, name)
    newFolderName.value = ''
    showNewFolderInput.value = false
    loadDirs(dirCurrentPath.value)
  } catch (e: any) {
    message.error(e?.message || t('common.operationFailed'))
  }
}

async function openDirPicker() {
  showDirPicker.value = true
  await loadWorkDirHistory()
  // 起点优先取当前已设置的工作目录，否则返回上次浏览位置；都无效时由后端取默认
  const prefer = form.working_dir.trim()
  if (prefer && (await loadDirs(prefer))) return
  await loadDirs(undefined)
}

function applyRecommendedDir(path: string) {
  form.working_dir = path
  showDirPicker.value = false
}

function applyDirCurrent() {
  if (!dirCurrentPath.value) return
  form.working_dir = dirCurrentPath.value
  showDirPicker.value = false
}

function openCreateModal() {
  editingJob.value = null
  form.name = ''
  form.schedule = ''
  form.prompt = ''
  form.script = ''
  form.working_dir = ''
  form.no_agent = false
  showModal.value = true
}

function openEditModal(job: CronJob) {
  editingJob.value = job
  form.name = job.name
  form.schedule = job.schedule
  form.prompt = job.prompt || ''
  form.script = job.script || ''
  form.working_dir = job.working_dir || ''
  form.no_agent = job.no_agent
  showModal.value = true
}

function openLogsModal(job: CronJob) {
  cronStore.loadLogs(job.id)
  showLogsModal.value = true
}

async function handleSave() {
  if (!form.name.trim()) {
    message.warning(t('cron.enterName'))
    return
  }
  if (!form.schedule.trim()) {
    message.warning(t('cron.enterSchedule'))
    return
  }

  saving.value = true
  try {
    const data: Partial<CronJob> = {
      name: form.name,
      schedule: form.schedule,
      no_agent: form.no_agent,
      working_dir: form.working_dir.trim(),
    }

    if (form.no_agent) {
      data.script = form.script
    } else {
      data.prompt = form.prompt
    }

    if (editingJob.value) {
      await cronStore.updateJob(editingJob.value.id, data)
      message.success(t('cron.jobUpdated'))
    } else {
      await cronStore.createJob(data)
      message.success(t('cron.created'))
    }
    showModal.value = false
  } catch (e: any) {
    message.error(e.message || t('common.operationFailed'))
  } finally {
    saving.value = false
  }
}

async function handleDelete(id: string) {
  await cronStore.deleteJob(id)
  message.success(t('cron.deleted'))
}

async function handleTrigger(id: string) {
  await cronStore.triggerJob(id)
  message.success(t('cron.jobTriggered'))
}

async function handlePause(id: string) {
  await cronStore.pauseJob(id)
  message.success(t('cron.jobPaused'))
}

async function handleResume(id: string) {
  await cronStore.resumeJob(id)
  message.success(t('cron.jobResumed'))
}

onMounted(() => cronStore.loadJobs())
</script>

<style scoped>
.dir-picker-recommended {
  padding: 4px 0 8px;
  border-bottom: 1px solid #f0f0f0;
  margin-bottom: 4px;
}

.dir-picker-recommended-title {
  font-size: 12px;
  color: #999;
  padding: 4px 12px 6px;
}

.dir-picker-recommended-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 12px;
  transition: background 0.15s;
}

.dir-picker-recommended-item:hover {
  background: #f0f0f0;
}

.dir-picker-recommended-path {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dir-picker-breadcrumb {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 0;
  border-bottom: 1px solid #f0f0f0;
}

.dir-picker-current {
  font-size: 12px;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: monospace;
  color: #666;
}

.dir-picker-new-folder {
  padding: 8px 0;
  border-bottom: 1px solid #f0f0f0;
}

.dir-picker-list {
  max-height: 300px;
  overflow-y: auto;
  padding: 8px 0;
}

.dir-picker-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 13px;
  transition: background 0.15s;
}

.dir-picker-item:hover {
  background: #f0f0f0;
}

.dir-picker-empty,
.dir-picker-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}
</style>