<template>
  <div class="cron-page">
    <!-- 页头：一句话概览 + 操作 -->
    <header class="cron-head">
      <div class="cron-head-text">
        <h2 class="cron-title">{{ t('cron.title') }}</h2>
        <p class="cron-subtitle">{{ summaryText }}</p>
      </div>
      <n-space align="center" :size="8">
        <n-button quaternary circle :loading="cronStore.loading" :title="t('common.refresh')" @click="cronStore.loadJobs()">
          <template #icon><n-icon><RefreshOutline /></n-icon></template>
        </n-button>
        <n-button type="primary" @click="openCreateModal">
          <template #icon><n-icon><AddOutline /></n-icon></template>
          {{ t('cron.createJob') }}
        </n-button>
      </n-space>
    </header>

    <n-spin :show="cronStore.loading">
      <!-- 空态 -->
      <div v-if="!cronStore.jobs.length" class="cron-empty">
        <div class="cron-empty-icon"><n-icon size="28"><CalendarOutline /></n-icon></div>
        <div class="cron-empty-title">{{ t('cron.emptyTitle') }}</div>
        <div class="cron-empty-hint">{{ t('cron.emptyHint') }}</div>
        <n-button type="primary" size="small" style="margin-top: 16px;" @click="openCreateModal">
          + {{ t('cron.createJob') }}
        </n-button>
      </div>

      <!-- 任务列表 -->
      <div v-else class="job-list">
        <article
          v-for="job in cronStore.jobs"
          :key="job.id"
          class="job-card"
          :class="{ 'is-paused': job.state === 'inactive' }"
        >
          <span class="job-rail" :class="railClass(job)"></span>

          <div class="job-body">
            <div class="job-top">
              <div class="job-name-wrap">
                <span class="job-name">{{ job.name }}</span>
                <span class="job-badge" :class="stateClass(job.state)">
                  <span v-if="job.state === 'running'" class="pulse-dot"></span>
                  {{ stateLabel(job.state) }}
                </span>
                <span class="job-mode">{{ job.no_agent ? t('cron.scriptModeLabel') : t('cron.agentModeLabel') }}</span>
              </div>

              <div class="job-actions">
                <n-button size="tiny" secondary @click="openLogs(job)">
                  <template #icon><n-icon><ListOutline /></n-icon></template>
                  {{ t('cron.logs') }}
                </n-button>
                <n-button size="tiny" type="primary" ghost @click="handleTrigger(job.id)">
                  {{ t('cron.runNow') }}
                </n-button>
                <n-button size="tiny" quaternary :title="t('common.edit')" @click="openEditModal(job)">
                  <template #icon><n-icon><CreateOutline /></n-icon></template>
                </n-button>
                <n-dropdown trigger="click" :options="moreOptions(job)" @select="handleMore($event, job)">
                  <n-button size="tiny" quaternary>
                    <template #icon><n-icon><EllipsisHorizontalOutline /></n-icon></template>
                  </n-button>
                </n-dropdown>
              </div>
            </div>

            <div class="job-schedule-row">
              <n-icon size="14" class="job-schedule-icon"><TimeOutline /></n-icon>
              <span class="job-schedule-text">{{ job.schedule_display }}</span>
              <code class="job-cron">{{ job.schedule }}</code>
            </div>

            <p v-if="job.prompt || job.script" class="job-prompt">
              <span class="job-prompt-tag">{{ job.no_agent ? '$' : '›' }}</span>{{ job.prompt || job.script }}
            </p>

            <div class="job-meta">
              <span class="meta-item">
                <span class="meta-label">{{ t('cron.nextRun') }}</span>
                <span class="meta-value">{{ job.state === 'inactive' ? '—' : (job.next_run_at ? formatTime(job.next_run_at) : '-') }}</span>
                <span v-if="job.state !== 'inactive' && job.next_run_at" class="meta-hint">{{ relativeTo(job.next_run_at) }}</span>
              </span>
              <span class="meta-item">
                <span class="meta-label">{{ t('cron.previousRun') }}</span>
                <span class="meta-value">{{ job.last_run_at ? relativePast(job.last_run_at) : t('cron.neverRun') }}</span>
                <span v-if="job.last_status" class="run-chip" :class="statusClass(job.last_status)">{{ statusLabel(job.last_status) }}</span>
              </span>
              <span class="meta-item">
                <span class="meta-label">{{ t('cron.runCount') }}</span>
                <span class="meta-value">{{ job.run_count }}</span>
              </span>
              <span v-if="job.working_dir" class="meta-item meta-dir" :title="job.working_dir">
                <n-icon size="13"><FolderOutline /></n-icon>
                <span class="meta-value mono">{{ job.working_dir }}</span>
              </span>
            </div>

            <div v-if="job.last_error" class="job-error">
              <n-icon size="14"><AlertCircleOutline /></n-icon>
              <span class="job-error-text">{{ job.last_error }}</span>
            </div>
          </div>
        </article>
      </div>
    </n-spin>

    <!-- ===== 执行记录抽屉：宽屏左右分栏，手机端单列切换 ===== -->
    <n-drawer v-model:show="showLogsDrawer" placement="right" :width="drawerWidth" class="cron-logs-drawer">
      <n-drawer-content
        closable
        :native-scrollbar="false"
        body-content-style="padding: 0; display: flex; flex-direction: column; overflow: hidden;"
      >
        <template #header>
          <div class="logs-head">
            <span class="logs-head-title">{{ activeJob?.name || t('cron.executionLogs') }}</span>
            <span v-if="activeJob" class="logs-head-sub">{{ activeJob.schedule_display }}</span>
          </div>
        </template>

        <div class="logs-layout">
          <!-- 左：运行历史（手机端在详情态隐藏） -->
          <aside class="run-list" :class="{ 'is-hidden': isNarrow && mobileShowDetail }">
            <div class="run-list-tools">
              <n-input v-model:value="logQuery" size="small" clearable :placeholder="t('cron.searchRuns')">
                <template #prefix><n-icon size="14"><SearchOutline /></n-icon></template>
              </n-input>
              <div class="run-filter">
                <button
                  v-for="f in filters"
                  :key="f.value"
                  type="button"
                  class="run-filter-btn"
                  :class="{ active: logFilter === f.value }"
                  @click="logFilter = f.value"
                >
                  {{ f.label }}<span class="run-filter-count">{{ f.count }}</span>
                </button>
              </div>
            </div>

            <div class="run-list-scroll">
              <div
                v-for="log in filteredLogs"
                :key="log.id"
                class="run-item"
                :class="{ active: log.id === selectedLogId }"
                @click="selectLog(log.id)"
              >
                <span class="run-dot" :class="statusClass(log.status)"></span>
                <div class="run-item-body">
                  <div class="run-item-row">
                    <span class="run-item-time">{{ formatShort(log.started_at) }}</span>
                    <span class="run-item-dur">{{ formatDuration(log.duration) || t('cron.running') }}</span>
                  </div>
                  <div class="run-item-preview">{{ previewOf(log) }}</div>
                </div>
                <n-icon v-if="isNarrow" class="run-item-chevron" size="14"><ChevronForwardOutline /></n-icon>
              </div>
              <div v-if="!filteredLogs.length" class="run-list-empty">
                {{ cronStore.logs.length ? t('cron.noMatchedRuns') : t('cron.noLogs') }}
              </div>
            </div>
          </aside>

          <!-- 右：单次运行详情（手机端仅详情态显示） -->
          <section class="run-detail" :class="{ 'is-hidden': isNarrow && !mobileShowDetail }">
            <div v-if="isNarrow" class="mobile-detail-nav">
              <button type="button" class="back-btn" @click="mobileShowDetail = false">
                <n-icon size="15"><ChevronBackOutline /></n-icon>
                {{ t('cron.backToRuns') }}
              </button>
            </div>
            <template v-if="activeLog">
              <div class="detail-head">
                <div class="detail-badges">
                  <span class="run-chip" :class="statusClass(activeLog.status)">{{ statusLabel(activeLog.status) }}</span>
                  <span class="detail-meta-item">{{ t('cron.startedAt') }} {{ formatTime(activeLog.started_at) }}</span>
                  <span v-if="activeLog.duration" class="detail-meta-item">{{ t('cron.durationLabel') }} {{ formatDuration(activeLog.duration) }}</span>
                  <span v-if="activeLog.trigger" class="detail-meta-item">{{ activeLog.trigger === 'manual' ? t('cron.triggerManual') : t('cron.triggerSchedule') }}</span>
                </div>
                <n-button v-if="activeLog.output || activeLog.error" size="tiny" quaternary @click="copyOutput">
                  <template #icon><n-icon><CopyOutline /></n-icon></template>
                  {{ copied ? t('cron.copied') : t('cron.copyOutput') }}
                </n-button>
              </div>

              <div v-if="activeLog.work_dir" class="detail-workdir">
                <n-icon size="13"><FolderOpenOutline /></n-icon>
                <code class="detail-workdir-path" :title="activeLog.work_dir">{{ activeLog.work_dir }}</code>
                <button type="button" class="link-btn" @click="copyText(activeLog.work_dir, t('cron.copied'))">{{ t('cron.copyPath') }}</button>
              </div>

              <div ref="detailScrollRef" class="detail-scroll">
                <div v-if="activeLog.error" class="detail-error">
                  <div class="detail-error-title">
                    <n-icon size="15"><AlertCircleOutline /></n-icon>{{ t('cron.errorTitle') }}
                  </div>
                  <pre>{{ activeLog.error }}</pre>
                </div>

                <div v-if="activeLog.status === 'running'" class="detail-running">
                  <n-spin size="small" />
                  <span>{{ t('cron.runningHint') }}</span>
                </div>

                <MarkdownBlock v-if="activeLog.output" :text="activeLog.output" />

                <div v-if="isTruncated(activeLog)" class="detail-truncated">{{ t('cron.outputTruncated') }}</div>
                <div
                  v-if="!activeLog.output && !activeLog.error && activeLog.status !== 'running'"
                  class="detail-nothing"
                >{{ t('cron.noOutput') }}</div>
              </div>
            </template>

            <div v-else class="detail-placeholder">
              <n-icon size="26"><DocumentTextOutline /></n-icon>
              <span>{{ t('cron.selectRunHint') }}</span>
            </div>
          </section>
        </div>
      </n-drawer-content>
    </n-drawer>

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
import {
  RefreshOutline, FolderOpenOutline, FolderOutline, AddOutline, CalendarOutline,
  ListOutline, CreateOutline, EllipsisHorizontalOutline, TimeOutline, SearchOutline,
  CopyOutline, AlertCircleOutline, DocumentTextOutline, ChevronBackOutline, ChevronForwardOutline,
} from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import { useMessage, useDialog } from 'naive-ui'
import type { DropdownOption } from 'naive-ui'
import {
  NButton, NDrawer, NDrawerContent, NDropdown, NForm, NFormItem, NIcon,
  NInput, NInputGroup, NInputNumber, NModal, NRadio, NRadioButton, NRadioGroup,
  NSelect, NSpace, NSpin, NText,
} from 'naive-ui'
import { useCronStore } from '@/stores/cron'
import { useConfigStore } from '@/stores/config'
import type { CronJob, ExecutionLog } from '@/api/cron'
import { getCronNextRuns, parseCronNatural } from '@/api/cron'
import { getLocale } from '@/locales'
import MarkdownBlock from '@/components/MarkdownBlock.vue'
import * as sessionsApi from '@/api/sessions'

const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()
const cronStore = useCronStore()
const configStore = useConfigStore()
const showModal = ref(false)
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

// ===== 页头概览 =====
const summaryText = computed(() => {
  const jobs = cronStore.jobs
  if (!jobs.length) return t('cron.emptyHint')
  const active = jobs.filter(j => j.state === 'active' || j.state === 'running').length
  const running = jobs.filter(j => j.state === 'running').length
  const failed = jobs.filter(j => j.last_status === 'failed').length
  let s = t('cron.summary', { total: jobs.length, active, paused: jobs.length - active })
  if (running) s += ' · ' + t('cron.summaryRunning', { n: running })
  if (failed) s += ' · ' + t('cron.summaryFailed', { n: failed })
  return s
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

// ===== 状态与时间格式化 =====
function stateLabel(state: string) {
  const map: Record<string, string> = { active: t('cron.stateActive'), inactive: t('cron.stateInactive'), running: t('cron.stateRunning') }
  return map[state] || state
}

function stateClass(state: string) {
  if (state === 'running') return 'st-running'
  if (state === 'inactive') return 'st-idle'
  return 'st-active'
}

function statusLabel(s: string) {
  if (s === 'success') return t('cron.success')
  if (s === 'failed') return t('cron.failed')
  if (s === 'running') return t('cron.running')
  return s || '-'
}

function statusClass(s: string) {
  if (s === 'success') return 'c-ok'
  if (s === 'failed') return 'c-fail'
  if (s === 'running') return 'c-run'
  return 'c-idle'
}

// 卡片左侧色条：失败优先于"启用中"，一眼能看出这条任务出过事
function railClass(job: CronJob) {
  if (job.state === 'running') return 'rail-running'
  if (job.last_status === 'failed') return 'rail-failed'
  if (job.state === 'inactive') return 'rail-paused'
  return 'rail-active'
}

function formatTime(timeStr?: string) {
  if (!timeStr) return ''
  const d = new Date(timeStr)
  if (isNaN(d.getTime())) return timeStr
  const locale = getLocale() === 'zh' ? 'zh-CN' : 'en-US'
  return d.toLocaleString(locale, { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' })
}

// 运行列表里空间小，只给 月/日 时:分；跨年时补年份
function formatShort(timeStr?: string) {
  if (!timeStr) return ''
  const d = new Date(timeStr)
  if (isNaN(d.getTime())) return timeStr
  const pad = (n: number) => String(n).padStart(2, '0')
  const sameYear = d.getFullYear() === new Date().getFullYear()
  const datePart = sameYear ? `${pad(d.getMonth() + 1)}/${pad(d.getDate())}` : `${d.getFullYear()}/${pad(d.getMonth() + 1)}/${pad(d.getDate())}`
  return `${datePart} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

const REL_MIN = 60_000
const REL_HOUR = 3_600_000
const REL_DAY = 86_400_000

// 过去的时刻 → "3 分钟前 / 2 小时前 / 4 天前"
function relativePast(timeStr?: string) {
  if (!timeStr) return ''
  const d = new Date(timeStr)
  if (isNaN(d.getTime())) return timeStr
  const diff = Date.now() - d.getTime()
  if (diff < REL_MIN) return t('cron.justNow')
  if (diff < REL_HOUR) return t('cron.minutesAgo', { n: Math.floor(diff / REL_MIN) })
  if (diff < REL_DAY) return t('cron.hoursAgo', { n: Math.floor(diff / REL_HOUR) })
  if (diff < 7 * REL_DAY) return t('cron.daysAgo', { n: Math.floor(diff / REL_DAY) })
  return formatTime(timeStr)
}

// 未来的时刻 → "12 分钟后 / 3 小时后 / 2 天后"
function relativeTo(timeStr?: string) {
  if (!timeStr) return ''
  const d = new Date(timeStr)
  if (isNaN(d.getTime())) return ''
  const diff = d.getTime() - Date.now()
  if (diff <= 0) return t('cron.soon')
  if (diff < REL_HOUR) return t('cron.inMinutes', { n: Math.max(1, Math.round(diff / REL_MIN)) })
  if (diff < REL_DAY) return t('cron.inHours', { n: Math.round(diff / REL_HOUR) })
  return t('cron.inDays', { n: Math.round(diff / REL_DAY) })
}

// Go 的 duration.String() 形如 "27.5260752s" / "1m12.5s" / "350ms"，
// 直接展示对人不友好；解析成毫秒再按量级挑精度。
function parseGoDuration(d: string): number | null {
  const re = /(\d+(?:\.\d+)?)(ns|us|µs|ms|s|m|h)/g
  const units: Record<string, number> = { ns: 1e-6, us: 1e-3, 'µs': 1e-3, ms: 1, s: 1000, m: 60000, h: 3600000 }
  let total = 0
  let found = false
  let m: RegExpExecArray | null
  while ((m = re.exec(d)) !== null) {
    total += parseFloat(m[1]) * units[m[2]]
    found = true
  }
  return found ? total : null
}

function formatDuration(d?: string) {
  if (!d) return ''
  const ms = parseGoDuration(d)
  if (ms === null) return d
  if (ms < 1000) return `${Math.round(ms)}ms`
  if (ms < REL_MIN) {
    const sec = ms / 1000
    return `${sec < 10 ? sec.toFixed(1) : Math.round(sec)}s`
  }
  const totalSec = Math.round(ms / 1000)
  const mm = Math.floor(totalSec / 60)
  const ss = totalSec % 60
  if (mm < 60) return ss ? `${mm}m ${ss}s` : `${mm}m`
  return `${Math.floor(mm / 60)}h ${mm % 60}m`
}

function setSchedule(s: string) {
  form.schedule = s
}

// ===== 执行记录抽屉 =====
const showLogsDrawer = ref(false)
const activeJob = ref<CronJob | null>(null)
const selectedLogId = ref('')
type LogFilter = 'all' | 'success' | 'failed'
const logFilter = ref<LogFilter>('all')
const logQuery = ref('')
const copied = ref(false)
const detailScrollRef = ref<HTMLElement | null>(null)
let copiedTimer: number | null = null

const viewportWidth = ref(window.innerWidth)
function onResize() { viewportWidth.value = window.innerWidth }
// 断点与 App.vue 的移动端规范保持一致（768px）
const isNarrow = computed(() => viewportWidth.value <= 768)
const drawerWidth = computed(() => (viewportWidth.value <= 880 ? '100%' : '1000px'))

// 手机端抽屉只有一列：默认看运行历史，点某次运行切到详情，
// 详情顶部给返回按钮。桌面端两栏并存，这个标记不起作用。
const mobileShowDetail = ref(false)

const filters = computed<{ value: LogFilter; label: string; count: number }[]>(() => [
  { value: 'all', label: t('cron.filterAll'), count: cronStore.logs.length },
  { value: 'success', label: t('cron.filterSuccess'), count: cronStore.logs.filter(l => l.status === 'success').length },
  { value: 'failed', label: t('cron.filterFailed'), count: cronStore.logs.filter(l => l.status === 'failed').length },
])

const filteredLogs = computed(() => {
  const q = logQuery.value.trim().toLowerCase()
  return cronStore.logs.filter(l => {
    if (logFilter.value !== 'all' && l.status !== logFilter.value) return false
    if (!q) return true
    return (l.output || '').toLowerCase().includes(q) || (l.error || '').toLowerCase().includes(q)
  })
})

const activeLog = computed<ExecutionLog | null>(
  () => cronStore.logs.find(l => l.id === selectedLogId.value) || null
)

// 筛掉当前选中的那条时（换筛选/搜索），自动落到结果里的第一条
watch(filteredLogs, (list) => {
  if (!list.some(l => l.id === selectedLogId.value)) {
    selectedLogId.value = list[0]?.id || ''
  }
})

function selectLog(id: string) {
  selectedLogId.value = id
  if (isNarrow.value) mobileShowDetail.value = true
  nextTick(() => {
    if (detailScrollRef.value) detailScrollRef.value.scrollTop = 0
  })
}

// 列表里的一行摘要：优先错误，其次输出首个非空行（去掉 markdown 记号）
function previewOf(log: ExecutionLog) {
  if (log.status === 'running') return t('cron.runningHint')
  const src = log.error || log.output || ''
  if (!src) return t('cron.noOutput')
  const line = src.split('\n').map(s => s.trim()).find(s => s.length > 0) || ''
  return line.replace(/^[#>*\-\s]+/, '').replace(/\*\*/g, '').slice(0, 120)
}

// 后端把超长输出截到 2000 字符并以 ... 结尾，命中时提示去哪里看全文
function isTruncated(log: ExecutionLog) {
  const out = log.output || ''
  return out.length >= 2000 && out.endsWith('...')
}

async function copyText(text: string, okMsg?: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.success(okMsg || t('common.copied'))
  } catch {
    message.error(t('common.copyFailed'))
  }
}

async function copyOutput() {
  const log = activeLog.value
  if (!log) return
  const text = log.output || log.error || ''
  if (!text) return
  try {
    await navigator.clipboard.writeText(text)
    copied.value = true
    if (copiedTimer) window.clearTimeout(copiedTimer)
    copiedTimer = window.setTimeout(() => { copied.value = false }, 1500)
  } catch {
    message.error(t('common.copyFailed'))
  }
}

// 打开面板时若任务正在跑（或历史里有 running 记录），按 4s 轮询刷新，
// 跑完后自动停——否则用户看到的是永远停在"执行中"的旧快照。
let logsPollTimer: number | null = null

function startLogsPolling() {
  stopLogsPolling()
  logsPollTimer = window.setInterval(async () => {
    const job = activeJob.value
    if (!job || !showLogsDrawer.value) { stopLogsPolling(); return }
    const running = job.state === 'running' || cronStore.logs.some(l => l.status === 'running')
    if (!running) { stopLogsPolling(); return }
    await Promise.all([cronStore.loadLogs(job.id, 50), cronStore.loadJobs(true)])
  }, 4000)
}

function stopLogsPolling() {
  if (logsPollTimer) {
    window.clearInterval(logsPollTimer)
    logsPollTimer = null
  }
}

async function openLogs(job: CronJob) {
  activeJob.value = job
  selectedLogId.value = ''
  logFilter.value = 'all'
  logQuery.value = ''
  mobileShowDetail.value = false
  showLogsDrawer.value = true
  await cronStore.loadLogs(job.id, 50)
  selectedLogId.value = cronStore.logs[0]?.id || ''
  startLogsPolling()
}

watch(showLogsDrawer, (show) => {
  if (!show) {
    stopLogsPolling()
    activeJob.value = null
    mobileShowDetail.value = false
  }
})

onMounted(() => {
  window.addEventListener('resize', onResize)
})
onUnmounted(() => {
  window.removeEventListener('resize', onResize)
  if (copiedTimer) window.clearTimeout(copiedTimer)
})

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

// ===== 任务操作 =====
const moreOptions = (job: CronJob): DropdownOption[] => [
  job.state === 'inactive'
    ? { label: t('cron.resume'), key: 'resume' }
    : { label: t('cron.pause'), key: 'pause' },
  { label: t('common.edit'), key: 'edit' },
  { type: 'divider', key: 'd1' },
  { label: t('common.delete'), key: 'delete' },
]

async function handleMore(key: string | number, job: CronJob) {
  if (key === 'pause') {
    await handlePause(job.id)
  } else if (key === 'resume') {
    await handleResume(job.id)
  } else if (key === 'edit') {
    openEditModal(job)
  } else if (key === 'delete') {
    handleDelete(job.id)
  }
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
/* ===== 页头 ===== */
.cron-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  margin-bottom: 18px;
  flex-wrap: wrap;
}

.cron-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  color: #1f2329;
  letter-spacing: 0.2px;
}

.cron-subtitle {
  margin: 4px 0 0;
  font-size: 12.5px;
  color: #9aa1ab;
}

/* ===== 空态 ===== */
.cron-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  padding: 56px 20px;
  border: 1px dashed #e3e6ec;
  border-radius: 12px;
  background: #fbfcfd;
}

.cron-empty-icon {
  width: 52px;
  height: 52px;
  display: flex;
  align-items: center;
  justify-content: center;
  border-radius: 14px;
  background: #eef2f8;
  color: #7c8798;
  margin-bottom: 14px;
}

.cron-empty-title { font-size: 14px; font-weight: 600; color: #41464f; }
.cron-empty-hint { font-size: 12.5px; color: #9aa1ab; margin-top: 6px; text-align: center; max-width: 420px; line-height: 1.7; }

/* ===== 任务卡片 ===== */
.job-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.job-card {
  position: relative;
  background: #fff;
  border: 1px solid #ebeef3;
  border-radius: 10px;
  padding: 14px 16px 13px 20px;
  transition: border-color 0.16s, box-shadow 0.16s;
}

.job-card:hover {
  border-color: #dde3ec;
  box-shadow: 0 4px 14px rgba(31, 35, 41, 0.055);
}

.job-card.is-paused { background: #fcfcfd; }
.job-card.is-paused .job-name { color: #6b7280; }

.job-rail {
  position: absolute;
  left: 0;
  top: 12px;
  bottom: 12px;
  width: 3px;
  border-radius: 0 3px 3px 0;
  background: #d9dee6;
}
.job-rail.rail-active { background: #18a058; }
.job-rail.rail-running { background: #f0a020; }
.job-rail.rail-failed { background: #d03050; }
.job-rail.rail-paused { background: #c8cdd6; }

.job-top {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 12px;
  flex-wrap: wrap;
}

.job-name-wrap {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  min-width: 0;
}

.job-name {
  font-size: 14.5px;
  font-weight: 600;
  color: #1f2329;
}

.job-badge {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  font-size: 11px;
  line-height: 16px;
  padding: 1px 7px;
  border-radius: 10px;
  font-weight: 500;
}
.job-badge.st-active { color: #14864b; background: #e9f8ef; }
.job-badge.st-running { color: #b5701a; background: #fff5e6; }
.job-badge.st-idle { color: #7a8290; background: #f2f4f7; }

.pulse-dot {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: #f0a020;
  animation: pulse 1.2s ease-in-out infinite;
}
@keyframes pulse {
  0%, 100% { opacity: 1; transform: scale(1); }
  50% { opacity: 0.35; transform: scale(0.8); }
}

.job-mode {
  font-size: 11px;
  line-height: 16px;
  padding: 1px 7px;
  border-radius: 4px;
  color: #7a8290;
  background: #f4f6f9;
}

.job-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
}

.job-schedule-row {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-top: 7px;
  font-size: 12.5px;
  color: #5c6169;
  flex-wrap: wrap;
}

.job-schedule-icon { color: #a8aeb8; }

.job-schedule-text { font-weight: 500; }

.job-cron {
  font-family: 'SF Mono', 'Consolas', 'Menlo', monospace;
  font-size: 11px;
  color: #7a8290;
  background: #f5f7fa;
  padding: 1px 6px;
  border-radius: 4px;
}

.job-prompt {
  margin: 8px 0 0;
  font-size: 12.5px;
  line-height: 1.65;
  color: #8a919e;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  word-break: break-word;
}

.job-prompt-tag {
  color: #b6bcc6;
  margin-right: 5px;
  font-family: 'SF Mono', 'Consolas', monospace;
}

.job-meta {
  display: flex;
  align-items: center;
  gap: 6px 20px;
  flex-wrap: wrap;
  margin-top: 11px;
  padding-top: 10px;
  border-top: 1px dashed #f0f2f5;
  font-size: 12px;
  color: #5c6169;
}

.meta-item { display: inline-flex; align-items: center; gap: 5px; min-width: 0; }
.meta-label { color: #a3a9b3; }
.meta-hint { color: #b0b6c0; font-size: 11px; }
.meta-value { font-variant-numeric: tabular-nums; }
.meta-value.mono {
  font-family: 'SF Mono', 'Consolas', 'Menlo', monospace;
  font-size: 11.5px;
  max-width: 280px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.meta-dir { color: #8a919e; max-width: 100%; }

.run-chip {
  display: inline-block;
  font-size: 11px;
  line-height: 16px;
  padding: 1px 7px;
  border-radius: 10px;
  font-weight: 500;
}
.run-chip.c-ok { color: #14864b; background: #e9f8ef; }
.run-chip.c-fail { color: #c22b45; background: #fdeef1; }
.run-chip.c-run { color: #b5701a; background: #fff5e6; }
.run-chip.c-idle { color: #7a8290; background: #f2f4f7; }

.job-error {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin-top: 10px;
  padding: 7px 10px;
  border-radius: 7px;
  background: #fff5f6;
  border: 1px solid #ffe2e6;
  color: #c22b45;
  font-size: 12px;
  line-height: 1.6;
}
.job-error .n-icon { margin-top: 2px; flex-shrink: 0; }
.job-error-text {
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
  word-break: break-word;
}

/* ===== 执行记录抽屉 ===== */
.logs-head { display: flex; align-items: baseline; gap: 10px; min-width: 0; }
.logs-head-title {
  font-size: 15px;
  font-weight: 600;
  color: #1f2329;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}
.logs-head-sub { font-size: 12px; color: #9aa1ab; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex-shrink: 0; }

.logs-layout {
  display: flex;
  flex: 1;
  min-height: 0;
  height: 100%;
}

.run-list {
  width: 296px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  min-height: 0;
  border-right: 1px solid #eef0f4;
  background: #fafbfc;
}

.run-list-tools { padding: 12px 12px 8px; }

.run-filter { display: flex; gap: 4px; margin-top: 8px; }

.run-filter-btn {
  flex: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 4px;
  font-size: 12px;
  line-height: 22px;
  padding: 2px 0;
  border: 1px solid transparent;
  border-radius: 6px;
  background: transparent;
  color: #6b7280;
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}
.run-filter-btn:hover { background: #f0f2f5; }
.run-filter-btn.active {
  background: #fff;
  border-color: #e0e4ea;
  color: #1f2329;
  font-weight: 600;
  box-shadow: 0 1px 2px rgba(31, 35, 41, 0.05);
}
.run-filter-count { font-size: 10.5px; color: #a3a9b3; font-weight: 400; }

.run-list-scroll { flex: 1; overflow-y: auto; padding: 2px 8px 14px; }

.run-item {
  display: flex;
  align-items: flex-start;
  gap: 8px;
  padding: 9px 10px;
  border-radius: 8px;
  border: 1px solid transparent;
  cursor: pointer;
  transition: background 0.14s, border-color 0.14s;
}
.run-item:hover { background: #f1f4f8; }
.run-item.active {
  background: #fff;
  border-color: #e0e4ea;
  box-shadow: 0 2px 6px rgba(31, 35, 41, 0.06);
}

.run-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  margin-top: 6px;
  flex-shrink: 0;
  background: #c8cdd6;
}
.run-dot.c-ok { background: #18a058; }
.run-dot.c-fail { background: #d03050; }
.run-dot.c-run { background: #f0a020; animation: pulse 1.2s ease-in-out infinite; }

.run-item-body { min-width: 0; flex: 1; }

.run-item-row {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}

.run-item-time {
  font-size: 12px;
  font-weight: 600;
  color: #3d434d;
  font-variant-numeric: tabular-nums;
}

.run-item-dur { font-size: 11px; color: #a3a9b3; }

.run-item-preview {
  margin-top: 3px;
  font-size: 11.5px;
  line-height: 1.5;
  color: #9aa1ab;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.run-item-chevron {
  color: #c3c8d1;
  align-self: center;
  flex-shrink: 0;
}

.run-list-empty {
  padding: 32px 12px;
  text-align: center;
  font-size: 12px;
  color: #b0b6c0;
}

.run-detail {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  min-height: 0;
  background: #fff;
}

.detail-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  padding: 12px 20px;
  border-bottom: 1px solid #f0f2f5;
  flex-wrap: wrap;
}

/* 手机端详情顶部的返回条（桌面端不渲染） */
.mobile-detail-nav {
  padding: 6px 12px;
  border-bottom: 1px solid #f0f2f5;
  background: #fafbfc;
}

.back-btn {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  border: none;
  background: transparent;
  color: #2563eb;
  font-size: 13px;
  padding: 6px 4px;
  cursor: pointer;
}
.back-btn:active { opacity: 0.6; }

/* 手机端单列切换用：谁不参与当前视图谁隐藏 */
.is-hidden { display: none !important; }

.detail-badges {
  display: flex;
  align-items: center;
  gap: 6px 14px;
  flex-wrap: wrap;
  font-size: 12px;
  color: #6b7280;
}

.detail-meta-item { font-variant-numeric: tabular-nums; }

.detail-workdir {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 20px;
  background: #f8f9fb;
  border-bottom: 1px solid #f0f2f5;
  font-size: 11.5px;
  color: #8a919e;
  min-width: 0;
}

.detail-workdir-path {
  font-family: 'SF Mono', 'Consolas', 'Menlo', monospace;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}

.link-btn {
  border: none;
  background: transparent;
  color: #2563eb;
  font-size: 11.5px;
  cursor: pointer;
  padding: 0;
  flex-shrink: 0;
}
.link-btn:hover { text-decoration: underline; }

.detail-scroll {
  flex: 1;
  overflow-y: auto;
  padding: 18px 22px 32px;
}

.detail-error {
  margin-bottom: 16px;
  padding: 10px 12px;
  border-radius: 8px;
  background: #fff5f6;
  border: 1px solid #ffe2e6;
}
.detail-error-title {
  display: flex;
  align-items: center;
  gap: 5px;
  font-size: 12.5px;
  font-weight: 600;
  color: #c22b45;
  margin-bottom: 6px;
}
.detail-error pre {
  margin: 0;
  font-family: 'SF Mono', 'Consolas', 'Menlo', monospace;
  font-size: 12px;
  line-height: 1.6;
  color: #a3233a;
  white-space: pre-wrap;
  word-break: break-word;
}

.detail-running {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
  font-size: 12.5px;
  color: #b5701a;
}

.detail-truncated {
  margin-top: 16px;
  padding: 8px 11px;
  border-radius: 6px;
  background: #f8f9fb;
  border: 1px dashed #e3e6ec;
  font-size: 11.5px;
  color: #8a919e;
  line-height: 1.6;
}

.detail-nothing {
  padding: 28px 0;
  text-align: center;
  font-size: 12.5px;
  color: #b0b6c0;
}

.detail-placeholder {
  flex: 1;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 10px;
  color: #b6bcc6;
  font-size: 12.5px;
}

@media (max-width: 880px) {
  .run-list { width: 240px; }
  .detail-scroll { padding: 14px 16px 24px; }
  .detail-head { padding: 10px 16px; }
  .detail-workdir { padding: 7px 16px; }
}

/* ===== 手机端（断点与 App.vue 的 768 一致） =====
   页面本身有 24px 内边距，小屏上太浪费宽度：这里收回一半，
   并让任务卡片的操作按钮单独占一行，避免把标题挤成一列字。 */
@media (max-width: 768px) {
  .cron-page { margin: 0 -12px; }

  .cron-head { margin-bottom: 12px; gap: 10px; }
  .cron-title { font-size: 18px; }
  .cron-subtitle { font-size: 12px; line-height: 1.5; }

  .cron-empty { padding: 40px 16px; }

  .job-card { padding: 12px 12px 11px 16px; border-radius: 9px; }
  .job-top { flex-direction: column; align-items: stretch; gap: 9px; }
  .job-name { font-size: 14px; }
  .job-schedule-row { margin-top: 6px; font-size: 12px; }

  .job-actions { width: 100%; gap: 6px; flex-wrap: wrap; }
  /* tiny 按钮默认 22px，手指点不准：抬到 30px */
  .job-actions :deep(.n-button) { height: 30px; padding: 0 10px; }
  .job-actions :deep(.n-button .n-button__icon) { margin-right: 3px; }

  .job-prompt { margin-top: 7px; font-size: 12px; }
  .job-meta { margin-top: 9px; padding-top: 9px; gap: 7px 14px; font-size: 11.5px; }
  .meta-value.mono { max-width: 150px; }
  .job-error { font-size: 11.5px; }

  /* 抽屉单列：列表与详情互斥显示，各自占满可用高度 */
  .logs-layout { flex-direction: column; }
  .run-list { width: 100%; flex: 1; min-height: 0; border-right: none; }
  .run-detail { flex: 1; min-height: 0; }

  .run-list-tools { padding: 10px 12px 8px; }
  .run-item { padding: 11px 12px; }
  .run-item-time { font-size: 12.5px; }
  .run-item-preview { font-size: 12px; }

  .logs-head-title { font-size: 14px; }
  .logs-head-sub { font-size: 11.5px; }

  .detail-head { padding: 10px 14px; gap: 8px; }
  .detail-badges { gap: 6px 10px; font-size: 11.5px; }
  .detail-workdir { padding: 7px 14px; }
  .detail-scroll { padding: 14px 14px 28px; }
  .detail-error pre { font-size: 11.5px; }
}

@media (prefers-color-scheme: dark) {
  .cron-title { color: #eceef1; }
  .job-card { background: #1f2126; border-color: #2e3138; }
  .job-card:hover { border-color: #3a3e46; box-shadow: 0 4px 14px rgba(0, 0, 0, 0.3); }
  .job-card.is-paused { background: #1b1d21; }
  .job-name { color: #eceef1; }
  .job-schedule-row { color: #b6bcc6; }
  .job-cron { background: #2a2d33; color: #9aa1ab; }
  .job-prompt { color: #8f969f; }
  .job-mode { background: #2a2d33; color: #9aa1ab; }
  .job-meta { border-top-color: #2e3138; color: #b6bcc6; }
  .job-error { background: #2c1f23; border-color: #4a2831; color: #f08a9c; }
  .cron-empty { background: #1b1d21; border-color: #2e3138; }
  .cron-empty-icon { background: #2a2d33; color: #9aa1ab; }
  .run-list { background: #1b1d21; border-right-color: #2e3138; }
  .run-filter-btn.active { background: #26292f; border-color: #3a3e46; color: #eceef1; }
  .run-item:hover { background: #24272d; }
  .run-item.active { background: #26292f; border-color: #3a3e46; }
  .run-item-time { color: #d8dbe0; }
  .run-detail { background: #1f2126; }
  .detail-head { border-bottom-color: #2e3138; }
  .detail-workdir { background: #232529; border-bottom-color: #2e3138; }
  .detail-error { background: #2c1f23; border-color: #4a2831; }
  .detail-truncated { background: #232529; border-color: #3a3e46; }
}

/* ===== 工作目录选择器 ===== */
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

<style>
/* 抽屉被 naive-ui teleport 到 body：根元素上没有本组件的 data-v 属性，
   scoped 里的 .cron-logs-drawer 选择器会失效，所以这几条放全局块。
   手机上默认的 24px 头部留白太占地方，收紧到 14px。 */
@media (max-width: 768px) {
  .cron-logs-drawer .n-drawer-header {
    padding: 12px 14px;
  }
  .cron-logs-drawer .n-drawer-body-content-wrapper {
    overscroll-behavior: contain;
  }
}
</style>
