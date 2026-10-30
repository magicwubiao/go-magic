<template>
  <div>
    <!--
      页面头：标题 + 两类连接器的切换。

      MCP 与 CLI 的运行时机制完全不同（长连接 JSON-RPC vs 一次性子进程），
      但对用户来说是同一件事——"把我外部的软件接进来"。分成两个页面会让
      "连接器"这个概念在导航里出现两次，所以合成一页两个页签，各自保留
      自己的工具栏、列表形态与弹窗。
    -->
    <div class="mcp-page-head">
      <div class="mcp-page-title">
        <h2>{{ t('mcp.title') }}</h2>
        <span class="mcp-page-subtitle">{{ t('mcp.subtitle') }}</span>
      </div>
      <n-tabs
        v-model:value="activeTab"
        type="segment"
        size="small"
        class="mcp-page-tabs"
      >
        <n-tab name="mcp">
          <span class="mcp-tab-label">
            MCP
            <n-tag size="tiny" :bordered="false" round>{{ mcpStore.servers.length }}</n-tag>
          </span>
        </n-tab>
        <n-tab name="cli">
          <span class="mcp-tab-label">
            CLI
            <n-tag size="tiny" :bordered="false" round>{{ cliCount }}</n-tag>
          </span>
        </n-tab>
      </n-tabs>
    </div>

    <!--
      两个页签都用 v-show 而不是 v-if：
       1. CLI 面板要在首屏就把连接器数量报给页签徽标（v-if 下徽标一直是 0，
          直到用户点进 CLI 页签）；
       2. 切页签时保留各自的搜索词/筛选态，回来不用重设。
    -->
    <div v-show="activeTab === 'mcp'">
      <n-space justify="end" style="margin-bottom: 12px;">
        <n-button @click="openCatalogModal">
          <template #icon>
            <component :is="GridOutline" />
          </template>
          {{ t('mcp.catalogButton') }}
        </n-button>
        <n-button type="primary" @click="openAddModal">
          <template #icon>
            <component :is="AddOutline" />
          </template>
          {{ t('mcp.addServer') }}
        </n-button>
        <n-dropdown
          v-if="mcpStore.servers.length"
          trigger="click"
          :options="batchOptions"
          @select="handleBatchAction"
        >
          <n-button :loading="batchBusy">
            <template #icon>
              <component :is="LayersOutline" />
            </template>
            {{ t('mcp.batchOps') }}
          </n-button>
        </n-dropdown>
        <n-button @click="handleRefresh" :loading="mcpStore.loading">
          <template #icon>
            <component :is="RefreshOutline" />
          </template>
          {{ t('common.refresh') }}
        </n-button>
      </n-space>

    <n-spin v-if="mcpStore.loading && !mcpStore.servers.length" />
    <template v-else>
      <!--
        列表优先：搜索框 + 一行计数摘要 + 连接器列表。
        此前是「3 张统计卡 + 失败告警条 + 6 列表格」——总数/已连/未连在三处
        重复，传输协议与健康检查时间挤占了列表空间。状态收敛为行首色点，
        失败原因就近显示在名字下方（比顶部汇总条更靠近问题本身）。
      -->
      <!-- 工具条：搜索 + 状态筛选 + 动作。形态参照 WorkBuddy 连接器页：
           一排检索/筛选，下面一片卡片，点卡片看详情（抽屉里放工具清单与开关）。 -->
      <div class="conn-toolbar">
        <n-input
          v-model:value="searchQuery"
          clearable
          :placeholder="t('mcp.searchServers')"
          class="conn-search"
        >
          <template #prefix>
            <component :is="SearchOutline" />
          </template>
        </n-input>

        <div class="conn-chips">
          <button
            v-for="chip in statusChips"
            :key="chip.key"
            type="button"
            class="conn-chip"
            :class="{ 'is-active': statusFilter === chip.key }"
            @click="statusFilter = chip.key"
          >
            {{ chip.label }}
          </button>
        </div>

        <n-space :size="8" class="conn-toolbar-actions">
          <n-button size="small" @click="openCatalogModal">
            <template #icon><component :is="GridOutline" /></template>
            {{ t('mcp.catalogButton') }}
          </n-button>
          <n-button size="small" type="primary" @click="openAddModal">
            <template #icon><component :is="AddOutline" /></template>
            {{ t('mcp.addServer') }}
          </n-button>
          <n-dropdown
            v-if="mcpStore.servers.length"
            trigger="click"
            :options="batchOptions"
            @select="handleBatchAction"
          >
            <n-button size="small" :loading="batchBusy">
              <template #icon><component :is="LayersOutline" /></template>
              {{ t('mcp.batchOps') }}
            </n-button>
          </n-dropdown>
          <n-button size="small" @click="handleRefresh" :loading="mcpStore.loading">
            <template #icon><component :is="RefreshOutline" /></template>
            {{ t('common.refresh') }}
          </n-button>
        </n-space>
      </div>

      <div class="conn-count">
        {{ t('mcp.listSummary', { total: mcpStore.servers.length, connected: mcpStore.connectedServers.length }) }}
      </div>

      <div v-if="visibleServers.length" class="conn-grid">
        <div
          v-for="row in visibleServers"
          :key="row.name"
          class="conn-card"
          :class="{ 'is-idle': !row.connected, 'is-error': !row.connected && !!row.last_error }"
          @click="openDetail(row)"
        >
          <div class="conn-card-head">
            <div class="conn-avatar" :class="statusDotClass(row)">{{ avatarText(row.name) }}</div>
            <div class="conn-ident">
              <div class="conn-name">
                <span class="conn-name-text">{{ row.name }}</span>
                <span class="conn-dot" :class="statusDotClass(row)" />
              </div>
              <div class="conn-sub">
                <span>{{ row.transport || 'stdio' }}</span>
                <span v-if="row.tool_count"> · {{ t('mcp.toolCountLabel', { count: row.tool_count }) }}</span>
              </div>
            </div>
            <n-dropdown
              trigger="click"
              :options="rowMenuOptions(row)"
              @select="(key: string) => handleRowMenu(key, row)"
            >
              <n-button text size="tiny" class="conn-more" @click.stop>
                <template #icon><component :is="EllipsisHorizontalOutline" /></template>
              </n-button>
            </n-dropdown>
          </div>

          <!-- 失败原因直接占位在描述行：比"点开才发现连不上"更有用 -->
          <div
            class="conn-desc"
            :class="{ 'is-error': !row.connected && !!row.last_error }"
            :title="(!row.connected && row.last_error) || row.url || row.command || ''"
          >
            {{ (!row.connected && row.last_error) || row.server_name || row.command || row.url || t('mcp.connNoDesc') }}
          </div>

          <div class="conn-foot" @click.stop>
            <n-tag v-if="row.enabled === false" size="tiny" :bordered="false">
              {{ t('mcp.stateDisabled') }}
            </n-tag>
            <n-tag v-else-if="row.state === 'unauthorized'" size="tiny" type="warning" :bordered="false">
              {{ t('mcp.stateUnauthorized') }}
            </n-tag>
            <span v-else class="conn-state" :class="{ 'is-on': row.connected }">
              {{ row.connected ? t('mcp.connected') : t('mcp.disconnected') }}
            </span>

            <n-button
              v-if="row.connected"
              size="tiny"
              :loading="busy[row.name]"
              @click="handleDisconnect(row.name)"
            >
              {{ t('mcp.disconnect') }}
            </n-button>
            <n-button
              v-else
              size="tiny"
              type="primary"
              :loading="busy[row.name]"
              @click="handleConnect(row)"
            >
              {{ row.last_error ? t('mcp.reconnect') : t('mcp.connect') }}
            </n-button>
          </div>
        </div>
      </div>

      <n-empty
        v-else
        :description="mcpStore.servers.length ? t('mcp.noSearchResult') : t('mcp.noServers')"
        style="margin-top: 16px;"
      >
        <template #extra>
          <n-button v-if="!mcpStore.servers.length" size="small" @click="openAddModal">
            {{ t('mcp.addServer') }}
          </n-button>
        </template>
      </n-empty>
    </template>
    </div>

    <!-- CLI 页签：面板自成一体（自己的数据、自己的弹窗），父组件只收一个计数。 -->
    <CLIConnectorsPanel v-show="activeTab === 'cli'" @count="cliCount = $event" />

    <!-- 详情抽屉：卡片承载"概览"，细节（工具清单/开关/调用统计）放这里，
         不再让每一行在列表里向下撑开——卡片网格一旦有行被撑高，整片就歪了。 -->
    <n-drawer
      :show="!!detailName"
      :width="520"
      placement="right"
      @update:show="(v: boolean) => { if (!v) detailName = '' }"
    >
      <n-drawer-content :title="detailName" closable>
        <component :is="renderExpandedRow(detailRow)" v-if="detailRow" />
        <n-empty v-else :description="t('mcp.noServers')" />
      </n-drawer-content>
    </n-drawer>

    <!-- Add/Edit Server Modal -->
    <n-modal
      v-model:show="showAddModal"
      :title="isEditing ? t('mcp.editServer') : t('mcp.addServer')"
      preset="card"
      class="modal-responsive modal-scroll"
      style="width: 520px; max-width: 96vw; max-height: 85vh;"
    >
      <!-- 弹窗打开后第一个可聚焦元素是"服务名称"输入框（naive 的 focus trap 行为）。
           从别处复制一段 JSON 直接 Ctrl+V，内容会整段落进名字框——而 JSON 输入框在
           另一个分支里，用户看到的就是"粘贴没反应、内容跑到表单里了"。
           这里在捕获阶段先看一眼剪贴板：像 JSON 就拦下来，切到 JSON 模式再填入。 -->
      <div class="mcp-modal-body" @paste.capture="onModalPaste">
        <n-space vertical>
          <n-form-item v-if="!isEditing" :label="t('mcp.inputMode')">
            <n-radio-group v-model:value="inputMode" size="small">
              <n-radio-button value="form">{{ t('mcp.modeForm') }}</n-radio-button>
              <n-radio-button value="json">{{ t('mcp.modeJSON') }}</n-radio-button>
            </n-radio-group>
          </n-form-item>

          <!--
            两种录入方式各自包一层**带唯一 key** 的容器，这一步不是装饰，是修 bug 的：

            两支若都写成 <template v-if>/<template v-else>，编译器会产出两个 Fragment，
            Vue 认为"同一位置、同类节点"，于是**原地复用** DOM 与组件实例，只做 props 补丁；
            两支里的 NFormItem → NInput 恰好同类型，JSON 分支的控件就被复用成了表单分支
            那个单行 input（placeholder 还是 e.g., filesystem，rows/type 全被吃掉）。
            现象就是"切到 JSON 之后，JSON 配置下面还是个小单行框"。
            显式 key 让两支强制卸载/重建，彻底断掉复用。
          -->
          <!-- JSON 模式：粘贴别的 MCP 客户端（Claude Desktop / Cursor / Cline）
               或 config.json 里的片段即可，支持一次导入多个服务器。 -->
          <div v-if="jsonMode" key="mcp-mode-json">
            <n-form-item :label="t('mcp.jsonConfig')" required>
              <n-input
                key="mcp-json-input"
                ref="jsonInputRef"
                v-model:value="jsonText"
                type="textarea"
                :rows="12"
                :placeholder="jsonPlaceholder"
              />
            </n-form-item>
            <div class="json-preview">
              <n-text v-if="jsonPreview.error" type="error" style="font-size: 12px;">
                {{ t('mcp.jsonInvalid') }}{{ jsonPreview.error }}
              </n-text>
              <n-text v-else-if="jsonPreview.names.length" depth="3" style="font-size: 12px;">
                {{ t('mcp.jsonDetected', { count: jsonPreview.names.length }) }}
                <span class="json-names">{{ jsonPreview.names.join(', ') }}</span>
              </n-text>
              <n-text v-else depth="3" style="font-size: 12px;">
                {{ t('mcp.jsonHint') }}
              </n-text>
            </div>
          </div>

          <div v-else key="mcp-mode-form">
            <n-form-item :label="t('mcp.serverName')" required>
              <n-input
                key="mcp-name-input"
                v-model:value="formData.name"
                :disabled="isEditing"
                placeholder="e.g., filesystem"
              />
            </n-form-item>

            <n-form-item :label="t('mcp.transport')" required>
              <n-select
                key="mcp-transport-select"
                v-model:value="formData.transport"
                :options="transportOptions"
                placeholder="Select transport"
              />
            </n-form-item>

            <n-form-item :label="t('mcp.command')" v-if="formData.transport === 'stdio'" required>
              <n-input
                key="mcp-command-input"
                v-model:value="formData.command"
                placeholder="e.g., npx"
              />
            </n-form-item>

            <n-form-item :label="t('mcp.args')" v-if="formData.transport === 'stdio'">
              <n-input
                key="mcp-args-input"
                v-model:value="formData.argsStr"
                type="textarea"
                :rows="3"
                placeholder="-y @modelcontextprotocol/server-filesystem /tmp"
              />
            </n-form-item>

            <n-form-item :label="t('mcp.url')" v-if="isRemote" required>
              <n-input
                key="mcp-url-input"
                v-model:value="formData.url"
                placeholder="https://example.com/mcp"
              />
            </n-form-item>

            <!--
              请求头是远程连接器唯一的认证入口：云端 MCP 基本都要求
              Authorization 或 X-API-Key。放在 URL 下面，因为它是"URL 的凭据"。
              值的回填是 *** 掩码，保存时后端按 key 还原成磁盘真值。
            -->
            <n-form-item v-if="isRemote" :label="t('mcp.headers')">
              <n-input
                key="mcp-headers-input"
                v-model:value="formData.headersStr"
                type="textarea"
                :rows="2"
                :placeholder="t('mcp.headersPlaceholder')"
              />
              <template #feedback>
                <n-text depth="3" style="font-size: 12px;">{{ t('mcp.headersHint') }}</n-text>
              </template>
            </n-form-item>

            <n-form-item :label="t('mcp.env')" v-if="!isRemote">
              <n-input
                key="mcp-env-input"
                v-model:value="formData.envStr"
                type="textarea"
                :rows="2"
                placeholder="KEY=value&#10;ANOTHER_KEY=value"
              />
            </n-form-item>

            <n-form-item :label="t('mcp.behavior')">
              <n-space vertical size="small" style="width: 100%;">
                <n-space align="center" size="small">
                  <n-switch v-model:value="formData.autoConnect" size="small" />
                  <n-text style="font-size: 13px;">{{ t('mcp.autoConnect') }}</n-text>
                </n-space>
                <n-space align="center" size="small">
                  <n-switch v-model:value="formData.requireApproval" size="small" />
                  <n-text style="font-size: 13px;">{{ t('mcp.requireApproval') }}</n-text>
                </n-space>
                <n-input
                  v-model:value="formData.profilesStr"
                  size="small"
                  :placeholder="t('mcp.profilesPlaceholder')"
                />
                <n-text depth="3" style="font-size: 12px;">{{ t('mcp.profilesHint') }}</n-text>

                <n-space align="center" size="small">
                  <n-switch
                    v-model:value="formData.useOAuth"
                    size="small"
                    :disabled="formData.transport === 'stdio'"
                  />
                  <n-text style="font-size: 13px;">{{ t('mcp.oauthToggle') }}</n-text>
                </n-space>
                <template v-if="formData.useOAuth && isRemote">
                  <n-text depth="3" style="font-size: 12px;">{{ t('mcp.oauthHint') }}</n-text>
                  <n-input
                    v-model:value="formData.oauthClientId"
                    size="small"
                    :placeholder="t('mcp.oauthClientId')"
                  />
                  <n-input
                    v-model:value="formData.oauthClientSecret"
                    type="password"
                    show-password-on="click"
                    size="small"
                    :placeholder="t('mcp.oauthClientSecret')"
                  />
                  <n-input
                    v-model:value="formData.oauthScopes"
                    size="small"
                    :placeholder="t('mcp.oauthScopes')"
                  />
                </template>
              </n-space>
            </n-form-item>
          </div>
        </n-space>
      </div>

      <template #footer>
        <n-space justify="end">
          <n-button :disabled="saving" @click="showAddModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="saving" :disabled="saving" @click="handleSave">
            {{ t('common.save') }}
          </n-button>
        </n-space>
      </template>
    </n-modal>

    <!--
      从目录添加：内置一批常见 MCP server 的命令/参数/需要的凭据，
      用户只需挑一个、填自己的 token。终结点仍是 POST /api/mcp/servers，
      与手工填写完全同一条路径，目录不引入新的运行时概念。
    -->
    <n-modal
      v-model:show="showCatalogModal"
      :title="t('mcp.catalogTitle')"
      preset="card"
      class="modal-responsive"
      style="width: 720px; max-width: 96vw; max-height: 85vh;"
    >
      <n-input
        v-model:value="catalogQuery"
        clearable
        :placeholder="t('mcp.catalogSearch')"
        style="margin-bottom: 12px;"
        @update:value="onCatalogQueryChange"
      >
        <template #prefix>
          <component :is="SearchOutline" />
        </template>
      </n-input>

      <n-space v-if="catalogCategories.length" size="small" style="margin-bottom: 12px;">
        <n-tag
          v-for="c in catalogCategories"
          :key="c.id"
          size="small"
          checkable
          :checked="catalogCategory === c.id"
          @update:checked="(v: boolean) => toggleCatalogCategory(c.id, v)"
        >
          {{ c.label }}
        </n-tag>
      </n-space>

      <n-spin v-if="catalogLoading" />
      <n-scrollbar v-else style="max-height: 52vh;">
        <div class="mcp-catalog-list">
          <div v-for="item in catalogItems" :key="item.id" class="mcp-catalog-item">
            <div class="mcp-catalog-main">
              <div class="mcp-catalog-name">{{ item.name }}</div>
              <div class="mcp-catalog-desc">{{ item.description }}</div>
              <n-space size="small" style="margin-top: 6px;">
                <n-tag size="tiny" :bordered="false">{{ item.transport }}</n-tag>
                <n-tag v-for="tag in (item.tags || []).slice(0, 3)" :key="tag" size="tiny" :bordered="false">
                  {{ tag }}
                </n-tag>
              </n-space>
            </div>
            <n-button size="small" type="primary" secondary @click="installFromCatalog(item)">
              {{ t('mcp.catalogInstall') }}
            </n-button>
          </div>
          <n-empty v-if="!catalogItems.length" :description="t('mcp.catalogEmpty')" style="margin: 24px 0;" />
        </div>
      </n-scrollbar>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, nextTick, watch, h } from 'vue'
import { useMessage } from 'naive-ui'
import { NAlert, NButton, NCard, NCollapse, NCollapseItem, NDrawer, NDrawerContent, NDropdown, NEmpty, NFormItem, NIcon, NInput, NModal, NRadioButton, NRadioGroup, NScrollbar, NSelect, NSpace, NSpin, NSwitch, NTab, NTabs, NTag, NText } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import {
  AddOutline,
  RefreshOutline,
  SearchOutline,
  EllipsisHorizontalOutline,
  CopyOutline,
  GridOutline,
  LayersOutline,
} from '@vicons/ionicons5'
import CLIConnectorsPanel from '@/components/CLIConnectorsPanel.vue'
import { useMCPStore } from '@/stores/mcp'
import { getMCPCatalog, getMCPOAuthStatus, getMCPToolStats, mcpErrorMessage, serverConfigToJSON, setMCPToolEnabled, signOutMCPOAuth, startMCPOAuth, testMCPServer } from '@/api/mcp'
import type { CatalogCategory, CatalogConnector, MCPConfig, MCPServer, MCPToolStat } from '@/api/mcp'

const { t } = useI18n()
const message = useMessage()
const mcpStore = useMCPStore()

/** 页签：mcp（长连接 JSON-RPC）/ cli（一次性子进程）。 */
const activeTab = ref<'mcp' | 'cli'>('mcp')
/** CLI 页签的连接器数量，由面板上报（面板自己管数据，父组件不做二次拉取）。 */
const cliCount = ref(0)

const showAddModal = ref(false)
const isEditing = ref(false)
const serverTools = ref<Record<string, any[]>>({})
/** 每个连接器下各工具的调用次数（展开时拉一次，用于判断"到底在不在用"） */
const toolStats = ref<Record<string, Record<string, MCPToolStat>>>({})
const toolsLoading = ref<Record<string, boolean>>({})
const batchBusy = ref(false)
// 受控展开：卡片点开后，详情抽屉里展示该服务器的工具清单。
const expandedKeys = ref<string[]>([])
/** 当前详情抽屉指向的连接器（空 = 抽屉关闭）。 */
const detailName = ref('')
const detailRow = computed(
  () => mcpStore.servers.find((s) => s.name === detailName.value) ?? null,
)

/** 点卡片：打开抽屉并确保工具清单已加载（与展开同一套加载逻辑）。 */
function openDetail(row: MCPServer) {
  detailName.value = row.name
  if (!isExpanded(row.name)) {
    expandedKeys.value = [...expandedKeys.value, row.name]
    void loadToolsIfNeeded(row.name)
  }
}

/** 卡片头像文字：取名字首字符，WorkBuddy 连接器卡片同款做法。 */
function avatarText(name: string): string {
  const trimmed = (name || '').trim()
  if (!trimmed) return '?'
  return trimmed.slice(0, 2).toUpperCase()
}

// 添加弹窗的两种录入方式：表单（单服务器）与 JSON（可一次导入多个）。
const inputMode = ref<'form' | 'json'>('form')
const jsonText = ref('')
// naive 的 n-input 实例：切到 JSON 模式后把光标放进去，用户点完"JSON"就能直接
// Ctrl+V，不会再出现"光标还在表单里"的错位粘贴。
const jsonInputRef = ref<any>(null)

// 只有"新增 + JSON 模式"才走 JSON 分支；编辑既有服务器永远走表单。
// 单独抽成 computed：模板里两处（分支条件、粘贴判断）共用一份口径，避免写岔。
const jsonMode = computed(() => inputMode.value === 'json' && !isEditing.value)

const jsonPlaceholder = `{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"],
      "env": { "TOKEN": "xxx" }
    }
  }
}`

const formData = reactive({
  name: '',
  transport: 'stdio',
  command: '',
  argsStr: '',
  url: '',
  envStr: '',
  /** 请求头，一行一个 KEY=value。远程连接器的认证全靠它。 */
  headersStr: '',
  /** 限定生效的分身，逗号分隔；空 = 所有分身 */
  profilesStr: '',
  /** 每次调用都要人工确认（给没写 annotations 的 server 兜底） */
  requireApproval: false,
  /** 启动时自动连接 */
  autoConnect: true,
  /** 启用 OAuth 2.1 授权（仅远程连接器有意义） */
  useOAuth: false,
  oauthClientId: '',
  oauthClientSecret: '',
  oauthScopes: '',
})

const transportOptions = [
  { label: 'STDIO', value: 'stdio' },
  { label: 'HTTP', value: 'http' },
  { label: 'SSE', value: 'sse' },
]

/** 远程传输（URL + headers 这两块只对它们有意义）。 */
const isRemote = computed(() => formData.transport === 'sse' || formData.transport === 'http')

/**
 * 本地预检粘贴的 JSON —— 只为在提交前给出即时反馈（列出识别到的服务器名）。
 * 真正的权威解析在后端（internal/mcp/import.go），它接受的形态更多
 * （jsonc 注释、http 传输、args 整串写法…），所以这里识别不出来时不做拦截。
 */
const jsonPreview = computed<{ names: string[]; error: string }>(() => {
  const text = jsonText.value.trim()
  if (!text) return { names: [], error: '' }

  let raw: any
  try {
    raw = JSON.parse(text)
  } catch (e: any) {
    return { names: [], error: String(e?.message ?? e) }
  }

  const names = new Set<string>()
  const collect = (node: any): boolean => {
    if (Array.isArray(node)) {
      node.forEach((item) => {
        if (item && typeof item === 'object' && typeof item.name === 'string') names.add(item.name)
      })
      return true
    }
    if (!node || typeof node !== 'object') return false
    for (const key of ['mcpServers', 'servers', 'mcp']) {
      if (node[key] && typeof node[key] === 'object') return collect(node[key])
    }
    if (typeof node.name === 'string' && (node.command || node.url || node.type || node.transport)) {
      names.add(node.name)
      return true
    }
    let sawObject = false
    Object.entries(node).forEach(([key, value]) => {
      if (value && typeof value === 'object' && !Array.isArray(value)) {
        names.add(key)
        sawObject = true
      }
    })
    return sawObject
  }

  if (!collect(raw)) return { names: [], error: '' }
  return { names: [...names], error: '' }
})

// n-data-table 的列已被列表取代：状态收敛为行首色点，主动作随状态变化，
// 低频动作进「…」菜单，失败原因就近显示。展开仍是受控的（见 expandedKeys）。

// 展开行：失败原因 + 工具清单（含参数 schema / 调用名 / 复制）。
function renderExpandedRow(row: MCPServer) {
  const tools = serverTools.value[row.name] ?? []
  const loading = !!toolsLoading.value[row.name]

  const children: any[] = []

  if (!row.connected && row.last_error) {
    children.push(h(NAlert, {
      type: row.state === 'unauthorized' ? 'warning' : 'error',
      showIcon: true,
      style: 'margin-bottom: 12px;',
    }, {
      header: () => (row.state === 'unauthorized' ? t('mcp.authFailed') : t('mcp.connectFailed')),
      default: () => h('div', null, [
        h('div', { class: 'error-detail' }, row.last_error),
        // error_hint 是后端按 error_kind 给的动作建议：补凭据 / 查网络 / 改 URL。
        row.error_hint
          ? h(NText, { depth: 3, style: 'font-size: 12px; display: block; margin-top: 6px;' }, { default: () => row.error_hint })
          : null,
      ].filter(Boolean)),
    }))
  }

  // OAuth：配置了就展示状态与登录/登出入口。unauthorized 状态下这是
  // 最重要的一颗按钮——错误提示说"凭据有问题"，这颗按钮就是修复动作。
  if (row.oauth?.enabled) {
    const stateKey: Record<string, string> = {
      authorized: 'mcp.oauthStateAuthorized',
      awaiting_authorization: 'mcp.oauthStateAwaiting',
      failed: 'mcp.oauthStateFailed',
      expired: 'mcp.oauthStateExpired',
      none: 'mcp.oauthStateNone',
    }
    children.push(h(NSpace, { size: 8, align: 'center', style: 'margin-bottom: 12px;' }, {
      default: () => [
        h(NTag, {
          size: 'small',
          type: row.oauth!.state === 'authorized' ? 'success' : (row.oauth!.state === 'failed' ? 'error' : 'warning'),
          bordered: false,
        }, { default: () => t(stateKey[row.oauth!.state] || 'mcp.oauthStateNone') }),
        h(NButton, {
          size: 'small',
          type: row.oauth!.state === 'authorized' ? 'default' : 'primary',
          secondary: true,
          loading: oauthBusy.value,
          onClick: () => handleOAuthSignIn(row),
        }, { default: () => t('mcp.oauthSignIn') }),
        row.oauth!.has_token
          ? h(NButton, {
              size: 'small',
              quaternary: true,
              onClick: () => handleOAuthSignOut(row),
            }, { default: () => t('mcp.oauthSignOut') })
          : null,
        row.oauth!.error
          ? h(NText, { depth: 3, style: 'font-size: 12px;' }, { default: () => row.oauth!.error })
          : null,
      ].filter(Boolean),
    }))
  }

  if (row.connected && (row.server_name || row.protocol)) {
    children.push(h(NText, { depth: 3, style: 'font-size: 12px; display: block; margin-bottom: 8px;' }, {
      default: () => [
        row.server_name ? `${row.server_name}${row.server_version ? ' v' + row.server_version : ''}` : '',
        row.protocol ? ` · MCP ${row.protocol}` : '',
      ].join(''),
    }))
  }

  const toolHeaderExtra = h(NSpace, { size: 8, align: 'center' }, {
    default: () => [
      h(NText, { depth: 3, style: 'font-size: 12px;' }, {
        default: () => t('mcp.toolCountLabel', { count: tools.length }),
      }),
      h(NButton, {
        text: true, size: 'small', disabled: !row.connected,
        onClick: () => handleRefreshTools(row.name),
      }, {
        icon: () => h(NIcon, null, { default: () => h(RefreshOutline) }),
        default: () => t('mcp.refreshTools'),
      }),
    ],
  })

  // 被屏蔽的工具不进注册表，模型完全看不到；这里逐个开关。
  const disabledSet = new Set(row.disabled_tools || [])

  let toolBody: any
  if (loading) {
    toolBody = h(NSpin, { size: 'small' })
  } else if (tools.length) {
    toolBody = h(NCollapse, null, {
      default: () => tools.map((tool: any) =>
        h(NCollapseItem, { key: tool.name, name: tool.name }, {
          header: () => h(NSpace, { size: 6, align: 'center' }, {
            default: () => [
              h(NText, { code: true }, { default: () => tool.name }),
              disabledSet.has(tool.name)
                ? h(NTag, { size: 'tiny', type: 'warning', bordered: false }, { default: () => t('mcp.toolHidden') })
                : null,
            ].filter(Boolean),
          }),
          'header-extra': () =>
            h(NSpace, { size: 8, align: 'center' }, {
              default: () => [
                h(NSwitch, {
                  size: 'small',
                  value: !disabledSet.has(tool.name),
                  'onUpdate:value': (v: boolean) => handleToggleTool(row, tool.name, v),
                  onClick: (e: Event) => e.stopPropagation(),
                }),
                h(NButton, {
                  text: true, size: 'tiny',
                  onClick: (e: Event) => { e.stopPropagation(); handleCopyToolName(row.name, tool.name) },
                }, { icon: () => h(NIcon, null, { default: () => h(CopyOutline) }) }),
              ],
            }),
          default: () =>
            h(NSpace, { vertical: true, size: 8 }, {
              default: () => [
                tool.description
                  ? h(NText, { depth: 2, style: 'font-size: 13px;' }, { default: () => tool.description })
                  : h(NText, { depth: 3, style: 'font-size: 12px;' }, { default: () => t('mcp.noDescription') }),
                (() => {
                  const st = toolStats.value[row.name]?.[tool.name]
                  if (!st || !st.calls) return null
                  return h(NText, { depth: 3, style: 'font-size: 12px;' }, {
                    default: () => (st.failures
                      ? t('mcp.toolUsageFailed', { calls: st.calls, failures: st.failures })
                      : t('mcp.toolUsage', { calls: st.calls })),
                  })
                })(),
                hasSchema(tool)
                  ? h('div', null, [
                      h(NText, { depth: 3, style: 'font-size: 12px; display: block; margin-bottom: 4px;' }, { default: () => t('mcp.parameters') }),
                      h('pre', { class: 'schema-pre' }, formatSchema(tool)),
                    ])
                  : null,
                h(NText, { depth: 3, style: 'font-size: 12px;' }, {
                  default: () => [
                    t('mcp.callAs') + ' ',
                    h(NText, { code: true }, { default: () => registryToolName(row.name, tool.name) }),
                  ],
                }),
              ].filter(Boolean),
            }),
        }),
      ),
    })
  } else {
    toolBody = h(NEmpty, {
      description: row.connected ? t('mcp.noTools') : t('mcp.connectFirst'),
    })
  }

  children.push(h(NCard, { size: 'small', title: t('mcp.tools') }, {
    'header-extra': () => toolHeaderExtra,
    default: () => toolBody,
  }))

  return h('div', { class: 'expanded-row' }, children)
}

const searchQuery = ref('')
/** 状态筛选：全部 / 已连接 / 未连接 / 异常。 */
const statusFilter = ref<'all' | 'connected' | 'idle' | 'error'>('all')
const statusChips = computed(() => [
  { key: 'all' as const, label: t('common.all') },
  { key: 'connected' as const, label: t('mcp.connected') },
  { key: 'idle' as const, label: t('mcp.disconnected') },
  { key: 'error' as const, label: t('common.error') },
])
/** 每行的操作进行态：连接/断开按钮显示 loading，避免重复点击。 */
const busy = reactive<Record<string, boolean>>({})

/**
 * 列表可见项：搜索词同时匹配服务器名与失败原因（用户常按报错内容找），
 * 状态筛选是"我只想看连不上的那些"的快捷入口。
 */
const visibleServers = computed(() => {
  let list = mcpStore.servers
  switch (statusFilter.value) {
    case 'connected':
      list = list.filter(s => s.connected)
      break
    case 'idle':
      list = list.filter(s => !s.connected && !s.last_error)
      break
    case 'error':
      list = list.filter(s => !s.connected && !!s.last_error)
      break
  }
  const q = searchQuery.value.trim().toLowerCase()
  if (q) {
    list = list.filter(s =>
      s.name.toLowerCase().includes(q)
      || (s.last_error ?? '').toLowerCase().includes(q),
    )
  }
  return list
})

/**
 * 行首色点：绿=已连接、红=失败、灰=未连接。
 * 用颜色而不是「已连接/未连接」文字标签，是因为状态在这里是**一眼扫过**的
 * 信息，不需要逐行阅读；失败原因本身会显示在名字下方。
 */
function statusDotClass(row: MCPServer): string {
  if (row.connected) return 'is-ok'
  if (row.last_error) return 'is-error'
  return 'is-idle'
}

function isExpanded(name: string): boolean {
  return expandedKeys.value.includes(name)
}

/** 行内「…」菜单：低频动作，不参与每次操作的主路径决策。 */
function rowMenuOptions(row: MCPServer) {
  const options: any[] = []
  if (row.configured) {
    options.push({ label: t('common.edit'), key: 'edit' })
  }
  if (row.connected) {
    options.push({
      label: t('mcp.healthCheck'),
      key: 'health',
      disabled: !!busy[row.name],
    })
  }
  if (row.configured) {
    options.push({
      label: t('mcp.testConnection'),
      key: 'test',
      disabled: !!busy[row.name],
    })
  }
  options.push({ label: t('mcp.copyConfig'), key: 'copy' })
  options.push({ type: 'divider', key: 'd1' })
  options.push({ label: t('common.delete'), key: 'delete' })
  return options
}

async function handleRowMenu(key: string, row: MCPServer) {
  switch (key) {
    case 'edit':
      openEditModal(row)
      return
    case 'health':
      await handleHealthCheck(row.name)
      return
    case 'test':
      await handleTest(row)
      return
    case 'copy':
      handleCopyConfig(row)
      return
    case 'delete':
      await handleDelete(row.name)
      return
  }
}

const batchOptions = computed(() => [
  {
    label: t('mcp.batchReconnectDisconnected'),
    key: 'reconnect',
    disabled: mcpStore.disconnectedServers.length === 0,
  },
  {
    label: t('mcp.batchHealthCheckAll'),
    key: 'health',
    disabled: mcpStore.connectedServers.length === 0,
  },
])

async function handleBatchAction(key: string) {
  if (key === 'reconnect') {
    const names = mcpStore.disconnectedServers.map(s => s.name)
    if (!names.length) return
    batchBusy.value = true
    try {
      const results = await mcpStore.reconnectMany(names)
      const ok = results.filter(r => r.ok).length
      const bad = results.filter(r => !r.ok)
      if (bad.length) {
        message.warning(
          `${t('mcp.batchReconnectResult', { ok, failed: bad.length })} ${bad.map(b => `${b.name}: ${b.error}`).join('; ')}`,
          { duration: 8000 },
        )
      } else {
        message.success(t('mcp.batchReconnectResult', { ok, failed: 0 }))
      }
    } finally {
      batchBusy.value = false
    }
    return
  }

  if (key === 'health') {
    batchBusy.value = true
    try {
      const results = await mcpStore.healthCheckAll()
      const bad = results.filter(r => !r.healthy)
      if (bad.length) {
        message.warning(
          `${t('mcp.batchHealthResult', { ok: results.length - bad.length, failed: bad.length })} ${bad.map(b => `${b.name}: ${b.error ?? ''}`).join('; ')}`,
          { duration: 8000 },
        )
      } else {
        message.success(t('mcp.batchHealthResult', { ok: results.length, failed: 0 }))
      }
    } finally {
      batchBusy.value = false
    }
  }
}

function hasSchema(tool: any): boolean {
  const schema = tool?.inputSchema
  if (!schema || typeof schema !== 'object') return false
  const props = schema.properties
  return !!props && Object.keys(props).length > 0
}

function formatSchema(tool: any): string {
  const schema = tool?.inputSchema ?? {}
  const props = schema.properties ?? {}
  const required: string[] = Array.isArray(schema.required) ? schema.required : []
  const lines = Object.entries(props).map(([key, value]: [string, any]) => {
    const type = value?.type ?? 'any'
    const desc = value?.description ? ` — ${value.description}` : ''
    const req = required.includes(key) ? ' *' : ''
    return `  ${key}: ${type}${req}${desc}`
  })
  return lines.length ? lines.join('\n') : JSON.stringify(schema, null, 2)
}

/** 工具在 Agent 注册表里的名字：mcp_<server>_<tool>。 */
function registryToolName(serverName: string, toolName: string): string {
  return `mcp_${serverName}_${toolName}`
}

async function copyText(text: string, okMsg: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.success(okMsg)
  } catch {
    message.error(t('common.copyFailed'))
  }
}

function handleCopyConfig(row: MCPServer) {
  copyText(serverConfigToJSON(row), t('mcp.configCopied'))
}

function handleCopyToolName(serverName: string, toolName: string) {
  copyText(registryToolName(serverName, toolName), t('common.copied'))
}

async function handleRefresh() {
  await mcpStore.loadServers()
  message.success(t('common.refreshed'))
}

/** 展开某行时懒加载工具清单（已缓存则不重复请求）。 */
async function loadToolsIfNeeded(name: string) {
  if (serverTools.value[name]) return
  toolsLoading.value[name] = true
  try {
    serverTools.value[name] = await mcpStore.loadServerTools(name)
    // 统计是附加信息，失败不影响工具清单展示。
    try {
      toolStats.value[name] = await getMCPToolStats(name)
    } catch {
      toolStats.value[name] = {}
    }
  } finally {
    toolsLoading.value[name] = false
  }
}

async function handleHealthCheck(name: string) {
  try {
    await mcpStore.healthCheck(name)
    const status = mcpStore.getHealthStatus(name)
    if (status) {
      message.success(t('mcp.healthOK', { name }))
    } else {
      const detail = mcpStore.getLastError(name)
      message.error(detail ? `${t('mcp.healthFailed', { name })}: ${detail}` : t('mcp.healthFailed', { name }))
    }
    // 健康检查会改变 connected / last_error / last_health_check，刷新以同步界面
    await mcpStore.loadServers()
    if (mcpStore.servers.find(s => s.name === name)?.connected) {
      serverTools.value[name] = await mcpStore.loadServerTools(name)
    }
  } catch (e: any) {
    message.error(mcpErrorMessage(e) || t('mcp.healthFailed', { name }))
  }
}

/**
 * 行内主动作（未连接时）：连上或重连。
 * 用 reconnectServer 而不是 connectServer —— 后者要求传完整 config（表单路径
 * 才有的东西），而这里只要"用已存盘的配置重新连上"。后端 handleMCPConnect
 * 对已配置的 server 同样走这条路，两种情况都覆盖。
 */
async function handleConnect(row: MCPServer) {
  if (busy[row.name]) return
  busy[row.name] = true
  try {
    await mcpStore.reconnectServer(row.name)
    message.success(t('mcp.reconnected', { name: row.name }))
    // 连上后工具清单变了，清缓存，等展开时重新拉
    delete serverTools.value[row.name]
  } catch (e: any) {
    message.error(mcpErrorMessage(e) || t('mcp.reconnectFailed', { name: row.name }))
    await mcpStore.loadServers()
  } finally {
    busy[row.name] = false
  }
}

async function handleDisconnect(name: string) {
  if (busy[name]) return
  busy[name] = true
  try {
    await mcpStore.disconnectServer(name)
    message.success(t('mcp.serverDisconnected', { name }))
    // 断开后工具不再可用，清掉缓存避免误导
    delete serverTools.value[name]
    if (isExpanded(name)) {
      expandedKeys.value = expandedKeys.value.filter(k => k !== name)
    }
  } catch (e: any) {
    message.error(mcpErrorMessage(e) || t('mcp.disconnectFailed', { name }))
  } finally {
    busy[name] = false
  }
}

// --- 内置连接器目录 ---

const showCatalogModal = ref(false)
const catalogItems = ref<CatalogConnector[]>([])
const catalogCategories = ref<CatalogCategory[]>([])
const catalogQuery = ref('')
const catalogCategory = ref('')
const catalogLoading = ref(false)
let catalogTimer: ReturnType<typeof setTimeout> | null = null

async function loadCatalog() {
  catalogLoading.value = true
  try {
    const res = await getMCPCatalog(catalogQuery.value.trim() || undefined, catalogCategory.value || undefined)
    catalogItems.value = res.connectors || []
    catalogCategories.value = res.categories || []
  } catch (e: any) {
    message.error(mcpErrorMessage(e) || t('mcp.catalogFailed'))
    catalogItems.value = []
  } finally {
    catalogLoading.value = false
  }
}

function openCatalogModal() {
  showCatalogModal.value = true
  if (!catalogItems.value.length) void loadCatalog()
}

/** 搜索防抖：目录是本地接口，但逐字请求仍会让列表闪烁。 */
function onCatalogQueryChange() {
  if (catalogTimer) clearTimeout(catalogTimer)
  catalogTimer = setTimeout(() => { void loadCatalog() }, 250)
}

function toggleCatalogCategory(id: string, checked: boolean) {
  catalogCategory.value = checked ? id : ''
  void loadCatalog()
}

/**
 * 选中目录条目 → 预填表单（而不是直接安装）。
 *
 * 直接安装会跳过一个必然要做的步骤：填凭据。目录里没有凭据，也不该有。
 * 预填后把光标交给用户，填完再保存，失败原因能当场看到。
 */
function installFromCatalog(item: CatalogConnector) {
  showCatalogModal.value = false
  isEditing.value = false
  inputMode.value = 'form'
  jsonText.value = ''
  resetForm()

  formData.name = item.default_name || item.id
  formData.transport = item.transport || 'stdio'
  formData.command = item.command || ''
  formData.argsStr = (item.args || []).join(' ')
  formData.url = item.url || ''

  const envLines: string[] = []
  const headerLines: string[] = []
  for (const req of item.requires || []) {
    if (req.where === 'header') headerLines.push(`${req.key}=`)
    else if (req.where === 'arg') {
      // arg 类凭据（如飞书的 App ID/Secret）走命令行参数：把 Args 里的
      // {{KEY}} 占位符替换成醒目的 <label> 提示，用户在参数框里就地改。
      const token = `{{${req.key}}}`
      formData.argsStr = formData.argsStr.split(token).join(`<${req.label}>`)
    } else envLines.push(`${req.key}=`)
  }
  formData.envStr = envLines.join('\n')
  formData.headersStr = headerLines.join('\n')
  showAddModal.value = true

  if ((item.requires || []).length) {
    message.info(t('mcp.catalogFillRequired', { name: item.name }), { duration: 6000 })
  }
}

// --- 试连与工具开关 ---

/**
 * 试连：把"保存 → 后台连接 → 刷新列表才发现失败"缩成一次点击。
 * 失败时同时给出分类建议（补凭据 / 查网络 / 改 URL），这是后端 error_kind
 * 存在的意义。
 */
async function handleTest(row: MCPServer) {
  if (busy[row.name]) return
  busy[row.name] = true
  try {
    const res = await testMCPServer(row.name)
    if (res.ok) {
      message.success(t('mcp.testOK', { name: row.name, count: res.tool_count ?? 0, ms: res.elapsed_ms ?? 0 }))
    } else {
      const hint = res.error_hint ? ` — ${res.error_hint}` : ''
      message.error(`${res.error || t('mcp.testFailed', { name: row.name })}${hint}`, { duration: 10000 })
    }
    await mcpStore.loadServers()
  } catch (e: any) {
    message.error(mcpErrorMessage(e) || t('mcp.testFailed', { name: row.name }))
  } finally {
    busy[row.name] = false
  }
}

// --- OAuth 2.1 授权 ---

const oauthBusy = ref(false)

/**
 * 发起 OAuth 授权：后端完成发现/注册后返回 authorize URL，浏览器打开它，
 * 用户点"允许"后回调落 token，后端自动重连。这里轮询状态直到授权完成
 * 或超时（3 分钟）——轮询而不是事件推送，是因为 SSE bus 上不该为一次
 * 交互性流程加一种新事件类型。
 */
async function handleOAuthSignIn(row: MCPServer) {
  if (oauthBusy.value) return
  oauthBusy.value = true
  try {
    const res = await startMCPOAuth(row.name)
    if (res.already_authorized) {
      message.success(t('mcp.oauthAlready'))
      await mcpStore.loadServers()
      return
    }
    if (!res.authorize_url) {
      message.error(t('mcp.oauthFailed'))
      return
    }
    window.open(res.authorize_url, '_blank', 'noopener')
    message.info(t('mcp.oauthPopup'), { duration: 8000 })

    const deadline = Date.now() + 3 * 60 * 1000
    while (Date.now() < deadline) {
      await new Promise(r => setTimeout(r, 2000))
      let st
      try {
        st = await getMCPOAuthStatus(row.name)
      } catch {
        continue // 单次轮询失败不终止等待
      }
      if (st.state === 'authorized') {
        message.success(t('mcp.oauthDone', { name: row.name }))
        await mcpStore.loadServers()
        return
      }
      if (st.state === 'failed') {
        message.error(st.error || t('mcp.oauthFailed'), { duration: 10000 })
        await mcpStore.loadServers()
        return
      }
    }
    message.warning(t('mcp.oauthTimeout'))
  } catch (e: any) {
    message.error(mcpErrorMessage(e) || t('mcp.oauthFailed'))
  } finally {
    oauthBusy.value = false
  }
}

async function handleOAuthSignOut(row: MCPServer) {
  try {
    await signOutMCPOAuth(row.name)
    message.success(t('mcp.oauthSignedOut'))
    await mcpStore.loadServers()
  } catch (e: any) {
    message.error(mcpErrorMessage(e) || t('mcp.oauthFailed'))
  }
}

/** 单独启用/屏蔽某个工具：屏蔽的工具不会进模型看到的工具表。 */
async function handleToggleTool(row: MCPServer, toolName: string, enabled: boolean) {
  try {
    await setMCPToolEnabled(row.name, toolName, enabled)
    message.success(enabled
      ? t('mcp.toolEnabled', { tool: toolName })
      : t('mcp.toolDisabled', { tool: toolName }))
    await mcpStore.loadServers()
  } catch (e: any) {
    message.error(mcpErrorMessage(e) || t('mcp.toolToggleFailed'))
    await mcpStore.loadServers()
  }
}

async function handleDelete(name: string) {
  try {
    await mcpStore.removeServer(name)
    delete serverTools.value[name]
    expandedKeys.value = expandedKeys.value.filter(k => k !== name)
    message.success(t('mcp.deleted', { name }))
  } catch (e: any) {
    message.error(mcpErrorMessage(e) || t('mcp.deleteFailed', { name }))
  }
}

async function handleRefreshTools(name: string) {
  try {
    serverTools.value[name] = await mcpStore.refreshTools(name)
    message.success(t('mcp.toolsRefreshed'))
  } catch (e: any) {
    message.error(mcpErrorMessage(e) || t('mcp.refreshToolsFailed'))
  }
}

// 切到 JSON 模式后把光标移进 JSON 输入框。
watch(inputMode, (mode) => {
  if (mode !== 'json') return
  nextTick(() => jsonInputRef.value?.focus?.())
})

/**
 * 弹窗内的粘贴兜底。
 *
 * 为什么需要：弹窗打开后 naive 的 focus trap 会把焦点放在第一个可聚焦元素上，
 * 也就是"服务名称"输入框。此时从别处复制一段 mcpServers JSON 直接 Ctrl+V，
 * 整段 JSON 会落进名字框，而 JSON 输入框在另一个分支里是空的 —— 用户看到的
 * 就是"粘贴没反应、内容跑到表单里了"。服务器名的合法字符只有 [A-Za-z0-9_.-]，
 * 以 { 或 [ 开头的一定是配置，所以在捕获阶段拦下来换成 JSON 模式。
 *
 * 注意：从文档/README/聊天里复制时经常把 Markdown 代码围栏（```json ... ```）
 * 一起带走，那样首字符是 ` 而不是 {，只按"首字符"判断仍然会漏 —— 所以先剥围栏。
 */
function stripCodeFence(text: string): string {
  const m = text.match(/^\s*`{3,}[^\n]*\r?\n([\s\S]*?)\r?\n?\s*`{3,}\s*$/)
  return m ? m[1] : text
}

function onModalPaste(e: ClipboardEvent) {
  // 编辑模式没有 JSON 分支；已经在 JSON 模式下就交给 textarea 自己处理
  if (jsonMode.value) return

  const raw = e.clipboardData?.getData('text') ?? ''
  const text = stripCodeFence(raw)
  const trimmed = text.trim()
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) return

  e.preventDefault()
  jsonText.value = text
  inputMode.value = 'json'
  message.info(t('mcp.jsonPasted'), { duration: 3000 })
}

/** 把 {K: V} 渲染成 "K=V" 的文本块（表单里用换行分隔）。 */
function kvToLines(map?: Record<string, string>): string {
  return Object.entries(map || {}).map(([k, v]) => `${k}=${v}`).join('\n')
}

/**
 * 把 "K=V" 文本块解析成对象。
 *
 * 按第一个 '=' 切分，值里可以再出现 '='（JWT / base64 的 padding 很常见）。
 * 空行与没有 '=' 的行直接跳过——宁可少一个头，也不要发出一个 key 为空、
 * 服务端只会回 400 的请求。
 */
function linesToKV(text: string): Record<string, string> | undefined {
  const out: Record<string, string> = {}
  for (const raw of text.split('\n')) {
    const line = raw.trim()
    if (!line) continue
    const idx = line.indexOf('=')
    if (idx <= 0) continue
    const key = line.slice(0, idx).trim()
    const value = line.slice(idx + 1).trim()
    if (!key) continue
    out[key] = value
  }
  return Object.keys(out).length ? out : undefined
}

function resetForm() {
  Object.assign(formData, {
    name: '',
    transport: 'stdio',
    command: '',
    argsStr: '',
    url: '',
    envStr: '',
    headersStr: '',
    profilesStr: '',
    requireApproval: false,
    autoConnect: true,
    useOAuth: false,
    oauthClientId: '',
    oauthClientSecret: '',
    oauthScopes: '',
  })
}

function openAddModal() {
  isEditing.value = false
  inputMode.value = 'form'
  jsonText.value = ''
  resetForm()
  showAddModal.value = true
}

function openEditModal(row: MCPServer) {
  isEditing.value = true
  inputMode.value = 'form'
  Object.assign(formData, {
    name: row.name,
    transport: row.transport || 'stdio',
    command: row.command || '',
    argsStr: (row.args || []).join(' '),
    url: row.url || '',
    // env / headers 的值是 *** 掩码，原样回传时由后端还原成磁盘上的真值
    envStr: kvToLines(row.env),
    headersStr: kvToLines(row.headers),
    profilesStr: (row.profiles || []).join(', '),
    requireApproval: !!row.require_approval,
    autoConnect: row.enabled !== false,
    useOAuth: !!row.oauth?.enabled,
    // client_secret 是 *** 掩码回显，原样回传时后端还原成磁盘真值
    oauthClientId: row.oauth?.client_id || '',
    oauthClientSecret: row.oauth?.client_secret || '',
    oauthScopes: (row.oauth?.scopes || []).join(', '),
  })
  showAddModal.value = true
}

/**
 * 把表单里的 OAuth 字段聚合成 API 载荷。
 *
 * 关键点是 client_secret 的掩码透传：编辑表单回显的是 ***，用户没改就
 * 原样回传，后端据此保留磁盘真值；清空输入框则真的清掉密钥。
 * stdio 连接器没有 URL，OAuth 无从谈起，直接返回 undefined。
 */
function buildOAuthConfig(): MCPConfig['oauth'] {
  if (!formData.useOAuth || !isRemote.value) return undefined
  const scopes = formData.oauthScopes
    .split(',')
    .map(s => s.trim())
    .filter(Boolean)
  return {
    client_id: formData.oauthClientId.trim() || undefined,
    client_secret: formData.oauthClientSecret || undefined,
    scopes: scopes.length ? scopes : undefined,
  }
}

// saving 同时承担两件事：给保存按钮一个 loading 态，以及**防重入**。
// 以前没有这个状态，用户点一次没反应就会连点，每次点击都发一个 PUT，
// 后端各起一遍连接——把"看起来卡住"放大成真的卡住。
const saving = ref(false)

async function handleSave() {
  if (saving.value) return
  saving.value = true
  try {
    await doSave()
  } finally {
    saving.value = false
  }
}

async function doSave() {
  if (inputMode.value === 'json' && !isEditing.value) {
    await handleImportJSON()
    return
  }

  if (!formData.name) {
    message.error(t('mcp.serverNameRequired'))
    return
  }

  if (formData.transport === 'stdio' && !formData.command) {
    message.error(t('mcp.commandRequired'))
    return
  }

  if (isRemote.value && !formData.url) {
    message.error(t('mcp.urlRequired'))
    return
  }

  const profiles = formData.profilesStr
    .split(',')
    .map(p => p.trim())
    .filter(Boolean)

  const config: MCPConfig = {
    command: formData.transport === 'stdio' ? formData.command : '',
    args: formData.transport === 'stdio' ? formData.argsStr.split(/\s+/).filter(Boolean) : [],
    transport: formData.transport,
    url: isRemote.value ? formData.url : '',
    env: formData.transport === 'stdio' ? formData.envStr.split('\n').filter(Boolean) : undefined,
    headers: isRemote.value ? linesToKV(formData.headersStr) : undefined,
    enabled: formData.autoConnect,
    profiles: profiles.length ? profiles : undefined,
    require_approval: formData.requireApproval || undefined,
    oauth: buildOAuthConfig(),
  }

  try {
    if (isEditing.value) {
      await mcpStore.updateServer(formData.name, config)
      message.success(t('mcp.serverUpdated'))
    } else {
      await mcpStore.addServer(formData.name, config)
      // 保存是异步连接的：配置已落盘，连接在后台跑。
      // 明确告诉用户"已保存，正在连接"，避免看到列表里"未连接"而误以为失败。
      message.success(t('mcp.serverAddedConnecting'))
    }
    showAddModal.value = false
  } catch (e: any) {
    // 保存失败时**不关闭弹窗**：用户填的内容要留着，能直接改完重试。
    message.error(mcpErrorMessage(e) || t('mcp.saveFailed'))
  }
}

async function handleImportJSON() {
  if (!jsonText.value.trim()) {
    message.error(t('mcp.jsonRequired'))
    return
  }
  try {
    const result = await mcpStore.importServersFromJSON(jsonText.value)
    const failed = result?.failed ?? []
    if (failed.length) {
      // 部分成功：配置已经写盘，只是有几个连不上（报错内容来自后端）。
      const detail = failed.map(f => `${f.name}: ${f.error}`).join('; ')
      message.warning(
        `${t('mcp.jsonPartial', { added: result?.added?.length ?? 0, failed: failed.length })} ${detail}`,
        { duration: 8000 },
      )
    } else {
      // 连接在后台进行；成功提示里要带上这一点，否则用户看到列表里
      // 刚加的条目还是"未连接"会以为没生效。
      message.success(t('mcp.jsonAddedConnecting', { count: result?.added?.length ?? 0 }))
    }
    if (result?.warning) {
      message.warning(result.warning, { duration: 8000 })
    }
    showAddModal.value = false
  } catch (e: any) {
    message.error(mcpErrorMessage(e) || t('mcp.saveFailed'))
  }
}

onMounted(async () => {
  await mcpStore.loadServers()
})
</script>

<style scoped>
/* 页面头：左侧标题 + 右侧页签，窄屏自动换行（页签不压缩标题） */
.mcp-page-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 16px;
}

/* 页面标题 + 一行副标题：协议细节收进副标题，主标题统一叫「连接器」 */
.mcp-page-title {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.mcp-page-tabs {
  min-width: 180px;
}

.mcp-tab-label {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.mcp-page-title h2 {
  margin: 0;
}

.mcp-page-subtitle {
  font-size: 12px;
  color: #9ca3af;
}

/* 列表优先：搜索框 + 计数摘要 + 每行一个主动作 */
.mcp-list-row.is-idle {
  opacity: 0.78;
}

.mcp-list-row + .mcp-list-row {
  border-top: 0.5px solid rgba(128, 128, 128, 0.18);
}

.mcp-list-row.is-expanded {
  background: rgba(128, 128, 128, 0.04);
}

/* 状态即色点：绿=已连接、红=失败、灰=未连接 */
/* 失败原因就近显示在名字下方，比顶部汇总条更靠近问题本身 */
/* JSON 模式的预检提示 */
.json-preview {
  margin-top: -12px;
  padding: 0 2px 4px;
  line-height: 1.5;
  word-break: break-all;
}

.json-names {
  color: var(--primary-color, #2080f0);
  font-family: var(--font-family-mono, monospace);
}

/* 失败汇总提示条里的一行：服务器名 + 原因 */
/* 展开行内的容器留白 */
.expanded-row {
  padding: 0;
}

.error-detail {
  font-family: var(--font-family-mono, monospace);
  font-size: 12px;
  line-height: 1.6;
  word-break: break-all;
  white-space: pre-wrap;
}

/* 工具参数 schema 预览块 */
.schema-pre {
  margin: 0;
  padding: 8px 10px;
  border-radius: 4px;
  background: rgba(128, 128, 128, 0.08);
  font-family: var(--font-family-mono, monospace);
  font-size: 12px;
  line-height: 1.6;
  overflow-x: auto;
  white-space: pre;
}

/* 概览卡片在窄屏下堆叠 */
@media (max-width: 640px) {
  :deep(.n-grid) {
    grid-template-columns: 1fr !important;
  }
}

/* 移动端:表格横向滚动 + 底部安全区 */
@media (max-width: 768px) {
  :deep(.n-data-table-wrapper) {
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
  }

  .expanded-row {
    padding: 12px 8px;
  }
}
</style>

<!--
  目录弹窗的样式必须写在全局块里：n-modal 会 teleport 到 body，
  scoped 选择器带的 data-v-xxx 在弹窗内容上不存在，写了也不生效。
-->
<style>
.mcp-catalog-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding-right: 4px;
}

.mcp-catalog-item {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding: 12px;
  border: 1px solid var(--n-border-color, rgba(128, 128, 128, 0.2));
  border-radius: 8px;
}

.mcp-catalog-main {
  min-width: 0;
  flex: 1;
}

.mcp-catalog-name {
  font-weight: 600;
  font-size: 14px;
  margin-bottom: 2px;
}

.mcp-catalog-desc {
  font-size: 12px;
  opacity: 0.75;
  line-height: 1.5;
}
</style>
