<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 24px;" align="center">
      <h2>{{ t('mcp.title') }}</h2>
      <n-space>
        <n-button type="primary" @click="showAddModal = true">
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
            <n-tag :type="row.connected ? 'success' : 'error'" size="small">
              {{ row.connected ? t('mcp.statusConnected') : t('mcp.statusDisconnected') }}
            </n-tag>
          </template>

          <template #body-cell:transport="{ row }">
            <n-tag :type="row.transport === 'stdio' ? 'info' : 'warning'" size="small">
              {{ row.transport.toUpperCase() }}
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
      style="width: 500px;"
      @positive-click="handleSave"
    >
      <n-space vertical>
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
      </n-space>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import {
  AddOutline,
  RefreshOutline,
  HeartOutline,
  RefreshCircleOutline,
  PowerOutline,
  TrashOutline,
} from '@vicons/ionicons5'
import { useMCPStore } from '@/stores/mcp'
import type { MCPConfig } from '@/api/mcp'

const { t } = useI18n()
const message = useMessage()
const mcpStore = useMCPStore()

const showAddModal = ref(false)
const isEditing = ref(false)
const serverTools = ref<Record<string, any[]>>({})

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
    width: 200,
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
    message.error(e.message || t('mcp.healthFailed', { name }))
  }
}

async function handleReconnect(name: string) {
  try {
    await mcpStore.reconnectServer(name)
    message.success(t('mcp.reconnected', { name }))
  } catch (e: any) {
    message.error(e.message || t('mcp.reconnectFailed', { name }))
  }
}

async function handleDisconnect(name: string) {
  try {
    await mcpStore.disconnectServer(name)
    message.success(t('mcp.serverDisconnected', { name }))
  } catch (e: any) {
    message.error(e.message || t('mcp.disconnectFailed', { name }))
  }
}

async function handleDelete(name: string) {
  try {
    await mcpStore.removeServer(name)
    delete serverTools.value[name]
    message.success(t('mcp.deleted', { name }))
  } catch (e: any) {
    message.error(e.message || t('mcp.deleteFailed', { name }))
  }
}

async function handleRefreshTools(name: string) {
  try {
    serverTools.value[name] = await mcpStore.refreshTools(name)
    message.success(t('mcp.toolsRefreshed'))
  } catch (e: any) {
    message.error(e.message || t('mcp.refreshToolsFailed'))
  }
}

function openAddModal() {
  isEditing.value = false
  formData.name = ''
  formData.transport = 'stdio'
  formData.command = ''
  formData.argsStr = ''
  formData.url = ''
  formData.envStr = ''
  showAddModal.value = true
}

async function handleSave() {
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
    message.error(e.message || t('mcp.saveFailed'))
  }
}

onMounted(async () => {
  await mcpStore.loadServers()
})
</script>