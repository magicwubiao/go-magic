<template>
  <div>
    <h2 style="margin-bottom: 24px;">System</h2>
    <n-spin v-if="systemStore.loading" />
    <n-grid v-else :cols="2" :x-gap="16" :y-gap="16">
      <n-gi>
        <n-card title="System Information">
          <n-descriptions :column="1">
            <n-descriptions-item label="Version">
              {{ systemStore.info?.version }}
            </n-descriptions-item>
            <n-descriptions-item label="Platform">
              {{ systemStore.info?.platform }}
            </n-descriptions-item>
            <n-descriptions-item label="Architecture">
              {{ systemStore.info?.arch }}
            </n-descriptions-item>
            <n-descriptions-item label="Go Version">
              {{ systemStore.info?.go_version }}
            </n-descriptions-item>
          </n-descriptions>
        </n-card>
      </n-gi>

      <n-gi>
        <n-card title="System Status">
          <n-descriptions :column="1">
            <n-descriptions-item label="Health">
              <n-tag :type="systemStore.health?.status === 'ok' ? 'success' : 'error'">
                {{ systemStore.health?.status || 'unknown' }}
              </n-tag>
            </n-descriptions-item>
            <n-descriptions-item label="Uptime">
              {{ formatUptime(systemStore.stats?.uptime) }}
            </n-descriptions-item>
            <n-descriptions-item label="Memory Usage">
              {{ formatBytes(systemStore.stats?.memory_usage) }}
            </n-descriptions-item>
            <n-descriptions-item label="Goroutines">
              {{ systemStore.stats?.goroutines }}
            </n-descriptions-item>
          </n-descriptions>
        </n-card>
      </n-gi>
    </n-grid>
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import { useSystemStore } from '@/stores/system'

const systemStore = useSystemStore()

function formatUptime(seconds?: number): string {
  if (!seconds) return 'N/A'
  const hours = Math.floor(seconds / 3600)
  const mins = Math.floor((seconds % 3600) / 60)
  return `${hours}h ${mins}m`
}

function formatBytes(bytes?: number): string {
  if (!bytes) return 'N/A'
  const mb = bytes / 1024 / 1024
  return `${mb.toFixed(2)} MB`
}

onMounted(() => {
  systemStore.loadAll()
})
</script>
