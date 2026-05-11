<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { NButton, NTag, NSpin, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'

interface Gateway {
  profile: string
  host: string
  port: number
  pid?: number
  running: boolean
}

const { t } = useI18n()
const message = useMessage()

const gateways = ref<Gateway[]>([])
const loading = ref(false)

async function fetchStatus() {
  loading.value = true
  try {
    const res = await fetch('/api/gateway/status')
    const data = await res.json()
    gateways.value = data.gateways || []
  } catch (err: any) {
    console.error('Failed to fetch gateway status:', err)
    // Fallback to CLI
    try {
      const res = await fetch('/api/cli', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command: 'gateway status', args: [] })
      })
      const data = await res.json()
      if (data.output) {
        parseStatus(data.output)
      }
    } catch (e) {
      console.error('CLI fallback failed:', e)
    }
  } finally {
    loading.value = false
  }
}

function parseStatus(output: string) {
  // Parse "gateway status" output
  const lines = output.split('\n')
  gateways.value = []
  for (const line of lines) {
    if (line.includes('profile') || line.includes('running') || line.includes('stopped')) {
      const parts = line.split(/\s+/)
      if (parts.length >= 2) {
        gateways.value.push({
          profile: parts[0] || 'default',
          host: 'localhost',
          port: 8642,
          running: line.includes('running') || line.includes('active')
        })
      }
    }
  }
}

async function handleToggle(profile: string, running: boolean) {
  try {
    const cmd = running ? 'gateway stop' : 'gateway start'
    const res = await fetch('/api/cli', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ command: cmd, args: [], profile })
    })
    const data = await res.json()
    if (data.error) {
      message.error(data.error)
    } else {
      message.success(running ? t('gateways.stopped') : t('gateways.started'))
      await fetchStatus()
    }
  } catch (err: any) {
    message.error(err.message)
  }
}

onMounted(fetchStatus)
</script>

<template>
  <div class="gateways-view">
    <header class="page-header">
      <h2 class="header-title">{{ t('gateways.title') }}</h2>
      <NButton size="small" @click="fetchStatus" :loading="loading">
        {{ t('common.refresh') }}
      </NButton>
    </header>

    <div class="gateways-content">
      <NSpin :show="loading" size="large">
        <div v-if="gateways.length === 0" class="empty-state">
          {{ t('common.noData') }}
        </div>

        <div v-else class="gateway-list">
          <div v-for="gw in gateways" :key="gw.profile" class="gateway-card">
            <div class="gateway-info">
              <div class="gateway-name">{{ gw.profile }}</div>
              <div class="gateway-meta">
                <span class="meta-item">{{ gw.host }}:{{ gw.port }}</span>
                <span v-if="gw.pid" class="meta-item">PID: {{ gw.pid }}</span>
              </div>
            </div>
            <div class="gateway-actions">
              <NTag :type="gw.running ? 'success' : 'default'" size="small" round>
                {{ gw.running ? t('gateways.running') : t('gateways.stopped') }}
              </NTag>
              <NButton
                size="small"
                :type="gw.running ? 'warning' : 'primary'"
                round
                @click="handleToggle(gw.profile, gw.running)"
              >
                {{ gw.running ? t('common.stop') : t('common.start') }}
              </NButton>
            </div>
          </div>
        </div>
      </NSpin>
    </div>
  </div>
</template>

<style scoped lang="scss">
.gateways-view {
  height: calc(100 * var(--vh));
  display: flex;
  flex-direction: column;
}

.gateways-content {
  flex: 1;
  overflow-y: auto;
  padding: 20px;
}

.empty-state {
  text-align: center;
  color: #909399;
  padding: 40px 0;
}

.gateway-list {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.gateway-card {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 16px 20px;
  background: #1a1a2e;
  border: 1px solid #303133;
  border-radius: 8px;
  transition: border-color 0.2s;

  &:hover {
    border-color: #909399;
  }
}

.gateway-name {
  font-size: 14px;
  font-weight: 600;
  color: #e6e6e6;
  margin-bottom: 4px;
}

.gateway-meta {
  display: flex;
  gap: 12px;
}

.meta-item {
  font-size: 12px;
  color: #909399;
}

.gateway-actions {
  display: flex;
  align-items: center;
  gap: 8px;
}
</style>
