<template>
  <div class="ap-page">
    <!-- 页头：只留一个动作（重新扫描已含列表刷新），不再放两个相似按钮 -->
    <div class="ap-header">
      <div>
        <h2 class="ap-title">{{ t('agentplugins.title') }}</h2>
        <n-text depth="3" style="font-size: 12px;">
          {{ t('agentplugins.subtitle') }}
        </n-text>
      </div>
      <n-button size="small" :loading="store.loading" @click="handleReload">
        <template #icon><component :is="RefreshOutline" /></template>
        {{ t('agentplugins.reload') }}
      </n-button>
    </div>

    <n-spin v-if="store.loading && !store.plugins.length" style="margin: 48px 0;" />

    <template v-else>
      <!-- ===== 已安装插件 ===== -->
      <div class="ap-section">
        <div class="ap-section-head">
          <n-icon size="16"><component :is="ExtensionPuzzleOutline" /></n-icon>
          <span class="ap-section-title">{{ t('agentplugins.installedTitle') }}</span>
          <n-text v-if="store.plugins.length" depth="3" style="font-size: 12px;">
            {{ t('agentplugins.installedSummary', {
              total: store.plugins.length,
              active: store.activePlugins.length,
              skills: store.totalSkills,
              tools: store.totalMCPTools,
            }) }}
          </n-text>
          <span class="ap-section-spacer" />
          <n-button size="tiny" quaternary :loading="refreshing" @click="handleRefreshInstalled">
            <template #icon><component :is="RefreshOutline" /></template>
            {{ t('common.refresh') }}
          </n-button>
          <n-button size="tiny" quaternary @click="triggerFileInput">
            <template #icon><component :is="CloudUploadOutline" /></template>
            {{ t('agentplugins.installZip') }}
          </n-button>
        </div>

        <!-- 拖拽安装：瘦条，不再占一整卡 -->
        <div
          class="drop-strip"
          :class="{ 'drop-strip--active': isDragging }"
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
          <n-icon size="16" :color="isDragging ? '#2080f0' : undefined">
            <component :is="CloudUploadOutline" />
          </n-icon>
          <span>{{ isDragging ? t('agentplugins.dropHere') : t('agentplugins.dragToInstall') }}</span>
          <n-text depth="3" style="font-size: 12px;">{{ t('agentplugins.dropHint') }}</n-text>
          <n-spin v-if="store.installing" :size="14" style="margin-left: auto;" />
        </div>

        <n-alert v-if="store.scanDir" type="info" style="margin-top: 8px;" :show-icon="false">
          <span style="font-size: 12px;">{{ t('agentplugins.scanDir') }}: <n-text code>{{ store.scanDir }}</n-text></span>
        </n-alert>

        <n-empty
          v-if="!store.plugins.length"
          :description="t('agentplugins.noPlugins')"
          style="padding: 40px 0;"
        />

        <!-- 插件行卡：一行摘要，点开看详情 -->
        <div v-for="plugin in store.plugins" :key="plugin.name" class="ap-plugin">
          <div class="ap-plugin-row" @click="toggleExpand(plugin.name)">
            <n-avatar
              v-if="plugin.icon"
              :src="`/api/agent-plugins-icon/${encodeURIComponent(plugin.name)}`"
              :size="24"
              object-fit="contain"
              round
              class="ap-plugin-icon"
            >
              <n-icon size="14"><component :is="ExtensionPuzzleOutline" /></n-icon>
            </n-avatar>
            <span v-else class="ap-plugin-icon ap-plugin-icon--ph">
              <n-icon size="14"><component :is="ExtensionPuzzleOutline" /></n-icon>
            </span>

            <span class="ap-plugin-name">{{ plugin.name }}</span>
            <n-tag v-if="plugin.version" size="tiny" :bordered="false">v{{ plugin.version }}</n-tag>
            <n-tag v-if="plugin.rejected" size="tiny" :bordered="false" type="error">
              {{ t('agentplugins.rejected') }}
            </n-tag>
            <n-tag v-else-if="!plugin.enabled" size="tiny" :bordered="false" type="warning">
              {{ t('agentplugins.disabled') }}
            </n-tag>
            <n-tag v-else size="tiny" :bordered="false" type="success">
              {{ t('agentplugins.loaded') }}
            </n-tag>
            <n-tag v-if="plugin.mcp_disabled" size="tiny" :bordered="false" type="warning">
              {{ t('agentplugins.mcpDisabled') }}
            </n-tag>
            <!-- 未就绪：缺凭据 / 命令没装。原因放在 tooltip 里，点开关时会弹引导 -->
            <n-tooltip v-if="!plugin.ready && !plugin.rejected" placement="bottom">
              <template #trigger>
                <n-tag size="tiny" :bordered="false" type="warning" style="cursor: help;">
                  {{ t('agentplugins.notReady') }}
                  <template v-if="(plugin.blockers || []).length">
                    · {{ t('agentplugins.notReadyCount', { count: (plugin.blockers || []).length }) }}
                  </template>
                </n-tag>
              </template>
              <div v-for="(b, i) in plugin.blockers || []" :key="i">{{ blockerText(b) }}</div>
            </n-tooltip>

            <span class="ap-plugin-stats">{{ pluginStats(plugin) }}</span>

            <span class="ap-plugin-actions" @click.stop>
              <n-tooltip>
                <template #trigger>
                  <n-switch
                    size="small"
                    :value="plugin.enabled"
                    :disabled="plugin.rejected || !!store.pending[plugin.name]"
                    :loading="!!store.pending[plugin.name]"
                    @update:value="(v: boolean) => handleToggle(plugin, v)"
                  />
                </template>
                {{ plugin.enabled ? t('agentplugins.clickToDisable') : t('agentplugins.clickToEnable') }}
              </n-tooltip>
              <n-popconfirm @positive-click="handleUninstall(plugin.name)">
                <template #trigger>
                  <n-button
                    text
                    size="tiny"
                    type="error"
                    :loading="!!store.pending[plugin.name]"
                    :disabled="!!store.pending[plugin.name]"
                  >
                    <template #icon><component :is="TrashOutline" /></template>
                  </n-button>
                </template>
                {{ t('agentplugins.confirmUninstall', { name: plugin.name }) }}
              </n-popconfirm>
              <component
                :is="expanded[plugin.name] ? ChevronUpOutline : ChevronDownOutline"
                class="ap-plugin-chevron"
              />
            </span>
          </div>

          <!-- 摘要行：描述 + 示例问法（pill），默认可见 -->
          <div v-if="!expanded[plugin.name]" class="ap-plugin-brief">
            <span v-if="plugin.description" class="ap-plugin-desc">{{ plugin.description }}</span>
            <span
              v-for="(ex, i) in (plugin.examples || []).slice(0, 3)"
              :key="i"
              class="ap-example-pill"
            >{{ ex }}</span>
          </div>

          <!-- 展开详情 -->
          <n-collapse-transition :show="!!expanded[plugin.name]">
            <div class="ap-plugin-detail">
              <n-alert
                v-if="plugin.fatal_error"
                type="error"
                style="margin-bottom: 12px;"
                :show-icon="true"
              >
                <template #header>{{ t('agentplugins.fatalError') }}</template>
                {{ plugin.fatal_error }}
              </n-alert>
              <n-alert
                v-if="!plugin.enabled && !plugin.rejected"
                type="warning"
                style="margin-bottom: 12px;"
                :show-icon="true"
              >
                {{ t('agentplugins.disabledHint') }}
              </n-alert>
              <div v-if="plugin.description" class="ap-detail-desc">{{ plugin.description }}</div>

              <!-- 示例问法 -->
              <div v-if="plugin.examples && plugin.examples.length" class="ap-detail-sec">
                <div class="ap-detail-label">{{ t('agentplugins.examplesTitle') }}</div>
                <div class="ap-example-pills">
                  <span v-for="(ex, i) in plugin.examples" :key="i" class="ap-example-pill">{{ ex }}</span>
                </div>
              </div>

              <!-- 路径 -->
              <div class="ap-detail-sec">
                <div class="ap-detail-rows">
                  <div class="ap-detail-row">
                    <span class="ap-detail-key">{{ t('agentplugins.root') }}</span>
                    <n-text code class="ap-detail-code">{{ plugin.root }}</n-text>
                  </div>
                  <div class="ap-detail-row">
                    <span class="ap-detail-key">{{ t('agentplugins.dataDir') }}</span>
                    <n-text code class="ap-detail-code">{{ plugin.data_dir }}</n-text>
                  </div>
                </div>
              </div>

              <!-- 技能 -->
              <div v-if="plugin.skills && plugin.skills.length" class="ap-detail-sec">
                <div class="ap-detail-label">{{ t('agentplugins.skillsList') }}</div>
                <div class="ap-detail-tags">
                  <n-tag v-for="sk in plugin.skills" :key="sk" size="small" type="info" :bordered="false">
                    {{ sk }}
                  </n-tag>
                </div>
              </div>

              <!-- MCP 服务器 -->
              <div v-if="plugin.mcp_servers && plugin.mcp_servers.length" class="ap-detail-sec">
                <div class="ap-detail-label">{{ t('agentplugins.mcpServersList') }}</div>
                <div v-for="m in plugin.mcp_servers" :key="m.name" class="ap-entry">
                  <div class="ap-entry-head">
                    <span class="ap-entry-name">{{ m.name }}</span>
                    <n-tag size="tiny" :bordered="false" :type="m.type === 'stdio' ? 'info' : 'warning'">
                      {{ m.type }}
                    </n-tag>
                    <n-tag
                      v-if="m.error_kind === 'auth'"
                      size="tiny"
                      :bordered="false"
                      type="warning"
                    >
                      {{ t('agentplugins.unauthorized') }}
                    </n-tag>
                    <n-tag
                      v-else
                      size="tiny"
                      :bordered="false"
                      :type="m.error ? 'error' : (m.connected ? 'success' : 'default')"
                    >
                      {{ m.error ? t('agentplugins.entryError') : (m.connected ? t('agentplugins.connected') : t('agentplugins.disconnected')) }}
                    </n-tag>
                    <!-- OAuth 状态：等待授权 / 失败 / 过期 都要在行内可见 -->
                    <n-tag
                      v-if="m.oauth_available && m.oauth_state && m.oauth_state !== 'authorized'"
                      size="tiny"
                      :bordered="false"
                      type="warning"
                    >
                      {{ oauthStateText(m.oauth_state) }}
                    </n-tag>
                    <span class="ap-entry-spacer" />
                    <!-- OAuth 优先：点一下去浏览器登录，不需要 API key -->
                    <n-button
                      v-if="m.oauth_available && m.oauth_state !== 'authorized'"
                      size="tiny"
                      type="primary"
                      secondary
                      :loading="oauthBusy === oauthKey(plugin.name, m.name)"
                      @click="handleOAuthSignIn(plugin.name, m.name)"
                    >
                      {{ t('agentplugins.oauthSignIn') }}
                    </n-button>
                    <n-button
                      v-else-if="m.error_kind === 'auth' || (m.required_env && m.required_env.length && !m.connected)"
                      size="tiny"
                      type="primary"
                      secondary
                      :loading="!!store.pending[plugin.name] || autoBusy === plugin.name"
                      @click="handleAuthorize(plugin.name)"
                    >
                      {{ t('agentplugins.authorize') }}
                    </n-button>
                    <n-button
                      v-else-if="m.error || !m.connected"
                      size="tiny"
                      secondary
                      :loading="!!store.pending[plugin.name]"
                      @click="handleConnect(plugin.name, m.name)"
                    >
                      {{ t('agentplugins.retry') }}
                    </n-button>
                    <n-button
                      v-if="m.oauth_available && m.oauth_state === 'authorized'"
                      size="tiny"
                      text
                      @click="handleOAuthSignOut(plugin.name, m.name)"
                    >
                      {{ t('agentplugins.oauthSignOut') }}
                    </n-button>
                  </div>
                  <div class="ap-entry-desc">
                    <span class="ap-entry-meta">{{ t('agentplugins.toolCount') }}: {{ m.tools }}</span>
                    <span
                      v-if="m.required_env && m.required_env.length && !m.connected"
                      class="ap-entry-warn"
                    >
                      {{ t('agentplugins.needCredentials', { keys: m.required_env.join(', ') }) }}
                    </span>
                    <!-- OAuth 可用且未授权：说清"不用找 key，点登录就行" -->
                    <span
                      v-if="m.oauth_available && m.oauth_state !== 'authorized' && !m.connected"
                      class="ap-entry-hint"
                    >
                      {{ t('agentplugins.oauthHint') }}
                    </span>
                    <!-- auth 类失败缺凭据行已用中文说明，不再重复透出后端英文 error -->
                    <span v-if="m.error && m.error_kind !== 'auth'" class="ap-entry-err">{{ m.error }}</span>
                    <span v-if="m.oauth_error" class="ap-entry-err">{{ m.oauth_error }}</span>
                  </div>
                </div>
              </div>

              <!-- CLI 连接器（cli.json 扩展组件） -->
              <div v-if="plugin.cli_connectors && plugin.cli_connectors.length" class="ap-detail-sec">
                <div class="ap-detail-label">{{ t('agentplugins.cliList') }}</div>
                <div v-for="c in plugin.cli_connectors" :key="c.name" class="ap-entry">
                  <div class="ap-entry-head">
                    <span class="ap-entry-name">{{ c.name }}</span>
                    <n-tag size="tiny" :bordered="false" type="info">{{ c.command }}</n-tag>
                    <n-tag size="tiny" :bordered="false" :type="c.error ? 'error' : 'success'">
                      {{ c.error ? t('agentplugins.entryError') : t('agentplugins.cliRegistered', { count: c.tools?.length || 0 }) }}
                    </n-tag>
                    <!-- 命令没装：CLI 唯一的前置阻塞，给一键安装（命令来自内置目录） -->
                    <n-tag v-if="c.binary_found === false" size="tiny" :bordered="false" type="warning">
                      {{ t('agentplugins.cliBinaryMissing') }}
                    </n-tag>
                    <span class="ap-entry-spacer" />
                    <n-button
                      v-if="c.binary_found === false && c.catalog_id"
                      size="tiny"
                      type="primary"
                      secondary
                      @click="openInstallDialog(plugin.name, c.catalog_id, plugin.name)"
                    >
                      {{ t('agentplugins.cliInstall') }}
                    </n-button>
                    <n-button
                      v-if="c.required_env && c.required_env.length"
                      size="tiny"
                      type="primary"
                      secondary
                      :loading="!!store.pending[plugin.name] || autoBusy === plugin.name"
                      @click="handleAuthorize(plugin.name)"
                    >
                      {{ t('agentplugins.authorize') }}
                    </n-button>
                  </div>
                  <div class="ap-entry-desc">
                    <span v-if="c.tools && c.tools.length" class="ap-entry-meta">{{ c.tools.join(' · ') }}</span>
                    <span
                      v-if="c.required_env && c.required_env.length"
                      class="ap-entry-warn"
                    >
                      {{ t('agentplugins.needCredentials', { keys: c.required_env.join(', ') }) }}
                    </span>
                    <span v-if="c.error" class="ap-entry-err">{{ c.error }}</span>
                  </div>
                </div>
              </div>

              <!-- 警告 -->
              <div v-if="plugin.warnings && plugin.warnings.length" class="ap-detail-sec">
                <div class="ap-detail-label">{{ t('agentplugins.warnings') }}</div>
                <n-alert
                  v-for="(w, i) in plugin.warnings"
                  :key="i"
                  type="warning"
                  :show-icon="false"
                  style="margin-bottom: 6px;"
                >
                  <span style="font-size: 12px;">{{ w }}</span>
                </n-alert>
              </div>
            </div>
          </n-collapse-transition>
        </div>
      </div>

      <!-- ===== MCP 连接器：目录条目即插件，装到上方"已安装" ===== -->
      <div class="ap-section">
        <div class="ap-section-head">
          <n-icon size="16"><component :is="ServerOutline" /></n-icon>
          <span class="ap-section-title">{{ t('agentplugins.mcpCatalogTitle') }}</span>
          <n-text depth="3" style="font-size: 12px;">{{ t('agentplugins.mcpCatalogSubtitle') }}</n-text>
        </div>
        <CatalogPackCards kind="mcp" @installed="onCatalogInstalled" />
      </div>

      <!-- ===== CLI 应用：同上，一张卡一个工具包 ===== -->
      <div class="ap-section">
        <div class="ap-section-head">
          <n-icon size="16"><component :is="TerminalOutline" /></n-icon>
          <span class="ap-section-title">{{ t('agentplugins.cliCatalogTitle') }}</span>
          <n-text depth="3" style="font-size: 12px;">{{ t('agentplugins.cliCatalogSubtitle') }}</n-text>
        </div>
        <CatalogPackCards kind="cli" @installed="onCatalogInstalled" />
      </div>
      <!-- 凭据授权弹窗 -->
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
          <!-- 自动探测已经填好的部分：告诉用户"剩下的才是真的要你动手"，避免以为全都要手打 -->
          <n-alert
            v-if="credModal.autoFilled.length"
            type="success"
            :bordered="false"
            :show-icon="false"
            style="margin-bottom: 12px;"
          >
            <span style="font-size: 12px;">
              {{ t('agentplugins.credentialsAutoHint', {
                keys: credModal.autoFilled.join(', '),
                count: credModal.keys.length,
              }) }}
            </span>
          </n-alert>
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

      <!-- 未就绪引导：把"启用也调不通"的原因摆出来，并给一键修复入口 -->
      <n-modal
        v-model:show="blockerModal.show"
        preset="card"
        :title="t('agentplugins.blockersTitle', { name: blockerModal.plugin })"
        style="max-width: 560px;"
      >
        <n-text depth="3" style="font-size: 12px; display: block; margin-bottom: 12px;">
          {{ t('agentplugins.blockersHint') }}
        </n-text>
        <div v-for="(b, i) in blockerModal.items" :key="i" class="ap-blocker">
          <span class="ap-blocker-text">{{ blockerText(b) }}</span>
          <n-button
            v-if="b.kind === 'oauth' && b.name"
            size="tiny"
            type="primary"
            secondary
            @click="fromBlockerOAuth(b)"
          >
            {{ t('agentplugins.blockerOAuth') }}
          </n-button>
          <n-button
            v-else-if="b.kind === 'auth'"
            size="tiny"
            type="primary"
            secondary
            @click="fromBlockerAuthorize"
          >
            {{ t('agentplugins.blockerAuthorize') }}
          </n-button>
          <n-button
            v-else-if="b.kind === 'missing_binary' && b.catalog_id"
            size="tiny"
            type="primary"
            secondary
            @click="fromBlockerInstall(b)"
          >
            {{ t('agentplugins.blockerInstall') }}
          </n-button>
          <n-button
            v-else-if="b.kind === 'connect' && b.name"
            size="tiny"
            type="primary"
            secondary
            @click="fromBlockerConnect(b)"
          >
            {{ t('agentplugins.retry') }}
          </n-button>
        </div>
        <n-space justify="end" style="margin-top: 16px;">
          <n-button size="small" @click="blockerModal.show = false">{{ t('common.cancel') }}</n-button>
          <n-button size="small" type="primary" ghost @click="fromBlockerContinue">
            {{ t('agentplugins.blockerContinue') }}
          </n-button>
        </n-space>
      </n-modal>

      <!-- CLI 一键安装：插件详情与目录卡共用同一个弹窗 -->
      <CLIInstallDialog
        v-model:show="installModal.show"
        :plugin="installModal.plugin"
        :catalog-id="installModal.catalogId"
        :display-name="installModal.name"
        @done="onCLIInstallDone"
      />
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { NAlert, NAvatar, NButton, NCollapseTransition, NEmpty, NForm, NFormItem, NIcon, NInput, NModal, NPopconfirm, NSpace, NSpin, NSwitch, NTag, NText, NTooltip } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import {
  RefreshOutline,
  ExtensionPuzzleOutline,
  ChevronDownOutline,
  ChevronUpOutline,
  CloudUploadOutline,
  TrashOutline,
  ServerOutline,
  TerminalOutline,
} from '@vicons/ionicons5'
import { useAgentPluginsStore } from '@/stores/agentplugins'
import type { AgentPlugin, AgentPluginBlocker } from '@/api/agentplugins'
import CatalogPackCards from '@/components/CatalogPackCards.vue'
import CLIInstallDialog from '@/components/CLIInstallDialog.vue'

const { t } = useI18n()
const message = useMessage()
const store = useAgentPluginsStore()

// 每个插件卡片的展开状态(默认全部收起,保持页面干净)。
const expanded = reactive<Record<string, boolean>>({})

// 拖拽安装状态。
const isDragging = ref(false)
const fileInputRef = ref<HTMLInputElement | null>(null)
// dragenter/dragleave 计数:子元素切换会触发多次 enter/leave,用计数避免闪烁。
let dragCounter = 0

function toggleExpand(name: string) {
  expanded[name] = !expanded[name]
}

/** 插件行的内联统计："技能 2 · MCP 1/2 · CLI 工具 5"。 */
function pluginStats(plugin: AgentPlugin): string {
  const parts: string[] = []
  const skills = plugin.skills?.length || 0
  if (skills) parts.push(t('agentplugins.skills') + ' ' + skills)
  const servers = plugin.mcp_servers || []
  if (servers.length) {
    const ok = servers.filter((m) => m.connected).length
    parts.push('MCP ' + ok + '/' + servers.length)
  }
  const cliTools = (plugin.cli_connectors || []).reduce((n, c) => n + (c.tools?.length || 0), 0)
  if (cliTools) parts.push('CLI ' + cliTools)
  return parts.join(' · ')
}

const refreshing = ref(false)

/** 轻量刷新：只重读已安装列表，不触发全量重扫（重扫在页头按钮）。 */
async function handleRefreshInstalled() {
  refreshing.value = true
  try {
    await store.loadPlugins()
  } finally {
    refreshing.value = false
  }
}

async function handleReload() {
  try {
    // reload 本身就会回读插件列表,一个动作覆盖"重扫 + 刷新"。
    const res = await store.reload()
    message.success(
      t('agentplugins.reloaded', { count: res.count, skills: res.skills }),
    )
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
    // 刚装的插件展开详情,方便确认装上了什么。
    expanded[res.name] = true
  } catch (e: any) {
    message.error(e.message || t('agentplugins.installFailed'))
  }
}

// === 官方技能包 / 目录插件包一键安装 ===

/**
 * 安装成功后：刷新插件列表（卡片状态由后端 installed 决定），
 * 需要凭据的走"自动授权"——环境变量 / 已登录 CLI / 别的插件填过的同名凭据
 * 都直接拿来用，只有真的拿不到的才弹表单。
 */
async function onCatalogInstalled(payload: { name: string; requiredEnv: string[] }) {
  await store.loadPlugins()
  expanded[payload.name] = true
  if (payload.requiredEnv.length) {
    await handleAuthorize(payload.name)
  }
}

// === 启用/禁用 ===

/**
 * 开关入口：启用前先看就绪度。
 *
 * 缺凭据 / 命令没装时后端会拒绝启用（409 + blockers），与其让用户撞一次墙，
 * 不如在这里就弹引导：去授权 → 一键安装 → 仍然启用。
 */
async function handleToggle(plugin: AgentPlugin, enabled: boolean) {
  if (!enabled) {
    await setEnabled(plugin.name, false)
    return
  }
  if ((plugin.blockers || []).length) {
    blockerModal.plugin = plugin.name
    blockerModal.items = plugin.blockers || []
    blockerModal.show = true
    return
  }
  await setEnabled(plugin.name, true)
}

async function setEnabled(name: string, enabled: boolean, force = false) {
  try {
    await store.setEnabled(name, enabled, force)
    message.success(enabled ? t('agentplugins.enabled', { name }) : t('agentplugins.disabledMsg', { name }))
  } catch (e: any) {
    message.error(e.message || t('agentplugins.toggleFailed'))
  }
}

// 未就绪引导弹窗：只承载"为什么还不能用 + 下一步点什么"。
const blockerModal = reactive({
  show: false,
  plugin: '',
  items: [] as AgentPluginBlocker[],
})

/** 把一条 blocker 翻译成一句人话（tooltip 与弹窗共用）。 */
function blockerText(b: AgentPluginBlocker): string {
  if (b.kind === 'oauth') {
    return t('agentplugins.blockerOAuthText', { name: b.name || '' })
  }
  if (b.kind === 'auth') {
    return t('agentplugins.blockerAuth', { name: b.name || '', keys: (b.keys || []).join(', ') })
  }
  if (b.kind === 'connect') {
    return t('agentplugins.blockerConnect', { name: b.name || '', detail: b.detail || '' })
  }
  if (b.kind === 'missing_binary') {
    return t('agentplugins.blockerCli', { name: b.name || '', command: b.command || '' })
  }
  return t('agentplugins.blockerRejected')
}

function fromBlockerOAuth(b: AgentPluginBlocker) {
  const name = blockerModal.plugin
  blockerModal.show = false
  if (b.name) void handleOAuthSignIn(name, b.name)
}

function fromBlockerAuthorize() {
  const name = blockerModal.plugin
  blockerModal.show = false
  void handleAuthorize(name)
}

function fromBlockerInstall(b: AgentPluginBlocker) {
  const name = blockerModal.plugin
  blockerModal.show = false
  openInstallDialog(name, b.catalog_id || '', name)
}

function fromBlockerConnect(b: AgentPluginBlocker) {
  const name = blockerModal.plugin
  blockerModal.show = false
  if (b.name) void handleConnect(name, b.name)
}

async function fromBlockerContinue() {
  const name = blockerModal.plugin
  blockerModal.show = false
  await setEnabled(name, true, true)
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
  /** 已经被自动填好的 key（只用于提示，表单里不再重复出现） */
  autoFilled: string[]
}>({
  show: false,
  loading: false,
  plugin: '',
  keys: [],
  values: {},
  masked: {},
  autoFilled: [],
})

/** 自动授权进行中的插件名（按钮 loading 用）。 */
const autoBusy = ref('')

/**
 * 授权入口：先自动，再手填。
 *
 * 绝大多数情况（token 已在环境变量里、gh 已登录、别的插件填过同名 key）
 * 到这里就结束了，用户一个字符都不用敲；剩下的才弹表单。
 */
async function handleAuthorize(name: string) {
  if (autoBusy.value) return
  autoBusy.value = name
  try {
    const res = await store.authorizeAuto(name)
    if (res.filled.length) {
      message.success(
        t('agentplugins.autoFilled', {
          count: res.filled.length,
          keys: res.filled.map((f) => f.key).join(', '),
        }),
      )
      // store.authorizeAuto 已经在填入后刷新过列表，这里不再多打一次请求。
    }
    if (res.missing.length) {
      await openCredentials(name, res.missing, res.filled.map((f) => f.key))
    } else {
      if (!res.filled.length) message.success(t('agentplugins.autoDone'))
    }
  } catch (e: any) {
    // 探测接口失败不该把用户挡在门外：退回手填。
    message.error(e.message || t('agentplugins.autoFailed'))
    await openCredentials(name)
  } finally {
    autoBusy.value = ''
  }
}

/**
 * 打开授权表单:拉掩码视图,值为空 = 待填,*** = 已设置(留空即保持)。
 *
 * only 非空时只渲染这几项（自动探测剩下的），其余已就绪的 key 不再打扰用户。
 */
async function openCredentials(name: string, only?: string[], autoFilled: string[] = []) {
  credModal.plugin = name
  credModal.show = true
  credModal.loading = true
  credModal.keys = []
  credModal.values = {}
  credModal.masked = {}
  credModal.autoFilled = [...autoFilled]
  try {
    const cred = await store.loadCredentials(name)
    const all = Object.keys(cred.env || {}).sort()
    const keys = only && only.length ? only : all
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

// === OAuth 授权（远程 MCP 条目）===

/** 进行中的授权：键为 "plugin/server"，按钮 loading 用。 */
const oauthBusy = ref('')

function oauthKey(plugin: string, server: string): string {
  return plugin + '/' + server
}

/** 把授权状态翻成一句短标签（行内 tag 用）。 */
function oauthStateText(state: string): string {
  switch (state) {
    case 'awaiting_authorization':
      return t('agentplugins.oauthAwaiting')
    case 'failed':
      return t('agentplugins.oauthFailedState')
    case 'expired':
      return t('agentplugins.oauthExpired')
    case 'authorized':
      return t('agentplugins.oauthAuthorized')
    default:
      return t('agentplugins.oauthStateNone')
  }
}

/**
 * 发起 OAuth 授权：后端完成发现/注册后返回 authorize URL，浏览器打开它；
 * 用户点"允许"后回调落 token，后端自动重连该条目。这里轮询直到授权完成
 * 或超时（3 分钟）——与连接器页同一套交互，用户不需要回页面再点"重试"。
 */
async function handleOAuthSignIn(plugin: string, server: string) {
  const key = oauthKey(plugin, server)
  if (oauthBusy.value) return
  oauthBusy.value = key
  try {
    const res = await store.startOAuth(plugin, server)
    if (res.already_authorized) {
      message.success(t('agentplugins.oauthAlready'))
      await store.loadPlugins()
      return
    }
    if (!res.authorize_url) {
      message.error(t('agentplugins.oauthFailedMsg'))
      return
    }
    window.open(res.authorize_url, '_blank', 'noopener')
    message.info(t('agentplugins.oauthPopup'), { duration: 8000 })

    const deadline = Date.now() + 3 * 60 * 1000
    while (Date.now() < deadline) {
      await new Promise((r) => setTimeout(r, 2000))
      let st
      try {
        st = await store.oauthStatus(plugin, server)
      } catch {
        continue // 单次轮询失败不终止等待
      }
      if (st.state === 'authorized') {
        message.success(t('agentplugins.oauthDone', { name: server }))
        await store.loadPlugins()
        return
      }
      if (st.state === 'failed') {
        message.error(st.error || t('agentplugins.oauthFailedMsg'), { duration: 10000 })
        await store.loadPlugins()
        return
      }
    }
    message.warning(t('agentplugins.oauthTimeout'))
  } catch (e: any) {
    message.error(e.message || t('agentplugins.oauthFailedMsg'))
  } finally {
    oauthBusy.value = ''
  }
}

async function handleOAuthSignOut(plugin: string, server: string) {
  try {
    await store.signOutOAuth(plugin, server)
    message.success(t('agentplugins.oauthSignedOut'))
  } catch (e: any) {
    message.error(e.message || t('agentplugins.oauthFailedMsg'))
  }
}

// === CLI 一键安装 ===

/**
 * 安装弹窗状态。plugin 与 catalogId 二选一：
 * 已安装插件传 name（后端反查目录条目），插件详情里的连接器也走这条路径。
 */
const installModal = reactive({
  show: false,
  plugin: '',
  catalogId: '',
  name: '',
})

function openInstallDialog(plugin: string, catalogId: string, name: string) {
  installModal.plugin = plugin
  installModal.catalogId = catalogId
  installModal.name = name
  installModal.show = true
}

/** 安装任务结束：命令可用后刷新列表，"未就绪"标记会随之消失。 */
async function onCLIInstallDone() {
  await store.loadPlugins()
}

onMounted(async () => {
  await store.loadPlugins()
})
</script>

<style scoped>
/* ===== 页面骨架 ===== */
.ap-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 20px;
}
.ap-title {
  margin: 0 0 2px;
  font-size: 20px;
}
.ap-section {
  margin-bottom: 28px;
}
.ap-section-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
}
.ap-section-title {
  font-size: 15px;
  font-weight: 600;
}
.ap-section-spacer {
  flex: 1;
}

/* ===== 拖拽安装瘦条 ===== */
.drop-strip {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  border: 1.5px dashed #d0d0d0;
  border-radius: 8px;
  font-size: 13px;
  cursor: pointer;
  color: #606266;
  background: #fafafa;
  transition: border-color 0.2s, background-color 0.2s;
  user-select: none;
}
.drop-strip:hover,
.drop-strip--active {
  border-color: #2080f0;
  background: #f0f8ff;
  color: #2080f0;
}

/* ===== 插件行卡 ===== */
.ap-plugin {
  margin-top: 12px;
  border: 1px solid #e5e6eb;
  border-radius: 10px;
  background: #fff;
  overflow: hidden;
  transition: border-color 0.2s, box-shadow 0.2s;
}
.ap-plugin:hover {
  border-color: #c8d3e8;
  box-shadow: 0 2px 10px rgba(0, 0, 0, 0.04);
}
.ap-plugin-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 10px 14px;
  cursor: pointer;
  flex-wrap: wrap;
}
.ap-plugin-icon {
  flex-shrink: 0;
}
.ap-plugin-icon--ph {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 24px;
  height: 24px;
  border-radius: 50%;
  background: #f0f2f5;
  color: #909399;
}
.ap-plugin-name {
  font-weight: 600;
  font-size: 14px;
}
.ap-plugin-stats {
  flex: 1;
  min-width: 100px;
  text-align: right;
  font-size: 12px;
  color: #909399;
}
.ap-plugin-actions {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  flex-shrink: 0;
}
.ap-plugin-chevron {
  /* 裸 <component :is> 渲染的 SVG 没有固有尺寸,必须显式约束,否则撑满整行 */
  width: 14px;
  height: 14px;
  flex-shrink: 0;
  color: #909399;
}
/* 收起态摘要行：描述 + 前三个示例 pill */
.ap-plugin-brief {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
  padding: 0 14px 10px 46px;
}
.ap-plugin-desc {
  font-size: 12px;
  color: #909399;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 420px;
}
.ap-example-pill {
  display: inline-block;
  padding: 2px 10px;
  border-radius: 999px;
  font-size: 12px;
  color: #4a6da7;
  background: rgba(32, 128, 240, 0.08);
  border: 1px solid rgba(32, 128, 240, 0.14);
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ===== 展开详情 ===== */
.ap-plugin-detail {
  padding: 4px 14px 14px 46px;
  border-top: 1px solid #f0f1f4;
  margin-top: 0;
}
.ap-detail-desc {
  margin-top: 10px;
  font-size: 13px;
  color: #606266;
}
.ap-detail-sec {
  margin-top: 14px;
}
.ap-detail-label {
  font-size: 12px;
  font-weight: 600;
  color: #909399;
  margin-bottom: 8px;
}
.ap-detail-pills,
.ap-detail-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.ap-detail-rows {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.ap-detail-row {
  display: flex;
  align-items: baseline;
  gap: 8px;
  font-size: 12px;
}
.ap-detail-key {
  color: #909399;
  flex-shrink: 0;
  width: 64px;
}
.ap-detail-code {
  font-size: 12px;
  word-break: break-all;
}
/* MCP / CLI 条目行 */
.ap-entry {
  padding: 8px 0;
  border-bottom: 1px dashed #ebedf0;
}
.ap-entry:last-child {
  border-bottom: none;
}
.ap-entry-head {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}
.ap-entry-name {
  font-size: 13px;
  font-weight: 600;
}
.ap-entry-spacer {
  flex: 1;
}
.ap-entry-desc {
  display: flex;
  align-items: baseline;
  gap: 12px;
  flex-wrap: wrap;
  margin-top: 4px;
  font-size: 12px;
}
.ap-entry-meta {
  color: #909399;
}
.ap-entry-warn {
  color: #d48806;
}
.ap-entry-hint {
  color: #2080f0;
}
.ap-entry-err {
  color: #d03050;
  word-break: break-all;
}

/* ===== 未就绪引导 ===== */
.ap-blocker {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 10px 0;
  border-bottom: 1px dashed #ebedf0;
}
.ap-blocker:last-of-type {
  border-bottom: none;
}
.ap-blocker-text {
  flex: 1;
  font-size: 13px;
  color: #606266;
  word-break: break-all;
}

/* ===== 深色模式 ===== */
@media (prefers-color-scheme: dark) {
  .drop-strip {
    background: #1f1f1f;
    border-color: #3a3a3a;
    color: #b8b8b8;
  }
  .drop-strip:hover,
  .drop-strip--active {
    border-color: #4a7dc0;
    background: #1c2733;
    color: #78a9ff;
  }
  .ap-plugin {
    background: #18181c;
    border-color: #2c2c30;
  }
  .ap-plugin:hover {
    border-color: #3d4a63;
    box-shadow: 0 2px 10px rgba(0, 0, 0, 0.4);
  }
  .ap-plugin-icon--ph {
    background: #26262b;
    color: #7d7d7d;
  }
  .ap-plugin-stats,
  .ap-detail-label,
  .ap-detail-key,
  .ap-entry-meta,
  .ap-plugin-desc,
  .ap-plugin-chevron {
    color: #7d7d7d;
  }
  .ap-detail-desc {
    color: #b8b8b8;
  }
  .ap-plugin-detail {
    border-top-color: #26262b;
  }
  .ap-entry {
    border-bottom-color: #26262b;
  }
  .ap-example-pill {
    color: #9ab4d8;
    background: rgba(80, 148, 240, 0.12);
    border-color: rgba(80, 148, 240, 0.2);
  }
  .ap-entry-warn {
    color: #e0a030;
  }
  .ap-entry-err {
    color: #e05c74;
  }
}
</style>
