<template>
  <div>
    <!-- 页面头：标题 + 主操作。ACP 是"接外部编码 agent"，与 MCP 连接器同类
         （低频接入配置），入口放在左下角"设置"菜单里，与自定义连接器并列。 -->
    <div class="acp-page-head">
      <div class="acp-page-title">
        <h2>{{ t('acp.title') }}</h2>
        <span class="acp-page-subtitle">{{ t('acp.subtitle') }}</span>
      </div>
      <n-space :size="8" align="center">
        <n-button size="small" type="primary" @click="openCreate">
          <template #icon><component :is="AddOutline" /></template>
          {{ t('acp.add') }}
        </n-button>
        <!-- 调用日志：排障入口。模型经 acp_prompt 的调用在聊天页只看到结果，
             agent 挂了/没反应时来这里看过程记录与 stderr。 -->
        <n-button size="small" @click="openLogs">
          <template #icon><component :is="DocumentTextOutline" /></template>
          {{ t('acp.logs') }}
        </n-button>
        <n-button size="small" :loading="loading" @click="load">
          <template #icon><component :is="RefreshOutline" /></template>
          {{ t('common.refresh') }}
        </n-button>
      </n-space>
    </div>

    <!-- 配置文件路径 + 工具可见性：这两条是排障时最先要看的 -->
    <div class="acp-meta">
      <div class="acp-meta-row">
        <span class="acp-meta-label">{{ t('acp.configPath') }}</span>
        <code class="acp-meta-value">{{ configPath || '—' }}</code>
      </div>
      <n-alert :type="toolsAvailable ? 'success' : 'warning'" :bordered="false" class="acp-tools-alert">
        {{ toolsAvailable ? t('acp.toolsOn') : t('acp.toolsOff') }}
      </n-alert>
    </div>

    <!-- 工具条：搜索 + 状态筛选（形态与连接器页一致，conn-* 是全局样式） -->
    <div class="conn-toolbar">
      <n-input v-model:value="search" clearable :placeholder="t('acp.search')" class="conn-search">
        <template #prefix><component :is="SearchOutline" /></template>
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
    </div>

    <n-spin v-if="loading && !agents.length" />
    <template v-else>
      <div v-if="visibleAgents.length" class="conn-grid">
        <div
          v-for="row in visibleAgents"
          :key="row.name"
          class="conn-card"
          :class="{ 'is-idle': !row.enabled || !row.connected }"
          @click="openEdit(row)"
        >
          <div class="conn-card-head">
            <div class="conn-avatar" :class="dotClass(row)">{{ avatarText(row.name) }}</div>
            <div class="conn-ident">
              <div class="conn-name">
                <span class="conn-name-text">{{ row.name }}</span>
                <span class="conn-dot" :class="dotClass(row)" />
              </div>
              <div class="conn-sub">
                <span class="acp-command" :title="row.command">{{ row.command }}</span>
              </div>
            </div>
            <!-- 裸 <component :is> 渲染的 vicons 没有固有尺寸，必须给宽度（见
                 CLIConnectorsPanel 的 .cli-adv-chevron 踩坑记录） -->
            <n-dropdown
              trigger="click"
              :options="rowMenu(row)"
              @select="(key: string) => handleRowMenu(key, row)"
            >
              <n-button text size="tiny" class="conn-more acp-more" @click.stop>
                <template #icon><component :is="EllipsisHorizontalOutline" /></template>
              </n-button>
            </n-dropdown>
          </div>

          <div class="conn-desc" :title="row.description || row.workspace || ''">
            {{ row.description || row.workspace || t('acp.noDesc') }}
          </div>

          <div class="conn-foot" @click.stop>
            <n-tag v-if="!row.enabled" size="tiny" :bordered="false">{{ t('acp.disabled') }}</n-tag>
            <span v-else class="conn-state" :class="{ 'is-on': row.connected }">
              {{ row.connected ? t('acp.connected') : t('acp.idle') }}
            </span>
            <n-button size="tiny" :loading="!!busy[row.name]" @click="runTest(row)">
              {{ row.connected ? t('acp.retest') : t('acp.test') }}
            </n-button>
          </div>
        </div>
      </div>

      <n-empty
        v-else
        :description="agents.length ? t('acp.noMatch') : t('acp.empty')"
        style="margin-top: 24px;"
      >
        <template #extra>
          <div class="acp-empty-extra">
            <n-button v-if="!agents.length" size="small" type="primary" @click="openCreate">
              {{ t('acp.add') }}
            </n-button>
            <div v-if="!agents.length" class="acp-empty-hint">
              {{ t('acp.emptyHint') }}
            </div>
          </div>
        </template>
      </n-empty>
    </template>

    <!-- 编辑/新建弹窗。字段多（命令/参数/描述/工作区/cwd/附加目录/env/超时/
         三个开关/白名单），必须带 modal-scroll，否则卡片高度超出视口、底部的
         保存按钮永远在屏幕外（naive-ui 的 card 自身不滚动）。 -->
    <n-modal
      v-model:show="showEditor"
      preset="card"
      class="modal-responsive modal-scroll"
      style="width: 720px; max-width: 96vw;"
      :title="editingName ? t('acp.edit') : t('acp.add')"
    >
      <div class="acp-form">
        <n-alert v-if="activeTplId" type="info" :bordered="false" class="acp-tpl-note">
          {{ t('acp.tplNote', { name: tplName(activeTplId) }) }}
        </n-alert>
        <div class="acp-form-grid">
          <div class="acp-field">
            <label class="acp-label">{{ t('acp.fieldName') }}</label>
            <n-input v-model:value="form.name" size="small" placeholder="claude" />
            <div class="acp-hint">{{ t('acp.nameHint') }}</div>
          </div>
          <div class="acp-field">
            <label class="acp-label">{{ t('acp.fieldCommand') }}</label>
            <n-input v-model:value="form.command" size="small" placeholder="npx" />
            <div class="acp-hint">{{ t('acp.commandHint') }}</div>
          </div>
        </div>

        <div class="acp-field">
          <label class="acp-label">{{ t('acp.fieldArgs') }}</label>
          <n-input
            v-model:value="form.argsStr"
            type="textarea"
            size="small"
            :autosize="{ minRows: 2, maxRows: 6 }"
            placeholder="-y
@zed-industries/claude-code-acp"
          />
          <div class="acp-hint">{{ t('acp.argsHint') }}</div>
        </div>

        <div class="acp-field">
          <label class="acp-label">{{ t('acp.fieldDescription') }}</label>
          <n-input v-model:value="form.description" size="small" :placeholder="t('acp.descriptionPlaceholder')" />
          <div class="acp-hint">{{ t('acp.descriptionHint') }}</div>
        </div>

        <div class="acp-form-grid">
          <div class="acp-field">
            <label class="acp-label">{{ t('acp.fieldWorkspace') }}</label>
            <!-- 工作区支持点击选择：与设置页的工作目录同款 DirPickerModal，
                 输入框仍保留（用户可以手填不存在于列表里的路径）。 -->
            <div class="acp-input-row">
              <n-input v-model:value="form.workspace" size="small" placeholder="D:/project/demo" class="acp-input-grow" />
              <n-button size="small" secondary @click="showWorkspacePicker = true">
                <template #icon><n-icon :component="FolderOpenOutline" /></template>
              </n-button>
            </div>
            <div class="acp-hint">{{ t('acp.workspaceHint') }}</div>
          </div>
          <div class="acp-field">
            <label class="acp-label">{{ t('acp.fieldCwd') }}</label>
            <n-input v-model:value="form.cwd" size="small" :placeholder="t('acp.optional')" />
            <div class="acp-hint">{{ t('acp.cwdHint') }}</div>
          </div>
        </div>

        <div class="acp-field">
          <label class="acp-label">{{ t('acp.fieldAdditionalDirs') }}</label>
          <n-input
            v-model:value="form.additionalDirsStr"
            type="textarea"
            size="small"
            :autosize="{ minRows: 2, maxRows: 5 }"
            :placeholder="t('acp.optional')"
          />
          <div class="acp-hint">{{ t('acp.additionalDirsHint') }}</div>
        </div>

        <div class="acp-field">
          <label class="acp-label">{{ t('acp.fieldEnv') }}</label>
          <n-input
            v-model:value="form.envStr"
            type="textarea"
            size="small"
            :autosize="{ minRows: 2, maxRows: 6 }"
            placeholder="ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}"
          />
          <div class="acp-hint">{{ t('acp.envHint') }}</div>
        </div>

        <div class="acp-form-grid">
          <div class="acp-field">
            <label class="acp-label">{{ t('acp.fieldTimeout') }}</label>
            <n-input-number
              v-model:value="form.timeoutSeconds"
              size="small"
              :min="0"
              :max="3600"
              :step="60"
              style="width: 100%"
            />
            <div class="acp-hint">{{ t('acp.timeoutHint') }}</div>
          </div>
          <div class="acp-field">
            <label class="acp-label">{{ t('acp.enabledLabel') }}</label>
            <n-switch v-model:value="form.enabled" size="small" />
            <div class="acp-hint">{{ t('acp.enabledHint') }}</div>
          </div>
        </div>

        <div class="acp-field">
          <label class="acp-label">{{ t('acp.allowWrite') }}</label>
          <div class="acp-switch-row">
            <n-switch v-model:value="form.allowWrite" size="small" />
            <span class="acp-switch-hint">{{ t('acp.allowWriteHint') }}</span>
          </div>
        </div>

        <!-- 审批白名单：auto_approve。默认只放行只读类，写/执行要显式勾选。 -->
        <div class="acp-field">
          <label class="acp-label">{{ t('acp.approveTitle') }}</label>
          <div class="acp-approve-row">
            <n-switch v-model:value="form.approveAll" size="small" />
            <span class="acp-switch-hint">{{ t('acp.approveAllHint') }}</span>
          </div>
          <n-checkbox-group
            v-if="!form.approveAll"
            v-model:value="form.autoApprove"
            class="acp-kinds"
          >
            <n-checkbox v-for="k in toolKinds" :key="k" :value="k" :label="t(`acp.kind.${k}`)" />
          </n-checkbox-group>
          <div class="acp-hint">{{ t('acp.approveHint') }}</div>
        </div>
      </div>

      <template #footer>
        <div class="acp-modal-footer">
          <n-button size="small" @click="showEditor = false">{{ t('common.cancel') }}</n-button>
          <n-button size="small" type="primary" :loading="saving" @click="save">
            {{ t('common.save') }}
          </n-button>
        </div>
      </template>
    </n-modal>

    <!-- 模板选择层：接入引导的第一步。卡片上的命令行预览直接渲染模板数据
         （含 npx 包名），不经 i18n——包名里的 @ 与 vue-i18n 的链接语法冲突，
         文案一律放 acp.tpl.<id>.* 且不含包名。 -->
    <n-modal
      v-model:show="showPicker"
      preset="card"
      class="modal-responsive"
      style="width: 560px; max-width: 96vw;"
      :title="t('acp.pickTitle')"
    >
      <div class="acp-pick">
        <div class="acp-pick-sub">{{ t('acp.pickSubtitle') }}</div>
        <n-spin v-if="catalogLoading && !catalog" size="small" style="margin: 20px 0;" />
        <div v-else-if="catalog?.templates?.length" class="acp-pick-list">
          <div
            v-for="tpl in catalog.templates"
            :key="tpl.id"
            class="acp-pick-card"
            @click="applyTemplate(tpl)"
          >
            <div class="acp-pick-head">
              <span class="acp-pick-name">{{ tplName(tpl.id) }}</span>
              <n-tag v-if="tplRuntimeOk(tpl) === true" size="tiny" type="success" :bordered="false">
                {{ t('acp.runtimeOk') }}
              </n-tag>
              <n-tag v-else-if="tplRuntimeOk(tpl) === false" size="tiny" type="warning" :bordered="false">
                {{ t('acp.runtimeMissing') }}
              </n-tag>
            </div>
            <div class="acp-pick-desc">{{ tplDesc(tpl.id) }}</div>
            <code class="acp-pick-cmd">{{ [tpl.command, ...(tpl.args || [])].join(' ') }}</code>
            <div v-if="tpl.env_keys?.length" class="acp-pick-envs">
              <span class="acp-pick-env-label">{{ t('acp.pickNeedsEnv') }}</span>
              <n-tag v-for="k in tpl.env_keys" :key="k.name" size="tiny" :bordered="false">
                {{ k.name }}
              </n-tag>
            </div>
          </div>
        </div>
        <n-alert v-else-if="!catalogLoading" type="warning" :bordered="false" style="margin: 12px 0;">
          {{ t('acp.pickEmpty') }}
        </n-alert>
        <div class="acp-pick-footer">
          <n-button size="small" @click="openManualCreate">{{ t('acp.pickManual') }}</n-button>
        </div>
      </div>
    </n-modal>

    <!-- 连接结果：能力协商是"这个 agent 到底能不能看图/收音频"的唯一依据，
         直接摊开给用户看，省得靠猜。 -->
    <n-modal
      v-model:show="showTest"
      preset="card"
      class="modal-responsive"
      style="width: 560px; max-width: 96vw;"
      :title="t('acp.testTitle')"
    >
      <div v-if="testError" class="acp-test-error">
        <n-alert type="error" :bordered="false" :title="t('acp.testFailed')">
          <div class="acp-error-text">{{ testError }}</div>
        </n-alert>
      </div>
      <div v-else-if="testResult" class="acp-test-body">
        <div class="acp-test-head">
          <n-tag type="success" size="small" :bordered="false">{{ t('acp.testOK') }}</n-tag>
          <span class="acp-test-name">
            {{ testResult.agent_info?.title || testResult.agent_info?.name || testResult.agent }}
            <span v-if="testResult.agent_info?.version" class="acp-test-version">v{{ testResult.agent_info.version }}</span>
          </span>
          <span class="acp-test-elapsed">{{ t('acp.elapsed') }} {{ testResult.elapsed_ms }}ms</span>
        </div>

        <div class="acp-kv">
          <span class="acp-kv-key">{{ t('acp.protocol') }}</span>
          <span class="acp-kv-val">v{{ testResult.protocol_version }}</span>
        </div>
        <div class="acp-kv">
          <span class="acp-kv-key">{{ t('acp.session') }}</span>
          <span class="acp-kv-val acp-mono">{{ testResult.session_id }}</span>
        </div>
        <div class="acp-kv">
          <span class="acp-kv-key">{{ t('acp.roots') }}</span>
          <span class="acp-kv-val acp-mono">{{ (testResult.roots || []).join(', ') || '—' }}</span>
        </div>
        <div v-if="testResult.auth_methods?.length" class="acp-kv">
          <span class="acp-kv-key">{{ t('acp.authMethods') }}</span>
          <span class="acp-kv-val">{{ testResult.auth_methods.join(', ') }}</span>
        </div>

        <div class="acp-cap-title">{{ t('acp.capTitle') }}</div>
        <div class="acp-caps">
          <n-tag v-for="cap in capTags" :key="cap.key" size="small" :type="cap.on ? 'success' : 'default'" :bordered="false">
            {{ cap.label }}
          </n-tag>
        </div>
      </div>
    </n-modal>

    <!-- 调用日志：prompt/建连/重置的过程记录 + 存活连接的 stderr 尾巴。
         点击一行展开输入/输出预览与 stderr。记录只存进程内存（最近 200 条），
         服务重启即清空——这是排障辅助，不是审计日志。 -->
    <n-modal
      v-model:show="showLogs"
      preset="card"
      class="modal-responsive modal-scroll"
      style="width: 760px; max-width: 96vw;"
      :title="t('acp.logsTitle')"
    >
      <div class="acp-logs">
        <div class="acp-logs-hint">{{ t('acp.logsHint') }}</div>
        <n-spin v-if="logsLoading && !logRecords.length" size="small" style="margin: 16px 0;" />
        <template v-else>
          <!-- 存活连接的 stderr：agent 还没被调用也会说话（npm 装包、鉴权失败），这里常驻可看 -->
          <div v-if="logStderrEntries.length" class="acp-log-stderr">
            <div class="acp-log-stderr-title">{{ t('acp.stderrTitle') }}</div>
            <div v-for="[name, tail] in logStderrEntries" :key="name" class="acp-log-stderr-item">
              <n-tag size="tiny" :bordered="false">{{ name }}</n-tag>
              <pre class="acp-log-pre">{{ tail }}</pre>
            </div>
          </div>

          <n-empty v-if="!logRecords.length" :description="t('acp.logEmpty')" style="margin: 16px 0;" />
          <div v-else class="acp-log-list">
            <div
              v-for="rec in logRecords"
              :key="rec.id"
              class="acp-log-item"
              :class="{ 'is-error': rec.status === 'error' }"
              @click="toggleLogDetail(rec.id)"
            >
              <div class="acp-log-row">
                <span class="acp-log-time">{{ fmtLogTime(rec.start) }}</span>
                <n-tag size="tiny" :bordered="false">{{ rec.agent }}</n-tag>
                <n-tag size="tiny" :bordered="false" :type="kindTagType(rec.kind)">
                  {{ t(`acp.logKind.${rec.kind}`) }}
                </n-tag>
                <n-tag size="tiny" :bordered="false" :type="statusTagType(rec.status)">
                  {{ t(`acp.logStatus.${rec.status}`) }}
                </n-tag>
                <span class="acp-log-meta">{{ rec.duration_ms }}ms</span>
                <span v-if="rec.stop_reason" class="acp-log-meta">{{ rec.stop_reason }}</span>
                <span v-if="rec.tool_calls" class="acp-log-meta">{{ t('acp.logTools', { n: rec.tool_calls }) }}</span>
              </div>
              <div v-if="rec.error" class="acp-log-error">{{ rec.error }}</div>
              <div v-if="openLogId === rec.id" class="acp-log-detail">
                <div v-if="rec.prompt_preview" class="acp-log-block">
                  <div class="acp-log-block-label">{{ t('acp.logPrompt') }}</div>
                  <pre class="acp-log-pre">{{ rec.prompt_preview }}</pre>
                </div>
                <div v-if="rec.reply_preview" class="acp-log-block">
                  <div class="acp-log-block-label">{{ t('acp.logReply') }}</div>
                  <pre class="acp-log-pre">{{ rec.reply_preview }}</pre>
                </div>
                <div v-if="rec.stderr" class="acp-log-block">
                  <div class="acp-log-block-label">{{ t('acp.stderrTitle') }}</div>
                  <pre class="acp-log-pre">{{ rec.stderr }}</pre>
                </div>
              </div>
            </div>
          </div>
        </template>
      </div>
    </n-modal>

    <!-- 工作区目录选择：与设置页/聊天页共用的 DirPickerModal（推荐目录 + 浏览 + 新建文件夹） -->
    <DirPickerModal
      v-model:show="showWorkspacePicker"
      :title="t('acp.fieldWorkspace')"
      :current="form.workspace"
      @select="(p: string) => (form.workspace = p)"
    />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { NAlert, NButton, NCheckbox, NCheckboxGroup, NDropdown, NEmpty, NIcon, NInput, NInputNumber, NModal, NSpace, NSpin, NSwitch, NTag, useDialog, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { AddOutline, DocumentTextOutline, EllipsisHorizontalOutline, FolderOpenOutline, RefreshOutline, SearchOutline } from '@vicons/ionicons5'
import DirPickerModal from '@/components/DirPickerModal.vue'
import {
  createACPAgent,
  deleteACPAgent,
  disconnectACPAgent,
  listACPAgents,
  listACPLogs,
  listACPTemplates,
  resetACPAgentSession,
  testACPAgent,
  updateACPAgent,
} from '@/api/acp'
import type { ACPAgent, ACPAgentPayload, ACPTestResult, ACPCatalog, ACPTemplate, ACPCallRecord } from '@/api/acp'

const { t } = useI18n()
const message = useMessage()
const dialog = useDialog()

/** 与 internal/acp 的 ToolKind 枚举保持一致（config.Validate 会拒绝未知值）。 */
const toolKinds = ['read', 'edit', 'delete', 'move', 'search', 'execute', 'think', 'fetch', 'switch_mode', 'other'] as const
type ToolKind = (typeof toolKinds)[number]

const loading = ref(false)
const agents = ref<ACPAgent[]>([])
const configPath = ref('')
const toolsAvailable = ref(false)
const search = ref('')
const statusFilter = ref<'all' | 'connected' | 'idle' | 'disabled'>('all')
const busy = ref<Record<string, boolean>>({})

const showEditor = ref(false)
const editingName = ref('')
const saving = ref(false)

// 模板选择层：点「添加」先看到内置 agent 模板，选完预填表单，只剩 Key 和目录要补。
// catalog 加载失败时降级为直接开空白表单——模板是加速器，不是硬依赖。
const showPicker = ref(false)
const catalog = ref<ACPCatalog | null>(null)
const catalogLoading = ref(false)
const catalogLoaded = ref(false)
// 当前表单来自哪个模板（仅用于表单顶部提示"已按模板预填"）。
const activeTplId = ref('')

const showTest = ref(false)
const testResult = ref<ACPMessageResult | null>(null)
const testError = ref('')

// 调用日志弹窗。打开时全量拉一次（无自动刷新）；点行展开详情。
const showLogs = ref(false)
const logsLoading = ref(false)
const logRecords = ref<ACPCallRecord[]>([])
const logStderr = ref<Record<string, string>>({})
const openLogId = ref<number | null>(null)

// 工作区目录选择器（DirPickerModal）
const showWorkspacePicker = ref(false)

// ACPMessageResult 只是本文件内的别名：api 层返回的是 ACPTestResult。
type ACPMessageResult = ACPTestResult

const form = reactive({
  name: '',
  command: '',
  argsStr: '',
  description: '',
  workspace: '',
  additionalDirsStr: '',
  cwd: '',
  envStr: '',
  timeoutSeconds: 0,
  allowWrite: false,
  enabled: true,
  autoApprove: [] as string[],
  approveAll: false,
})

function linesToArray(value: string): string[] {
  return value
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
}

function avatarText(name: string): string {
  const trimmed = (name || '').trim()
  return trimmed ? trimmed.slice(0, 2).toUpperCase() : '?'
}

function dotClass(row: ACPAgent): string {
  if (!row.enabled) return 'is-off'
  return row.connected ? 'is-on' : 'is-idle'
}

const visibleAgents = computed(() => {
  const q = search.value.trim().toLowerCase()
  return agents.value.filter((row) => {
    if (statusFilter.value === 'connected' && !row.connected) return false
    if (statusFilter.value === 'idle' && (row.connected || !row.enabled)) return false
    if (statusFilter.value === 'disabled' && row.enabled) return false
    if (!q) return true
    return [row.name, row.command, row.description]
      .filter(Boolean)
      .some((field) => String(field).toLowerCase().includes(q))
  })
})

const statusChips = computed(() => {
  const connected = agents.value.filter((a) => a.connected).length
  const disabled = agents.value.filter((a) => !a.enabled).length
  const idle = agents.value.length - connected - disabled
  return [
    { key: 'all' as const, label: `${t('acp.chipAll')} ${agents.value.length}` },
    { key: 'connected' as const, label: `${t('acp.connected')} ${connected}` },
    { key: 'idle' as const, label: `${t('acp.idle')} ${Math.max(idle, 0)}` },
    { key: 'disabled' as const, label: `${t('acp.disabled')} ${disabled}` },
  ]
})

const capTags = computed(() => {
  const caps = testResult.value?.capabilities
  if (!caps) return []
  return [
    { key: 'image', label: t('acp.capImage'), on: caps.image },
    { key: 'audio', label: t('acp.capAudio'), on: caps.audio },
    { key: 'load_session', label: t('acp.capLoadSession'), on: caps.load_session },
    { key: 'embedded', label: t('acp.capEmbedded'), on: caps.embedded_context },
    { key: 'mcp_http', label: 'MCP HTTP', on: caps.mcp_http },
    { key: 'mcp_sse', label: 'MCP SSE', on: caps.mcp_sse },
  ]
})

async function load() {
  loading.value = true
  try {
    const list = await listACPAgents()
    agents.value = list.agents || []
    configPath.value = list.config_path
    toolsAvailable.value = list.tools_available
  } catch (err: any) {
    message.error(err?.message || t('acp.loadFailed'))
  } finally {
    loading.value = false
  }
}

function resetForm() {
  form.name = ''
  form.command = ''
  form.argsStr = ''
  form.description = ''
  form.workspace = ''
  form.additionalDirsStr = ''
  form.cwd = ''
  form.envStr = ''
  form.timeoutSeconds = 0
  form.allowWrite = false
  form.enabled = true
  form.autoApprove = ['read', 'search', 'think', 'fetch']
  form.approveAll = false
}

async function ensureCatalog() {
  if (catalogLoaded.value || catalogLoading.value) return
  catalogLoading.value = true
  try {
    catalog.value = await listACPTemplates()
    catalogLoaded.value = true
  } catch {
    // 拿不到模板就只显示"手动配置"入口；别让一次失败堵死创建路径。
    catalog.value = null
  } finally {
    catalogLoading.value = false
  }
}

function openCreate() {
  resetForm()
  editingName.value = ''
  activeTplId.value = ''
  void ensureCatalog()
  showPicker.value = true
}

/** 模板声明的依赖是否都在本机 PATH 上。 */
function tplRuntimeOk(tpl: ACPTemplate): boolean | null {
  if (!catalog.value || !tpl.needs_runtime.length) return null
  return tpl.needs_runtime.every((b) =>
    catalog.value!.runtimes.some((p) => p.binary === b && p.found),
  )
}

function tplName(id: string): string {
  return t(`acp.tpl.${id}.name`)
}

function tplDesc(id: string): string {
  return t(`acp.tpl.${id}.desc`)
}

/**
 * 应用模板：预填命令/参数/白名单；凭据变量预填 `${VAR}` 引用——
 * 这是最省事的默认（宿主环境里已有 Key 时直接能跑），用户也能改成字面量。
 * 工作区留空让用户自己选，其余保持默认（allow_write 关）。
 */
function applyTemplate(tpl: ACPTemplate) {
  resetForm()
  editingName.value = ''
  activeTplId.value = tpl.id
  form.name = tpl.id
  form.command = tpl.command
  form.argsStr = (tpl.args || []).join('\n')
  form.envStr = (tpl.env_keys || [])
    .map((k) => `${k.name}=\${${k.name}}`)
    .join('\n')
  form.autoApprove = (tpl.auto_approve || []).filter((k) => k.toLowerCase() !== 'all')
  showPicker.value = false
  showEditor.value = true
}

function openManualCreate() {
  resetForm()
  editingName.value = ''
  activeTplId.value = ''
  showPicker.value = false
  showEditor.value = true
}

function openEdit(row: ACPAgent) {
  resetForm()
  editingName.value = row.name
  form.name = row.name
  form.command = row.command || ''
  form.argsStr = (row.args || []).join('\n')
  form.description = row.description || ''
  form.workspace = row.workspace || ''
  form.additionalDirsStr = (row.additional_dirs || []).join('\n')
  form.cwd = row.cwd || ''
  form.envStr = (row.env || []).join('\n')
  form.timeoutSeconds = row.timeout_seconds || 0
  form.allowWrite = !!row.allow_write
  form.enabled = row.enabled
  const kinds = row.auto_approve || []
  form.approveAll = kinds.some((k) => k.toLowerCase() === 'all')
  form.autoApprove = kinds.filter((k) => k.toLowerCase() !== 'all')
  showEditor.value = true
}

async function save() {
  if (!form.name.trim() || !form.command.trim()) {
    message.warning(t('acp.nameRequired'))
    return
  }
  const payload: ACPAgentPayload = {
    name: form.name.trim(),
    command: form.command.trim(),
    args: linesToArray(form.argsStr),
    env: linesToArray(form.envStr),
    cwd: form.cwd.trim(),
    workspace: form.workspace.trim(),
    additional_dirs: linesToArray(form.additionalDirsStr),
    description: form.description.trim(),
    timeout_seconds: form.timeoutSeconds || 0,
    allow_write: form.allowWrite,
    auto_approve: form.approveAll ? ['all'] : (form.autoApprove as ToolKind[]),
    enabled: form.enabled,
  }

  saving.value = true
  try {
    if (editingName.value) {
      await updateACPAgent(editingName.value, payload)
    } else {
      await createACPAgent(payload)
    }
    message.success(t('acp.saved'))
    showEditor.value = false
    await load()
  } catch (err: any) {
    message.error(err?.message || t('acp.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function runTest(row: ACPAgent) {
  busy.value = { ...busy.value, [row.name]: true }
  testResult.value = null
  testError.value = ''
  try {
    testResult.value = await testACPAgent(row.name)
  } catch (err: any) {
    testError.value = err?.message || String(err)
  } finally {
    busy.value = { ...busy.value, [row.name]: false }
    showTest.value = true
    // 测试即建连，列表里的"已连接"要立刻反映出来。
    void load()
  }
}

async function resetSession(row: ACPAgent) {
  busy.value = { ...busy.value, [row.name]: true }
  try {
    await resetACPAgentSession(row.name)
    message.success(t('acp.resetDone', { name: row.name }))
    await load()
  } catch (err: any) {
    message.error(err?.message || t('acp.resetFailed'))
  } finally {
    busy.value = { ...busy.value, [row.name]: false }
  }
}

async function disconnect(row: ACPAgent) {
  busy.value = { ...busy.value, [row.name]: true }
  try {
    await disconnectACPAgent(row.name)
    await load()
  } catch (err: any) {
    message.error(err?.message || t('acp.disconnectFailed'))
  } finally {
    busy.value = { ...busy.value, [row.name]: false }
  }
}

/** 打开日志弹窗：全量拉最近记录 + 存活连接的 stderr。 */
async function openLogs() {
  showLogs.value = true
  logsLoading.value = true
  openLogId.value = null
  try {
    const res = await listACPLogs()
    logRecords.value = res.records || []
    logStderr.value = res.stderr || {}
  } catch (err: any) {
    message.error(err?.message || t('acp.logLoadFailed'))
  } finally {
    logsLoading.value = false
  }
}

const logStderrEntries = computed(() => Object.entries(logStderr.value))

function toggleLogDetail(id: number) {
  openLogId.value = openLogId.value === id ? null : id
}

function fmtLogTime(value: string): string {
  const d = new Date(value)
  if (Number.isNaN(d.getTime())) return value
  return d.toLocaleTimeString()
}

function kindTagType(kind: ACPCallRecord['kind']) {
  if (kind === 'connect') return 'info'
  if (kind === 'reset') return 'warning'
  return 'default'
}

function statusTagType(status: ACPCallRecord['status']) {
  if (status === 'ok') return 'success'
  if (status === 'error') return 'error'
  return 'warning'
}

function confirmDelete(row: ACPAgent) {
  dialog.warning({
    title: t('acp.delete'),
    content: `${t('acp.deleteConfirm', { name: row.name })} ${t('acp.deleteHint')}`,
    positiveText: t('common.confirm'),
    negativeText: t('common.cancel'),
    onPositiveClick: async () => {
      try {
        await deleteACPAgent(row.name)
        message.success(t('acp.deleted', { name: row.name }))
        await load()
      } catch (err: any) {
        message.error(err?.message || t('acp.deleteFailed'))
      }
    },
  })
}

function rowMenu(row: ACPAgent) {
  return [
    { label: t('acp.edit'), key: 'edit' },
    { label: t('acp.test'), key: 'test', disabled: !row.enabled },
    { label: t('acp.reset'), key: 'reset', disabled: !row.connected },
    { label: t('acp.disconnect'), key: 'disconnect', disabled: !row.connected },
    { type: 'divider' as const, key: 'divider' },
    { label: t('acp.delete'), key: 'delete' },
  ]
}

function handleRowMenu(key: string, row: ACPAgent) {
  switch (key) {
    case 'edit':
      openEdit(row)
      break
    case 'test':
      void runTest(row)
      break
    case 'reset':
      void resetSession(row)
      break
    case 'disconnect':
      void disconnect(row)
      break
    case 'delete':
      confirmDelete(row)
      break
  }
}

onMounted(load)
</script>

<style scoped>
.acp-page-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 16px;
}

.acp-page-title {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.acp-page-title h2 {
  margin: 0;
}

.acp-page-subtitle {
  font-size: 12px;
  color: #9ca3af;
}

.acp-meta {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-bottom: 16px;
}

.acp-meta-row {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: #6b7280;
  flex-wrap: wrap;
}

.acp-meta-label {
  flex-shrink: 0;
}

.acp-meta-value {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  color: #374151;
  background: rgba(128, 128, 128, 0.1);
  padding: 2px 6px;
  border-radius: 4px;
  word-break: break-all;
}

.acp-tools-alert {
  font-size: 12px;
}

.acp-command {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  display: inline-block;
  max-width: 100%;
}

/* 状态点：conn-* 全局样式只有 is-ok / is-error 两个色，
   ACP 需要"启用但没连"的中间态，在这里补三个（scoped 对模板内元素仍然生效）。 */
.conn-dot.is-on {
  background: #18a058;
}

.conn-dot.is-idle {
  background: #c9cdd4;
}

.conn-dot.is-off {
  background: #c9cdd4;
  opacity: 0.55;
}

/* 裸图标撑满容器的兜底（见 CLI 连接器的同名踩坑） */
.acp-more {
  width: 22px;
  height: 22px;
  flex-shrink: 0;
}

.acp-more :deep(.n-icon) {
  width: 16px;
  height: 16px;
}

.acp-empty-extra {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
}

.acp-empty-hint {
  font-size: 12px;
  color: #9ca3af;
  max-width: 520px;
  text-align: center;
  line-height: 1.6;
}

.acp-form {
  display: flex;
  flex-direction: column;
  gap: 14px;
}

.acp-form-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 14px;
}

@media (max-width: 640px) {
  .acp-form-grid {
    grid-template-columns: 1fr;
  }
}

.acp-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

/* 输入框 + 选择按钮并排（工作区目录选择） */
.acp-input-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.acp-input-grow {
  flex: 1;
}

.acp-label {
  font-size: 12px;
  font-weight: 500;
  color: #4b5563;
}

.acp-hint {
  font-size: 11px;
  color: #9ca3af;
  line-height: 1.5;
}

.acp-switch-row,
.acp-approve-row {
  display: flex;
  align-items: flex-start;
  gap: 10px;
}

.acp-switch-hint {
  font-size: 11px;
  color: #9ca3af;
  line-height: 1.5;
}

.acp-kinds {
  display: flex;
  flex-wrap: wrap;
  gap: 6px 16px;
  margin: 4px 0 2px;
}

.acp-modal-footer {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}

.acp-test-head {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 12px;
  flex-wrap: wrap;
}

.acp-test-name {
  font-weight: 500;
  font-size: 13px;
}

.acp-test-version {
  font-size: 12px;
  color: #9ca3af;
  margin-left: 4px;
}

.acp-test-elapsed {
  margin-left: auto;
  font-size: 11px;
  color: #9ca3af;
}

.acp-kv {
  display: flex;
  gap: 10px;
  font-size: 12px;
  padding: 3px 0;
}

.acp-kv-key {
  width: 76px;
  flex-shrink: 0;
  color: #9ca3af;
}

.acp-kv-val {
  color: #374151;
  word-break: break-all;
}

.acp-mono {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}

.acp-cap-title {
  margin-top: 12px;
  font-size: 12px;
  color: #9ca3af;
}

.acp-caps {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 6px;
}

.acp-error-text {
  white-space: pre-wrap;
  word-break: break-word;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
}

/* 调用日志弹窗 */
.acp-logs {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.acp-logs-hint {
  font-size: 11px;
  color: #9ca3af;
  line-height: 1.5;
}

.acp-log-stderr {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.acp-log-stderr-title {
  font-size: 12px;
  color: #9ca3af;
}

.acp-log-stderr-item {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.acp-log-list {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.acp-log-item {
  display: flex;
  flex-direction: column;
  gap: 6px;
  border: 1px solid rgba(128, 128, 128, 0.18);
  border-radius: 8px;
  padding: 8px 12px;
  cursor: pointer;
}

.acp-log-item:hover {
  border-color: rgba(128, 128, 128, 0.4);
}

.acp-log-item.is-error {
  border-color: rgba(208, 48, 79, 0.35);
}

.acp-log-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.acp-log-time {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  color: #6b7280;
}

.acp-log-meta {
  font-size: 11px;
  color: #9ca3af;
}

.acp-log-error {
  font-size: 12px;
  color: #d03050;
  white-space: pre-wrap;
  word-break: break-word;
}

.acp-log-detail {
  display: flex;
  flex-direction: column;
  gap: 8px;
  border-top: 1px dashed rgba(128, 128, 128, 0.25);
  padding-top: 8px;
}

.acp-log-block {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.acp-log-block-label {
  font-size: 11px;
  color: #9ca3af;
}

.acp-log-pre {
  margin: 0;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-word;
  color: #374151;
  background: rgba(128, 128, 128, 0.08);
  padding: 8px 10px;
  border-radius: 6px;
  max-height: 220px;
  overflow-y: auto;
}

/* 模板选择层 */
.acp-pick-sub {
  font-size: 12px;
  color: #6b7280;
  margin-bottom: 12px;
  line-height: 1.6;
}

.acp-pick-list {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.acp-pick-card {
  display: flex;
  flex-direction: column;
  gap: 6px;
  border: 1px solid rgba(128, 128, 128, 0.25);
  border-radius: 8px;
  padding: 12px 14px;
  cursor: pointer;
  transition: border-color 0.15s, background 0.15s;
}

.acp-pick-card:hover {
  border-color: #18a058;
  background: rgba(24, 160, 88, 0.06);
}

.acp-pick-head {
  display: flex;
  align-items: center;
  gap: 8px;
}

.acp-pick-name {
  font-weight: 500;
  font-size: 14px;
}

.acp-pick-desc {
  font-size: 12px;
  color: #6b7280;
  line-height: 1.6;
}

.acp-pick-cmd {
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
  font-size: 12px;
  color: #374151;
  background: rgba(128, 128, 128, 0.1);
  padding: 4px 8px;
  border-radius: 4px;
  word-break: break-all;
}

.acp-pick-envs {
  display: flex;
  align-items: center;
  gap: 6px;
  flex-wrap: wrap;
}

.acp-pick-env-label {
  font-size: 11px;
  color: #9ca3af;
}

.acp-pick-footer {
  margin-top: 14px;
  display: flex;
  justify-content: flex-end;
}

.acp-tpl-note {
  margin-bottom: 2px;
}

/* 深色模式：与连接器页一致的取色口径 */
@media (prefers-color-scheme: dark) {
  .acp-page-subtitle,
  .acp-hint,
  .acp-switch-hint,
  .acp-kv-key,
  .acp-cap-title,
  .acp-test-elapsed,
  .acp-empty-hint {
    color: #8b93a1;
  }

  .acp-meta-row {
    color: #9aa3b2;
  }

  .acp-label {
    color: #c9d1d9;
  }

  .acp-meta-value {
    color: #d1d5db;
    background: rgba(255, 255, 255, 0.08);
  }

  .acp-kv-val {
    color: #d1d5db;
  }

  .acp-pick-sub,
  .acp-pick-desc,
  .acp-pick-env-label {
    color: #8b93a1;
  }

  .acp-pick-cmd {
    color: #d1d5db;
    background: rgba(255, 255, 255, 0.08);
  }

  .acp-logs-hint,
  .acp-log-stderr-title,
  .acp-log-meta,
  .acp-log-block-label {
    color: #8b93a1;
  }

  .acp-log-time {
    color: #9aa3b2;
  }

  .acp-log-error {
    color: #e88080;
  }

  .acp-log-pre {
    color: #d1d5db;
    background: rgba(255, 255, 255, 0.08);
  }
}
</style>
