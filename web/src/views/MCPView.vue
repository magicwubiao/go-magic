<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 24px;" align="center">
      <h2>{{ t('mcp.title') }}</h2>
      <n-space>
        <n-button type="primary" @click="openAddModal">
          <template #icon>
            <component :is="AddOutline" />
          </template>
          {{ t('mcp.addServer') }}
        </n-button>
        <n-button @click="handleRefresh">
          <template #icon>
            <component :is="RefreshOutline" />
          </template>
          {{ t('common.refresh') }}
        </n-button>
      </n-space>
    </n-space>

    <n-spin v-if="mcpStore.loading" />
    <template v-else>
      <!-- Overview Cards -->
      <n-grid :cols="3" :x-gap="12" style="margin-bottom: 24px;">
        <n-gi>
          <n-card hoverable>
            <n-statistic :label="t('mcp.totalServers')" :value="mcpStore.servers.length" />
          </n-card>
        </n-gi>
        <n-gi>
          <n-card hoverable>
            <n-statistic :label="t('mcp.connected')" :value="mcpStore.connectedServers.length" />
          </n-card>
        </n-gi>
        <n-gi>
          <n-card hoverable>
            <n-statistic :label="t('mcp.disconnected')" :value="mcpStore.disconnectedServers.length" />
          </n-card>
        </n-gi>
      </n-grid>

      <!-- Server List -->
      <n-card :title="t('mcp.servers')">
        <n-table
          :columns="columns"
          :data="mcpStore.servers"
          row-key="name"
          :pagination="false"
          @update:expanded-row-keys="handleRowExpand"
        >
          <template #body-cell:status="{ row }">
            <n-space :size="4" align="center">
              <n-tag :type="row.connected ? 'success' : 'error'" size="small">
                {{ row.connected ? t('mcp.statusConnected') : t('mcp.statusDisconnected') }}
              </n-tag>
              <n-tag v-if="row.configured === false" size="small" :bordered="false">
                {{ t('mcp.notConfigured') }}
              </n-tag>
            </n-space>
          </template>

          <template #body-cell:transport="{ row }">
            <n-tag :type="row.transport === 'stdio' ? 'info' : 'warning'" size="small">
              {{ (row.transport || 'stdio').toUpperCase() }}
            </n-tag>
          </template>

          <template #body-cell:last_health_check="{ row }">
            <n-text depth="3" style="font-size: 12px;">
              {{ row.last_health_check ? formatTime(row.last_health_check) : '-' }}
            </n-text>
          </template>

          <template #body-cell:actions="{ row }">
            <n-space :size="8">
              <n-button
                text
                size="small"
                @click="openEditModal(row)"
                :disabled="!row.configured"
              >
                <template #icon>
                  <component :is="CreateOutline" />
                </template>
              </n-button>
              <n-button
                text
                size="small"
                @click="handleHealthCheck(row.name)"
                :disabled="!row.connected"
              >
                <template #icon>
                  <component :is="HeartOutline" />
                </template>
              </n-button>
              <n-button
                  text
                  size="small"
                  @click="handleReconnect(row.name)"
                  :disabled="row.connected"
                >
                  <template #icon>
                    <component :is="RefreshCircleOutline" />
                  </template>
                </n-button>
              <n-button
                text
                size="small"
                type="error"
                @click="handleDisconnect(row.name)"
                :disabled="!row.connected"
              >
                <template #icon>
                  <component :is="PowerOutline" />
                </template>
              </n-button>
              <n-button
                text
                size="small"
                @click="handleDelete(row.name)"
              >
                <template #icon>
                  <component :is="TrashOutline" />
                </template>
              </n-button>
            </n-space>
          </template>

          <template #expanded-row="{ row }">
            <div style="padding: 16px;">
              <n-space vertical>
                <n-card size="small" :title="t('mcp.tools')">
                  <n-list v-if="serverTools[row.name]?.length > 0">
                    <n-list-item v-for="tool in serverTools[row.name]" :key="tool.name">
                      <n-thing :title="tool.name">
                        <template #description>
                          {{ tool.description }}
                        </template>
                      </n-thing>
                    </n-list-item>
                  </n-list>
                  <n-empty v-else :description="t('mcp.noTools')" />
                </n-card>
                <n-button
                  text
                  size="small"
                  @click="handleRefreshTools(row.name)"
                  :disabled="!row.connected"
                >
                  <template #icon>
                    <component :is="RefreshOutline" />
                  </template>
                  {{ t('mcp.refreshTools') }}
                </n-button>
              </n-space>
            </div>
          </template>
        </n-table>

        <n-empty v-if="!mcpStore.servers.length" :description="t('mcp.noServers')" />
      </n-card>
    </template>

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

        <!-- JSON 模式：粘贴别的 MCP 客户端（Claude Desktop / Cursor / Cline）
             或 config.json 里的片段即可，支持一次导入多个服务器。 -->
        <template v-if="inputMode === 'json' && !isEditing">
          <n-form-item :label="t('mcp.jsonConfig')" required>
            <n-input
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
        </template>

        <template v-else>
          <n-form-item :label="t('mcp.serverName')" required>
            <n-input
              v-model:value="formData.name"
              :disabled="isEditing"
              placeholder="e.g., filesystem"
            />
          </n-form-item>

          <n-form-item :label="t('mcp.transport')" required>
            <n-select
              v-model:value="formData.transport"
              :options="transportOptions"
              placeholder="Select transport"
            />
          </n-form-item>

          <n-form-item :label="t('mcp.command')" v-if="formData.transport === 'stdio'" required>
            <n-input
              v-model:value="formData.command"
              placeholder="e.g., npx"
            />
          </n-form-item>

          <n-form-item :label="t('mcp.args')" v-if="formData.transport === 'stdio'">
            <n-input
              v-model:value="formData.argsStr"
              type="textarea"
              :rows="3"
              placeholder="-y @modelcontextprotocol/server-filesystem /tmp"
            />
          </n-form-item>

          <n-form-item :label="t('mcp.url')" v-if="formData.transport === 'sse'" required>
            <n-input
              v-model:value="formData.url"
              placeholder="http://localhost:8080/mcp"
            />
          </n-form-item>

          <n-form-item :label="t('mcp.env')">
            <n-input
              v-model:value="formData.envStr"
              type="textarea"
              :rows="2"
              placeholder="KEY=value&#10;ANOTHER_KEY=value"
            />
          </n-form-item>
        </template>
      </n-space>
      </div>

      <template #footer>
        <n-space justify="end">
          <n-button @click="showAddModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="handleSave">{{ t('common.save') }}</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted, nextTick, watch } from 'vue'
import { useMessage } from 'naive-ui'
import { NButton, NCard, NEmpty, NFormItem, NGi, NGrid, NInput, NList, NListItem, NModal, NRadioButton, NRadioGroup, NSelect, NSpace, NSpin, NStatistic, NTable, NTag, NText, NThing } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import {
  AddOutline,
  RefreshOutline,
  HeartOutline,
  RefreshCircleOutline,
  PowerOutline,
  TrashOutline,
  CreateOutline,
} from '@vicons/ionicons5'
import { useMCPStore } from '@/stores/mcp'
import { mcpErrorMessage } from '@/api/mcp'
import type { MCPConfig, MCPServer } from '@/api/mcp'

const { t } = useI18n()
const message = useMessage()
const mcpStore = useMCPStore()

const showAddModal = ref(false)
const isEditing = ref(false)
const serverTools = ref<Record<string, any[]>>({})

// 添加弹窗的两种录入方式：表单（单服务器）与 JSON（可一次导入多个）。
const inputMode = ref<'form' | 'json'>('form')
const jsonText = ref('')
// naive 的 n-input 实例：切到 JSON 模式后把光标放进去，用户点完"JSON"就能直接
// Ctrl+V，不会再出现"光标还在表单里"的错位粘贴。
const jsonInputRef = ref<any>(null)

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
})

const transportOptions = [
  { label: 'STDIO', value: 'stdio' },
  { label: 'SSE', value: 'sse' },
]

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

const columns = [
  {
    title: t('mcp.serverName'),
    key: 'name',
    render: (row: any) => ({
      type: 'expand',
      expandTrigger: 'row',
      children: row.name,
    }),
  },
  {
    title: t('mcp.status'),
    key: 'status',
    width: 120,
  },
  {
    title: t('mcp.transport'),
    key: 'transport',
    width: 100,
  },
  {
    title: t('mcp.toolCount'),
    key: 'tool_count',
    width: 100,
    align: 'center',
  },
  {
    title: t('mcp.lastHealthCheck'),
    key: 'last_health_check',
    width: 180,
  },
  {
    title: t('common.actions'),
    key: 'actions',
    width: 240,
  },
]

function formatTime(timeStr: string): string {
  if (!timeStr) return '-'
  return new Date(timeStr).toLocaleString()
}

async function handleRefresh() {
  await mcpStore.loadServers()
  message.success(t('common.refreshed'))
}

async function handleRowExpand(keys: string[]) {
  for (const key of keys) {
    if (!serverTools.value[key]) {
      serverTools.value[key] = await mcpStore.loadServerTools(key)
    }
  }
}

async function handleHealthCheck(name: string) {
  try {
    await mcpStore.healthCheck(name)
    const status = mcpStore.getHealthStatus(name)
    if (status) {
      message.success(t('mcp.healthOK', { name }))
    } else {
      message.error(t('mcp.healthFailed', { name }))
    }
  } catch (e: any) {
    message.error(mcpErrorMessage(e) || t('mcp.healthFailed', { name }))
  }
}

async function handleReconnect(name: string) {
  try {
    await mcpStore.reconnectServer(name)
    message.success(t('mcp.reconnected', { name }))
  } catch (e: any) {
    message.error(mcpErrorMessage(e) || t('mcp.reconnectFailed', { name }))
  }
}

async function handleDisconnect(name: string) {
  try {
    await mcpStore.disconnectServer(name)
    message.success(t('mcp.serverDisconnected', { name }))
  } catch (e: any) {
    message.error(mcpErrorMessage(e) || t('mcp.disconnectFailed', { name }))
  }
}

async function handleDelete(name: string) {
  try {
    await mcpStore.removeServer(name)
    delete serverTools.value[name]
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
  // 编辑模式没有 JSON 分支；JSON 模式下交给 textarea 自己处理
  if (isEditing.value || inputMode.value === 'json') return

  const raw = e.clipboardData?.getData('text') ?? ''
  const text = stripCodeFence(raw)
  const trimmed = text.trim()
  if (!trimmed.startsWith('{') && !trimmed.startsWith('[')) return

  e.preventDefault()
  jsonText.value = text
  inputMode.value = 'json'
  message.info(t('mcp.jsonPasted'), { duration: 3000 })
}

function openAddModal() {
  isEditing.value = false
  inputMode.value = 'form'
  jsonText.value = ''
  Object.assign(formData, { name: '', transport: 'stdio', command: '', argsStr: '', url: '', envStr: '' })
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
    // env 的值是 *** 掩码，原样回传时由后端还原成磁盘上的真值
    envStr: Object.entries(row.env || {}).map(([k, v]) => `${k}=${v}`).join('\n'),
  })
  showAddModal.value = true
}

async function handleSave() {
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

  if (formData.transport === 'sse' && !formData.url) {
    message.error(t('mcp.urlRequired'))
    return
  }

  const config: MCPConfig = {
    command: formData.command,
    args: formData.argsStr.split(/\s+/).filter(Boolean),
    transport: formData.transport,
    url: formData.url,
    env: formData.envStr.split('\n').filter(Boolean),
  }

  try {
    if (isEditing.value) {
      await mcpStore.updateServer(formData.name, config)
      message.success(t('mcp.serverUpdated'))
    } else {
      await mcpStore.addServer(formData.name, config)
      message.success(t('mcp.serverAdded'))
    }
    showAddModal.value = false
  } catch (e: any) {
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
      message.success(t('mcp.jsonAdded', { count: result?.added?.length ?? 0 }))
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
/* Action buttons inside table cells: more compact to fit narrower columns */
.btn-condensed {
  padding: 0 6px;
}

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

/* 移动端:表格横向滚动 + 底部安全区 */
@media (max-width: 768px) {
  :deep(.n-data-table-wrapper) {
    overflow-x: auto;
    -webkit-overflow-scrolling: touch;
  }
}
</style>
