<template>
  <!-- MCP 连接器目录：直接铺成插件卡（和官方技能包同一视觉语言），
       点「一键安装」即接入；需要凭据的条目就地弹表单填一次。
       卡片网格复用全局 conn-* 样式（styles/connectors.css）。 -->
  <div>
    <div class="conn-toolbar">
      <n-input
        v-model:value="query"
        size="small"
        clearable
        :placeholder="t('mcp.catalogSearch')"
        class="conn-search"
      >
        <template #prefix>
          <n-icon size="14"><component :is="SearchOutline" /></n-icon>
        </template>
      </n-input>

      <div class="conn-chips">
        <button
          type="button"
          class="conn-chip"
          :class="{ 'is-active': !category }"
          @click="category = ''"
        >
          {{ t('common.all') }}
        </button>
        <button
          v-for="c in categories"
          :key="c.id"
          type="button"
          class="conn-chip"
          :class="{ 'is-active': category === c.id }"
          @click="category = category === c.id ? '' : c.id"
        >
          {{ c.label }}
        </button>
      </div>

      <n-space :size="8" class="conn-toolbar-actions">
        <n-button size="small" :loading="loading" @click="load">
          <template #icon><component :is="RefreshOutline" /></template>
          {{ t('common.refresh') }}
        </n-button>
      </n-space>
    </div>

    <n-spin v-if="loading && !items.length" :size="18" />
    <template v-else>
      <div v-if="shown.length" class="conn-grid">
        <div v-for="item in shown" :key="item.id" class="conn-card" @click="openForm(item)">
          <div class="conn-card-head">
            <div class="conn-avatar" :class="{ 'is-ok': isInstalled(item) }">
              {{ avatarText(item.name) }}
            </div>
            <div class="conn-ident">
              <div class="conn-name">
                <span class="conn-name-text">{{ item.name }}</span>
                <span v-if="isInstalled(item)" class="conn-dot is-ok" />
              </div>
              <div class="conn-sub">
                {{ item.transport === 'stdio' ? item.command : item.url }}
              </div>
            </div>
            <n-tag size="tiny" :bordered="false">{{ item.transport }}</n-tag>
          </div>

          <div class="conn-desc">{{ item.description }}</div>

          <div class="conn-foot" @click.stop>
            <span v-if="isInstalled(item)" class="conn-state is-on">
              {{ t('agentplugins.catalogInstalled') }}
            </span>
            <span v-else class="conn-state">{{ stateText(item) }}</span>

            <n-button v-if="isInstalled(item)" size="tiny" secondary @click="openMCPPage">
              {{ t('agentplugins.catalogManage') }}
            </n-button>
            <n-button
              v-else
              size="tiny"
              type="primary"
              :loading="installingId === item.id"
              @click="startInstall(item)"
            >
              {{ t('agentplugins.officialInstall') }}
            </n-button>
          </div>
        </div>
      </div>
      <n-empty v-else :description="t('mcp.catalogEmpty')" style="padding: 12px 0;" />

      <div v-if="filtered.length > COLLAPSED" class="conn-more">
        <n-button text size="tiny" @click="expanded = !expanded">
          {{
            expanded
              ? t('agentplugins.catalogShowLess')
              : t('agentplugins.catalogShowAll', { count: filtered.length })
          }}
        </n-button>
      </div>
    </template>

    <!-- 需要凭据：预填目录模板，用户补齐后保存，保存即添加并后台连接 -->
    <n-modal
      v-model:show="form.show"
      preset="card"
      :title="t('agentplugins.mcpInstallTitle', { name: form.title })"
      style="width: 620px;"
    >
      <n-form label-placement="top" size="small">
        <n-form-item :label="t('mcp.serverName')" required>
          <n-input v-model:value="form.name" />
        </n-form-item>

        <template v-if="form.transport === 'stdio'">
          <n-form-item :label="t('mcp.command')" required>
            <n-input v-model:value="form.command" placeholder="npx -y ..." />
          </n-form-item>
          <n-form-item :label="t('mcp.args')">
            <n-input v-model:value="form.argsStr" type="textarea" :rows="2" />
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
            <n-input
              v-model:value="form.headersStr"
              type="textarea"
              :rows="2"
              :placeholder="t('mcp.headersPlaceholder')"
            />
            <template #feedback>
              <n-text depth="3" style="font-size: 12px;">{{ t('mcp.headersHint') }}</n-text>
            </template>
          </n-form-item>
        </template>
      </n-form>

      <n-space justify="end">
        <n-button size="small" @click="form.show = false">{{ t('common.cancel') }}</n-button>
        <n-button size="small" type="primary" :loading="saving" @click="submit">
          {{ t('mcp.addServer') }}
        </n-button>
      </n-space>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { NButton, NEmpty, NForm, NFormItem, NIcon, NInput, NModal, NSpace, NSpin, NTag, NText } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { RefreshOutline, SearchOutline } from '@vicons/ionicons5'
import { getMCPCatalog } from '@/api/mcp'
import type { CatalogConnector } from '@/api/mcp'
import { useMCPStore } from '@/stores/mcp'

const { t } = useI18n()
const message = useMessage()
const router = useRouter()
const mcpStore = useMCPStore()

/** 收起态只铺一行卡，避免 20+ 张卡把插件页淹掉。 */
const COLLAPSED = 6

const loading = ref(false)
const expanded = ref(false)
const items = ref<CatalogConnector[]>([])
const categories = ref<{ id: string; label: string }[]>([])
const query = ref('')
const category = ref('')
const installingId = ref('')

const filtered = computed(() => {
  let list = items.value
  if (category.value) list = list.filter((e) => e.category === category.value)
  const q = query.value.trim().toLowerCase()
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

const shown = computed(() => {
  // 搜索/筛选时直接给全量结果：用户已经在主动找了，不该再让他点「显示全部」
  if (expanded.value || query.value.trim() || category.value) return filtered.value
  return filtered.value.slice(0, COLLAPSED)
})

/** 已安装判定：配置里存在同名 MCP server（连接失败也算已添加）。 */
function isInstalled(item: CatalogConnector): boolean {
  const name = item.default_name || item.id
  return mcpStore.servers.some((s) => s.name === name)
}

function stateText(item: CatalogConnector): string {
  const keys = (item.requires || []).map((r) => r.label || r.key)
  if (keys.length) return t('agentplugins.catalogNeedCreds', { keys: keys.join(', ') })
  return t('agentplugins.catalogNoCreds')
}

async function load() {
  loading.value = true
  try {
    const res = await getMCPCatalog()
    items.value = res.connectors || []
    categories.value = res.categories || []
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally {
    loading.value = false
  }
}

/** 预填表单：目录模板 + 凭据占位（arg 类凭据在参数里标成 <label> 就地改）。 */
const form = reactive({
  show: false,
  title: '',
  item: null as CatalogConnector | null,
  name: '',
  transport: 'stdio',
  command: '',
  argsStr: '',
  url: '',
  envStr: '',
  headersStr: '',
})
const saving = ref(false)

function fillForm(item: CatalogConnector) {
  form.item = item
  form.title = item.name
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
      form.argsStr = form.argsStr.split(`{{${req.key}}}`).join(`<${req.label}>`)
    } else envLines.push(`${req.key}=`)
  }
  form.envStr = envLines.join('\n')
  form.headersStr = headerLines.join('\n')
}

/** 卡片主按钮：无凭据的条目一键装完，有凭据的弹表单（凭据必须由用户填）。 */
function startInstall(item: CatalogConnector) {
  if (isInstalled(item)) {
    openMCPPage()
    return
  }
  if (!(item.requires || []).length) {
    void installDirect(item)
    return
  }
  openForm(item)
}

/** 点卡片 = 打开表单预览/微调（绝不静默安装）。 */
function openForm(item: CatalogConnector) {
  if (isInstalled(item)) {
    openMCPPage()
    return
  }
  fillForm(item)
  form.show = true
}

/** 无凭据条目：目录模板直接落成 ServerConfig。 */
async function installDirect(item: CatalogConnector) {
  const name = item.default_name || item.id
  const isStdio = (item.transport || 'stdio') === 'stdio'
  installingId.value = item.id
  try {
    await mcpStore.addServer(name, {
      command: isStdio ? item.command || '' : '',
      args: isStdio ? item.args || [] : [],
      transport: item.transport || 'stdio',
      url: isStdio ? '' : item.url || '',
      env: isStdio ? [] : undefined,
      headers: isStdio ? undefined : {},
      enabled: true,
    })
    message.success(t('mcp.serverAddedConnecting'))
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally {
    installingId.value = ''
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

async function submit() {
  const item = form.item
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
    message.success(t('mcp.serverAddedConnecting'))
    form.show = false
  } catch (e: any) {
    // 保存失败不关弹窗：用户填的内容要留着，能直接改完重试。
    message.error(String(e?.message || e))
  } finally {
    saving.value = false
  }
}

function openMCPPage() {
  void router.push('/mcp')
}

/** 卡片头像：取名称前两位。 */
function avatarText(name: string): string {
  const trimmed = (name || '').trim()
  if (!trimmed) return '?'
  return trimmed.slice(0, 2).toUpperCase()
}

onMounted(() => {
  void load()
  // 已安装状态靠 servers 列表判定：失败不影响目录本身可浏览。
  void mcpStore.loadServers().catch(() => {})
})
</script>
