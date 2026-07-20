<template>
  <div>
    <h2 style="margin-bottom: 24px;">{{ t('system.title') }}</h2>
    <n-spin v-if="systemStore.loading" />
    <n-grid v-else :cols="2" :x-gap="16" :y-gap="16">
      <n-gi>
        <n-card :title="t('system.systemInformation')">
          <n-descriptions :column="1">
            <n-descriptions-item :label="t('system.version')">
              {{ systemStore.info?.version }}
            </n-descriptions-item>
            <n-descriptions-item :label="t('system.platform')">
              {{ systemStore.info?.platform }}
            </n-descriptions-item>
            <n-descriptions-item :label="t('system.architecture')">
              {{ systemStore.info?.arch }}
            </n-descriptions-item>
            <n-descriptions-item :label="t('system.goVersion')">
              {{ systemStore.info?.go_version }}
            </n-descriptions-item>
          </n-descriptions>
        </n-card>
      </n-gi>

      <n-gi>
        <n-card :title="t('system.systemStatus')">
          <n-descriptions :column="1">
            <n-descriptions-item :label="t('system.health')">
              <n-tag :type="systemStore.health?.status === 'healthy' ? 'success' : 'error'">
                {{ systemStore.health?.status || 'unknown' }}
              </n-tag>
            </n-descriptions-item>
            <n-descriptions-item :label="t('system.uptime')">
              {{ formatUptime(systemStore.stats?.uptime) }}
            </n-descriptions-item>
            <n-descriptions-item :label="t('system.memoryUsage')">
              {{ formatBytes(systemStore.stats?.memory_usage) }}
            </n-descriptions-item>
            <n-descriptions-item :label="t('system.goroutines')">
              {{ systemStore.stats?.goroutines }}
            </n-descriptions-item>
          </n-descriptions>
        </n-card>
      </n-gi>

      <n-gi :span="2">
        <UpdateManager />
      </n-gi>
    </n-grid>
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSystemStore } from '@/stores/system'
import UpdateManager from '@/components/UpdateManager.vue'

const { t } = useI18n()
const systemStore = useSystemStore()

function formatUptime(seconds?: number): string {
  if (seconds === undefined || seconds === null || seconds < 0) return t('system.na')
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const secs = Math.floor(seconds % 60)

  const parts: string[] = []
  if (days > 0) parts.push(`${days}d`)
  if (hours > 0) parts.push(`${hours}h`)
  if (minutes > 0) parts.push(`${minutes}m`)
  if (secs > 0 && parts.length === 0) parts.push(`${secs}s`)
  return parts.join(' ') || '0s'
}

function formatBytes(bytes?: number): string {
  if (bytes === undefined || bytes === null) return t('system.na')
  const mb = bytes / 1024 / 1024
  return `${mb.toFixed(2)} MB`
}

onMounted(() => {
  systemStore.loadAll()
})
</script>
