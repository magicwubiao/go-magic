<template>
  <!-- MCP 连接器目录弹窗：从"跳到连接器页预填表单"改为就地闭环——
       选中条目 → 就地填凭据 → 保存，背后自动连接。 -->
  <n-modal
    :show="show"
    preset="card"
    :title="t('mcp.catalogTitle')"
    style="width: 820px;"
    @update:show="(v: boolean) => emit('update:show', v)"
  >
    <!-- 列表 -->
    <template v-if="!catalogPick">
      <div class="conn-toolbar">
        <n-input
          v-model:value="catalogSearch"
          clearable
          :placeholder="t('mcp.catalogSearch')"
          class="conn-search"
        >
          <template #prefix><component :is="SearchOutline" /></template>
        </n-input>

        <div class="conn-chips">
          <button
            v-for="c in categories"
            :key="c.id"
            type="button"
            class="conn-chip"
            :class="{ 'is-active': catalogCategory === c.id }"
            @click="catalogCategory = catalogCategory === c.id ? '' : c.id"
          >
            {{ c.label }}
          </button>
        </div>

        <n-space :size="8" class="conn-toolbar-actions">
          <n-button size="small" :loading="catalogLoading" @click="loadCatalog">
            <template #icon><component :is="RefreshOutline" /></template>
            {{ t('common.refresh') }}
          </n-button>
        </n-space>
      </div>

      <div v-if="visibleItems.length" class="conn-grid">
        <div
          v-for="item in visibleItems"
          :key="item.id"
          class="conn-card"
          @click="pickItem(item)"
        >
          <div class="conn-card-head">
            <div class="conn-avatar">{{ avatarText(item.name) }}</div>
            <div class="conn-ident">
              <div class="conn-name"><span class="conn-name-text">{{ item.name }}</span></div>
              <div class="conn-sub">
                <span>{{ item.transport === 'stdio' ? item.command : item.url }}</span>
              </div>
            </div>
          </div>

          <div class="conn-desc">{{ item.description }}</div>

          <div class="conn-foot" @click.stop>
            <span v-if="item.requires?.length" class="conn-state">{{ credSummary(item) }}</span>
            <span v-else />
            <n-button size="tiny" type="primary" @click="pickItem(item)">
              {{ t('mcp.catalogInstall') }}
            </n-button>
          </div>
        </div>
      </div>
      <n-empty v-else :description="t('mcp.catalogEmpty')" style="margin-top: 16px;" />
    </template>

    <!-- 安装表单：预填目录模板，用户补凭据后保存 -->
    <template v-else>
      <n-button text size="small" style="align-self: flex-start;" @click="catalogPick = null">
        ← {{ t('cli.catalogBack') }}
      </n-button>

      <div class="cli-catalog-head">
        <div class="conn-avatar">{{ avatarText(catalogPick.name) }}</div>
        <div>
          <div class="cli-catalog-title">{{ catalogPick.name }}</div>
          <div class="cli-catalog-sub">{{ catalogPick.description }}</div>
        </div>
      </div>

      <n-form label-placement="top" size="small" style="margin-top: 8px;">
        <n-form-item :label="t('mcp.serverName')" required>
          <n-input v-model:value="form.name" placeholder="feishu" />
        </n-form-item>

        <template v-if="form.transport === 'stdio'">
          <n-form-item :label="t('mcp.command')" required>
            <n-input v-model:value="form.command" placeholder="npx -y ..." />
          </n-form-item>
          <n-form-item :label="t('mcp.args')">
            <n-input v-model:value="form.argsStr" type="textarea" :rows="2" placeholder="--port 3000" />
          </n-form-item>
          <n-form-item :label="t('mcp.env')">
            <n-input v-model:value="form.envStr" type="textarea" :rows="2" placeholder="API_KEY=xxx" />
          </n-form-item>
        </template>
        <template v-else>
          <n-form-item :label="t('mcp.url')" required>
            <n-input v-model:value="form.url" placeholder="https://..." />
          </n-form-item>
          <n-form-item :label="t('mcp.headers')">
            <n-input v-model:value="form.headersStr" type="textarea" :rows="2" :placeholder="t('mcp.headersPlaceholder')" />
            <template #feedback>
              <n-text depth="3" style="font-size: 12px;">{{ t('mcp.headersHint') }}</n-text>
            </template>
          </n-form-item>
        </template>
      </n-form>

      <n-space justify="end">
        <n-button size="small" @click="catalogPick = null">{{ t('common.cancel') }}</n-button>
        <n-button size="small" type="primary" :loading="saving" @click="handleAdd">
          {{ t('mcp.addServer') }}
        </n-button>
      </n-space>
    </template>
  </n-modal>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { useMessage } from 'naive-ui'
import { NButton, NEmpty, NForm, NFormItem, NInput, NModal, NSpace, NText } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { RefreshOutline, SearchOutline } from '@vicons/ionicons5'
import { getMCPCatalog, mcpErrorMessage } from '@/api/mcp'
import type { CatalogConnector } from '@/api/mcp'
import { useMCPStore } from '@/stores/mcp'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{
  (e: 'update:show', v: boolean): void
  /** 添加成功，父组件可据此提示或刷新。 */
  (e: 'added', name: string): void
}>()

const { t } = useI18n()
const message = useMessage()
const mcpStore = useMCPStore()

const catalogLoading = ref(false)
const catalogItems = ref<CatalogConnector[]>([])
const categories = ref<{ id: string; label: string }[]>([])
const catalogSearch = ref('')
const catalogCategory = ref('')
/** 当前选中的目录条目（null = 显示列表）。 */
const catalogPick = ref<CatalogConnector | null>(null)
const saving = ref(false)

/** 预填表单（凭据占位与 MCPView 的目录流程同款）。 */
const form = reactive({
  name: '',
  transport: 'stdio',
  command: '',
  argsStr: '',
  url: '',
  envStr: '',
  headersStr: '',
})

const visibleItems = computed(() => {
  let list = catalogItems.value
  if (catalogCategory.value) {
    list = list.filter((e) => e.category === catalogCategory.value)
  }
  const q = catalogSearch.value.trim().toLowerCase()
  if (q) {
    list = list.filter(
      (e) =>
        e.name.toLowerCase().includes(q)
        || e.description.toLowerCase().includes(q)
        || (e.tags || []).some((tag) => tag.toLowerCase().includes(q)),
    )
  }
  return list
})

watch(
  () => props.show,
  (v) => {
    if (v && !catalogItems.value.length) void loadCatalog()
  },
)

async function loadCatalog() {
  catalogLoading.value = true
  try {
    const res = await getMCPCatalog()
    catalogItems.value = res.connectors || []
    categories.value = res.categories || []
  } catch (e: any) {
    message.error(mcpErrorMessage(e) || t('mcp.catalogFailed'))
  } finally {
    catalogLoading.value = false
  }
}

/** 需要补的凭据摘要（卡片右下角提示）。 */
function credSummary(item: CatalogConnector): string {
  const keys = (item.requires || []).map((r) => r.label || r.key)
  return keys.join(' / ')
}

/** 选中条目 → 预填表单（与 MCPView 目录流程同款转换）。 */
function pickItem(item: CatalogConnector) {
  catalogPick.value = item
  form.name = item.default_name || item.id
  form.transport = item.transport || 'stdio'
  form.command = item.command || ''
  form.argsStr = (item.args || []).join(' ')
  form.url = item.url || ''

  const envLines: string[] = []
  const headerLines: string[] = []
  for (const req of item.requires || []) {
    if (req.where === 'header') headerLines.push(`${req.key}=`)
    else if (req.where === 'arg') {
      // arg 类凭据（如飞书的 App ID/Secret）走命令行参数：把 Args 里的
      // {{KEY}} 占位符替换成醒目的 <label> 提示，用户在参数框里就地改。
      const token = `{{${req.key}}}`
      form.argsStr = form.argsStr.split(token).join(`<${req.label}>`)
    } else envLines.push(`${req.key}=`)
  }
  form.envStr = envLines.join('\n')
  form.headersStr = headerLines.join('\n')

  if ((item.requires || []).length) {
    message.info(t('mcp.catalogFillRequired', { name: item.name }), { duration: 6000 })
  }
}

/** "KEY=value" 多行文本 → Record（headers 用）。 */
function linesToKV(text: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const line of text.split('\n')) {
    const idx = line.indexOf('=')
    if (idx <= 0) continue
    const k = line.slice(0, idx).trim()
    if (k) out[k] = line.slice(idx + 1)
  }
  return out
}

async function handleAdd() {
  const item = catalogPick.value
  if (!item) return
  if (!form.name) {
    message.error(t('mcp.serverNameRequired'))
    return
  }
  const isStdio = form.transport === 'stdio'
  if (isStdio && !form.command) {
    message.error(t('mcp.commandRequired'))
    return
  }
  if (!isStdio && !form.url) {
    message.error(t('mcp.urlRequired'))
    return
  }

  saving.value = true
  try {
    await mcpStore.addServer(form.name, {
      command: isStdio ? form.command : '',
      args: isStdio ? form.argsStr.split(/\s+/).filter(Boolean) : [],
      transport: form.transport,
      url: isStdio ? '' : form.url,
      env: isStdio ? form.envStr.split('\n').filter(Boolean) : undefined,
      headers: isStdio ? undefined : linesToKV(form.headersStr),
      enabled: true,
    })
    // 保存是异步连接的：配置已落盘，连接在后台跑。
    message.success(t('mcp.serverAddedConnecting'))
    emit('added', form.name)
    catalogPick.value = null
    emit('update:show', false)
  } catch (e: any) {
    // 保存失败时不关弹窗：用户填的内容要留着，能直接改完重试。
    message.error(mcpErrorMessage(e) || t('mcp.saveFailed'))
  } finally {
    saving.value = false
  }
}

/** 卡片头像：取名称前两位。 */
function avatarText(name: string): string {
  const trimmed = (name || '').trim()
  if (!trimmed) return '?'
  return trimmed.slice(0, 2).toUpperCase()
}
</script>

<!-- 弹窗 teleport 到 body，样式放全局（conn-* 在 styles/connectors.css） -->
<style>
.mcp-catalog-toolbar {
  display: flex;
  align-items: center;
  gap: 8px;
}
</style>
