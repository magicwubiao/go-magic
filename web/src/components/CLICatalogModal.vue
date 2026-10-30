<template>
  <!-- CLI 应用目录弹窗：从 CLIConnectorsPanel 抽出，供连接器页与插件页共用。
       列表与详情在同一个弹窗里切换（详情带返回），比叠两层弹窗稳。 -->
  <n-modal
    :show="show"
    preset="card"
    :title="t('cli.catalogTitle')"
    style="width: 900px;"
    @update:show="(v: boolean) => emit('update:show', v)"
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
        <!-- ready 只证明"最后一次 Check 通过"：CLI 自己的 auth status 未必校验
             token 有效性，过期往往要等调用 401 才发现。 -->
        <span v-if="catalogDetail.ready === true" class="cli-catalog-note">
          {{ t('cli.catalogReadyCaveat') }}
        </span>
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

      <!-- 明文凭据被后端拦下：就地说明 + 逃生口，不逼用户重开弹窗。 -->
      <n-alert v-if="catalogPlaintextBlocked" type="warning" :bordered="false">
        <div class="cli-install-hint">{{ t('cli.catalogPlaintextHint') }}</div>
        <n-button
          size="tiny"
          style="margin-top: 6px;"
          :loading="catalogInstalling"
          @click="installCatalogEntry(true)"
        >
          {{ t('cli.catalogPlaintextForce') }}
        </n-button>
      </n-alert>

      <div v-if="catalogDetail.requires?.length">
        <div class="cli-catalog-label">{{ t('cli.catalogRequirements') }}</div>
        <div v-for="req in catalogDetail.requires" :key="req.key">
          <n-form-item :label="req.label" :required="req.required">
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
          <!-- 引用形态里的 ${} 会被 vue-i18n 当命名插值，所以由调用方拼进参数。 -->
          <div v-if="req.secret" class="cli-catalog-note">
            {{ t('cli.catalogSecretHint', { ref: '${' + req.key + '}' }) }}
          </div>
        </div>
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
          @click="installCatalogEntry(false)"
        >
          {{ catalogDetail.added ? t('cli.catalogAdded') : t('cli.catalogInstallAndEnable') }}
        </n-button>
      </n-space>
    </n-space>
  </n-modal>
</template>

<script setup lang="ts">
import { ref, reactive, computed, watch } from 'vue'
import { useMessage } from 'naive-ui'
import { NAlert, NButton, NEmpty, NFormItem, NInput, NModal, NSpace, NTag, NText } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { RefreshOutline, SearchOutline } from '@vicons/ionicons5'
import { listCLICatalog, checkCLICatalogEntry, installCLICatalogEntry } from '@/api/cli'
import type { CLICatalogEntry } from '@/api/cli'

const props = defineProps<{ show: boolean }>()
const emit = defineEmits<{
  (e: 'update:show', v: boolean): void
  /** 安装成功（连接器列表已变化），父组件据此刷新自己的列表。 */
  (e: 'installed', name: string): void
}>()

const { t } = useI18n()
const message = useMessage()

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
/** 后端拦下明文凭据时置位：显示就地说明 + 「仍要明文保存」逃生口。 */
const catalogPlaintextBlocked = ref(false)
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

// 弹窗打开时懒加载目录（首次才拉，之后吃缓存 + 手动刷新）。
watch(
  () => props.show,
  (v) => {
    if (v && !catalogEntries.value.length) void loadCatalog()
  },
)

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
  catalogPlaintextBlocked.value = false
  // 凭据输入框每次重新打开都清空：不要把一个应用的 token 带进另一个
  for (const key of Object.keys(catalogEnv)) delete catalogEnv[key]
}

/** 关闭目录弹窗并回到列表视图（装完点「完成」时用）。 */
function closeCatalog() {
  emit('update:show', false)
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

async function installCatalogEntry(forcePlaintext = false) {
  const entry = catalogDetail.value
  if (!entry) return
  catalogInstalling.value = true
  try {
    const res = await installCLICatalogEntry(entry.id, entry.default_name, { ...catalogEnv }, forcePlaintext)
    catalogPlaintextBlocked.value = false
    catalogInstallResult.value = { ready: res.ready, message: res.ready_message }
    // 不关弹窗：未就绪时正好接着展示登录命令。
    if (res.ready === false) {
      message.warning(t('cli.catalogInstalledNotReady', { name: entry.name }))
    } else {
      message.success(t('cli.catalogInstalledReady', { name: entry.name }))
    }
    // 通知父组件刷新背后的连接器列表。
    emit('installed', res.name)
    // 再复检一次：装上之后的结论比安装前的探测更有意义。
    await checkCatalogEntry()
  } catch (e: any) {
    const msg = String(e?.message || e)
    if (/plaintext credential/i.test(msg)) {
      // 后端拦下明文凭据：只靠 toast 用户会丢上下文，就地常驻说明 + 逃生口。
      catalogPlaintextBlocked.value = true
    }
    message.error(msg)
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

/** 卡片头像：取名称前两位，与 MCP 卡片同款。 */
function avatarText(name: string): string {
  const trimmed = (name || '').trim()
  if (!trimmed) return '?'
  return trimmed.slice(0, 2).toUpperCase()
}
</script>

<!-- 弹窗 teleport 到 body，样式按项目约定放全局（不 scoped）；
     conn-* 基础卡片样式在 styles/connectors.css（main.ts 全局引入）。 -->
<style>
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

@media (prefers-color-scheme: dark) {
  .cli-catalog-sub,
  .cli-catalog-tool-desc,
  .cli-catalog-note {
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
}
</style>
