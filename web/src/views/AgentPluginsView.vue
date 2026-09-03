<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 24px;" align="center">
      <div>
        <h2 style="margin: 0;">{{ t('agentplugins.title') }}</h2>
        <n-text depth="3" style="font-size: 12px;">
          {{ t('agentplugins.subtitle') }}
        </n-text>
      </div>
      <n-space>
        <n-button @click="handleReload" :loading="store.loading">
          <template #icon>
            <component :is="RefreshOutline" />
          </template>
          {{ t('agentplugins.reload') }}
        </n-button>
        <n-button @click="handleRefresh" :loading="store.loading">
          <template #icon>
            <component :is="RefreshCircleOutline" />
          </template>
          {{ t('common.refresh') }}
        </n-button>
      </n-space>
    </n-space>

    <n-spin v-if="store.loading && !store.plugins.length" />
    <template v-else>
      <!-- Overview Statistics -->
      <n-grid :cols="4" :x-gap="12" style="margin-bottom: 24px;">
        <n-gi>
          <n-card hoverable>
            <n-statistic :label="t('agentplugins.totalPlugins')" :value="store.plugins.length" />
          </n-card>
        </n-gi>
        <n-gi>
          <n-card hoverable>
            <n-statistic :label="t('agentplugins.activePlugins')" :value="store.activePlugins.length" />
          </n-card>
        </n-gi>
        <n-gi>
          <n-card hoverable>
            <n-statistic :label="t('agentplugins.totalSkills')" :value="store.totalSkills" />
          </n-card>
        </n-gi>
        <n-gi>
          <n-card hoverable>
            <n-statistic :label="t('agentplugins.totalMCPTools')" :value="store.totalMCPTools" />
          </n-card>
        </n-gi>
      </n-grid>

      <!-- Drag & Drop Install Zone -->
      <n-card style="margin-bottom: 24px;" :bordered="true">
        <div
          class="drop-zone"
          :class="{ 'drop-zone--active': isDragging }"
          @dragenter.prevent="onDragEnter"
          @dragover.prevent="onDragOver"
          @dragleave.prevent="onDragLeave"
          @drop.prevent="onDrop"
          @click="triggerFileInput"
        >
          <input
            ref="fileInputRef"
            type="file"
            accept=".zip"
            style="display: none;"
            @change="onFilePicked"
          />
          <n-space vertical align="center" :size="8">
            <n-icon size="36" :color="isDragging ? '#2080f0' : '#909399'">
              <component :is="CloudUploadOutline" />
            </n-icon>
            <n-text :type="isDragging ? 'primary' : undefined" strong>
              {{ isDragging ? t('agentplugins.dropHere') : t('agentplugins.dragToInstall') }}
            </n-text>
            <n-text depth="3" style="font-size: 12px;">
              {{ t('agentplugins.dropHint') }}
            </n-text>
            <n-button
              v-if="store.installing"
              type="primary"
              loading
              size="small"
              :disabled="true"
            >
              {{ t('agentplugins.installing') }}
            </n-button>
          </n-space>
        </div>
      </n-card>

      <!-- Scan Dir Hint -->
      <n-alert v-if="store.scanDir" type="info" style="margin-bottom: 16px;" :show-icon="true">
        {{ t('agentplugins.scanDir') }}: <n-text code>{{ store.scanDir }}</n-text>
      </n-alert>

      <!-- Empty State -->
      <n-empty v-if="!store.plugins.length" :description="t('agentplugins.noPlugins')" style="padding: 48px 0;" />

      <!-- Plugin Cards -->
      <n-grid :cols="1" :y-gap="16">
        <n-gi v-for="plugin in store.plugins" :key="plugin.name">
          <n-card hoverable>
            <!-- Header -->
            <template #header>
              <n-space align="center" :size="8">
                <n-icon size="18"><component :is="ExtensionPuzzleOutline" /></n-icon>
                <span style="font-weight: 600;">{{ plugin.name }}</span>
                <n-tag v-if="plugin.version" size="small" type="default">v{{ plugin.version }}</n-tag>
                <n-tag v-if="plugin.rejected" size="small" type="error">{{ t('agentplugins.rejected') }}</n-tag>
                <n-tag v-else-if="!plugin.enabled" size="small" type="warning">{{ t('agentplugins.disabled') }}</n-tag>
                <n-tag v-else size="small" type="success">{{ t('agentplugins.loaded') }}</n-tag>
                <n-tag v-if="plugin.mcp_disabled" size="small" type="warning">{{ t('agentplugins.mcpDisabled') }}</n-tag>
              </n-space>
            </template>

            <!-- Header Extra: enable/disable switch + uninstall -->
            <template #header-extra>
              <n-space align="center" :size="12">
                <n-tooltip>
                  <template #trigger>
                    <n-switch
                      :value="plugin.enabled"
                      :disabled="plugin.rejected || store.pending[plugin.name]"
                      :loading="store.pending[plugin.name]"
                      @update:value="(v: boolean) => handleToggle(plugin.name, v)"
                    />
                  </template>
                  {{ plugin.enabled ? t('agentplugins.clickToDisable') : t('agentplugins.clickToEnable') }}
                </n-tooltip>
                <n-popconfirm @positive-click="handleUninstall(plugin.name)">
                  <template #trigger>
                    <n-button
                      text
                      size="small"
                      type="error"
                      :loading="store.pending[plugin.name]"
                      :disabled="store.pending[plugin.name]"
                    >
                      <template #icon>
                        <component :is="TrashOutline" />
                      </template>
                      {{ t('agentplugins.uninstall') }}
                    </n-button>
                  </template>
                  {{ t('agentplugins.confirmUninstall', { name: plugin.name }) }}
                </n-popconfirm>
                <n-button text size="small" @click="toggleExpand(plugin.name)">
                  <template #icon>
                    <component :is="expanded[plugin.name] ? ChevronUpOutline : ChevronDownOutline" />
                  </template>
                  {{ expanded[plugin.name] ? t('common.collapse') : t('common.expand') }}
                </n-button>
              </n-space>
            </template>

            <!-- Description -->
            <n-text depth="3" v-if="plugin.description">{{ plugin.description }}</n-text>

            <!-- Disabled Hint -->
            <n-alert
              v-if="!plugin.enabled && !plugin.rejected"
              type="warning"
              style="margin-top: 12px;"
              :show-icon="true"
            >
              {{ t('agentplugins.disabledHint') }}
            </n-alert>

            <!-- Fatal Error -->
            <n-alert
              v-if="plugin.fatal_error"
              type="error"
              style="margin-top: 12px;"
              :show-icon="true"
            >
              <template #header>{{ t('agentplugins.fatalError') }}</template>
              {{ plugin.fatal_error }}
            </n-alert>

            <!-- Quick Stats -->
            <n-space style="margin-top: 12px;" :size="24">
              <n-statistic :label="t('agentplugins.skills')" :value="plugin.skills?.length || 0" />
              <n-statistic :label="t('agentplugins.mcpServers')" :value="plugin.mcp_servers?.length || 0" />
              <n-statistic
                :label="t('agentplugins.mcpConnected')"
                :value="plugin.mcp_servers?.filter(m => m.connected).length || 0"
              />
            </n-space>

            <!-- Expanded Details -->
            <n-collapse-transition :show="!!expanded[plugin.name]">
              <div style="margin-top: 16px;">
                <n-descriptions :column="1" label-placement="left" bordered size="small">
                  <n-descriptions-item :label="t('agentplugins.root')">
                    <n-text code>{{ plugin.root }}</n-text>
                  </n-descriptions-item>
                  <n-descriptions-item :label="t('agentplugins.dataDir')">
                    <n-text code>{{ plugin.data_dir }}</n-text>
                  </n-descriptions-item>
                </n-descriptions>

                <!-- Skills -->
                <n-card
                  v-if="plugin.skills && plugin.skills.length"
                  size="small"
                  :title="t('agentplugins.skillsList')"
                  style="margin-top: 12px;"
                >
                  <n-space>
                    <n-tag v-for="sk in plugin.skills" :key="sk" size="small" type="info">
                      {{ sk }}
                    </n-tag>
                  </n-space>
                </n-card>

                <!-- MCP Servers -->
                <n-card
                  v-if="plugin.mcp_servers && plugin.mcp_servers.length"
                  size="small"
                  :title="t('agentplugins.mcpServersList')"
                  style="margin-top: 12px;"
                >
                  <n-list bordered>
                    <n-list-item v-for="m in plugin.mcp_servers" :key="m.name">
                      <n-thing>
                        <template #header>
                          <n-space align="center" :size="6">
                            <span>{{ m.name }}</span>
                            <n-tag size="tiny" :type="m.type === 'stdio' ? 'info' : 'warning'">
                              {{ m.type }}
                            </n-tag>
                            <n-tag
                              size="tiny"
                              :type="m.error ? 'error' : (m.connected ? 'success' : 'default')"
                            >
                              {{ m.error ? t('agentplugins.entryError') : (m.connected ? t('agentplugins.connected') : t('agentplugins.disconnected')) }}
                            </n-tag>
                          </n-space>
                        </template>
                        <template #description>
                          <n-space vertical :size="4">
                            <n-text depth="3" style="font-size: 12px;">
                              {{ t('agentplugins.toolCount') }}: {{ m.tools }}
                            </n-text>
                            <n-text v-if="m.error" type="error" style="font-size: 12px;">
                              {{ m.error }}
                            </n-text>
                          </n-space>
                        </template>
                      </n-thing>
                    </n-list-item>
                  </n-list>
                </n-card>

                <!-- Warnings -->
                <n-card
                  v-if="plugin.warnings && plugin.warnings.length"
                  size="small"
                  :title="t('agentplugins.warnings')"
                  style="margin-top: 12px;"
                >
                  <n-space vertical>
                    <n-alert
                      v-for="(w, i) in plugin.warnings"
                      :key="i"
                      type="warning"
                      :show-icon="false"
                    >
                      {{ w }}
                    </n-alert>
                  </n-space>
                </n-card>
              </div>
            </n-collapse-transition>
          </n-card>
        </n-gi>
      </n-grid>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import {
  RefreshOutline,
  RefreshCircleOutline,
  ExtensionPuzzleOutline,
  ChevronDownOutline,
  ChevronUpOutline,
  CloudUploadOutline,
  TrashOutline,
} from '@vicons/ionicons5'
import { useAgentPluginsStore } from '@/stores/agentplugins'

const { t } = useI18n()
const message = useMessage()
const store = useAgentPluginsStore()

// 每个插件卡片的展开状态(默认展开首个)。
const expanded = reactive<Record<string, boolean>>({})

// 拖拽安装状态。
const isDragging = ref(false)
const fileInputRef = ref<HTMLInputElement | null>(null)
// dragenter/dragleave 计数:子元素切换会触发多次 enter/leave,用计数避免闪烁。
let dragCounter = 0

function toggleExpand(name: string) {
  expanded[name] = !expanded[name]
}

async function handleRefresh() {
  try {
    await store.loadPlugins()
    message.success(t('common.refreshed'))
  } catch (e: any) {
    message.error(e.message || t('agentplugins.loadFailed'))
  }
}

async function handleReload() {
  try {
    const res = await store.reload()
    message.success(
      t('agentplugins.reloaded', { count: res.count, skills: res.skills }),
    )
    if (store.plugins.length > 0) {
      expanded[store.plugins[0].name] = true
    }
  } catch (e: any) {
    message.error(e.message || t('agentplugins.reloadFailed'))
  }
}

// === 拖拽安装 ===

function onDragEnter(e: DragEvent) {
  dragCounter++
  // 仅当拖入的是文件时高亮。
  if (e.dataTransfer?.types?.includes('Files')) {
    isDragging.value = true
  }
}

function onDragOver(e: DragEvent) {
  // 必须 preventDefault 才能触发 drop。
  if (e.dataTransfer) e.dataTransfer.dropEffect = 'copy'
}

function onDragLeave() {
  dragCounter = Math.max(0, dragCounter - 1)
  if (dragCounter === 0) isDragging.value = false
}

function onDrop(e: DragEvent) {
  dragCounter = 0
  isDragging.value = false
  const files = e.dataTransfer?.files
  if (!files || files.length === 0) return
  const file = files[0]
  void handleInstall(file)
}

function triggerFileInput() {
  fileInputRef.value?.click()
}

function onFilePicked(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (file) void handleInstall(file)
  // 清空 input value 以便重复选择同一文件仍能触发 change。
  input.value = ''
}

async function handleInstall(file: File) {
  // 前端轻量校验:仅接受 .zip。
  if (!file.name.toLowerCase().endsWith('.zip')) {
    message.error(t('agentplugins.zipOnly'))
    return
  }
  try {
    const res = await store.install(file)
    message.success(t('agentplugins.installed', { name: res.name }))
    if (store.plugins.length > 0) {
      expanded[res.name] = true
    }
  } catch (e: any) {
    message.error(e.message || t('agentplugins.installFailed'))
  }
}

// === 启用/禁用 ===

async function handleToggle(name: string, enabled: boolean) {
  try {
    await store.setEnabled(name, enabled)
    message.success(enabled ? t('agentplugins.enabled', { name }) : t('agentplugins.disabledMsg', { name }))
  } catch (e: any) {
    message.error(e.message || t('agentplugins.toggleFailed'))
  }
}

// === 卸载 ===

async function handleUninstall(name: string) {
  try {
    await store.uninstall(name)
    message.success(t('agentplugins.uninstalled', { name }))
  } catch (e: any) {
    message.error(e.message || t('agentplugins.uninstallFailed'))
  }
}

onMounted(async () => {
  await store.loadPlugins()
  if (store.plugins.length > 0) {
    expanded[store.plugins[0].name] = true
  }
})
</script>

<style scoped>
.drop-zone {
  border: 2px dashed #d0d0d0;
  border-radius: 8px;
  padding: 32px 16px;
  text-align: center;
  cursor: pointer;
  transition: border-color 0.2s, background-color 0.2s;
  background: #fafafa;
}
.drop-zone:hover {
  border-color: #2080f0;
  background: #f5faff;
}
.drop-zone--active {
  border-color: #2080f0;
  background: #e8f4ff;
}
</style>
