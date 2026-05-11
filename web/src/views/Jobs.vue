<template>
  <div class="jobs-view">
    <n-card title="Scheduled Jobs">
      <template #header-extra>
        <n-space>
          <n-button type="primary" @click="showCreateModal = true">
            <template #icon>
              <n-icon :component="Add" />
            </template>
            New Job
          </n-button>
        </n-space>
      </template>

      <!-- Jobs List -->
      <n-data-table
        :columns="columns"
        :data="jobs"
        :bordered="false"
        :row-key="(row: Job) => row.id"
      />

      <!-- Job Detail Drawer -->
      <n-drawer v-model:show="showDetail" :width="500" placement="right">
        <n-drawer-content :title="selectedJob?.name || 'Job Details'">
          <n-descriptions v-if="selectedJob" :column="1" label-placement="top">
            <n-descriptions-item label="Name">
              {{ selectedJob.name }}
            </n-descriptions-item>
            <n-descriptions-item label="Schedule">
              <n-tag>{{ selectedJob.schedule }}</n-tag>
              <n-text depth="3" style="margin-left: 8px">
                {{ cronDescription(selectedJob.schedule) }}
              </n-text>
            </n-descriptions-item>
            <n-descriptions-item label="Command">
              <n-code :code="selectedJob.command" language="bash" />
            </n-descriptions-item>
            <n-descriptions-item label="Platform">
              <n-tag>{{ selectedJob.platform || 'All' }}</n-tag>
            </n-descriptions-item>
            <n-descriptions-item label="Status">
              <n-tag :type="selectedJob.enabled ? 'success' : 'default'">
                {{ selectedJob.enabled ? 'Enabled' : 'Disabled' }}
              </n-tag>
            </n-descriptions-item>
            <n-descriptions-item label="Created">
              {{ formatTime(selectedJob.createdAt) }}
            </n-descriptions-item>
            <n-descriptions-item label="Last Run">
              {{ selectedJob.lastRun ? formatTime(selectedJob.lastRun) : 'Never' }}
            </n-descriptions-item>
            <n-descriptions-item label="Next Run">
              {{ selectedJob.nextRun ? formatTime(selectedJob.nextRun) : 'N/A' }}
            </n-descriptions-item>
            <n-descriptions-item v-if="selectedJob.lastResult" label="Last Result">
              <n-tag :type="selectedJob.lastResult.success ? 'success' : 'error'">
                {{ selectedJob.lastResult.success ? 'Success' : 'Failed' }}
              </n-tag>
              <n-text v-if="selectedJob.lastResult.output" depth="3" style="margin-left: 8px">
                {{ selectedJob.lastResult.output.substring(0, 100) }}...
              </n-text>
            </n-descriptions-item>
          </n-descriptions>

          <template #footer>
            <n-space justify="space-between">
              <n-button @click="runNow" :loading="running">
                Run Now
              </n-button>
              <n-space>
                <n-button @click="editJob">Edit</n-button>
                <n-button @click="toggleJob">
                  {{ selectedJob.enabled ? 'Disable' : 'Enable' }}
                </n-button>
                <n-button type="error" @click="deleteJob">Delete</n-button>
              </n-space>
            </n-space>
          </template>
        </n-drawer-content>
      </n-drawer>
    </n-card>

    <!-- Create/Edit Job Modal -->
    <n-modal
      v-model:show="showCreateModal"
      preset="card"
      :title="editingJob ? 'Edit Job' : 'Create Job'"
      style="width: 600px"
    >
      <n-form :model="jobForm" label-placement="top">
        <n-form-item label="Name" required>
          <n-input v-model:value="jobForm.name" placeholder="Job name" />
        </n-form-item>

        <n-form-item label="Command" required>
          <n-input
            v-model:value="jobForm.command"
            type="textarea"
            placeholder="Command to execute"
            :rows="3"
          />
        </n-form-item>

        <n-form-item label="Schedule (Cron Expression)" required>
          <n-space vertical>
            <n-input v-model:value="jobForm.schedule" placeholder="* * * * *" />
            <n-space>
              <n-tag
                v-for="preset in cronPresets"
                :key="preset.label"
                checkable
                :checked="jobForm.schedule === preset.cron"
                @update:checked="jobForm.schedule = preset.cron"
              >
                {{ preset.label }}
              </n-tag>
            </n-space>
            <n-text depth="3" v-if="jobForm.schedule">
              {{ cronDescription(jobForm.schedule) }}
            </n-text>
          </n-space>
        </n-form-item>

        <n-form-item label="Platform">
          <n-select
            v-model:value="jobForm.platform"
            :options="platformOptions"
            placeholder="All platforms"
            clearable
          />
        </n-form-item>

        <n-form-item label="Description">
          <n-input
            v-model:value="jobForm.description"
            type="textarea"
            placeholder="Job description"
            :rows="2"
          />
        </n-form-item>
      </n-form>

      <template #footer>
        <n-space justify="end">
          <n-button @click="showCreateModal = false">Cancel</n-button>
          <n-button type="primary" @click="saveJob" :loading="saving">
            {{ editingJob ? 'Update' : 'Create' }}
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import {
  NCard,
  NButton,
  NIcon,
  NSpace,
  NDataTable,
  NTag,
  NText,
  NCode,
  NDrawer,
  NDrawerContent,
  NDescriptions,
  NDescriptionsItem,
  NModal,
  NForm,
  NFormItem,
  NInput,
  NSelect,
  NDivider,
} from 'naive-ui'
import { Add } from '@vicons/ionicons5'

interface Job {
  id: string
  name: string
  command: string
  schedule: string
  platform?: string
  description?: string
  enabled: boolean
  createdAt: number
  lastRun?: number
  nextRun?: number
  lastResult?: {
    success: boolean
    output: string
    duration: number
  }
}

const jobs = ref<Job[]>([])
const showDetail = ref(false)
const showCreateModal = ref(false)
const selectedJob = ref<Job | null>(null)
const editingJob = ref<Job | null>(null)
const saving = ref(false)
const running = ref(false)

const jobForm = reactive({
  name: '',
  command: '',
  schedule: '',
  platform: null as string | null,
  description: '',
})

const cronPresets = [
  { label: 'Every minute', cron: '* * * * *' },
  { label: 'Every 5 minutes', cron: '*/5 * * * *' },
  { label: 'Every hour', cron: '0 * * * *' },
  { label: 'Every day at midnight', cron: '0 0 * * *' },
  { label: 'Every Monday', cron: '0 0 * * 1' },
  { label: 'First day of month', cron: '0 0 1 * *' },
]

const platformOptions = [
  { label: 'All Platforms', value: '' },
  { label: 'CLI', value: 'cli' },
  { label: 'Telegram', value: 'telegram' },
  { label: 'Discord', value: 'discord' },
  { label: 'Slack', value: 'slack' },
]

const columns = [
  { title: 'Name', key: 'name' },
  {
    title: 'Schedule',
    key: 'schedule',
    render: (row: Job) => h('code', row.schedule),
  },
  {
    title: 'Next Run',
    key: 'nextRun',
    render: (row: Job) => (row.nextRun ? formatTime(row.nextRun) : 'N/A'),
  },
  {
    title: 'Last Run',
    key: 'lastRun',
    render: (row: Job) => (row.lastRun ? formatTime(row.lastRun) : 'Never'),
  },
  {
    title: 'Status',
    key: 'enabled',
    render: (row: Job) =>
      h(
        NTag,
        { type: row.enabled ? 'success' : 'default', size: 'small' },
        { default: () => (row.enabled ? 'Enabled' : 'Disabled') }
      ),
  },
  {
    title: 'Action',
    key: 'actions',
    width: 100,
    render: (row: Job) =>
      h(
        NSpace,
        { size: 'small' },
        {
          default: () => [
            h(
              NButton,
              {
                size: 'small',
                quaternary: true,
                onClick: () => {
                  selectedJob.value = row
                  showDetail.value = true
                },
              },
              { default: () => 'View' }
            ),
          ],
        }
      ),
  },
]

function cronDescription(cron: string): string {
  const parts = cron.split(' ')
  if (parts.length !== 5) return ''

  const [minute, hour, day, month, weekday] = parts

  if (cron === '* * * * *') return 'Every minute'
  if (cron === '*/5 * * * *') return 'Every 5 minutes'
  if (cron === '0 * * * *') return 'Every hour'
  if (cron === '0 0 * * *') return 'Daily at midnight'
  if (cron === '0 0 * * 1') return 'Every Monday at midnight'
  if (cron === '0 0 1 * *') return 'First day of every month'

  // Basic description
  let desc = ''
  if (minute !== '*') desc += `At minute ${minute} `
  if (hour !== '*') desc += `of hour ${hour} `
  if (day !== '*') desc += `on day ${day} `
  if (month !== '*') desc += `of month ${month} `
  if (weekday !== '*') desc += `on weekday ${weekday}`

  return desc || 'Custom schedule'
}

function formatTime(timestamp: number): string {
  return new Date(timestamp).toLocaleString()
}

async function loadJobs() {
  try {
    const res = await fetch('/api/jobs')
    if (res.ok) {
      jobs.value = await res.json()
    }
  } catch (e) {
    console.error('Failed to load jobs:', e)
  }
}

function editJob() {
  if (!selectedJob.value) return
  editingJob.value = selectedJob.value
  Object.assign(jobForm, {
    name: selectedJob.value.name,
    command: selectedJob.value.command,
    schedule: selectedJob.value.schedule,
    platform: selectedJob.value.platform || null,
    description: selectedJob.value.description || '',
  })
  showDetail.value = false
  showCreateModal.value = true
}

async function saveJob() {
  if (!jobForm.name || !jobForm.command || !jobForm.schedule) return
  saving.value = true

  try {
    const method = editingJob.value ? 'PUT' : 'POST'
    const url = editingJob.value ? `/api/jobs/${editingJob.value.id}` : '/api/jobs'

    const res = await fetch(url, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(jobForm),
    })

    if (res.ok) {
      showCreateModal.value = false
      editingJob.value = null
      resetForm()
      loadJobs()
    }
  } catch (e) {
    console.error('Failed to save job:', e)
  } finally {
    saving.value = false
  }
}

async function toggleJob() {
  if (!selectedJob.value) return
  try {
    await fetch(`/api/jobs/${selectedJob.value.id}/toggle`, {
      method: 'POST',
    })
    selectedJob.value.enabled = !selectedJob.value.enabled
    loadJobs()
  } catch (e) {
    console.error('Failed to toggle job:', e)
  }
}

async function runNow() {
  if (!selectedJob.value) return
  running.value = true
  try {
    await fetch(`/api/jobs/${selectedJob.value.id}/run`, {
      method: 'POST',
    })
    loadJobs()
  } catch (e) {
    console.error('Failed to run job:', e)
  } finally {
    running.value = false
  }
}

async function deleteJob() {
  if (!selectedJob.value) return
  if (!confirm('Are you sure you want to delete this job?')) return

  try {
    await fetch(`/api/jobs/${selectedJob.value.id}`, {
      method: 'DELETE',
    })
    showDetail.value = false
    loadJobs()
  } catch (e) {
    console.error('Failed to delete job:', e)
  }
}

function resetForm() {
  jobForm.name = ''
  jobForm.command = ''
  jobForm.schedule = ''
  jobForm.platform = null
  jobForm.description = ''
}

onMounted(() => {
  loadJobs()
})
</script>

<style lang="scss" scoped>
.jobs-view {
  padding: 16px;
}
</style>
