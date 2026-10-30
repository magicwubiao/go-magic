<template>
  <!-- CLI 应用目录：按插件卡铺开。本机没装命令的条目仍要点开看安装方式
       （服务端不代跑包管理器），其余可一键添加并启用。 -->
  <div>
    <div class="conn-toolbar">
      <n-input
        v-model:value="query"
        size="small"
        clearable
        :placeholder="t('cli.catalogSearch')"
        class="conn-search"
      >
        <template #prefix>
          <n-icon size="14"><component :is="SearchOutline" /></n-icon>
        </template>
      </n-input>

      <div class="conn-chips">
        <button
          v-for="chip in chips"
          :key="chip.key"
          type="button"
          class="conn-chip"
          :class="{ 'is-active': category === chip.key }"
          @click="category = chip.key"
        >
          {{ chip.label }}
        </button>
      </div>

      <n-space :size="8" class="conn-toolbar-actions">
        <n-button size="small" :loading="loading" @click="load">
          <template #icon><component :is="RefreshOutline" /></template>
          {{ t('common.refresh') }}
        </n-button>
      </n-space>
    </div>

    <n-spin v-if="loading && !entries.length" :size="18" />
    <template v-else>
      <div v-if="filtered.length" class="conn-grid">
        <div
          v-for="entry in filtered"
          :key="entry.id"
          class="conn-card"
          :class="{ 'is-idle': !entry.installed }"
          @click="openDetail(entry)"
        >
          <div class="conn-card-head">
            <div class="conn-avatar" :class="{ 'is-ok': entry.added }">
              {{ avatarText(entry.name) }}
            </div>
            <div class="conn-ident">
              <div class="conn-name">
                <span class="conn-name-text">{{ entry.name }}</span>
                <span v-if="entry.added" class="conn-dot is-ok" />
              </div>
              <div class="conn-sub">
                {{ entry.connector.command }} ·
                {{ t('cli.toolCountLabel', { count: entry.connector.tools?.length || 0 }) }}
              </div>
            </div>
          </div>

          <div class="conn-desc">{{ entry.description }}</div>

          <div class="conn-foot" @click.stop>
            <span class="conn-state" :class="{ 'is-on': entry.added }">
              {{ stateText(entry) }}
            </span>

            <n-button v-if="entry.added" size="tiny" secondary @click="openMCPPage">
              {{ t('agentplugins.catalogManage') }}
            </n-button>
            <n-button
              v-else-if="!entry.installed || (entry.requires || []).length"
              size="tiny"
              type="primary"
              @click="openDetail(entry)"
            >
              {{ entry.installed ? t('agentplugins.officialInstall') : t('agentplugins.catalogViewInstall') }}
            </n-button>
            <n-button
              v-else
              size="tiny"
              type="primary"
              :loading="installingId === entry.id"
              @click="installDirect(entry)"
            >
              {{ t('agentplugins.officialInstall') }}
            </n-button>
          </div>
        </div>
      </div>
      <n-empty v-else :description="t('mcp.noSearchResult')" style="padding: 12px 0;" />
    </template>

    <!-- 详情：安装方式 / 登录引导 / 凭据 / 工具清单 -->
    <n-modal
      v-model:show="detailShow"
      preset="card"
      :title="detail?.name || t('cli.catalogTitle')"
      style="width: 680px;"
    >
      <template v-if="detail">
        <div class="cli-catalog-head">
          <div class="conn-avatar" :class="{ 'is-ok': detail.installed }">
            {{ avatarText(detail.name) }}
          </div>
          <div>
            <div class="cli-catalog-sub">{{ detail.description }}</div>
            <div class="cli-catalog-status" style="margin-top: 6px;">
              <span class="conn-chip">
                {{
                  detail.installed
                    ? detail.version || t('cli.catalogInstalled')
                    : t('cli.catalogNeedsInstall')
                }}
              </span>
              <span v-if="detail.ready === true" class="conn-chip is-ok">
                {{ t('cli.catalogReadyOk') }}
              </span>
              <span v-else-if="detail.ready === false" class="conn-chip is-warn">
                {{ t('cli.catalogNotReady') }}
              </span>
            </div>
          </div>
        </div>

        <n-alert v-if="!detail.installed" type="warning" :bordered="false" style="margin-top: 10px;">
          <div class="cli-install-hint">
            {{ t('cli.catalogNoBinary', { command: detail.connector.command }) }}
          </div>
          <div v-for="cmd in detail.install_commands" :key="cmd" class="cli-install-cmd">
            <code>{{ cmd }}</code>
            <n-button text size="tiny" @click="copyText(cmd)">{{ t('cli.catalogCopy') }}</n-button>
          </div>
        </n-alert>

        <n-alert
          v-if="detail.login_commands?.length && detail.ready !== true"
          type="info"
          :bordered="false"
          style="margin-top: 10px;"
        >
          <div class="cli-install-hint">{{ t('cli.catalogLoginHint') }}</div>
          <div v-for="cmd in detail.login_commands" :key="cmd" class="cli-install-cmd">
            <code>{{ cmd }}</code>
            <n-button text size="tiny" @click="copyText(cmd)">{{ t('cli.catalogCopy') }}</n-button>
          </div>
          <n-button text size="tiny" style="margin-top: 6px;" :loading="checking" @click="check">
            {{ t('cli.catalogCheckAgain') }}
          </n-button>
        </n-alert>

        <n-alert
          v-if="installResult"
          :type="installResult.ready === false ? 'warning' : 'success'"
          :bordered="false"
          style="margin-top: 10px;"
        >
          <div class="cli-install-hint">
            {{
              installResult.ready === false
                ? t('cli.catalogInstalledNotReady', { name: detail.name })
                : t('cli.catalogInstalledReady', { name: detail.name })
            }}
          </div>
        </n-alert>

        <n-alert v-if="plaintextBlocked" type="warning" :bordered="false" style="margin-top: 10px;">
          <div class="cli-install-hint">{{ t('cli.catalogPlaintextHint') }}</div>
          <n-button size="tiny" style="margin-top: 6px;" :loading="installing" @click="install(true)">
            {{ t('cli.catalogPlaintextForce') }}
          </n-button>
        </n-alert>

        <div v-if="detail.requires?.length" style="margin-top: 12px;">
          <div class="cli-catalog-label">{{ t('cli.catalogRequirements') }}</div>
          <n-form-item v-for="req in detail.requires" :key="req.key" :label="req.label" :required="req.required">
            <n-input
              v-model:value="envValues[req.key]"
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

        <div style="margin-top: 12px;">
          <div class="cli-catalog-label">{{ t('cli.catalogTools') }}</div>
          <div v-for="tool in detail.connector.tools" :key="tool.name" class="cli-catalog-tool">
            <n-text code>{{ detail.default_name }}_{{ tool.name }}</n-text>
            <span class="cli-catalog-tool-desc">{{ tool.description }}</span>
          </div>
        </div>

        <n-space justify="end" align="center" style="margin-top: 14px;">
          <a
            v-if="detail.docs_url"
            :href="detail.docs_url"
            target="_blank"
            rel="noreferrer"
            class="cli-catalog-link"
          >
            {{ t('cli.catalogDocs') }}
          </a>
          <n-button :loading="checking" @click="check">{{ t('cli.catalogRecheck') }}</n-button>
          <n-button v-if="installResult" type="primary" @click="detailShow = false">
            {{ t('common.done') }}
          </n-button>
          <n-button
            v-else
            type="primary"
            :loading="installing"
            :disabled="!detail.installed"
            @click="install(false)"
          >
            {{ t('cli.catalogInstallAndEnable') }}
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { NAlert, NButton, NEmpty, NFormItem, NIcon, NInput, NModal, NSpace, NSpin, NText } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useRouter } from 'vue-router'
import { RefreshOutline, SearchOutline } from '@vicons/ionicons5'
import { listCLICatalog, checkCLICatalogEntry, installCLICatalogEntry } from '@/api/cli'
import type { CLICatalogEntry } from '@/api/cli'

const { t } = useI18n()
const message = useMessage()
const router = useRouter()

const loading = ref(false)
const entries = ref<CLICatalogEntry[]>([])
const query = ref('')
const category = ref('all')
const installingId = ref('')

const chips = computed(() => [
  { key: 'all', label: t('common.all') },
  { key: 'dev', label: t('cli.catalogCategoryDev') },
  { key: 'data', label: t('cli.catalogCategoryData') },
  { key: 'cloud', label: t('cli.catalogCategoryCloud') },
])

const filtered = computed(() => {
  let list = entries.value
  if (category.value !== 'all') list = list.filter((e) => e.category === category.value)
  const q = query.value.trim().toLowerCase()
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

function stateText(entry: CLICatalogEntry): string {
  if (entry.added) return t('cli.catalogAdded')
  if (!entry.installed) return t('cli.catalogNeedsInstall')
  if (entry.ready === true) return t('cli.catalogReadyOk')
  if (entry.ready === false) return t('cli.catalogNotReady')
  return t('cli.catalogReady')
}

async function load() {
  loading.value = true
  try {
    const res = await listCLICatalog()
    entries.value = res.entries
    if (detail.value) {
      const fresh = res.entries.find((e) => e.id === detail.value?.id)
      if (fresh) detail.value = fresh
    }
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally {
    loading.value = false
  }
}

// === 详情 / 安装 ===

const detailShow = ref(false)
const detail = ref<CLICatalogEntry | null>(null)
const installResult = ref<{ ready?: boolean; message?: string } | null>(null)
const plaintextBlocked = ref(false)
const installing = ref(false)
const checking = ref(false)
const envValues = reactive<Record<string, string>>({})

function openDetail(entry: CLICatalogEntry) {
  detail.value = entry
  detailShow.value = true
  installResult.value = null
  plaintextBlocked.value = false
  // 凭据每次重开都清空：别把一个应用的 token 带进另一个
  for (const key of Object.keys(envValues)) delete envValues[key]
}

async function check() {
  const entry = detail.value
  if (!entry) return
  checking.value = true
  try {
    const fresh = await checkCLICatalogEntry(entry.id)
    detail.value = fresh
    if (installResult.value) {
      installResult.value = { ready: fresh.ready, message: fresh.ready_message }
    }
    const idx = entries.value.findIndex((e) => e.id === fresh.id)
    if (idx >= 0) entries.value[idx] = fresh
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally {
    checking.value = false
  }
}

async function install(forcePlaintext = false) {
  const entry = detail.value
  if (!entry) return
  installing.value = true
  try {
    const res = await installCLICatalogEntry(entry.id, entry.default_name, { ...envValues }, forcePlaintext)
    plaintextBlocked.value = false
    installResult.value = { ready: res.ready, message: res.ready_message }
    if (res.ready === false) {
      message.warning(t('cli.catalogInstalledNotReady', { name: entry.name }))
    } else {
      message.success(t('cli.catalogInstalledReady', { name: entry.name }))
    }
    await check()
  } catch (e: any) {
    const msg = String(e?.message || e)
    if (/plaintext credential/i.test(msg)) plaintextBlocked.value = true
    message.error(msg)
  } finally {
    installing.value = false
  }
}

/** 卡片直装：命令已在 PATH 且不需要凭据时一步到位。 */
async function installDirect(entry: CLICatalogEntry) {
  installingId.value = entry.id
  try {
    const res = await installCLICatalogEntry(entry.id, entry.default_name, {}, false)
    if (res.ready === false) {
      message.warning(t('cli.catalogInstalledNotReady', { name: entry.name }))
    } else {
      message.success(t('cli.catalogInstalledReady', { name: entry.name }))
    }
    await load()
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally {
    installingId.value = ''
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

function openMCPPage() {
  void router.push('/mcp')
}

function avatarText(name: string): string {
  const trimmed = (name || '').trim()
  if (!trimmed) return '?'
  return trimmed.slice(0, 2).toUpperCase()
}

onMounted(() => {
  void load()
})
</script>
