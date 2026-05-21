<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>Cron Jobs</h2>
      <n-button type="primary" @click="showAddJob = true">+ New Job</n-button>
    </n-space>

    <n-spin v-if="cronStore.loading" />
    <n-list v-else bordered>
      <n-list-item v-for="job in cronStore.jobs" :key="job.id">
        <n-thing :title="job.name">
          <template #description>
            <n-space vertical>
              <n-text depth="3">Schedule: <n-tag size="small">{{ job.schedule }}</n-tag></n-text>
              <n-text depth="3">Command: {{ job.command }}</n-text>
              <n-space v-if="job.last_run">
                <n-text depth="3">Last: {{ new Date(job.last_run).toLocaleString() }}</n-text>
              </n-space>
            </n-space>
          </template>
          <template #header-extra>
            <n-tag :type="job.status === 'active' ? 'success' : job.status === 'paused' ? 'warning' : 'error'">
              {{ job.status }}
            </n-tag>
          </template>
          <template #action>
            <n-space>
              <n-button size="small" @click="cronStore.triggerJob(job.id)">▶ Trigger</n-button>
              <n-button v-if="job.status === 'active'" size="small" @click="cronStore.pauseJob(job.id)">⏸ Pause</n-button>
              <n-button v-else size="small" type="primary" @click="cronStore.resumeJob(job.id)">▶ Resume</n-button>
              <n-button size="small" type="error" @click="deleteJob(job.id)">Delete</n-button>
            </n-space>
          </template>
        </n-thing>
      </n-list-item>
    </n-list>

    <!-- Add Job Modal -->
    <n-modal v-model:show="showAddJob" title="New Cron Job">
      <n-card style="width: 500px;">
        <n-form>
          <n-form-item label="Name">
            <n-input v-model:value="newJob.name" placeholder="Job name" />
          </n-form-item>
          <n-form-item label="Schedule (cron)">
            <n-input v-model:value="newJob.schedule" placeholder="0 8 * * *" />
          </n-form-item>
          <n-form-item label="Command">
            <n-input v-model:value="newJob.command" placeholder="Command to execute" />
          </n-form-item>
        </n-form>
        <template #footer>
          <n-space justify="end">
            <n-button @click="showAddJob = false">Cancel</n-button>
            <n-button type="primary" @click="addJob">Create</n-button>
          </n-space>
        </template>
      </n-card>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useCronStore } from '@/stores/cron'

const message = useMessage()
const cronStore = useCronStore()
const showAddJob = ref(false)
const newJob = reactive({ name: '', schedule: '', command: '' })

async function addJob() {
  if (!newJob.name || !newJob.schedule) return
  await cronStore.createJob({ ...newJob })
  newJob.name = ''
  newJob.schedule = ''
  newJob.command = ''
  showAddJob.value = false
  message.success('Job created')
}

async function deleteJob(id: string) {
  await cronStore.deleteJob(id)
  message.success('Job deleted')
}

onMounted(() => cronStore.loadJobs())
</script>
