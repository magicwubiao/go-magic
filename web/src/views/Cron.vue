<template>
  <div class="cron-view">
    <n-card title="Scheduled Jobs">
      <template #header-extra>
        <n-button type="primary" @click="showCreateModal = true">
          <template #icon>
            <n-icon><Add /></n-icon>
          </template>
          New Job
        </n-button>
      </template>

      <n-tabs type="line">
        <n-tab-pane name="list" tab="Jobs">
          <n-empty v-if="jobs.length === 0" description="No scheduled jobs" />
          <n-list v-else hoverable clickable>
            <n-list-item v-for="job in jobs" :key="job.id">
              <template #prefix>
                <n-tag :type="job.enabled ? 'success' : 'default'" size="small">
                  {{ job.enabled ? 'Active' : 'Paused' }}
                </n-tag>
              </template>
              <n-thing :title="job.name" :description="job.schedule" />
              <template #suffix>
                <n-space>
                  <n-button size="tiny" @click="toggleJob(job)">
                    {{ job.enabled ? 'Pause' : 'Resume' }}
                  </n-button>
                  <n-button size="tiny" type="error" ghost @click="deleteJob(job.id)">
                    Delete
                  </n-button>
                </n-space>
              </template>
            </n-list-item>
          </n-list>
        </n-tab-pane>
      </n-tabs>
    </n-card>

    <n-modal v-model:show="showCreateModal" preset="card" title="Create Job" style="width: 600px">
      <n-form :model="newJob" label-placement="top">
        <n-form-item label="Job Name">
          <n-input v-model:value="newJob.name" placeholder="e.g., Daily Report" />
        </n-form-item>
        <n-form-item label="Schedule (Cron Expression)">
          <n-input v-model:value="newJob.schedule" placeholder="e.g., 0 9 * * *" />
          <template #feedback>
            <n-text depth="3">Format: minute hour day month weekday</n-text>
          </template>
        </n-form-item>
        <n-form-item label="Command">
          <n-input v-model:value="newJob.command" type="textarea" placeholder="Task description" />
        </n-form-item>
        <n-form-item label="Platform">
          <n-select v-model:value="newJob.platform" :options="platformOptions" placeholder="Select platform" />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showCreateModal = false">Cancel</n-button>
          <n-button type="primary" @click="createJob">Create</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { NCard, NButton, NIcon, NTabs, NTabPane, NList, NListItem, NThing, NTag, NSpace, NModal, NForm, NFormItem, NInput, NSelect, NEmpty, NText } from 'naive-ui'
import { Add } from '@vicons/ionicons5'

interface CronJob {
  id: string
  name: string
  schedule: string
  command: string
  platform: string
  enabled: boolean
  lastRun?: string
  nextRun?: string
}

const jobs = ref<CronJob[]>([])
const showCreateModal = ref(false)
const newJob = ref({
  name: '',
  schedule: '',
  command: '',
  platform: ''
})

const platformOptions = [
  { label: 'CLI', value: 'cli' },
  { label: 'Telegram', value: 'telegram' },
  { label: 'Discord', value: 'discord' }
]

const loadJobs = async () => {
  // TODO: Call API
  jobs.value = []
}

const createJob = async () => {
  // TODO: Call API
  showCreateModal.value = false
  loadJobs()
}

const toggleJob = async (job: CronJob) => {
  // TODO: Call API
}

const deleteJob = async (id: string) => {
  // TODO: Call API
  loadJobs()
}

onMounted(() => {
  loadJobs()
})
</script>
