<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>{{ t('cron.title') }}</h2>
      <n-button type="primary" @click="openCreateModal">+ {{ t('cron.createJob') }}</n-button>
    </n-space>

    <n-spin :show="cronStore.loading">
      <n-empty v-if="!cronStore.jobs.length" :description="t('cron.noJobs')" />

      <n-space vertical>
        <n-card v-for="job in cronStore.jobs" :key="job.id" size="small">
          <n-space justify="space-between" align="center">
            <n-space align="center" :size="12">
              <n-tag :type="stateType(job.state)" size="small">{{ stateLabel(job.state) }}</n-tag>
              <n-text strong style="font-size: 15px;">{{ job.name }}</n-text>
              <n-text depth="3" style="font-size: 12px;">{{ job.description }}</n-text>
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
    <n-modal v-model:show="showModal" :title="editingJob ? t('cron.editJob') : t('cron.createJob')" preset="card" style="width: 550px;">
      <n-form label-placement="top">
        <n-form-item :label="t('cron.jobName')" required>
          <n-input v-model:value="form.name" :placeholder="t('cron.jobName')" />
        </n-form-item>
        <n-form-item :label="t('common.description')">
          <n-input v-model:value="form.description" :placeholder="t('cron.jobDescription')" />
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
    <n-modal v-model:show="showLogsModal" :title="t('cron.executionLogs')" preset="card" style="width: 700px;">
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
          <n-text v-if="log.output" depth="3" style="font-size: 12px; display: block; margin-top: 4px; white-space: pre-wrap;">{{ log.output }}</n-text>
          <n-text v-if="log.error" type="error" style="font-size: 12px; display: block; margin-top: 4px;">{{ log.error }}</n-text>
        </n-timeline-item>
      </n-timeline>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import { useCronStore } from '@/stores/cron'
import type { CronJob } from '@/api/cron'

const { t } = useI18n()
const message = useMessage()
const cronStore = useCronStore()
const showModal = ref(false)
const showLogsModal = ref(false)
const editingJob = ref<CronJob | null>(null)
const saving = ref(false)

const form = reactive({
  name: '',
  description: '',
  schedule: '',
  prompt: '',
  script: '',
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

function formatTime(t: string) {
  try {
    const d = new Date(t)
    return d.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', second: '2-digit' })
  } catch {
    return t
  }
}

function setSchedule(s: string) {
  form.schedule = s
}

function openCreateModal() {
  editingJob.value = null
  form.name = ''
  form.description = ''
  form.schedule = ''
  form.prompt = ''
  form.script = ''
  form.no_agent = false
  showModal.value = true
}

function openEditModal(job: CronJob) {
  editingJob.value = job
  form.name = job.name
  form.description = job.description || ''
  form.schedule = job.schedule
  form.prompt = job.prompt || ''
  form.script = job.script || ''
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
      description: form.description,
      schedule: form.schedule,
      no_agent: form.no_agent,
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
