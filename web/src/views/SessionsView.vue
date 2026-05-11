<template>
  <div class="sessions-view">
    <n-card title="Sessions">
      <template #header-extra>
        <n-button type="primary" @click="createSession">New Session</n-button>
      </template>

      <n-data-table
        :columns="columns"
        :data="sessions"
        :pagination="pagination"
        :row-key="row => row.id"
      />
    </n-card>
  </div>
</template>

<script setup>
import { h } from 'vue'
import { NTag } from 'naive-ui'

const sessions = ref([
  { id: '1', title: 'Research Project', platform: 'cli', messages: 42, lastActive: '2024-01-15 10:30' },
  { id: '2', title: 'Coding Assistant', platform: 'telegram', messages: 128, lastActive: '2024-01-15 09:15' },
  { id: '3', title: 'Daily Standup', platform: 'discord', messages: 15, lastActive: '2024-01-14 18:00' }
])

const pagination = { pageSize: 10 }

const columns = [
  { title: 'Title', key: 'title' },
  { title: 'Platform', key: 'platform', render: (row) => h(NTag, { type: 'info' }, () => row.platform) },
  { title: 'Messages', key: 'messages', width: 100 },
  { title: 'Last Active', key: 'lastActive', width: 150 },
  { title: 'Actions', key: 'actions', width: 120, render: () => [
    h('n-button', { size: 'small', quaternary: true }, () => 'Resume'),
    h('n-button', { size: 'small', quaternary: true, type: 'error' }, () => 'Delete')
  ]}
]

const createSession = () => {
  console.log('Create new session')
}
</script>

<script>
import { ref } from 'vue'
export default { name: 'SessionsView' }
</script>
