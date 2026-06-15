<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>{{ t('plugins.title') }}</h2>
      <n-space>
        <n-button @click="pluginsStore.rescan()">🔄 {{ t('plugins.rescan') }}</n-button>
        <n-button type="primary" @click="showInstall = true">+ {{ t('plugins.install') }}</n-button>
      </n-space>
    </n-space>

    <n-spin v-if="pluginsStore.loading" />
    <template v-else>
      <!-- Plugin Statistics Overview -->
      <n-card :title="t('plugins.statistics')" style="margin-bottom: 24px;" v-if="pluginStats.length > 0">
        <n-grid :cols="4" :x-gap="12">
          <n-gi>
            <n-statistic :label="t('plugins.totalCalls')" :value="totalCalls" />
          </n-gi>
          <n-gi>
            <n-statistic :label="t('plugins.avgSuccessRate')" :value="avgSuccessRate.toFixed(1)" suffix="%" />
          </n-gi>
          <n-gi>
            <n-statistic :label="t('plugins.topPlugin')" :value="topPluginName" />
          </n-gi>
          <n-gi>
            <n-statistic :label="t('plugins.failingPlugins')" :value="failingPluginsCount" />
          </n-gi>
        </n-grid>
      </n-card>

      <!-- Top Plugins -->
      <n-card :title="t('plugins.topPlugins')" style="margin-bottom: 24px;" v-if="topPlugins.length > 0">
        <n-list>
          <n-list-item v-for="(plugin, index) in topPlugins" :key="plugin.plugin_id">
            <n-thing :title="`${index + 1}. ${plugin.plugin_name}`">
              <template #description>
                <n-space>
                  <n-tag :type="getSuccessRateType(plugin.success_rate)" size="small">
                    {{ (plugin.success_rate * 100).toFixed(0) }}% {{ t('plugins.successRate') }}
                  </n-tag>
                  <n-tag size="small">{{ plugin.total_calls }} {{ t('plugins.calls') }}</n-tag>
                  <n-tag v-if="plugin.trend !== 'stable'" :type="getTrendType(plugin.trend)" size="small">
                    {{ t(`plugins.trends.${plugin.trend}`) }}
                  </n-tag>
                </n-space>
              </template>
            </n-thing>
          </n-list-item>
        </n-list>
      </n-card>

      <!-- Plugins List -->
      <n-card :title="t('plugins.allPlugins')">
        <n-list bordered>
          <n-list-item v-for="plugin in pluginsStore.plugins" :key="plugin.id">
            <n-thing :title="plugin.name">
              <template #description>
                <n-space vertical>
                  <n-text depth="3">{{ plugin.description }}</n-text>
                  <n-space>
                    <n-tag size="small">{{ plugin.version }}</n-tag>
                    <n-tag size="small">{{ plugin.type }}</n-tag>
                    <n-tag size="small">{{ plugin.author }}</n-tag>
                    <n-tag v-if="getPluginStat(plugin.id)" :type="getSuccessRateType(getPluginStat(plugin.id)!.success_rate)" size="small">
                      {{ (getPluginStat(plugin.id)!.success_rate * 100).toFixed(0) }}%
                    </n-tag>
                  </n-space>
                </n-space>
              </template>
              <template #header-extra>
                <n-tag :type="plugin.enabled ? 'success' : 'default'">
                  {{ plugin.enabled ? t('plugins.enabled') : t('plugins.disabled') }}
                </n-tag>
              </template>
              <template #action>
                <n-space>
                  <n-button v-if="!plugin.enabled" size="small" type="primary" @click="pluginsStore.enablePlugin(plugin.id)">{{ t('common.enable') }}</n-button>
                  <n-button v-else size="small" @click="pluginsStore.disablePlugin(plugin.id)">{{ t('common.disable') }}</n-button>
                  <n-button size="small" type="error" @click="deletePlugin(plugin.id)">{{ t('common.delete') }}</n-button>
                </n-space>
              </template>
            </n-thing>
          </n-list-item>
        </n-list>
        <n-empty v-if="!pluginsStore.plugins.length" :description="t('plugins.noPlugins')" />
      </n-card>
    </template>

    <!-- Install Modal -->
    <n-modal v-model:show="showInstall" :title="t('plugins.installPlugin')">
      <n-card style="width: 500px;">
        <n-form>
          <n-form-item :label="t('plugins.pluginUrl')">
            <n-input v-model:value="installUrl" placeholder="https://github.com/user/plugin" />
          </n-form-item>
        </n-form>
        <template #footer>
          <n-space justify="end">
            <n-button @click="showInstall = false">{{ t('common.cancel') }}</n-button>
            <n-button type="primary" @click="install">{{ t('plugins.install') }}</n-button>
          </n-space>
        </template>
      </n-card>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { usePluginsStore } from '@/stores/plugins'
import { getPluginStatistics } from '@/api/plugins'

const { t } = useI18n()
const message = useMessage()
const pluginsStore = usePluginsStore()
const showInstall = ref(false)
const installUrl = ref('')

// Data
const pluginStats = ref<PluginStatistics[]>([])

// Types
interface PluginStatistics {
  plugin_id: string
  plugin_name: string
  total_calls: number
  success_calls: number
  failed_calls: number
  success_rate: number
  avg_duration: number
  last_used: string
  trend: string
}

// Computed
const totalCalls = computed(() => {
  return pluginStats.value.reduce((sum, p) => sum + p.total_calls, 0)
})

const avgSuccessRate = computed(() => {
  if (pluginStats.value.length === 0) return 0
  const total = pluginStats.value.reduce((sum, p) => sum + p.success_rate, 0)
  return (total / pluginStats.value.length) * 100
})

const topPluginName = computed(() => {
  if (pluginStats.value.length === 0) return '-'
  const top = pluginStats.value.reduce((max, p) => p.total_calls > max.total_calls ? p : max, pluginStats.value[0])
  return top.plugin_name
})

const failingPluginsCount = computed(() => {
  return pluginStats.value.filter(p => p.success_rate < 0.5 && p.total_calls > 5).length
})

const topPlugins = computed(() => {
  return [...pluginStats.value]
    .sort((a, b) => b.total_calls - a.total_calls)
    .slice(0, 5)
})

// Methods
function getSuccessRateType(rate: number): 'success' | 'warning' | 'error' {
  if (rate >= 0.8) return 'success'
  if (rate >= 0.5) return 'warning'
  return 'error'
}

function getTrendType(trend: string): 'success' | 'warning' | 'error' | 'default' {
  switch (trend) {
    case 'improving': return 'success'
    case 'declining': return 'error'
    case 'stable': return 'default'
    default: return 'default'
  }
}

function getPluginStat(pluginId: string): PluginStatistics | undefined {
  return pluginStats.value.find(p => p.plugin_id === pluginId)
}

async function install() {
  if (!installUrl.value) return
  await pluginsStore.installPlugin(installUrl.value)
  installUrl.value = ''
  showInstall.value = false
  message.success(t('plugins.installed'))
}

async function deletePlugin(id: string) {
  const plugin = pluginsStore.plugins.find(p => p.id === id)
  const confirmed = await message.warning(t('plugins.confirmDelete', { name: plugin?.name || id }), { positiveText: t('common.confirm'), negativeText: t('common.cancel'), closeable: false })
  if (!confirmed) return
  await pluginsStore.deletePlugin(id)
  message.success(t('plugins.deleted'))
}

onMounted(async () => {
  await pluginsStore.loadPlugins()
  
  // Load statistics
  try {
    const stats = await getPluginStatistics()
    pluginStats.value = stats
  } catch (e) {
    console.error('Failed to load plugin statistics:', e)
  }
})
</script>
