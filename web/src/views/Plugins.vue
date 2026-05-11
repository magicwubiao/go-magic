<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { NAlert, NButton, NInput, NTag, NSpin, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const message = useMessage()

interface Plugin {
  name: string
  version: string
  description: string
  author?: string
  path: string
  type: 'script' | 'binary' | 'http' | 'native'
  enabled: boolean
}

const loading = ref(false)
const plugins = ref<Plugin[]>([])
const searchQuery = ref('')
const typeFilter = ref<string | null>(null)
const error = ref('')

const filteredPlugins = computed(() => {
  const query = searchQuery.value.trim().toLowerCase()
  return plugins.value.filter(plugin => {
    if (typeFilter.value && plugin.type !== typeFilter.value) return false
    if (!query) return true
    return plugin.name.toLowerCase().includes(query) ||
           plugin.description.toLowerCase().includes(query)
  })
})

const summary = computed(() => ({
  total: plugins.value.length,
  enabled: plugins.value.filter(p => p.enabled).length,
  disabled: plugins.value.filter(p => !p.enabled).length
}))

async function loadPlugins() {
  loading.value = true
  error.value = ''
  try {
    const res = await fetch('/api/plugins')
    if (res.ok) {
      const data = await res.json()
      plugins.value = data.plugins || []
    } else {
      // CLI fallback
      const cliRes = await fetch('/api/cli', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command: 'plugin', args: ['list', '--json'] })
      })
      const cliData = await cliRes.json()
      if (cliData.output) {
        plugins.value = parsePluginList(cliData.output)
      }
    }
  } catch (err: any) {
    error.value = err?.message || t('plugins.loadFailed')
  } finally {
    loading.value = false
  }
}

function parsePluginList(output: string): Plugin[] {
  try {
    return JSON.parse(output)
  } catch {
    const lines = output.split('\n').filter(l => l.trim())
    const plugins: Plugin[] = []
    for (const line of lines) {
      const parts = line.split(/\s{2,}/)
      if (parts.length >= 2) {
        plugins.push({
          name: parts[0].trim(),
          description: parts[1].trim(),
          version: parts[2]?.trim() || '1.0.0',
          path: '',
          type: 'script',
          enabled: true
        })
      }
    }
    return plugins
  }
}

function getTypeColor(type: string): 'success' | 'info' | 'warning' | 'error' {
  switch (type) {
    case 'script': return 'success'
    case 'binary': return 'info'
    case 'http': return 'warning'
    case 'native': return 'error'
    default: return 'info'
  }
}

async function togglePlugin(name: string, enabled: boolean) {
  try {
    const res = await fetch('/api/cli', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        command: enabled ? 'plugin disable' : 'plugin enable',
        args: [name]
      })
    })
    const data = await res.json()
    if (data.error) {
      message.error(data.error)
    } else {
      message.success(enabled ? t('plugins.disabled') : t('plugins.enabled'))
      await loadPlugins()
    }
  } catch (err: any) {
    message.error(err.message)
  }
}

onMounted(loadPlugins)
</script>

<template>
  <div class="plugins-view">
    <header class="page-header">
      <h2 class="header-title">{{ t('plugins.title') }}</h2>
      <NButton size="small" @click="loadPlugins" :loading="loading">
        {{ t('plugins.refresh') }}
      </NButton>
    </header>

    <div class="plugins-content">
      <NAlert v-if="error" type="error" class="plugins-notice">
        {{ error }}
      </NAlert>

      <div class="summary-grid">
        <div class="summary-card">
          <span class="summary-label">{{ t('plugins.summary.total') }}</span>
          <strong>{{ summary.total }}</strong>
        </div>
        <div class="summary-card success">
          <span class="summary-label">{{ t('plugins.summary.enabled') }}</span>
          <strong>{{ summary.enabled }}</strong>
        </div>
        <div class="summary-card warning">
          <span class="summary-label">{{ t('plugins.summary.disabled') }}</span>
          <strong>{{ summary.disabled }}</strong>
        </div>
      </div>

      <div class="plugins-toolbar">
        <NInput
          v-model:value="searchQuery"
          :placeholder="t('plugins.searchPlaceholder')"
          size="small"
          clearable
          style="width: 200px"
        />
        <div class="type-legend">
          <NTag
            v-for="type in ['script', 'binary', 'http', 'native']"
            :key="type"
            :type="typeFilter === type ? 'primary' : 'default'"
            size="small"
            checkable
            :checked="typeFilter === type"
            @click="typeFilter = typeFilter === type ? null : type"
          >
            {{ type }}
          </NTag>
        </div>
      </div>

      <NSpin :show="loading" size="large">
        <div v-if="filteredPlugins.length === 0 && !loading" class="empty-state">
          {{ t('plugins.empty') }}
        </div>

        <div v-else class="plugins-list">
          <div
            v-for="plugin in filteredPlugins"
            :key="plugin.name"
            class="plugin-card"
          >
            <div class="plugin-info">
              <div class="plugin-header">
                <span class="plugin-name">{{ plugin.name }}</span>
                <NTag :type="getTypeColor(plugin.type)" size="tiny">
                  {{ plugin.type }}
                </NTag>
                <NTag :type="plugin.enabled ? 'success' : 'default'" size="tiny">
                  {{ plugin.enabled ? t('plugins.enabled') : t('plugins.disabled') }}
                </NTag>
              </div>
              <div class="plugin-desc">{{ plugin.description }}</div>
              <div v-if="plugin.author || plugin.version" class="plugin-meta">
                <span v-if="plugin.author">{{ plugin.author }}</span>
                <span v-if="plugin.author && plugin.version"> · </span>
                <span v-if="plugin.version">v{{ plugin.version }}</span>
              </div>
            </div>
            <div class="plugin-actions">
              <NButton
                size="small"
                :type="plugin.enabled ? 'warning' : 'primary'"
                @click="togglePlugin(plugin.name, plugin.enabled)"
              >
                {{ plugin.enabled ? t('plugins.disable') : t('plugins.enable') }}
              </NButton>
            </div>
          </div>
        </div>
      </NSpin>
    </div>
  </div>
</template>

<style scoped lang="scss">
.plugins-view {
  height: calc(100 * var(--vh));
  display: flex;
  flex-direction: column;
}

.plugins-content {
  flex: 1;
  overflow-y: auto;
  padding: 20px;
}

.plugins-notice {
  margin-bottom: 16px;
}

.summary-grid {
  display: grid;
  grid-template-columns: repeat(3, 1fr);
  gap: 12px;
  margin-bottom: 20px;
}

.summary-card {
  background: #1a1a2e;
  border: 1px solid #303133;
  border-radius: 8px;
  padding: 16px;
  text-align: center;

  &.success {
    border-color: rgba(63, 185, 80, 0.3);
  }

  &.warning {
    border-color: rgba(255, 215, 0, 0.3);
  }
}

.summary-label {
  display: block;
  font-size: 12px;
  color: #909399;
  margin-bottom: 4px;
}

.summary-card strong {
  font-size: 24px;
  color: #e6e6e6;
}

.plugins-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
}

.type-legend {
  display: flex;
  gap: 8px;
}

.empty-state {
  text-align: center;
  color: #909399;
  padding: 40px 0;
}

.plugins-list {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: 12px;
}

.plugin-card {
  background: #1a1a2e;
  border: 1px solid #303133;
  border-radius: 8px;
  padding: 16px;
  display: flex;
  justify-content: space-between;
  gap: 12px;
  transition: border-color 0.2s;

  &:hover {
    border-color: #909399;
  }
}

.plugin-info {
  flex: 1;
  overflow: hidden;
}

.plugin-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 8px;
}

.plugin-name {
  font-size: 14px;
  font-weight: 600;
  color: #e6e6e6;
}

.plugin-desc {
  font-size: 12px;
  color: #909399;
  margin-bottom: 4px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.plugin-meta {
  font-size: 11px;
  color: #606266;
}

.plugin-actions {
  display: flex;
  align-items: flex-start;
}
</style>
