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

      <n-space :size="8" class="conn-toolbar-actions">
        <n-button size="small" @click="openCatalog">
          <template #icon><component :is="GridOutline" /></template>
          {{ t('cli.catalogButton') }}
        </n-button>
        <n-button size="small" @click="handleRefresh" :loading="loading">
          <template #icon><component :is="RefreshOutline" /></template>
          {{ t('common.refresh') }}
        </n-button>
        <n-button size="small" type="primary" @click="openAdd">
          <template #icon><component :is="AddOutline" /></template>
          {{ t('cli.addConnector') }}
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

    <!-- 应用目录：点一个卡片 → 检测 → 填凭据 → 一键添加并启用。
         列表与详情在同一个弹窗里切换（详情带返回），比叠两层弹窗稳。 -->
    <n-modal
      v-model:show="showCatalog"
      preset="card"
      :title="t('cli.catalogTitle')"
      style="width: 900px;"
    >
      <!-- 列表 -->
      <template v-if="!catalogDetail">
        <div class="conn-toolbar">
          <n-input
            v-model:value="catalogSearch"
            clearable
            :placeholder="t('cli.catalogSearch')"
            class="conn-search"
          >
            <template #prefix><component :is="SearchOutline" /></template>
          </n-input>

          <div class="conn-chips">
            <button
              v-for="chip in catalogChips"
              :key="chip.key"
              type="button"
              class="conn-chip"
              :class="{ 'is-active': catalogCategory === chip.key }"
              @click="catalogCategory = chip.key"
            >
              {{ chip.label }}
            </button>
          </div>

          <n-space :size="8" class="conn-toolbar-actions">
            <n-button size="small" :loading="catalogLoading" @click="loadCatalog">
              <template #icon><component :is="RefreshOutline" /></template>
              {{ t('common.refresh') }}
            </n-button>
          </n-space>
        </div>

        <div v-if="visibleCatalog.length" class="conn-grid cli-catalog-grid">
          <div
            v-for="entry in visibleCatalog"
            :key="entry.id"
            class="conn-card"
            :class="{ 'is-idle': !entry.installed }"
            @click="openCatalogDetail(entry)"
          >
            <div class="conn-card-head">
              <div class="conn-avatar" :class="entry.installed ? 'is-ok' : ''">
                {{ avatarText(entry.name) }}
              </div>
              <div class="conn-ident">
                <div class="conn-name">
                  <span class="conn-name-text">{{ entry.name }}</span>
                  <span class="conn-dot" :class="entry.installed ? 'is-ok' : ''" />
                </div>
                <div class="conn-sub">
                  <span>{{ entry.connector.command }}</span>
                  <span> · {{ t('cli.toolCountLabel', { count: entry.connector.tools?.length || 0 }) }}</span>
                </div>
              </div>
            </div>

            <div class="conn-desc">{{ entry.description }}</div>

            <div class="conn-foot" @click.stop>
              <n-tag v-if="entry.added" size="tiny" type="success" :bordered="false">
                {{ t('cli.catalogAdded') }}
              </n-tag>
              <n-tag v-else-if="!entry.installed" size="tiny" :bordered="false">
                {{ t('cli.catalogNeedsInstall') }}
              </n-tag>
              <span v-else class="conn-state is-on">{{ t('cli.catalogReady') }}</span>

              <n-button size="tiny" type="primary" :disabled="entry.added" @click="openCatalogDetail(entry)">
                {{ entry.added ? t('common.edit') : t('cli.catalogInstall') }}
              </n-button>
            </div>
          </div>
        </div>
        <n-empty v-else :description="t('mcp.noSearchResult')" style="margin-top: 16px;" />
      </template>

      <!-- 详情 -->
      <n-space v-else vertical size="small">
        <n-button text size="small" style="align-self: flex-start;" @click="catalogDetail = null">
          ← {{ t('cli.catalogBack') }}
        </n-button>

        <div class="cli-catalog-head">
          <div class="conn-avatar" :class="catalogDetail.installed ? 'is-ok' : ''">
            {{ avatarText(catalogDetail.name) }}
          </div>
          <div>
            <div class="cli-catalog-title">{{ catalogDetail.name }}</div>
            <div class="cli-catalog-sub">{{ catalogDetail.description }}</div>
          </div>
        </div>

        <!-- 本机状态：装没装、哪个版本、登录好没好。
             ready 为 undefined 表示该条目没有检查项（git/jq 这类没有登录概念）。 -->
        <div class="cli-catalog-status">
          <span class="conn-chip">
            {{ catalogDetail.installed ? (catalogDetail.version || t('cli.catalogInstalled')) : t('cli.catalogNeedsInstall') }}
          </span>
          <span v-if="catalogDetail.ready === true" class="conn-chip is-ok">{{ t('cli.catalogReadyOk') }}</span>
          <span v-else-if="catalogDetail.ready === false" class="conn-chip is-warn">{{ t('cli.catalogNotReady') }}</span>
          <span v-if="catalogDetail.ready_message" class="cli-catalog-note">{{ catalogDetail.ready_message }}</span>
        </div>

        <n-alert v-if="!catalogDetail.installed" type="warning" :bordered="false">
          <div class="cli-install-hint">
            {{ t('cli.catalogNoBinary', { command: catalogDetail.connector.command }) }}
          </div>
          <div v-for="cmd in catalogDetail.install_commands" :key="cmd" class="cli-install-cmd">
            <code>{{ cmd }}</code>
            <n-button text size="tiny" @click="copyText(cmd)">{{ t('cli.catalogCopy') }}</n-button>
          </div>
        </n-alert>

        <!-- 登录引导：gh auth login / aws configure 都是交互式的，
             服务端不能代跑，只给命令 + 复检。 -->
        <n-alert
          v-if="catalogDetail.login_commands?.length && catalogDetail.ready !== true"
          type="info"
          :bordered="false"
        >
          <div class="cli-install-hint">{{ t('cli.catalogLoginHint') }}</div>
          <div v-for="cmd in catalogDetail.login_commands" :key="cmd" class="cli-install-cmd">
            <code>{{ cmd }}</code>
            <n-button text size="tiny" @click="copyText(cmd)">{{ t('cli.catalogCopy') }}</n-button>
          </div>
          <n-button
            text
            size="tiny"
            style="margin-top: 6px;"
            :loading="catalogChecking"
            @click="checkCatalogEntry"
          >
            {{ t('cli.catalogCheckAgain') }}
          </n-button>
        </n-alert>

        <!-- 装完当场自检的结论（不关弹窗，方便直接接着走登录流程） -->
        <n-alert
          v-if="catalogInstallResult"
          :type="catalogInstallResult.ready === false ? 'warning' : 'success'"
          :bordered="false"
        >
          <div class="cli-install-hint">
            {{
              catalogInstallResult.ready === false
                ? t('cli.catalogInstalledNotReady', { name: catalogDetail.name })
                : t('cli.catalogInstalledReady', { name: catalogDetail.name })
            }}
          </div>
          <div v-if="catalogInstallResult.message" class="cli-catalog-note">
            {{ catalogInstallResult.message }}
          </div>
        </n-alert>

        <div v-if="catalogDetail.requires?.length">
          <div class="cli-catalog-label">{{ t('cli.catalogRequirements') }}</div>
          <n-form-item
            v-for="req in catalogDetail.requires"
            :key="req.key"
            :label="req.label"
            :required="req.required"
          >
            <n-input
              v-model:value="catalogEnv[req.key]"
              :type="req.secret ? 'password' : 'text'"
              :placeholder="req.placeholder || req.key"
              show-password-on="click"
            />
            <template v-if="req.help_url" #feedback>
              <a :href="req.help_url" target="_blank" rel="noreferrer" class="cli-catalog-link">
                {{ t('cli.catalogGetToken') }}
              </a>
            </template>
          </n-form-item>
        </div>

        <div>
          <div class="cli-catalog-label">{{ t('cli.catalogTools') }}</div>
          <div v-for="tool in catalogDetail.connector.tools" :key="tool.name" class="cli-catalog-tool">
            <n-text code>{{ catalogDetail.default_name }}_{{ tool.name }}</n-text>
            <span class="cli-catalog-tool-desc">{{ tool.description }}</span>
          </div>
        </div>

        <n-space justify="end" align="center">
          <a
            v-if="catalogDetail.docs_url"
            :href="catalogDetail.docs_url"
            target="_blank"
            rel="noreferrer"
            class="cli-catalog-link"
          >
            {{ t('cli.catalogDocs') }}
          </a>
          <n-button :loading="catalogChecking" @click="checkCatalogEntry">
            {{ t('cli.catalogRecheck') }}
          </n-button>
          <n-button v-if="catalogInstallResult" type="primary" @click="closeCatalog">
            {{ t('common.done') }}
          </n-button>
          <n-button
            v-else
            type="primary"
            :loading="catalogInstalling"
            :disabled="!catalogDetail.installed || catalogDetail.added"
            @click="installCatalogEntry"
          >
            {{ catalogDetail.added ? t('cli.catalogAdded') : t('cli.catalogInstallAndEnable') }}
          </n-button>
        </n-space>
      </n-space>
    </n-modal>

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

        <n-form-item :label="t('cli.commonArgs')">
          <n-input
            v-model:value="form.argsStr"
            type="textarea"
            :rows="2"
            :placeholder="ARGS_EXAMPLE"
          />
        </n-form-item>

        <n-form-item :label="t('cli.description')">
          <n-input v-model:value="form.description" :placeholder="t('cli.descriptionPlaceholder')" />
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

        <n-divider style="margin: 4px 0;">
          {{ t('cli.toolsSection') }}
        </n-divider>

        <div v-for="(tool, idx) in formTools" :key="idx" class="cli-tool-editor">
          <n-space justify="space-between" align="center" style="margin-bottom: 6px;">
            <n-text strong style="font-size: 13px;">{{ t('cli.toolN', { n: idx + 1 }) }}</n-text>
            <n-button text size="tiny" type="error" @click="formTools.splice(idx, 1)">
              {{ t('common.delete') }}
            </n-button>
          </n-space>
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
        </div>

        <n-space size="small">
          <n-button size="small" @click="addTool">
            <template #icon><component :is="AddOutline" /></template>
            {{ t('cli.addTool') }}
          </n-button>
          <n-button size="small" quaternary @click="fillExample">{{ t('cli.fillExample') }}</n-button>
        </n-space>

        <n-alert type="default" :bordered="false" style="margin-top: 8px;">
          <span style="font-size: 12px;">{{ t('cli.templateHint') }}</span>
        </n-alert>
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
import { AddOutline, CreateOutline, EllipsisHorizontalOutline, GridOutline, PlayOutline, RefreshOutline, SearchOutline, SparklesOutline, TrashOutline } from '@vicons/ionicons5'
import {
  checkCLICatalogEntry,
  installCLICatalogEntry,
  listCLICatalog,
  listCLIConnectors,
  parseCLICommand,
  removeCLIConnector,
  saveCLIConnector,
  testCLIConnector,
} from '@/api/cli'
import type { CLICatalogEntry, CLIConnector, CLIConnectorView, CLITestResult } from '@/api/cli'

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

// ---- 应用目录：点卡片 → 检测 → 填凭据 → 一键添加并启用 ----
const showCatalog = ref(false)
const catalogLoading = ref(false)
const catalogInstalling = ref(false)
/** 就绪复检进行中（登录完点「复检」时用，与列表刷新区分开）。 */
const catalogChecking = ref(false)
const catalogEntries = ref<CLICatalogEntry[]>([])
const catalogSearch = ref('')
const catalogCategory = ref('all')
/** 当前打开详情的条目（null = 显示列表）。 */
const catalogDetail = ref<CLICatalogEntry | null>(null)
/** 刚装完的自检结论：null = 还没装。留着结论而不是直接关弹窗，
 *  用户可以顺着登录引导继续走，不用再翻回目录重进一次。 */
const catalogInstallResult = ref<{ ready?: boolean; message?: string } | null>(null)
/** 详情里填的凭据（key 是目录声明的环境变量名）。 */
const catalogEnv = reactive<Record<string, string>>({})

const catalogChips = computed(() => [
  { key: 'all', label: t('common.all') },
  { key: 'dev', label: t('cli.catalogCategoryDev') },
  { key: 'data', label: t('cli.catalogCategoryData') },
  { key: 'cloud', label: t('cli.catalogCategoryCloud') },
])

const visibleCatalog = computed(() => {
  let list = catalogEntries.value
  if (catalogCategory.value !== 'all') {
    list = list.filter((e) => e.category === catalogCategory.value)
  }
  const q = catalogSearch.value.trim().toLowerCase()
  if (q) {
    list = list.filter(
      (e) =>
        e.name.toLowerCase().includes(q)
        || e.description.toLowerCase().includes(q)
        || e.connector.command.toLowerCase().includes(q)
        || (e.tags || []).some((tag) => tag.toLowerCase().includes(q)),
    )
  }
  return list
})

function openCatalog() {
  catalogDetail.value = null
  showCatalog.value = true
  if (!catalogEntries.value.length) void loadCatalog()
}

/**
 * 拉目录并刷新安装状态。详情开着时同步更新那一份——用户去终端装完命令，
 * 回到这里点「重新检测」，按钮要当场变可用，而不是先退回列表再进来。
 */
async function loadCatalog() {
  catalogLoading.value = true
  try {
    const res = await listCLICatalog()
    catalogEntries.value = res.entries
    if (catalogDetail.value) {
      const fresh = res.entries.find((e) => e.id === catalogDetail.value?.id)
      if (fresh) catalogDetail.value = fresh
    }
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally {
    catalogLoading.value = false
  }
}

function openCatalogDetail(entry: CLICatalogEntry) {
  catalogDetail.value = entry
  catalogInstallResult.value = null
  // 凭据输入框每次重新打开都清空：不要把一个应用的 token 带进另一个
  for (const key of Object.keys(catalogEnv)) delete catalogEnv[key]
}

/** 关闭目录弹窗并回到列表视图（装完点「完成」时用）。 */
function closeCatalog() {
  showCatalog.value = false
  catalogDetail.value = null
  catalogInstallResult.value = null
}

/**
 * 就绪复检：跑一次版本探测 + 登录/服务状态检查。
 *
 * 与 loadCatalog 的区别：这里不重拉整个目录，只问这一条——
 * 用户刚在终端 `gh auth login` 完，等的就是这一次确认。
 */
async function checkCatalogEntry() {
  const entry = catalogDetail.value
  if (!entry) return
  catalogChecking.value = true
  try {
    const fresh = await checkCLICatalogEntry(entry.id)
    catalogDetail.value = fresh
    if (catalogInstallResult.value) {
      catalogInstallResult.value = { ready: fresh.ready, message: fresh.ready_message }
    }
    // 同步列表里的那一份，避免退回列表时还显示旧状态
    const idx = catalogEntries.value.findIndex((e) => e.id === fresh.id)
    if (idx >= 0) catalogEntries.value[idx] = fresh
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally {
    catalogChecking.value = false
  }
}

async function installCatalogEntry() {
  const entry = catalogDetail.value
  if (!entry) return
  catalogInstalling.value = true
  try {
    const res = await installCLICatalogEntry(entry.id, entry.default_name, { ...catalogEnv })
    catalogInstallResult.value = { ready: res.ready, message: res.ready_message }
    // 不关弹窗：未就绪时正好接着展示登录命令。
    if (res.ready === false) {
      message.warning(t('cli.catalogInstalledNotReady', { name: entry.name }))
    } else {
      message.success(t('cli.catalogInstalledReady', { name: entry.name }))
    }
    // 安装会改变连接器列表（新卡片出现），先刷新背后的面板。
    await handleRefresh()
    // 再复检一次：装上之后的结论比安装前的探测更有意义。
    await checkCatalogEntry()
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally {
    catalogInstalling.value = false
  }
}

/** 安装命令只做复制：执行安装是用户自己的决定（包管理器会改系统）。 */
async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.success(t('cli.catalogCopied'))
  } catch {
    message.warning(text)
  }
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
    if (formTools.value.length === 1 && !first.name && !first.argsStr && !first.paramsJson) {
      formTools.value = [draft]
    } else {
      formTools.value.push(draft)
    }
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

/* 应用目录：网格比连接器列表窄一点，900px 弹窗里能放三列 */
.cli-catalog-grid {
  grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
}

.cli-catalog-head {
  display: flex;
  align-items: center;
  gap: 12px;
}

.cli-catalog-title {
  font-size: 16px;
  font-weight: 600;
}

.cli-catalog-sub {
  font-size: 12px;
  color: #909399;
}

/* 状态行：版本 / 可用性 chip + 一句话说明（当前 context 或失败原因） */
.cli-catalog-status {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
}

.cli-catalog-status .conn-chip {
  cursor: default;
}

.cli-catalog-note {
  font-size: 12px;
  color: #909399;
  word-break: break-all;
}

.cli-catalog-label {
  font-size: 12px;
  font-weight: 600;
  color: #606266;
  margin: 4px 0 6px;
}

.cli-catalog-tool {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 3px 0;
  flex-wrap: wrap;
}

.cli-catalog-tool-desc {
  font-size: 12px;
  color: #909399;
}

.cli-catalog-link {
  font-size: 12px;
  color: #2080f0;
  text-decoration: none;
}

.cli-catalog-link:hover {
  text-decoration: underline;
}

.cli-install-hint {
  font-size: 12px;
  margin-bottom: 6px;
}

.cli-install-cmd {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-top: 4px;
}

.cli-install-cmd code {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12px;
  background: rgba(0, 0, 0, 0.05);
  border-radius: 4px;
  padding: 2px 6px;
}

.cli-quick .n-input {
  flex: 1;
}

.cli-tool-editor {
  border: 1px solid #ebedf0;
  border-radius: 8px;
  padding: 10px;
  background: #fcfcfd;
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
  .cli-catalog-sub,
  .cli-catalog-tool-desc {
    color: #9a9a9a;
  }

  .cli-catalog-label {
    color: #c4c4c4;
  }

  .cli-catalog-link {
    color: #78a9ff;
  }

  .cli-install-cmd code {
    background: rgba(255, 255, 255, 0.1);
  }

  .cli-tool-editor {
    background: #1f1f1f;
    border-color: #333;
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
