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
        <n-form-item :label="t('cron.scheduleInputMethod')">
          <n-radio-group v-model:value="scheduleMode" size="small">
            <n-radio-button value="builder">{{ t('cron.modeBuilder') }}</n-radio-button>
            <n-radio-button value="expr">{{ t('cron.modeExpr') }}</n-radio-button>
            <n-radio-button value="nl">{{ t('cron.modeNL') }}</n-radio-button>
          </n-radio-group>
        </n-form-item>

        <!-- 可视化构造器：选频率和参数，自动生成表达式 -->
        <template v-if="scheduleMode === 'builder'">
          <n-form-item :label="t('cron.builderFrequency')">
            <n-select v-model:value="builder.type" :options="freqOptions" />
          </n-form-item>
          <n-form-item v-if="builderNeedsTime" :label="t('cron.builderTime')">
            <n-space align="center" :size="8">
              <n-input-number v-model:value="builder.hour" :min="0" :max="23" style="width: 90px" />
              <n-text strong>:</n-text>
              <n-input-number v-model:value="builder.minute" :min="0" :max="59" style="width: 90px" />
            </n-space>
          </n-form-item>
          <n-form-item v-if="builder.type === 'weekly'" :label="t('cron.builderWeekday')">
            <n-select v-model:value="builder.weekdays" multiple :options="weekdayOptions" style="min-width: 220px;" />
          </n-form-item>
          <n-form-item v-if="builder.type === 'monthly'" :label="t('cron.builderMonthDay')">
            <n-input-number v-model:value="builder.monthDay" :min="1" :max="31" style="width: 120px" />
          </n-form-item>
          <n-form-item v-if="builder.type === 'interval'" :label="t('cron.builderInterval')">
            <n-space align="center" :size="8">
              <n-text depth="3">{{ t('cron.builderEvery') }}</n-text>
              <n-input-number v-model:value="builder.interval" :min="1" :max="builder.intervalUnit === 'minute' ? 59 : 23" style="width: 90px" />
              <n-select v-model:value="builder.intervalUnit" :options="intervalUnitOptions" style="width: 100px" />
            </n-space>
          </n-form-item>
          <n-form-item :label="t('cron.cronExpression')">
            <n-input :value="form.schedule" readonly />
            <template #feedback>
              <n-text depth="3" style="font-size: 12px;">{{ form.schedule }}</n-text>
            </template>
          </n-form-item>
        </template>

        <!-- 手写表达式 -->
        <n-form-item v-else-if="scheduleMode === 'expr'" :label="t('cron.cronExpression')" required>
          <n-input v-model:value="form.schedule" :placeholder="t('cron.cronExpressionPlaceholder')" />
        </n-form-item>

        <!-- 自然语言解析 -->
        <template v-else>
          <n-form-item :label="t('cron.nlInputLabel')">
            <n-input v-model:value="nlText" type="textarea" :rows="2" :placeholder="t('cron.nlInputPlaceholder')" />
          </n-form-item>
          <n-form-item :show-feedback="false">
            <n-button size="small" type="primary" :loading="nlLoading" :disabled="!nlText.trim()" @click="parseNatural">
              {{ t('cron.nlParse') }}
            </n-button>
          </n-form-item>
          <n-form-item :label="t('cron.cronExpression')" required>
            <n-input v-model:value="form.schedule" :placeholder="t('cron.cronExpressionPlaceholder')" />
          </n-form-item>
        </template>

        <n-form-item v-if="scheduleMode === 'expr'" :label="t('cron.quickPresets')">
          <n-space>
            <n-button size="tiny" @click="setSchedule('0 8 * * *')">{{ t('cron.daily8am') }}</n-button>
            <n-button size="tiny" @click="setSchedule('0 8 * * 1-5')">{{ t('cron.weekday8am') }}</n-button>
            <n-button size="tiny" @click="setSchedule('0 */2 * * *')">{{ t('cron.every2hours') }}</n-button>
            <n-button size="tiny" @click="setSchedule('0 0 * * *')">{{ t('cron.dailyMidnight') }}</n-button>
            <n-button size="tiny" @click="setSchedule('0 0 1 * *')">{{ t('cron.monthly1st') }}</n-button>
          </n-space>
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
import { ref, reactive, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { RefreshOutline, FolderOpenOutline, FolderOutline, AddOutline } from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import { useMessage, useDialog } from 'naive-ui'
import { NButton, NCard, NEmpty, NForm, NFormItem, NGi, NGrid, NIcon, NInput, NInputGroup, NInputNumber, NModal, NRadio, NRadioButton, NRadioGroup, NSelect, NSpace, NSpin, NTag, NText, NTimeline, NTimelineItem } from 'naive-ui'
import { useCronStore } from '@/stores/cron'
import { useConfigStore } from '@/stores/config'
import type { CronJob } from '@/api/cron'
import { getCronNextRuns, parseCronNatural } from '@/api/cron'
import { getLocale } from '@/locales'
import * as sessionsApi from '@/api/sessions'

const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()
const cronStore = useCronStore()
const configStore = useConfigStore()
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

// ===== 调度输入三模式：可视化构造器 / 手写表达式 / 自然语言 =====
type ScheduleMode = 'builder' | 'expr' | 'nl'
const scheduleMode = ref<ScheduleMode>('builder')
const nlText = ref('')
const nlLoading = ref(false)

const builder = reactive({
  type: 'daily',              // daily | weekdays | weekly | monthly | interval
  hour: 9,
  minute: 0,
  weekdays: [1] as number[],  // 0=周日 1=周一 ... 6=周六
  monthDay: 1,
  interval: 30,
  intervalUnit: 'minute',     // minute | hour
})

const freqOptions = computed(() => [
  { label: t('cron.freqDaily'), value: 'daily' },
  { label: t('cron.freqWeekdays'), value: 'weekdays' },
  { label: t('cron.freqWeekly'), value: 'weekly' },
  { label: t('cron.freqMonthly'), value: 'monthly' },
  { label: t('cron.freqInterval'), value: 'interval' },
])

const intervalUnitOptions = computed(() => [
  { label: t('cron.intervalMinute'), value: 'minute' },
  { label: t('cron.intervalHour'), value: 'hour' },
])

const weekdayOptions = computed(() => [
  { label: t('cron.weekdayMon'), value: 1 },
  { label: t('cron.weekdayTue'), value: 2 },
  { label: t('cron.weekdayWed'), value: 3 },
  { label: t('cron.weekdayThu'), value: 4 },
  { label: t('cron.weekdayFri'), value: 5 },
  { label: t('cron.weekdaySat'), value: 6 },
  { label: t('cron.weekdaySun'), value: 0 },
])

// daily/weekdays/weekly/monthly 需要选时刻
const builderNeedsTime = computed(() => builder.type !== 'interval')

// 构造器 → cron 表达式（5 字段）
const builderExpr = computed(() => {
  const m = Math.max(0, Math.min(59, builder.minute))
  const h = Math.max(0, Math.min(23, builder.hour))
  switch (builder.type) {
    case 'daily':
      return `${m} ${h} * * *`
    case 'weekdays':
      return `${m} ${h} * * 1-5`
    case 'weekly': {
      const dows = [...new Set(builder.weekdays)].sort((a, b) => a - b)
      return `${m} ${h} * * ${dows.length ? dows.join(',') : '*'}`
    }
    case 'monthly':
      return `${m} ${h} ${Math.max(1, Math.min(31, builder.monthDay))} * *`
    case 'interval': {
      const n = Math.max(1, builder.interval)
      return builder.intervalUnit === 'hour'
        ? `0 */${n} * * *`
        : (n <= 1 ? '* * * * *' : `*/${n} * * * *`)
    }
    default:
      return ''
  }
})

// 构造器参数变化同步回 form.schedule（仅 builder 模式）
watch(builderExpr, (expr) => {
  if (scheduleMode.value === 'builder' && expr) {
    form.schedule = expr
  }
})
// 切到 builder 模式时也立即同步一次
watch(scheduleMode, (mode) => {
  if (mode === 'builder' && builderExpr.value) {
    form.schedule = builderExpr.value
  }
})

// 尝试把已有表达式反解析成构造器参数；解析不了返回 false（回退表达式模式）
function parseScheduleToBuilder(schedule: string): boolean {
  const parts = schedule.trim().split(/\s+/)
  if (parts.length !== 5) return false
  const [min, hour, dom, mon, dow] = parts

  if (min.startsWith('*/') && hour === '*' && dom === '*' && mon === '*' && dow === '*') {
    const n = parseInt(min.slice(2), 10)
    if (!Number.isFinite(n) || n < 1 || n > 59) return false
    builder.type = 'interval'; builder.interval = n; builder.intervalUnit = 'minute'
    return true
  }
  if (min === '0' && hour.startsWith('*/') && dom === '*' && mon === '*' && dow === '*') {
    const n = parseInt(hour.slice(2), 10)
    if (!Number.isFinite(n) || n < 1 || n > 23) return false
    builder.type = 'interval'; builder.interval = n; builder.intervalUnit = 'hour'
    return true
  }
  const m = parseInt(min, 10), h = parseInt(hour, 10)
  if (!Number.isFinite(m) || !Number.isFinite(h) || m > 59 || h > 23 || mon !== '*') return false

  if (dom === '*' && dow === '*') {
    builder.type = 'daily'; builder.minute = m; builder.hour = h
    return true
  }
  if (dom === '*' && dow === '1-5') {
    builder.type = 'weekdays'; builder.minute = m; builder.hour = h
    return true
  }
  if (dom === '*' && /^\d+(,\d+)*$/.test(dow)) {
    const dows = dow.split(',').map(Number)
    if (dows.some(d => d > 7)) return false
    builder.type = 'weekly'; builder.minute = m; builder.hour = h
    builder.weekdays = dows.map(d => (d === 7 ? 0 : d))
    return true
  }
  if (dow === '*' && /^\d+$/.test(dom)) {
    const d = parseInt(dom, 10)
    if (d < 1 || d > 31) return false
    builder.type = 'monthly'; builder.minute = m; builder.hour = h; builder.monthDay = d
    return true
  }
  return false
}

// ===== 自然语言解析 =====
async function parseNatural() {
  const text = nlText.value.trim()
  if (!text) return
  nlLoading.value = true
  try {
    const res = await parseCronNatural(text, getLocale())
    form.schedule = res.schedule
    if (res.name && !form.name.trim()) form.name = res.name
    if (res.prompt && !form.no_agent && !form.prompt.trim()) form.prompt = res.prompt
    message.success(t('cron.nlParsed'))
  } catch (e: any) {
    message.error(e?.message || t('common.operationFailed'))
  } finally {
    nlLoading.value = false
  }
}

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

// 配置页「通用」中设置的全局工作目录（后端 cfg.WorkingDir）。选择器打开时
// 作为默认起点，与 chat 页的选择器行为一致。
const globalWorkDir = computed(() => configStore.config?.working_dir || '')

// 配置按需拉取（首次打开选择器时），避免页面每次进入都多一个请求。
async function ensureConfigLoaded(): Promise<void> {
  if (configStore.config) return
  try {
    await configStore.loadConfig()
  } catch {
    // 拉取失败不影响选择器：起点退回后端默认
  }
}

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
  // 起点优先取当前已设置的工作目录（编辑场景），否则取配置页设置的全局
  // 工作目录——与 chat 页选择器一致。全局目录不可读时再退回后端默认。
  await Promise.all([loadWorkDirHistory(), ensureConfigLoaded()])
  const prefer = form.working_dir.trim() || globalWorkDir.value
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
  nlText.value = ''
  // 新建默认进入构造器模式（每天 09:00），降低手写表达式的门槛
  builder.type = 'daily'
  builder.hour = 9
  builder.minute = 0
  scheduleMode.value = 'builder'
  form.schedule = builderExpr.value
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
  nlText.value = ''
  // 已有表达式能反解析就进构造器，否则回退表达式模式
  scheduleMode.value = parseScheduleToBuilder(job.schedule) ? 'builder' : 'expr'
  if (scheduleMode.value === 'builder') form.schedule = builderExpr.value
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
  // 保存前用后端真实校验兜底（前端字段数检查挡不住 99 99 * * * 这类值）
  try {
    const check = await getCronNextRuns(form.schedule.trim(), 1, getLocale())
    if (!check.valid) {
      message.error(t('cron.scheduleInvalid', { error: check.error || 'invalid' }))
      return
    }
  } catch {
    // 预览接口失败不阻塞保存，后端创建时仍会校验
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

function handleDelete(id: string) {
  const job = cronStore.jobs.find(j => j.id === id)
  // 删除不可恢复（任务定义连同执行历史一起删），必须二次确认
  dialog.warning({
    title: t('cron.deleteConfirmTitle'),
    content: t('cron.deleteConfirmContent', { name: job?.name || id }),
    positiveText: t('common.delete'),
    negativeText: t('common.cancel'),
    onPositiveClick: async () => {
      const ok = await cronStore.deleteJob(id)
      if (ok) message.success(t('cron.deleted'))
    },
  })
}

// ===== 立即执行的状态跟踪 =====
// trigger 接口只返回"已触发"，结果要等 agent 跑完。轮询任务状态，
// running 结束后把成功/失败反馈给用户，避免"点了没反应"。
let statusPollTimer: number | null = null

function startStatusPolling() {
  if (statusPollTimer) return
  let polls = 0
  statusPollTimer = window.setInterval(async () => {
    polls++
    await cronStore.loadJobs(true)
    const running = cronStore.jobs.some(j => j.state === 'running')
    if (!running || polls >= 60) {
      stopStatusPolling()
      const failed = cronStore.jobs.filter(j => j.last_status === 'failed' && j.last_error)
      if (failed.length) {
        message.error(t('cron.jobFailedNotify', { name: failed[0].name, error: (failed[0].last_error || '').slice(0, 160) }))
      } else if (!running) {
        message.success(t('cron.jobFinished'))
      }
    }
  }, 2000)
}

function stopStatusPolling() {
  if (statusPollTimer) {
    window.clearInterval(statusPollTimer)
    statusPollTimer = null
  }
}

onUnmounted(stopStatusPolling)

async function handleTrigger(id: string) {
  const ok = await cronStore.triggerJob(id)
  if (!ok) return
  message.info(t('cron.jobTriggered'))
  // 稍等片刻让状态落库为 running，再开始轮询
  window.setTimeout(() => startStatusPolling(), 500)
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