<template>
  <!-- 目录插件卡：内置目录（MCP 连接器 / CLI 应用）里的每一条都是一个真正的
       Agent Plugin，装上后进"已安装插件"列表，可启用/禁用/卸载/授权。
       两个目录共用本组件，只有详情里的辅助信息不同（MCP 调参数 / CLI 装命令）。 -->
  <div>
    <div class="conn-toolbar">
      <n-input
        v-model:value="query"
        size="small"
        clearable
        :placeholder="t('agentplugins.catalogSearchPlaceholder')"
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

    <n-spin v-if="loading && !packs.length" :size="18" />
    <template v-else>
      <div v-if="shown.length" class="conn-grid">
        <div
          v-for="pack in shown"
          :key="pack.name"
          class="conn-card"
          :class="{ 'is-idle': !pack.installed }"
          @click="openDetail(pack)"
        >
          <div class="conn-card-head">
            <div class="conn-avatar" :class="{ 'is-ok': pack.installed }">
              {{ avatarText(pack.display_name) }}
            </div>
            <div class="conn-ident">
              <div class="conn-name">
                <span class="conn-name-text">{{ pack.display_name }}</span>
                <span v-if="pack.installed" class="conn-dot is-ok" />
              </div>
              <div class="conn-sub">{{ subtitle(pack) }}</div>
            </div>
            <n-tag size="tiny" :bordered="false">
              {{ kind === 'mcp' ? (pack.transport || 'mcp') : 'cli' }}
            </n-tag>
          </div>

          <div class="conn-desc">{{ pack.description }}</div>

          <div class="conn-foot" @click.stop>
            <span class="conn-state" :class="{ 'is-on': pack.installed }">{{ stateText(pack) }}</span>
            <n-button v-if="pack.installed" size="tiny" secondary disabled>
              {{ t('agentplugins.catalogInstalled') }}
            </n-button>
            <n-button
              v-else
              size="tiny"
              type="primary"
              :loading="installingId === pack.id"
              @click="install(pack)"
            >
              {{ t('agentplugins.officialInstall') }}
            </n-button>
          </div>
        </div>
      </div>
      <n-empty v-else :description="t('mcp.noSearchResult')" style="padding: 12px 0;" />

      <div v-if="filtered.length > COLLAPSED && !query.trim() && !category" class="conn-more">
        <n-button text size="tiny" @click="expanded = !expanded">
          {{
            expanded
              ? t('agentplugins.catalogShowLess')
              : t('agentplugins.catalogShowAll', { count: filtered.length })
          }}
        </n-button>
      </div>
    </template>

    <!-- 详情：MCP 可调参数（路径/连接串）；CLI 给安装与登录命令 -->
    <n-modal
      v-model:show="detailShow"
      preset="card"
      :title="detail?.display_name || ''"
      style="width: 620px;"
    >
      <template v-if="detail">
        <div class="cpc-head">
          <div class="conn-avatar" :class="{ 'is-ok': detail.installed }">
            {{ avatarText(detail.display_name) }}
          </div>
          <div style="min-width: 0;">
            <div class="cpc-sub">{{ detail.description }}</div>
            <div class="cpc-tags">
              <span class="conn-chip">{{ kind === 'mcp' ? (detail.transport || 'mcp') : 'cli' }}</span>
              <span v-if="detail.category_label" class="conn-chip">{{ detail.category_label }}</span>
              <span v-if="(detail.required_env || []).length" class="conn-chip is-warn">
                {{ t('agentplugins.catalogNeedCreds', { keys: (detail.required_env || []).join(', ') }) }}
              </span>
            </div>
          </div>
        </div>

        <template v-if="kind === 'cli'">
          <n-alert v-if="!detail.binary_found" type="warning" :bordered="false" style="margin-top: 12px;">
            <div class="cpc-hint">
              {{ t('agentplugins.catalogCliMissing', { binary: detail.binary || '' }) }}
            </div>
            <div v-for="cmd in detail.install_commands || []" :key="cmd" class="cpc-cmd">
              <code>{{ cmd }}</code>
              <n-button text size="tiny" @click="copyText(cmd)">{{ t('cli.catalogCopy') }}</n-button>
            </div>
          </n-alert>

          <n-alert
            v-if="(detail.login_commands || []).length"
            type="info"
            :bordered="false"
            style="margin-top: 10px;"
          >
            <div class="cpc-hint">{{ t('cli.catalogLoginHint') }}</div>
            <div v-for="cmd in detail.login_commands" :key="cmd" class="cpc-cmd">
              <code>{{ cmd }}</code>
              <n-button text size="tiny" @click="copyText(cmd)">{{ t('cli.catalogCopy') }}</n-button>
            </div>
          </n-alert>

          <div v-if="(detail.tools || []).length" style="margin-top: 14px;">
            <div class="cpc-label">{{ t('cli.catalogTools') }}</div>
            <div class="cpc-tools">
              <n-tag v-for="tool in detail.tools" :key="tool" size="small" :bordered="false">
                {{ tool }}
              </n-tag>
            </div>
          </div>
        </template>

        <template v-else>
          <div style="margin-top: 14px;">
            <div class="cpc-label">{{ t('agentplugins.catalogArgs') }}</div>
            <n-input
              v-model:value="argsText"
              type="textarea"
              :rows="4"
              :disabled="detail.installed"
            />
            <div class="cpc-hint" style="margin-top: 4px;">
              {{ t('agentplugins.catalogArgsHint') }}
            </div>
          </div>
        </template>

        <n-space justify="end" align="center" style="margin-top: 16px;">
          <a
            v-if="detail.docs_url"
            :href="detail.docs_url"
            target="_blank"
            rel="noreferrer"
            class="cpc-link"
          >
            {{ t('cli.catalogDocs') }}
          </a>
          <n-button @click="detailShow = false">{{ t('common.cancel') }}</n-button>
          <n-button
            type="primary"
            :disabled="detail.installed"
            :loading="installingId === detail.id"
            @click="installFromDetail"
          >
            {{ detail.installed ? t('agentplugins.catalogInstalled') : t('agentplugins.officialInstall') }}
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { NAlert, NButton, NEmpty, NIcon, NInput, NModal, NSpace, NSpin, NTag } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { RefreshOutline, SearchOutline } from '@vicons/ionicons5'
import { getCatalogPacks, installCatalogPack } from '@/api/agentplugins'
import type { CatalogPack } from '@/api/agentplugins'

const props = defineProps<{ kind: 'mcp' | 'cli' }>()
const emit = defineEmits<{
  /** 安装成功：父组件刷新插件列表，并在需要凭据时打开授权表单。 */
  (e: 'installed', payload: { name: string; requiredEnv: string[] }): void
}>()

const { t } = useI18n()
const message = useMessage()

/** 收起态只铺一行卡，避免几十张卡把插件页淹掉。 */
const COLLAPSED = 6

const loading = ref(false)
const expanded = ref(false)
const packs = ref<CatalogPack[]>([])
const query = ref('')
const category = ref('')
const installingId = ref('')

const categories = computed(() => {
  const seen = new Map<string, string>()
  for (const p of packs.value) {
    if (p.category && !seen.has(p.category)) {
      seen.set(p.category, p.category_label || p.category)
    }
  }
  return Array.from(seen, ([id, label]) => ({ id, label }))
})

const filtered = computed(() => {
  let list = packs.value
  if (category.value) list = list.filter((p) => p.category === category.value)
  const q = query.value.trim().toLowerCase()
  if (q) {
    list = list.filter(
      (p) =>
        p.display_name.toLowerCase().includes(q)
        || p.description.toLowerCase().includes(q)
        || (p.tags || []).some((tag) => tag.toLowerCase().includes(q))
        || (p.binary || '').toLowerCase().includes(q),
    )
  }
  return list
})

const shown = computed(() => {
  // 搜索/筛选时给全量：用户已经在主动找，不该再点一次「显示全部」
  if (expanded.value || query.value.trim() || category.value) return filtered.value
  return filtered.value.slice(0, COLLAPSED)
})

/** 卡片副标题：MCP 给命令行/端点，CLI 给命令名 + 工具数。 */
function subtitle(pack: CatalogPack): string {
  if (props.kind === 'cli') {
    const n = (pack.tools || []).length
    return `${pack.binary || ''} · ${t('cli.toolCountLabel', { count: n })}`
  }
  const args = pack.args || []
  if (pack.transport === 'stdio') return args.length ? args.join(' ') : pack.name
  return pack.transport || ''
}

function stateText(pack: CatalogPack): string {
  if (pack.installed) return t('agentplugins.catalogInstalled')
  if (props.kind === 'cli' && !pack.binary_found) {
    return t('agentplugins.catalogCliMissingShort')
  }
  if ((pack.required_env || []).length) {
    return t('agentplugins.catalogNeedCreds', { keys: (pack.required_env || []).join(', ') })
  }
  return t('agentplugins.catalogNoCreds')
}

async function load() {
  loading.value = true
  try {
    packs.value = await getCatalogPacks(props.kind)
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally {
    loading.value = false
  }
}

// === 详情 ===

const detailShow = ref(false)
const detail = ref<CatalogPack | null>(null)
const argsText = ref('')

function openDetail(pack: CatalogPack) {
  detail.value = pack
  argsText.value = (pack.args || []).join('\n')
  detailShow.value = true
}

// === 安装 ===

async function install(pack: CatalogPack, args?: string[]) {
  installingId.value = pack.id
  try {
    const res = await installCatalogPack(props.kind, pack.id, args)
    message.success(t('agentplugins.catalogInstalledMsg', { name: pack.display_name }))
    detailShow.value = false
    emit('installed', { name: res.name, requiredEnv: res.required_env || [] })
    await load()
  } catch (e: any) {
    message.error(String(e?.message || e))
  } finally {
    installingId.value = ''
  }
}

/** 详情弹窗里安装：MCP 用文本框里的参数覆盖模板，CLI 用模板本身。 */
function installFromDetail() {
  const pack = detail.value
  if (!pack) return
  if (props.kind === 'mcp') {
    const args = argsText.value
      .split('\n')
      .map((s) => s.trim())
      .filter(Boolean)
    void install(pack, args)
    return
  }
  void install(pack)
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

/** 卡片头像：取名称前两位。 */
function avatarText(name: string): string {
  const trimmed = (name || '').trim()
  if (!trimmed) return '?'
  return trimmed.slice(0, 2).toUpperCase()
}

onMounted(() => {
  void load()
})
</script>
