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

    <!-- 官方技能包:内嵌"技能+连接器"组合,一键安装、授权即可用,无需手写配置 -->
    <n-card style="margin-bottom: 24px;" size="small">
      <template #header>
        <n-space align="center" :size="8">
          <n-icon size="18"><component :is="RocketOutline" /></n-icon>
          <span>{{ t('agentplugins.officialTitle') }}</span>
        </n-space>
      </template>
      <template #header-extra>
        <n-text depth="3" style="font-size: 12px;">{{ t('agentplugins.officialSubtitle') }}</n-text>
      </template>
      <n-grid
        v-if="store.officialPacks.length"
        cols="1 s:2 l:3"
        responsive="screen"
        :x-gap="12"
        :y-gap="12"
      >
        <n-gi v-for="pack in store.officialPacks" :key="pack.name">
          <div class="official-pack">
            <div class="official-pack-head">
              <span class="official-pack-name">{{ pack.name }}</span>
              <n-tag v-if="pack.version" size="tiny" type="default">v{{ pack.version }}</n-tag>
              <n-tag v-if="pack.installed" size="tiny" type="success">
                {{ t('agentplugins.officialInstalled') }}
              </n-tag>
            </div>
            <n-text depth="3" style="font-size: 12px; display: block; margin-top: 4px;">
              {{ pack.description }}
            </n-text>
            <div v-if="pack.examples && pack.examples.length" class="official-pack-examples">
              <div v-for="(ex, i) in pack.examples.slice(0, 2)" :key="i" class="official-pack-example">
                {{ ex }}
              </div>
            </div>
            <div class="official-pack-foot">
              <n-text
                v-if="pack.required_env && pack.required_env.length"
                type="warning"
                style="font-size: 12px;"
              >
                {{ t('agentplugins.officialNeedCreds', { keys: pack.required_env.join(', ') }) }}
              </n-text>
              <span v-else />
              <n-button
                v-if="!pack.installed"
                size="tiny"
                type="primary"
                secondary
                :loading="!!store.pending[pack.name]"
                @click="handleInstallOfficial(pack.name)"
              >
                {{ t('agentplugins.officialInstall') }}
              </n-button>
            </div>
          </div>
        </n-gi>
      </n-grid>
      <n-empty v-else :description="t('agentplugins.noPlugins')" style="padding: 16px 0;" />
    </n-card>

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
                <n-avatar
                  v-if="plugin.icon"
                  :src="`/api/agent-plugins-icon/${encodeURIComponent(plugin.name)}`"
                  :size="22"
                  object-fit="contain"
                  round
                >
                  <n-icon size="16"><component :is="ExtensionPuzzleOutline" /></n-icon>
                </n-avatar>
                <n-icon v-else size="18"><component :is="ExtensionPuzzleOutline" /></n-icon>
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

            <!-- Example Prompts ("你可以这样问") -->
            <div
              v-if="plugin.examples && plugin.examples.length"
              class="ap-examples"
            >
              <div class="ap-examples-title">{{ t('agentplugins.examplesTitle') }}</div>
              <div
                v-for="(ex, i) in plugin.examples"
                :key="i"
                class="ap-example-item"
              >
                <n-text depth="2" style="font-size: 13px;">{{ ex }}</n-text>
              </div>
            </div>

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
              <n-statistic
                v-if="plugin.cli_connectors && plugin.cli_connectors.length"
                :label="t('agentplugins.cliConnectors')"
                :value="plugin.cli_connectors.reduce((n, c) => n + (c.tools?.length || 0), 0)"
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
                              v-if="m.error_kind === 'auth'"
                              size="tiny"
                              type="warning"
                            >
                              {{ t('agentplugins.unauthorized') }}
                            </n-tag>
                            <n-tag
                              v-else
                              size="tiny"
                              :type="m.error ? 'error' : (m.connected ? 'success' : 'default')"
                            >
                              {{ m.error ? t('agentplugins.entryError') : (m.connected ? t('agentplugins.connected') : t('agentplugins.disconnected')) }}
                            </n-tag>
                          </n-space>
                        </template>
                        <template #header-extra>
                          <n-space :size="8">
                            <n-button
                              v-if="m.error_kind === 'auth' || (m.required_env && m.required_env.length && !m.connected)"
                              size="tiny"
                              type="primary"
                              secondary
                              :loading="!!store.pending[plugin.name]"
                              @click="openCredentials(plugin.name)"
                            >
                              {{ t('agentplugins.authorize') }}
                            </n-button>
                            <n-button
                              v-else-if="m.error"
                              size="tiny"
                              secondary
                              :loading="!!store.pending[plugin.name]"
                              @click="handleConnect(plugin.name, m.name)"
                            >
                              {{ t('agentplugins.retry') }}
                            </n-button>
                          </n-space>
                        </template>
                        <template #description>
                          <n-space vertical :size="4">
                            <n-text depth="3" style="font-size: 12px;">
                              {{ t('agentplugins.toolCount') }}: {{ m.tools }}
                            </n-text>
                            <n-text
                              v-if="m.required_env && m.required_env.length && !m.connected"
                              type="warning"
                              style="font-size: 12px;"
                            >
                              {{ t('agentplugins.needCredentials', { keys: m.required_env.join(', ') }) }}
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

                <!-- CLI Connectors (cli.json, 本实现的扩展组件) -->
                <n-card
                  v-if="plugin.cli_connectors && plugin.cli_connectors.length"
                  size="small"
                  :title="t('agentplugins.cliList')"
                  style="margin-top: 12px;"
                >
                  <n-list bordered>
                    <n-list-item v-for="c in plugin.cli_connectors" :key="c.name">
                      <n-thing>
                        <template #header>
                          <n-space align="center" :size="6">
                            <span>{{ c.name }}</span>
                            <n-tag size="tiny" type="info">{{ c.command }}</n-tag>
                            <n-tag size="tiny" :type="c.error ? 'error' : 'success'">
                              {{ c.error ? t('agentplugins.entryError') : t('agentplugins.cliRegistered', { count: c.tools?.length || 0 }) }}
                            </n-tag>
                          </n-space>
                        </template>
                        <template #header-extra>
                          <n-button
                            v-if="c.required_env && c.required_env.length"
                            size="tiny"
                            type="primary"
                            secondary
                            :loading="!!store.pending[plugin.name]"
                            @click="openCredentials(plugin.name)"
                          >
                            {{ t('agentplugins.authorize') }}
                          </n-button>
                        </template>
                        <template #description>
                          <n-space vertical :size="4">
                            <n-text v-if="c.tools && c.tools.length" depth="3" style="font-size: 12px;">
                              {{ c.tools.join(' · ') }}
                            </n-text>
                            <n-text
                              v-if="c.required_env && c.required_env.length"
                              type="warning"
                              style="font-size: 12px;"
                            >
                              {{ t('agentplugins.needCredentials', { keys: c.required_env.join(', ') }) }}
                            </n-text>
                            <n-text v-if="c.error" type="error" style="font-size: 12px;">
                              {{ c.error }}
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

      <!-- Credentials Modal(授权表单) -->
      <n-modal
        v-model:show="credModal.show"
        preset="card"
        :title="t('agentplugins.credentialsTitle', { name: credModal.plugin })"
        style="max-width: 520px;"
      >
        <n-spin v-if="credModal.loading" />
        <template v-else>
          <n-text depth="3" style="font-size: 12px; display: block; margin-bottom: 12px;">
            {{ t('agentplugins.credentialsHint') }}
          </n-text>
          <n-form label-placement="top" size="small">
            <n-form-item
              v-for="key in credModal.keys"
              :key="key"
              :label="key"
              required
            >
              <n-input
                v-model:value="credModal.values[key]"
                :placeholder="credModal.masked[key] === '***'
                  ? t('agentplugins.credentialsKeep')
                  : t('agentplugins.credentialsInput')"
                type="password"
                show-password-on="click"
                clearable
              />
            </n-form-item>
          </n-form>
          <n-space justify="end" style="margin-top: 8px;">
            <n-button size="small" @click="credModal.show = false">{{ t('common.cancel') }}</n-button>
            <n-button
              size="small"
              type="primary"
              :loading="!!store.pending[credModal.plugin]"
              @click="handleSaveCredentials"
            >
              {{ t('agentplugins.authorizeSave') }}
            </n-button>
          </n-space>
        </template>
      </n-modal>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { NAlert, NAvatar, NButton, NCard, NCollapseTransition, NDescriptions, NDescriptionsItem, NEmpty, NForm, NFormItem, NGi, NGrid, NIcon, NInput, NList, NListItem, NModal, NPopconfirm, NSpace, NSpin, NStatistic, NSwitch, NTag, NText, NThing, NTooltip } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import {
  RefreshOutline,
  RefreshCircleOutline,
  ExtensionPuzzleOutline,
  ChevronDownOutline,
  ChevronUpOutline,
  CloudUploadOutline,
  TrashOutline,
  RocketOutline,
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

// === 官方技能包一键安装 ===

async function handleInstallOfficial(name: string) {
  try {
    await store.installOfficial(name)
    message.success(t('agentplugins.installed', { name }))
  } catch (e: any) {
    message.error(e.message || t('agentplugins.officialInstallFailed'))
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

// === 凭据授权 ===

const credModal = reactive<{
  show: boolean
  loading: boolean
  plugin: string
  keys: string[]
  values: Record<string, string>
  masked: Record<string, string>
}>({
  show: false,
  loading: false,
  plugin: '',
  keys: [],
  values: {},
  masked: {},
})

/** 打开授权表单:拉掩码视图,值为空 = 待填,*** = 已设置(留空即保持)。 */
async function openCredentials(name: string) {
  credModal.plugin = name
  credModal.show = true
  credModal.loading = true
  credModal.keys = []
  credModal.values = {}
  credModal.masked = {}
  try {
    const cred = await store.loadCredentials(name)
    const keys = Object.keys(cred.env || {}).sort()
    credModal.keys = keys
    credModal.masked = { ...(cred.env || {}) }
    const values: Record<string, string> = {}
    for (const k of keys) values[k] = ''
    credModal.values = values
  } catch (e: any) {
    message.error(e.message || t('agentplugins.credentialsLoadFailed'))
    credModal.show = false
  } finally {
    credModal.loading = false
  }
}

/** 保存凭据:*** 原样回传(=保持原值),空串删除,其余为新值。 */
async function handleSaveCredentials() {
  const name = credModal.plugin
  const env: Record<string, string> = {}
  for (const k of credModal.keys) {
    const v = credModal.values[k] ?? ''
    env[k] = v === '' ? (credModal.masked[k] ?? '') : v
  }
  try {
    const res = await store.saveCredentials(name, env)
    credModal.show = false
    const failed = (res.entries || []).filter((e) => !e.ok)
    if (failed.length === 0) {
      message.success(t('agentplugins.credentialsSaved', { name }))
    } else {
      // 部分条目仍连不上:把第一类错误亮出来,用户可据此改凭据或查网络。
      const first = failed[0]
      if (first.error_kind === 'auth') {
        message.warning(t('agentplugins.credentialsRejected', { name }))
      } else {
        message.warning(t('agentplugins.credentialsPartial', { name, count: failed.length }))
      }
    }
  } catch (e: any) {
    message.error(e.message || t('agentplugins.credentialsSaveFailed'))
  }
}

// === 单条目重试 ===

async function handleConnect(name: string, server: string) {
  try {
    const res = await store.connectServer(name, server)
    if (res.ok) {
      message.success(t('agentplugins.connectOK', { name: server }))
    } else {
      message.error(res.error || t('agentplugins.connectFailed', { name: server }))
    }
  } catch (e: any) {
    message.error(e.message || t('agentplugins.connectFailed', { name: server }))
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
/* 官方技能包卡片 */
.official-pack {
  height: 100%;
  display: flex;
  flex-direction: column;
  padding: 12px 14px;
  border: 1px solid #e5e6eb;
  border-radius: 8px;
  background: #fafafa;
  transition: border-color 0.2s;
}
.official-pack:hover {
  border-color: #2080f0;
}
.official-pack-head {
  display: flex;
  align-items: center;
  gap: 8px;
}
.official-pack-name {
  font-weight: 600;
  font-size: 14px;
}
.official-pack-examples {
  margin-top: 8px;
  padding: 6px 10px;
  border-radius: 6px;
  background: rgba(32, 128, 240, 0.06);
}
.official-pack-example {
  font-size: 12px;
  color: #5a6b8a;
  padding: 3px 0;
}
.official-pack-foot {
  margin-top: auto;
  padding-top: 8px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  min-height: 28px;
}
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
/* 示例问法("你可以这样问")区块 */
.ap-examples {
  margin-top: 12px;
  padding: 12px 16px;
  border-radius: 8px;
  background: linear-gradient(135deg, rgba(32, 128, 240, 0.08), rgba(32, 128, 240, 0.03));
  border: 1px solid rgba(32, 128, 240, 0.15);
}
.ap-examples-title {
  font-size: 12px;
  font-weight: 600;
  color: #2080f0;
  margin-bottom: 8px;
}
.ap-example-item {
  padding: 6px 0;
  border-bottom: 1px dashed rgba(32, 128, 240, 0.12);
}
.ap-example-item:last-child {
  border-bottom: none;
}
</style>
