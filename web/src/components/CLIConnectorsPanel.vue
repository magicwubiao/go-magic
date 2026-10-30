<template>
  <div>
    <!-- 与 MCP 页签同一套卡片布局（样式在 styles/connectors.css）：
         上面检索/筛选，下面卡片，点卡片在右侧抽屉里看工具清单。 -->
    <div class="conn-toolbar">
      <n-input
        v-model:value="searchQuery"
        clearable
        :placeholder="t('cli.searchPlaceholder')"
        class="conn-search"
      >
        <template #prefix><component :is="SearchOutline" /></template>
      </n-input>

      <div class="conn-chips">
        <button
          v-for="chip in stateChips"
          :key="chip.key"
          type="button"
          class="conn-chip"
          :class="{ 'is-active': stateFilter === chip.key }"
          @click="stateFilter = chip.key"
        >
          {{ chip.label }}
        </button>
      </div>

      <!-- 按钮顺序与 MCP 页签保持一致：主操作在左、刷新固定在最右。 -->
      <n-space :size="8" class="conn-toolbar-actions">
        <n-button size="small" type="primary" @click="openAdd">
          <template #icon><component :is="AddOutline" /></template>
          {{ t('cli.addConnector') }}
        </n-button>
        <n-button size="small" @click="handleRefresh" :loading="loading">
          <template #icon><component :is="RefreshOutline" /></template>
          {{ t('common.refresh') }}
        </n-button>
      </n-space>
    </div>

    <div class="conn-count">{{ t('cli.listSummary', { total: connectors.length, enabled: enabledCount }) }}</div>

    <n-spin v-if="loading && !connectors.length" />

    <template v-else>
      <div v-if="visibleConnectors.length" class="conn-grid">
        <div
          v-for="row in visibleConnectors"
          :key="row.name"
          class="conn-card"
          :class="{ 'is-idle': !row.active }"
          @click="openDetail(row)"
        >
          <div class="conn-card-head">
            <div class="conn-avatar" :class="row.active ? 'is-ok' : ''">{{ avatarText(row.name) }}</div>
            <div class="conn-ident">
              <div class="conn-name">
                <span class="conn-name-text">{{ row.name }}</span>
                <span class="conn-dot" :class="row.active ? 'is-ok' : ''" />
              </div>
              <div class="conn-sub">
                <span>{{ row.command }}</span>
                <span> · {{ t('cli.toolCountLabel', { count: row.tools?.length || 0 }) }}</span>
              </div>
            </div>
            <n-dropdown
              trigger="click"
              :options="cardMenu()"
              @select="(key: string) => handleCardMenu(key, row)"
            >
              <n-button text size="tiny" class="conn-more" @click.stop>
                <template #icon><component :is="EllipsisHorizontalOutline" /></template>
              </n-button>
            </n-dropdown>
          </div>

          <div class="conn-desc" :title="row.description || ''">
            {{ row.description || t('cli.noDescription') }}
          </div>

          <div class="conn-foot" @click.stop>
            <n-space :size="4">
              <n-tag v-if="row.require_approval" size="tiny" :bordered="false" type="info">
                {{ t('cli.approvalTag') }}
              </n-tag>
              <span class="conn-state" :class="{ 'is-on': row.active }">
                {{ row.active ? t('cli.stateEnabled') : t('cli.stateDisabled') }}
              </span>
            </n-space>
            <n-button size="tiny" @click="openTest(row)">
              <template #icon><component :is="PlayOutline" /></template>
              {{ t('cli.test') }}
            </n-button>
          </div>
        </div>
      </div>

      <n-alert v-else-if="!connectors.length" type="info" :bordered="false">
        {{ t('cli.emptyHint') }}
      </n-alert>
      <n-empty v-else :description="t('mcp.noSearchResult')" style="margin-top: 16px;" />
    </template>

    <!-- 详情抽屉：工具清单 + 单工具的参数/参数模板。卡片只放概览。 -->
    <n-drawer
      :show="!!detailName"
      :width="520"
      placement="right"
      @update:show="(v: boolean) => { if (!v) detailName = '' }"
    >
      <n-drawer-content :title="detailName" closable>
        <template v-if="detailRow">
          <n-space :size="8" style="margin-bottom: 14px;">
            <n-button size="small" @click="openTest(detailRow)">
              <template #icon><component :is="PlayOutline" /></template>
              {{ t('cli.test') }}
            </n-button>
            <n-button size="small" @click="openEdit(detailRow)">
              <template #icon><component :is="CreateOutline" /></template>
              {{ t('common.edit') }}
            </n-button>
            <n-button size="small" type="error" @click="confirmDelete(detailRow)">
              <template #icon><component :is="TrashOutline" /></template>
              {{ t('common.delete') }}
            </n-button>
          </n-space>

          <div class="conn-drawer-tools">
            <div v-for="tool in detailRow.tools" :key="tool.name" class="cli-tool">
              <div class="cli-tool-head">
                <n-text code>{{ detailRow.name }}_{{ tool.name }}</n-text>
                <n-tag v-if="tool.read_only" size="tiny" :bordered="false" type="success">
                  {{ t('cli.readOnly') }}
                </n-tag>
                <n-tag v-if="tool.timeout_seconds" size="tiny" :bordered="false">
                  {{ tool.timeout_seconds }}s
                </n-tag>
              </div>
              <div class="cli-tool-desc">{{ tool.description }}</div>
              <pre class="cli-tool-args">{{ (tool.args || []).join('\n') }}</pre>
              <div v-if="tool.params?.length" class="cli-tool-params">
                <n-tag v-for="p in tool.params" :key="p.name" size="tiny" :bordered="false">
                  {{ p.name }}:{{ p.type || 'string' }}{{ p.required ? '*' : '' }}
                </n-tag>
              </div>
            </div>
          </div>
        </template>
      </n-drawer-content>
    </n-drawer>

    <!-- 新建 / 编辑 -->
    <n-modal v-model:show="showForm" preset="card" :title="editingName ? t('cli.editConnector') : t('cli.addConnector')" style="width: 760px;">
      <n-space vertical size="small">
        <!-- 快速导入：粘贴一条完整命令自动生成草稿。
             这是主要入口，手写模板降级为"微调"。 -->
        <div class="cli-quick">
          <n-input
            v-model:value="quickCmd"
            :placeholder="t('cli.quickImportHint')"
            @keydown.enter="applyQuickImport"
          >
            <template #prefix><component :is="SparklesOutline" /></template>
          </n-input>
          <n-button :loading="parsing" @click="applyQuickImport">
            {{ t('cli.quickParse') }}
          </n-button>
        </div>
        <n-text depth="3" style="font-size: 12px; margin-bottom: 4px;">
          {{ t('cli.quickImportNote') }}
        </n-text>

        <n-form-item :label="t('cli.name')" required>
          <n-input
            v-model:value="form.name"
            :disabled="!!editingName"
            placeholder="gh"
          />
        </n-form-item>

        <n-form-item :label="t('cli.command')" required>
          <n-input v-model:value="form.command" placeholder="gh / /usr/local/bin/kubectl" />
          <template #feedback>
            <n-text depth="3" style="font-size: 12px;">{{ t('cli.commandHint') }}</n-text>
          </template>
        </n-form-item>

        <!-- 高级设置：公共参数 / env / 行为开关默认收起。
             主路径只留"命令 + 描述 + 工具微调"，普通用户不需要看到模板语法。 -->
        <div class="cli-adv-toggle" @click="showAdvanced = !showAdvanced">
          <component
            :is="showAdvanced ? ChevronDownOutline : ChevronForwardOutline"
            class="cli-adv-chevron"
          />
          <span>{{ t('cli.advancedSettings') }}</span>
        </div>
        <template v-if="showAdvanced">
          <n-form-item :label="t('cli.commonArgs')">
            <n-input
              v-model:value="form.argsStr"
              type="textarea"
              :rows="2"
              :placeholder="ARGS_EXAMPLE"
            />
          </n-form-item>

          <n-form-item :label="t('cli.env')">
            <n-input
              v-model:value="form.envStr"
              type="textarea"
              :rows="2"
              placeholder="GH_TOKEN=xxxx&#10;NO_COLOR=1"
            />
            <template #feedback>
              <n-text depth="3" style="font-size: 12px;">{{ t('cli.envHint') }}</n-text>
            </template>
          </n-form-item>

          <n-space :size="12">
            <n-form-item :label="t('cli.timeout')" style="width: 160px;">
              <n-input-number v-model:value="form.timeoutSeconds" :min="1" :max="300" style="width: 100%;" />
            </n-form-item>
            <n-form-item :label="t('cli.workingDir')" style="flex: 1;">
              <n-input v-model:value="form.workingDir" :placeholder="t('cli.workingDirPlaceholder')" />
            </n-form-item>
          </n-space>

          <n-form-item :label="t('cli.behavior')">
            <n-space vertical size="small" style="width: 100%;">
              <n-space align="center" size="small">
                <n-switch v-model:value="form.enabled" size="small" />
                <n-text style="font-size: 13px;">{{ t('cli.enabled') }}</n-text>
              </n-space>
              <n-space align="center" size="small">
                <n-text style="font-size: 13px; width: 76px;">{{ t('cli.approvalMode') }}</n-text>
                <n-select
                  v-model:value="form.approvalMode"
                  size="small"
                  style="width: 260px;"
                  :options="approvalOptions"
                />
              </n-space>
              <n-input v-model:value="form.profilesStr" size="small" :placeholder="t('cli.profilesPlaceholder')" />
            </n-space>
          </n-form-item>
        </template>

        <n-divider style="margin: 4px 0;">
          {{ t('cli.toolsSection') }}
        </n-divider>

        <!-- 工具卡：默认收起只显示摘要（名字/描述/只读/参数个数），
             点开就地编辑。避免一屏塞下 N 个工具 × 7 个字段的编辑器。 -->
        <div v-for="(tool, idx) in formTools" :key="idx" class="cli-tool-card">
          <div class="cli-tool-card-head" @click="toggleTool(idx)">
            <component
              :is="expandedTool === idx ? ChevronDownOutline : ChevronForwardOutline"
              class="cli-adv-chevron"
            />
            <span class="cli-tool-card-title">{{ tool.name || t('cli.unnamedTool') }}</span>
            <span class="cli-tool-card-desc">{{ tool.description }}</span>
            <n-tag v-if="tool.readOnly" size="tiny" :bordered="false" type="success">
              {{ t('cli.readOnly') }}
            </n-tag>
            <n-tag v-if="paramCount(tool)" size="tiny" :bordered="false">
              {{ t('cli.paramsCount', { n: paramCount(tool) }) }}
            </n-tag>
            <n-button
              text
              size="tiny"
              type="error"
              class="cli-tool-card-del"
              @click.stop="removeTool(idx)"
            >
              {{ t('common.delete') }}
            </n-button>
          </div>
          <div v-if="expandedTool === idx" class="cli-tool-card-body">
            <n-space :size="8" style="margin-bottom: 6px;">
              <n-input v-model:value="tool.name" size="small" :placeholder="t('cli.toolName')" style="width: 200px;" />
              <n-input v-model:value="tool.description" size="small" :placeholder="t('cli.toolDescription')" style="flex: 1;" />
            </n-space>
            <n-input
              v-model:value="tool.argsStr"
              type="textarea"
              :rows="3"
              size="small"
              :placeholder="ARGS_EXAMPLE"
            />
            <n-space align="center" size="small" style="margin-top: 6px;">
              <n-switch v-model:value="tool.readOnly" size="small" />
              <n-text style="font-size: 12px;">{{ t('cli.readOnly') }}</n-text>
              <n-input v-model:value="tool.stdinParam" size="small" :placeholder="t('cli.stdinParam')" style="width: 200px;" />
              <n-input-number v-model:value="tool.timeoutSeconds" size="small" :min="0" :max="300" :placeholder="t('cli.timeout')" style="width: 130px;" />
            </n-space>
            <n-input
              v-model:value="tool.paramsJson"
              type="textarea"
              :rows="2"
              size="small"
              style="margin-top: 6px;"
              :placeholder="PARAMS_EXAMPLE"
            />
            <n-alert type="default" :bordered="false" style="margin-top: 8px;">
              <span style="font-size: 12px;">{{ t('cli.templateHint') }}</span>
            </n-alert>
          </div>
        </div>

        <n-space size="small">
          <n-button size="small" @click="addTool">
            <template #icon><component :is="AddOutline" /></template>
            {{ t('cli.addTool') }}
          </n-button>
          <n-button size="small" quaternary @click="fillExample">{{ t('cli.fillExample') }}</n-button>
        </n-space>
      </n-space>

      <template #footer>
        <n-space justify="end">
          <n-button @click="showForm = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="saving" @click="handleSave">{{ t('common.save') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- 试跑 -->
    <n-modal v-model:show="showTest" preset="card" :title="t('cli.testTitle')" style="width: 720px;">
      <n-space vertical size="small">
        <n-form-item :label="t('cli.pickTool')">
          <n-select v-model:value="testTool" :options="testToolOptions" />
        </n-form-item>
        <n-form-item :label="t('cli.testParams')">
          <n-input
            v-model:value="testParams"
            type="textarea"
            :rows="4"
            placeholder="{}"
          />
        </n-form-item>
        <n-button type="primary" :loading="testing" :disabled="!testTool" @click="runTest">
          <template #icon><component :is="PlayOutline" /></template>
          {{ t('cli.runTest') }}
        </n-button>

        <template v-if="testResult">
          <n-alert :type="testResult.ok ? 'success' : 'error'" :bordered="false">
            <span style="font-size: 12px;">
              {{ testResult.ok ? t('cli.testOk') : (testResult.error || t('cli.testFailed')) }}
              · {{ testResult.elapsed_ms }}ms
            </span>
          </n-alert>
          <template v-if="testResult.result">
            <div class="cli-output-label">
              {{ t('cli.exitCode') }}: {{ testResult.result.exit_code }}
            </div>
            <pre class="cli-output">{{ testResult.result.stdout || t('cli.noStdout') }}</pre>
            <template v-if="testResult.result.stderr">
              <div class="cli-output-label">{{ t('cli.stderr') }}</div>
              <pre class="cli-output is-error">{{ testResult.result.stderr }}</pre>
            </template>
            <n-text v-if="testResult.result.truncated" depth="3" style="font-size: 12px;">
              {{ testResult.result.truncated_hint }}
            </n-text>
          </template>
        </template>
      </n-space>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  NAlert,
  NButton,
  NDivider,
  NDrawer,
  NDrawerContent,
  NDropdown,
  NEmpty,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NSelect,
  NSpace,
  NSpin,
  NSwitch,
  NTag,
  NText,
  useDialog,
  useMessage,
} from 'naive-ui'
import { useI18n } from 'vue-i18n'
import {
  AddOutline,
  ChevronDownOutline,
  ChevronForwardOutline,
  CreateOutline,
  EllipsisHorizontalOutline,
  PlayOutline,
  RefreshOutline,
  SearchOutline,
  SparklesOutline,
  TrashOutline,
} from '@vicons/ionicons5'
import {
  listCLIConnectors,
  parseCLICommand,
  removeCLIConnector,
  saveCLIConnector,
  testCLIConnector,
} from '@/api/cli'
import type { CLIConnector, CLIConnectorView, CLITestResult } from '@/api/cli'

const emit = defineEmits<{ (e: 'count', n: number): void }>()

const { t } = useI18n()

// 代码示例不放进 i18n：内容与语言无关，而且 vue-i18n 会把 {{name}}、{...}
// 当成插值语法解析（编译期报错/运行期告警）。这里按字面量展示。
const ARGS_EXAMPLE = 'pr\nlist\n--limit\n{{limit}}'
const PARAMS_EXAMPLE = '[{"name":"limit","type":"integer","required":false}]'
const message = useMessage()
const dialog = useDialog()

const connectors = ref<CLIConnectorView[]>([])
const loading = ref(false)
const saving = ref(false)

const searchQuery = ref('')
/** 筛选：全部 / 已启用 / 未启用（enabled=false 或 profiles 不含当前分身）。 */
const stateFilter = ref<'all' | 'enabled' | 'disabled'>('all')
const stateChips = computed(() => [
  { key: 'all' as const, label: t('common.all') },
  { key: 'enabled' as const, label: t('cli.stateEnabled') },
  { key: 'disabled' as const, label: t('cli.stateDisabled') },
])

const enabledCount = computed(() => connectors.value.filter((c) => c.active).length)

const visibleConnectors = computed(() => {
  let list = connectors.value
  if (stateFilter.value === 'enabled') list = list.filter((c) => c.active)
  else if (stateFilter.value === 'disabled') list = list.filter((c) => !c.active)
  const q = searchQuery.value.trim().toLowerCase()
  if (q) {
    list = list.filter(
      (c) =>
        c.name.toLowerCase().includes(q)
        || (c.command || '').toLowerCase().includes(q)
        || (c.description || '').toLowerCase().includes(q),
    )
  }
  return list
})

/** 详情抽屉：卡片只放概览，工具清单/参数模板放这里。 */
const detailName = ref('')
const detailRow = computed(
  () => connectors.value.find((c) => c.name === detailName.value) ?? null,
)

function openDetail(row: CLIConnectorView) {
  detailName.value = row.name
}

/** 卡片头像：取连接器名前两位，与 MCP 卡片同款。 */
function avatarText(name: string): string {
  const trimmed = (name || '').trim()
  if (!trimmed) return '?'
  return trimmed.slice(0, 2).toUpperCase()
}

/** 卡片「…」菜单：低频动作不进主路径。 */
function cardMenu() {
  return [
    { label: t('common.edit'), key: 'edit' },
    { label: t('cli.test'), key: 'test' },
    { type: 'divider' as const },
    { label: t('common.delete'), key: 'delete' },
  ]
}

function handleCardMenu(key: string, row: CLIConnectorView) {
  if (key === 'edit') openEdit(row)
  else if (key === 'test') openTest(row)
  else if (key === 'delete') confirmDelete(row)
}

const showForm = ref(false)
const editingName = ref('')

interface ToolForm {
  name: string
  description: string
  argsStr: string
  readOnly: boolean
  stdinParam: string
  timeoutSeconds: number | null
  paramsJson: string
}

/**
 * 确认策略是三态，不是开关：
 *   inherit —— 不写 require_approval（后端 nil），按工具的 read_only 决定；
 *   always / never —— 显式覆盖连接器下所有工具。
 *
 * 用开关会丢语义：nil（跟随工具声明）被读成 true 再存回去，一次"只改超时"
 * 的编辑就会把所有只读工具也变成需要人工确认。
 */
const approvalOptions = computed(() => [
  { label: t('cli.approvalInherit'), value: 'inherit' as const },
  { label: t('cli.approvalAlways'), value: 'always' as const },
  { label: t('cli.approvalNever'), value: 'never' as const },
])

const form = reactive({
  name: '',
  command: '',
  argsStr: '',
  description: '',
  envStr: '',
  workingDir: '',
  timeoutSeconds: 60 as number | null,
  enabled: true,
  approvalMode: 'inherit' as 'inherit' | 'always' | 'never',
  profilesStr: '',
})
const formTools = ref<ToolForm[]>([])

// ---- 微调器交互：高级区与工具卡默认收起，主路径只留"命令 + 工具摘要" ----
const showAdvanced = ref(false)
/** 当前展开编辑的工具下标（null = 全部收起，同一时间只开一个）。 */
const expandedTool = ref<number | null>(null)

function toggleTool(idx: number) {
  expandedTool.value = expandedTool.value === idx ? null : idx
}

/** 参数声明个数：解析失败按 0 显示（编辑器里保存时才报错）。 */
function paramCount(tool: ToolForm): number {
  try {
    const arr = JSON.parse(tool.paramsJson)
    return Array.isArray(arr) ? arr.length : 0
  } catch {
    return 0
  }
}

/** 删除工具卡：同时收起展开态，避免下标错位指到别的工具上。 */
function removeTool(idx: number) {
  formTools.value.splice(idx, 1)
  expandedTool.value = null
}
// ---- 快速导入：粘贴一条命令 → 后端解析 → 草稿填进表单 ----
const quickCmd = ref('')
const parsing = ref(false)

/**
 * 解析结果填表。工具草稿进 formTools：
 * 默认的空工具槽被替换，否则追加（编辑已有连接器时不动原有工具）。
 */
async function applyQuickImport() {
  const input = quickCmd.value.trim()
  if (!input) {
    message.warning(t('cli.quickImportEmpty'))
    return
  }
  parsing.value = true
  try {
    const res = await parseCLICommand(input)
    form.command = res.command
    if (!form.name) {
      // 连接器名取命令基础名：去掉路径和 .exe，再清掉不合法字符。
      const base = res.command.split(/[\\/]/).pop() || res.command
      form.name = base.replace(/\.exe$/i, '').replace(/[^A-Za-z0-9_.-]/g, '') || 'cli'
    }
    const draft: ToolForm = {
      name: res.tool.name,
      description: res.tool.description,
      argsStr: (res.tool.args || []).join('\n'),
      readOnly: false,
      stdinParam: '',
      timeoutSeconds: 0,
      paramsJson: res.tool.params?.length ? JSON.stringify(res.tool.params) : '',
    }
    const first = formTools.value[0]
    let draftIdx: number
    if (formTools.value.length === 1 && !first.name && !first.argsStr && !first.paramsJson) {
      formTools.value = [draft]
      draftIdx = 0
    } else {
      formTools.value.push(draft)
      draftIdx = formTools.value.length - 1
    }
    // 解析结果自动展开成"预览"：用户粘完命令立刻看到生成了什么，可就地微调
    expandedTool.value = draftIdx
    showAdvanced.value = showAdvanced.value || (res.warnings || []).length > 0
    for (const w of res.warnings || []) {
      message.warning(w, { duration: 6000 })
    }
    message.success(t('cli.quickParsed'))
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally {
    parsing.value = false
  }
}

const showTest = ref(false)
const testTarget = ref<CLIConnectorView | null>(null)
const testTool = ref<string | null>(null)
const testParams = ref('{}')
const testing = ref(false)
const testResult = ref<CLITestResult | null>(null)

const testToolOptions = computed(() =>
  (testTarget.value?.tools || []).map((tool) => ({ label: tool.name, value: tool.name })),
)

async function handleRefresh() {
  loading.value = true
  try {
    connectors.value = await listCLIConnectors()
    emit('count', connectors.value.length)
    // 抽屉跟着列表走：被删掉的连接器不该留在抽屉里
    if (detailName.value && !connectors.value.some((c) => c.name === detailName.value)) {
      detailName.value = ''
    }
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally {
    loading.value = false
  }
}

/** 一行一个参数：不做 shell 拆分，值里有空格也不会被拆成两个参数。 */
function splitLines(text: string): string[] {
  return text
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line.length > 0)
}

function emptyTool(): ToolForm {
  return {
    name: '',
    description: '',
    argsStr: '',
    readOnly: false,
    stdinParam: '',
    timeoutSeconds: 0,
    paramsJson: '',
  }
}

function addTool() {
  formTools.value.push(emptyTool())
  // 新工具直接展开：点了"添加工具"就是要编辑它
  expandedTool.value = formTools.value.length - 1
}

function resetForm() {
  form.name = ''
  form.command = ''
  form.argsStr = ''
  form.description = ''
  form.envStr = ''
  form.workingDir = ''
  form.timeoutSeconds = 60
  form.enabled = true
  form.approvalMode = 'inherit'
  form.profilesStr = ''
  formTools.value = [emptyTool()]
  quickCmd.value = ''
  showAdvanced.value = false
  expandedTool.value = null
}

function openAdd() {
  editingName.value = ''
  resetForm()
  showForm.value = true
}

function openEdit(row: CLIConnectorView) {
  editingName.value = row.name
  form.name = row.name
  form.command = row.command || ''
  form.argsStr = (row.args || []).join('\n')
  form.description = row.description || ''
  form.envStr = (row.env || []).join('\n')
  form.workingDir = row.working_dir || ''
  form.timeoutSeconds = row.timeout_seconds || 60
  form.enabled = row.enabled !== false
  // undefined = 未设置（跟随工具声明）；显式 false/true 才是覆盖
  form.approvalMode =
    row.require_approval === undefined ? 'inherit' : row.require_approval ? 'always' : 'never'
  form.profilesStr = (row.profiles || []).join(', ')
  formTools.value = (row.tools || []).map((tool) => ({
    name: tool.name,
    description: tool.description,
    argsStr: (tool.args || []).join('\n'),
    readOnly: !!tool.read_only,
    stdinParam: tool.stdin_param || '',
    timeoutSeconds: tool.timeout_seconds || 0,
    paramsJson: tool.params?.length ? JSON.stringify(tool.params, null, 0) : '',
  }))
  // 已有的高级配置非空才自动展开，避免白屏一长串
  showAdvanced.value = !!(
    (row.args || []).length
    || (row.env || []).length
    || row.working_dir
    || (row.profiles || []).length
    || row.require_approval !== undefined
  )
  expandedTool.value = null
  showForm.value = true
}

/** 一键填入一个真实可用的例子：形态对了，用户改两行就能用。 */
function fillExample() {
  form.name = form.name || 'gh'
  form.command = form.command || 'gh'
  form.description = form.description || 'GitHub CLI'
  form.envStr = form.envStr || 'GH_TOKEN='
  formTools.value = [
    {
      name: 'pr_list',
      description: 'List pull requests of a repository',
      argsStr: 'pr\nlist\n--repo\n{{repo}}\n--limit\n{{limit}}\n--state\n{{state}}\n{{?json}}--json=number,title,state,url',
      readOnly: true,
      stdinParam: '',
      timeoutSeconds: 30,
      paramsJson: JSON.stringify([
        { name: 'repo', description: 'owner/name', required: true },
        { name: 'limit', type: 'integer', description: 'max rows', default: 20 },
        { name: 'state', enum: ['open', 'closed', 'merged', 'all'], default: 'open' },
        { name: 'json', type: 'boolean', description: 'output JSON fields' },
      ]),
    },
  ]
  // 示例填的是完整模板语法：展开高级区 + 工具卡，让用户看到可改的地方
  showAdvanced.value = true
  expandedTool.value = 0
}

function buildConnector(): CLIConnector {
  const tools = formTools.value.map((tool) => {
    let params: CLIConnector['tools'][number]['params']
    const raw = tool.paramsJson.trim()
    if (raw) {
      try {
        params = JSON.parse(raw)
      } catch (e: any) {
        throw new Error(`${t('cli.paramsInvalid')}: ${String(e?.message || e)}`)
      }
      if (!Array.isArray(params)) {
        throw new Error(t('cli.paramsInvalid'))
      }
    }
    return {
      name: tool.name.trim(),
      description: tool.description.trim(),
      args: splitLines(tool.argsStr),
      read_only: tool.readOnly,
      stdin_param: tool.stdinParam.trim() || undefined,
      timeout_seconds: tool.timeoutSeconds || undefined,
      params,
    }
  })

  return {
    command: form.command.trim(),
    args: splitLines(form.argsStr),
    description: form.description.trim(),
    working_dir: form.workingDir.trim() || undefined,
    env: splitLines(form.envStr),
    timeout_seconds: form.timeoutSeconds || undefined,
    enabled: form.enabled,
    profiles: form.profilesStr.split(',').map((p) => p.trim()).filter(Boolean),
    // inherit 显式传 undefined：JSON.stringify 会丢掉该字段，后端保持 nil
    require_approval:
      form.approvalMode === 'inherit' ? undefined : form.approvalMode === 'always',
    tools,
  }
}

async function handleSave() {
  if (!form.name.trim()) {
    message.warning(t('cli.nameRequired'))
    return
  }
  if (!form.command.trim()) {
    message.warning(t('cli.commandRequired'))
    return
  }
  if (!formTools.value.length) {
    message.warning(t('cli.toolsRequired'))
    return
  }

  let payload: CLIConnector
  try {
    payload = buildConnector()
  } catch (e: any) {
    message.error(String(e?.message || e))
    return
  }

  saving.value = true
  try {
    await saveCLIConnector(form.name.trim(), payload)
    message.success(t('cli.saved'))
    showForm.value = false
    await handleRefresh()
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally {
    saving.value = false
  }
}

function confirmDelete(row: CLIConnectorView) {
  dialog.warning({
    title: t('common.delete'),
    content: t('cli.deleteConfirm', { name: row.name }),
    positiveText: t('common.delete'),
    negativeText: t('common.cancel'),
    onPositiveClick: async () => {
      try {
        await removeCLIConnector(row.name)
        message.success(t('cli.deleted'))
        await handleRefresh()
      } catch (e: any) {
        message.error(String(e?.message || e))
      }
    },
  })
}

function openTest(row: CLIConnectorView) {
  // 试跑走未保存的改动：用户刚在表单里加的工具还没落盘也要能试
  const inForm = showForm.value && editingName.value === row.name
  testTarget.value = inForm ? ({ ...row, ...buildConnectorSafely() } as CLIConnectorView) : row
  testTool.value = testTarget.value?.tools?.[0]?.name || null
  testParams.value = '{}'
  testResult.value = null
  showTest.value = true
}

function buildConnectorSafely(): Partial<CLIConnector> {
  try {
    return buildConnector()
  } catch {
    return {}
  }
}

async function runTest() {
  if (!testTarget.value || !testTool.value) return
  let params: Record<string, unknown> = {}
  try {
    params = testParams.value.trim() ? JSON.parse(testParams.value) : {}
  } catch (e: any) {
    message.error(`${t('cli.paramsInvalid')}: ${String(e?.message || e)}`)
    return
  }
  testing.value = true
  testResult.value = null
  try {
    testResult.value = await testCLIConnector(testTarget.value, testTool.value, params)
  } catch (e: any) {
    testResult.value = { ok: false, error: String(e?.message || e), elapsed_ms: 0 }
  } finally {
    testing.value = false
  }
}

onMounted(handleRefresh)
</script>

<style scoped>
.cli-tool-head {
  display: flex;
  align-items: center;
  gap: 8px;
}

.cli-tool-desc {
  font-size: 12px;
  color: #606266;
  margin: 4px 0;
}

.cli-tool-args {
  margin: 0;
  padding: 8px 10px;
  background: #f7f8fa;
  border-radius: 6px;
  font-size: 12px;
  line-height: 1.6;
  color: #303133;
  white-space: pre-wrap;
  word-break: break-all;
}

.cli-tool-params {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 6px;
}

@media (prefers-color-scheme: dark) {
  .cli-tool-desc {
    color: #b6b6b6;
  }

  .cli-tool-args {
    background: #1f1f1f;
    color: #d8d8d8;
  }
}
</style>

<style>
/*
  弹窗（n-modal）会 teleport 到 body，scoped 样式对其中元素不可靠
  （data-v-xxx 匹配不上，naive-ui 的 preset card 更是整块隔开）。
  项目约定：弹窗内部的样式写在全局 style 块里，类名带前缀避免撞车。
*/
.cli-quick {
  display: flex;
  gap: 8px;
  align-items: center;
}

.cli-quick .n-input {
  flex: 1;
}

/* 高级设置 / 工具卡的折叠开关行 */
.cli-adv-toggle {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  font-size: 13px;
  color: #606266;
  user-select: none;
  padding: 2px 0;
}

.cli-adv-toggle:hover {
  color: #2080f0;
}

.cli-adv-chevron {
  /* 裸 <component :is> 渲染的 SVG 没有固有尺寸,必须显式约束,否则撑满整行 */
  width: 14px;
  height: 14px;
  flex-shrink: 0;
}

/* 工具卡：默认收起显示摘要，点开就地编辑 */
.cli-tool-card {
  border: 1px solid #ebedf0;
  border-radius: 8px;
  background: #fcfcfd;
  overflow: hidden;
}

.cli-tool-card-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 10px;
  cursor: pointer;
  flex-wrap: wrap;
}

.cli-tool-card-head:hover {
  background: #f5f6f8;
}

.cli-tool-card-title {
  font-weight: 600;
  font-size: 13px;
}

.cli-tool-card-desc {
  font-size: 12px;
  color: #909399;
  flex: 1;
  min-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cli-tool-card-body {
  padding: 0 10px 10px;
}

.cli-output-label {
  font-size: 12px;
  color: #909399;
}

.cli-output {
  margin: 0;
  padding: 10px;
  background: #f7f8fa;
  border-radius: 6px;
  font-size: 12px;
  line-height: 1.6;
  max-height: 260px;
  overflow: auto;
  white-space: pre-wrap;
  word-break: break-all;
}

.cli-output.is-error {
  background: #fff5f5;
  color: #d03050;
}

@media (prefers-color-scheme: dark) {
  .cli-adv-toggle {
    color: #b8b8b8;
  }

  .cli-adv-toggle:hover {
    color: #78a9ff;
  }

  .cli-tool-card {
    background: #1f1f1f;
    border-color: #333;
  }

  .cli-tool-card-head:hover {
    background: #292929;
  }

  .cli-tool-card-desc {
    color: #7d7d7d;
  }

  .cli-output-label {
    color: #7d7d7d;
  }

  .cli-output {
    background: #1f1f1f;
    color: #d8d8d8;
  }

  .cli-output.is-error {
    background: #2a1a1c;
    color: #ff8b8b;
  }
}
</style>
