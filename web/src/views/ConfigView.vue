<template>
  <div class="config-page">
    <!-- 页头：标题 + 未保存状态 + 重新加载 -->
    <div class="page-head">
      <div class="page-head-main">
        <div class="page-head-icon">
          <n-icon :component="SettingsOutline" :size="22" />
        </div>
        <div>
          <h2 class="page-head-title">{{ t('config.title') }}</h2>
          <p class="page-head-sub">{{ t('config.subtitle') }}</p>
        </div>
      </div>
      <div class="page-head-actions">
        <div class="live-pill">
          <span class="live-dot" :class="dirtyCount ? 'tone-warn' : 'tone-ok'" />
          <span class="live-label">
            {{ dirtyCount ? t('config.unsavedTitle', { count: dirtyCount }) : t('config.allSaved') }}
          </span>
        </div>
        <n-button secondary :loading="reloading" @click="handleReload">
          <template #icon><n-icon :component="RefreshOutline" /></template>
          {{ t('config.reload') }}
        </n-button>
      </div>
    </div>

    <n-spin :show="configStore.loading">
      <div class="config-grid">
        <!-- 侧边分区导航 -->
        <nav class="config-nav">
          <button
            v-for="s in sectionList"
            :key="s.key"
            type="button"
            class="nav-item"
            :class="{ active: active === s.key }"
            @click="active = s.key"
          >
            <n-icon :component="s.icon" :size="17" class="nav-icon" />
            <span class="nav-label">{{ s.label }}</span>
            <span v-if="isDirty(s.key)" class="nav-dot" />
          </button>
        </nav>

        <!-- 分区内容 -->
        <div class="config-main">
          <div class="section-heading">
            <div class="section-title-lg">{{ currentSection.label }}</div>
            <div class="hint">{{ currentSection.desc }}</div>
          </div>

          <!-- 通用 -->
          <template v-if="active === 'general'">
            <n-card size="small" class="setting-card">
              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.workingDirectory') }}</span>
                  <span class="hint">{{ t('config.workingDirectoryHint') }}</span>
                </div>
                <div class="control">
                  <n-input
                    v-model:value="generalForm.working_dir"
                    :placeholder="t('config.workingDirectory')"
                    style="width: 320px;"
                  />
                  <n-button secondary @click="openDirPicker('working')">
                    <template #icon><n-icon :component="FolderOpenOutline" /></template>
                  </n-button>
                </div>
              </div>

              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.language') }}</span>
                  <span class="hint">{{ t('config.languageHint') }}</span>
                </div>
                <div class="control">
                  <locale-switch />
                </div>
              </div>
            </n-card>
          </template>

          <!-- 智能体 -->
          <template v-if="active === 'agent'">
            <n-card size="small" class="setting-card">
              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.maxTurns') }}</span>
                  <span class="hint">{{ t('config.maxTurnsHint') }}</span>
                </div>
                <div class="control">
                  <n-input-number v-model:value="agentForm.max_turns" :min="1" :max="2000" style="width: 140px;" />
                </div>
              </div>

              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.maxIterations') }}</span>
                  <span class="hint">{{ t('config.maxIterationsHint') }}</span>
                </div>
                <div class="control">
                  <n-input-number v-model:value="agentForm.max_iterations" :min="1" :max="2000" style="width: 140px;" />
                </div>
              </div>

              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.turnTimeout') }}</span>
                  <span class="hint">{{ t('config.turnTimeoutHint') }}</span>
                </div>
                <div class="control">
                  <n-input-number
                    v-model:value="agentForm.turn_timeout_minutes"
                    :min="1"
                    :max="1440"
                    style="width: 140px;"
                  />
                  <span class="unit">{{ t('config.minutes') }}</span>
                </div>
              </div>
            </n-card>

            <n-card size="small" class="setting-card">
              <template #header>
                <div class="section-head">
                  <n-icon :component="HardwareChipOutline" :size="18" class="section-icon" />
                  <div>
                    <div class="section-title">{{ t('config.botMode') }}</div>
                    <div class="hint">{{ t('config.botModeHint') }}</div>
                  </div>
                </div>
              </template>

              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.botModeEnabled') }}</span>
                  <span class="hint">{{ t('config.botModeEnabledHint') }}</span>
                </div>
                <div class="control">
                  <n-switch v-model:value="botModeForm.enabled" />
                </div>
              </div>

              <n-alert v-if="botModeNeedsRestart" type="warning" :bordered="false" class="inline-alert">
                {{ t('config.botModeRestartHint') }}
              </n-alert>

              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.botModeHistoryWindow') }}</span>
                  <span class="hint">{{ t('config.botModeHistoryWindowHint') }}</span>
                </div>
                <div class="control">
                  <n-input-number v-model:value="botModeForm.history_window" :min="20" :max="2000" style="width: 140px;" />
                </div>
              </div>

              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.botModeTurnTimeout') }}</span>
                  <span class="hint">{{ t('config.botModeTurnTimeoutHint') }}</span>
                </div>
                <div class="control">
                  <n-input-number
                    v-model:value="botModeForm.turn_timeout_minutes"
                    :min="1"
                    :max="120"
                    style="width: 140px;"
                  />
                  <span class="unit">{{ t('config.minutes') }}</span>
                </div>
              </div>

              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.botModeInjectProtocol') }}</span>
                  <span class="hint">{{ t('config.botModeInjectProtocolHint') }}</span>
                </div>
                <div class="control">
                  <n-select
                    v-model:value="botModeForm.inject_bot_protocol"
                    :options="injectProtocolOptions"
                    style="width: 160px;"
                  />
                </div>
              </div>

              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.botModeRelayToken') }}</span>
                  <span class="hint">{{ t('config.botModeRelayTokenHint') }}</span>
                </div>
                <div class="control">
                  <n-input
                    v-model:value="botModeForm.relay_token"
                    type="password"
                    show-password-on="click"
                    :placeholder="t('config.botModeRelayTokenPlaceholder')"
                    style="width: 320px;"
                  />
                </div>
              </div>

              <n-alert v-if="!botModeForm.relay_token" type="info" :bordered="false" class="inline-alert">
                {{ t('config.botModeRelayTokenEmptyHint') }}
              </n-alert>
            </n-card>
          </template>

          <!-- 记忆 -->
          <template v-if="active === 'memory'">
            <n-card size="small" class="setting-card">
              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.enableMemory') }}</span>
                  <span class="hint">{{ t('config.enableMemoryHint') }}</span>
                </div>
                <div class="control">
                  <n-switch v-model:value="memoryForm.enabled" />
                </div>
              </div>
            </n-card>

            <n-card size="small" class="setting-card">
              <template #header>
                <div class="section-head">
                  <n-icon :component="BulbOutline" :size="18" class="section-icon" />
                  <div>
                    <div class="section-title">{{ t('config.cortex') }}</div>
                    <div class="hint">{{ t('config.cortexHint') }}</div>
                  </div>
                </div>
              </template>

              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.cortexEnabled') }}</span>
                  <span class="hint">{{ t('config.cortexEnabledHint') }}</span>
                </div>
                <div class="control">
                  <n-switch v-model:value="cortexForm.enabled" />
                </div>
              </div>

              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.cortexSkillPatternFreq') }}</span>
                  <span class="hint">{{ t('config.cortexSkillPatternFreqHint') }}</span>
                </div>
                <div class="control">
                  <n-input-number
                    v-model:value="cortexForm.skill_min_pattern_freq"
                    :min="1"
                    :max="10"
                    style="width: 140px;"
                  />
                </div>
              </div>
            </n-card>
          </template>

          <!-- 服务器 -->
          <template v-if="active === 'server'">
            <n-card size="small" class="setting-card">
              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.uploadUrlPrefix') }}</span>
                  <span class="hint">{{ t('config.uploadUrlPrefixHint') }}</span>
                </div>
                <div class="control">
                  <n-input
                    v-model:value="serverForm.upload_url_prefix"
                    :placeholder="t('config.uploadUrlPrefixPlaceholder')"
                    style="width: 360px;"
                  />
                </div>
              </div>

              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.fileStrategy') }}</span>
                  <span class="hint">{{ t('config.fileStrategyHint') }}</span>
                </div>
                <div class="control">
                  <n-select
                    v-model:value="serverForm.file_strategy"
                    :options="fileStrategyOptions"
                    style="width: 200px;"
                  />
                </div>
              </div>
            </n-card>
          </template>

          <!-- 浏览器（自动化工具用的 Chromium） -->
          <template v-if="active === 'browser'">
            <n-card size="small" class="setting-card">
              <template #header>
                <div class="section-head">
                  <n-icon :component="GlobeOutline" :size="18" class="section-icon" />
                  <div>
                    <div class="section-title">{{ t('config.browserAutomation') }}</div>
                    <div class="hint">{{ t('config.browserAutomationHint') }}</div>
                  </div>
                </div>
              </template>

              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.browserHeadless') }}</span>
                  <span class="hint">{{ t('config.browserHeadlessHint') }}</span>
                </div>
                <div class="control">
                  <n-switch v-model:value="browserForm.headless" />
                </div>
              </div>

              <n-alert v-if="!browserForm.headless" type="warning" :bordered="false" class="inline-alert">
                {{ t('config.browserHeadlessOffWarning') }}
              </n-alert>

              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.browserProfileDir') }}</span>
                  <span class="hint">{{ t('config.browserProfileDirHint') }}</span>
                </div>
                <div class="control">
                  <n-select
                    v-model:value="browserForm.dir_mode"
                    :options="browserProfileModeOptions"
                    style="width: 240px;"
                  />
                </div>
              </div>

              <div v-if="browserForm.dir_mode === 'custom'" class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.browserProfileDirPath') }}</span>
                  <span class="hint">{{ t('config.browserProfileDirPathHint') }}</span>
                </div>
                <div class="control">
                  <n-input
                    v-model:value="browserForm.dir"
                    :placeholder="browserProfileDefaultDir"
                    style="width: 320px;"
                  />
                  <n-button secondary @click="openDirPicker('browser')">
                    <template #icon><n-icon :component="FolderOpenOutline" /></template>
                  </n-button>
                </div>
              </div>

              <n-alert v-if="browserForm.dir_mode === 'temp'" type="info" :bordered="false" class="inline-alert">
                {{ t('config.browserProfileDirTempWarning') }}
              </n-alert>
            </n-card>
          </template>

          <!-- 安全 -->
          <template v-if="active === 'security'">
            <n-card size="small" class="setting-card">
              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.authStatus') }}</span>
                  <span class="hint">{{ t('config.authStatusHint') }}</span>
                </div>
                <div class="control">
                  <n-tag :type="authConfigured ? 'success' : 'warning'" :bordered="false">
                    {{ authConfigured ? t('config.passwordSet') : t('config.notConfigured') }}
                  </n-tag>
                </div>
              </div>

              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.resetPassword') }}</span>
                  <span class="hint">{{ t('config.resetPasswordHint') }}</span>
                </div>
                <div class="control">
                  <n-popconfirm @positive-click="resetPassword">
                    <template #trigger>
                      <n-button type="error" secondary>{{ t('config.resetPassword') }}</n-button>
                    </template>
                    {{ t('config.resetPasswordConfirm') }}
                  </n-popconfirm>
                </div>
              </div>
            </n-card>

            <!-- 审批设置已移至「审批」页面，这里只留入口，避免双入口改出不一致 -->
            <n-card size="small" class="setting-card">
              <template #header>
                <div class="section-head">
                  <n-icon :component="ShieldCheckmarkOutline" :size="18" class="section-icon" />
                  <div>
                    <div class="section-title">{{ t('config.approvalSection') }}</div>
                    <div class="hint">{{ t('config.approvalMovedHint') }}</div>
                  </div>
                </div>
              </template>
              <n-button secondary @click="goApproval">{{ t('config.goApproval') }}</n-button>
            </n-card>
          </template>

          <!-- 隐私脱敏 -->
          <template v-if="active === 'privacy'">
            <n-alert type="info" :bordered="false" class="inline-alert standalone">
              {{ t('config.privacyHint') }}
            </n-alert>

            <n-card size="small" class="setting-card">
              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.privacyEnabled') }}</span>
                  <span class="hint">{{ t('config.privacyEnabledHint') }}</span>
                </div>
                <div class="control">
                  <n-switch v-model:value="privacyForm.enabled" />
                </div>
              </div>

              <div class="sub-title">{{ t('config.piiFields') }}</div>
              <div class="toggle-grid" :class="{ dimmed: !privacyForm.enabled }">
                <div v-for="f in piiFields" :key="f.key" class="toggle-item">
                  <span>{{ f.label }}</span>
                  <n-switch v-model:value="privacyForm[f.key]" :disabled="!privacyForm.enabled" />
                </div>
              </div>

              <div class="row">
                <div class="row-label">
                  <span class="row-title">{{ t('config.secretRedaction') }}</span>
                  <span class="hint">{{ t('config.secretRedactionHint') }}</span>
                </div>
                <div class="control">
                  <n-switch v-model:value="privacyForm.secret_redaction" />
                </div>
              </div>
            </n-card>
          </template>

          <!-- 原始 JSON -->
          <template v-if="active === 'raw'">
            <n-card size="small" class="setting-card">
              <div class="hint standalone">{{ t('config.rawJsonHint') }}</div>
              <n-alert v-if="rawError" type="error" :bordered="false" class="inline-alert">
                {{ rawError }}
              </n-alert>
              <n-input
                v-model:value="rawJson"
                type="textarea"
                :rows="20"
                :placeholder="t('config.rawJson')"
                class="raw-editor"
              />
              <div class="raw-actions">
                <n-button type="primary" :loading="saving" @click="saveRaw">{{ t('common.save') }}</n-button>
                <n-button secondary @click="formatJson">{{ t('common.format') }}</n-button>
                <n-button secondary @click="loadRaw">{{ t('common.refresh') }}</n-button>
              </div>
            </n-card>
          </template>

          <!-- 当前分区的保存/还原（安全、原始 JSON 分区各有自己的操作，不显示） -->
          <div v-if="isSaveable(active)" class="section-actions">
            <span class="hint">
              {{ isDirty(active as SaveKey) ? t('config.unsavedHint') : t('config.allSavedHint') }}
            </span>
            <n-button
              type="primary"
              :disabled="!isDirty(active as SaveKey)"
              :loading="savingKey === active"
              @click="saveSection(active as SaveKey)"
            >
              <template #icon><n-icon :component="SaveOutline" /></template>
              {{ t('config.saveSection') }}
            </n-button>
            <n-button quaternary :disabled="!isDirty(active as SaveKey)" @click="resetSection(active as SaveKey)">
              {{ t('config.reset') }}
            </n-button>
          </div>
        </div>
      </div>
    </n-spin>

    <!-- 目录选择弹窗 -->
    <n-modal
      v-model:show="showDirPicker"
      preset="card"
      class="modal-responsive"
      :title="dirPickerTitle"
      style="width: 560px; max-width: 96vw;"
    >
      <div class="dir-picker">
        <div v-if="recommendedDirs.length" class="dir-recommended">
          <div class="dir-recommended-title">{{ t('chat.workDirRecommended') }}</div>
          <div
            v-for="d in recommendedDirs"
            :key="d"
            class="dir-recommended-item"
            :title="d"
            @click="applyRecommendedDir(d)"
          >
            <n-icon size="16"><FolderOpenOutline /></n-icon>
            <span class="dir-recommended-path">{{ d }}</span>
          </div>
        </div>
        <div class="dir-breadcrumb">
          <n-button size="tiny" quaternary :disabled="!dirParent" @click="navigateDir(dirParent)">
            <template #icon><n-icon><FolderOpenOutline /></n-icon></template>
            ..
          </n-button>
          <n-text class="dir-current" :title="dirCurrentPath">{{ dirCurrentPath }}</n-text>
          <div class="dir-actions">
            <n-input
              v-if="showNewFolderInput"
              v-model:value="newFolderName"
              size="tiny"
              :placeholder="t('chat.newFolderPlaceholder')"
              style="width: 140px;"
              @keyup.enter="createNewFolder"
              @blur="cancelNewFolder"
              ref="newFolderInputRef"
            />
            <n-button v-else size="tiny" quaternary :title="t('chat.newFolder')" @click="startNewFolder">
              <template #icon><n-icon><AddOutline /></n-icon></template>
            </n-button>
          </div>
        </div>
        <div class="dir-list">
          <div v-if="dirLoading" class="dir-loading">
            <n-spin size="small" /> <span style="margin-left: 8px;">{{ t('chat.workDirLoading') }}</span>
          </div>
          <div v-else-if="dirEntries.length === 0" class="dir-empty">
            {{ t('chat.workDirEmpty') }}
          </div>
          <div
            v-for="entry in dirEntries"
            v-else
            :key="entry.path"
            class="dir-item"
            @click="navigateDir(entry.path)"
          >
            <n-icon size="16"><FolderOutline /></n-icon>
            <span>{{ entry.name }}</span>
          </div>
        </div>
      </div>
      <template #footer>
        <n-space>
          <n-button @click="showDirPicker = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="selectWorkDir">{{ dirPickerConfirmLabel }}</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { reactive, ref, computed, onMounted, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { useMessage, useDialog } from 'naive-ui'
import {
  NAlert,
  NButton,
  NCard,
  NIcon,
  NInput,
  NInputNumber,
  NModal,
  NPopconfirm,
  NSelect,
  NSpace,
  NSpin,
  NSwitch,
  NTag,
  NText,
} from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useConfigStore } from '@/stores/config'
import { useAuthStore } from '@/stores/auth'
import { request } from '@/api/client'
import LocaleSwitch from '@/components/LocaleSwitch.vue'
import {
  AddOutline,
  BulbOutline,
  CodeSlashOutline,
  EyeOffOutline,
  FolderOpenOutline,
  FolderOutline,
  GlobeOutline,
  HardwareChipOutline,
  OptionsOutline,
  RefreshOutline,
  SaveOutline,
  ServerOutline,
  SettingsOutline,
  ShieldCheckmarkOutline,
} from '@vicons/ionicons5'
import { listDirs, createDir, listWorkDirHistory } from '@/api/sessions'

const { t } = useI18n()

const message = useMessage()
const dialog = useDialog()
const router = useRouter()
const configStore = useConfigStore()
const authStore = useAuthStore()
const saving = ref(false)
const rawJson = ref('')
const rawError = ref<string | null>(null)
const authConfigured = ref(false)
const reloading = ref(false)

// ---------------------------------------------------------------- 分区导航
const active = ref('general')

const sectionList = computed(() => [
  { key: 'general', label: t('config.general'), desc: t('config.sectionDesc.general'), icon: OptionsOutline },
  { key: 'agent', label: t('config.agent'), desc: t('config.sectionDesc.agent'), icon: HardwareChipOutline },
  { key: 'memory', label: t('config.memory'), desc: t('config.sectionDesc.memory'), icon: BulbOutline },
  { key: 'server', label: t('config.server'), desc: t('config.sectionDesc.server'), icon: ServerOutline },
  { key: 'browser', label: t('config.browser'), desc: t('config.sectionDesc.browser'), icon: GlobeOutline },
  { key: 'security', label: t('config.security'), desc: t('config.sectionDesc.security'), icon: ShieldCheckmarkOutline },
  { key: 'privacy', label: t('config.privacy'), desc: t('config.sectionDesc.privacy'), icon: EyeOffOutline },
  { key: 'raw', label: t('config.rawJson'), desc: t('config.sectionDesc.raw'), icon: CodeSlashOutline },
])

const currentSection = computed(() => sectionList.value.find(s => s.key === active.value) || sectionList.value[0])

// ---------------------------------------------------------------- 表单
const generalForm = reactive({
  working_dir: '',
})

const injectProtocolOptions = computed(() => [
  { label: t('config.botModeInjectDefault'), value: '' },
  { label: t('common.enabled'), value: 'on' },
  { label: t('common.disabled'), value: 'off' },
])

// Bot Mode enabled flag changed since page load -> manager must restart.
const botModeNeedsRestart = computed(
  () => botModeForm.enabled !== botModeEnabledAtLoad
)

const agentForm = reactive({
  // 与后端默认值保持一致（pkg/config/config.go）。150/200 的取值依据：
  // 单回合受回合时限（turn_timeout_minutes，默认 30 分钟）约束，每轮迭代
  // 10~30s，物理可达 60~180 轮，故 300 永远不会触发，150 才是真实生效的
  // 失控循环闸门。
  max_turns: 150,
  max_iterations: 200,
  // 单个回合的执行时限（分钟）。这是"一轮能跑多久"的真正硬约束，与
  // max_turns 是两道独立闸门：任务确实需要更长时间时应该调这项。
  turn_timeout_minutes: 30,
})

// Bot Mode section on the Agent tab (config.bot_mode.*)
const botModeForm = reactive({
  enabled: false,
  history_window: 200,
  // 0 = follow backend default（30 分钟，与 agent.turn_timeout_minutes 同一默认值）；
  // UI 显示的是实际生效值。
  turn_timeout_minutes: 30,
  // tri-state: null/'' = follow default, 'on'/'off' explicit
  inject_bot_protocol: '' as '' | 'on' | 'off',
  // Shared secret remote peers must present on /api/relay/v1/dm. Empty means
  // the relay only accepts calls from localhost.
  relay_token: '',
})
// Track the originally loaded enabled flag to warn when a restart is needed.
let botModeEnabledAtLoad = false

const memoryForm = reactive({
  enabled: true,
})

const cortexForm = reactive({
  enabled: true,
  skill_min_pattern_freq: 3,
})

const serverForm = reactive({
  upload_url_prefix: '',
  file_strategy: 'auto',
})

// 自动化浏览器（工具里那套 Chromium）的两项部署相关设置。
// profile 目录在 pkg/config 里是 *string 三态语义，UI 用「模式」表达：
//   default = 配置里不写这个键（服务端补 ~/.magic/browser-profile）
//   temp    = 显式空串（每次启动全新临时 profile，登录态不保留）
//   custom  = 指定目录（支持 ~ 开头，服务端落盘前会展开成绝对路径）
type BrowserProfileMode = 'default' | 'custom' | 'temp'

// 与 pkg/config.DefaultBrowserProfileDir 保持一致，只用于占位提示
const browserProfileDefaultDir = '~/.magic/browser-profile'

const browserForm = reactive({
  // 无头即 Chrome 的启动参数，改了要重启浏览器才生效：后端 PUT 时若无标签页
  // 会直接关掉，有标签页则留到下次启动（见 internal/server/config.go）。
  headless: true,
  dir_mode: 'default' as BrowserProfileMode,
  dir: '',
})

const browserProfileModeOptions = computed(() => [
  { label: t('config.browserProfileModeDefault'), value: 'default' },
  { label: t('config.browserProfileModeCustom'), value: 'custom' },
  { label: t('config.browserProfileModeTemp'), value: 'temp' },
])

const privacyForm = reactive({
  enabled: true,
  redact_phone: true,
  redact_email: true,
  redact_id_card: true,
  redact_bank_card: true,
  redact_ip: true,
  redact_address: true,
  // 顶层 secret_redaction（凭据脱敏），由「隐私脱敏」页面统一配置
  secret_redaction: false,
})

type PiiKey = 'redact_phone' | 'redact_email' | 'redact_id_card' | 'redact_bank_card' | 'redact_ip' | 'redact_address'

const piiFields = computed<{ key: PiiKey; label: string }[]>(() => [
  { key: 'redact_phone', label: t('config.privacyRedactPhone') },
  { key: 'redact_email', label: t('config.privacyRedactEmail') },
  { key: 'redact_id_card', label: t('config.privacyRedactIDCard') },
  { key: 'redact_bank_card', label: t('config.privacyRedactBankCard') },
  { key: 'redact_ip', label: t('config.privacyRedactIP') },
  { key: 'redact_address', label: t('config.privacyRedactAddress') },
])

const fileStrategyOptions = computed(() => [
  { label: t('config.fileStrategies.auto'), value: 'auto' },
  { label: t('config.fileStrategies.url'), value: 'url' },
  { label: t('config.fileStrategies.base64'), value: 'base64' },
])

// ---------------------------------------------------------------- 未保存检测
type SaveKey = 'general' | 'agent' | 'memory' | 'server' | 'browser' | 'privacy'

const SAVE_KEYS: SaveKey[] = ['general', 'agent', 'memory', 'server', 'browser', 'privacy']

// 各分区的服务端已生效快照；字符串比较即可，避免挨个字段比对
const snapshots = reactive<Record<SaveKey, string>>({
  general: '',
  agent: '',
  memory: '',
  server: '',
  browser: '',
  privacy: '',
})

// 注意：agent 与 bot_mode、memory 与 cortex 存在同名字段（turn_timeout_minutes /
// enabled），快照必须分组存放，否则后者会把前者的值覆盖掉，还原时改错分区。
function snapshotOf(key: SaveKey): string {
  switch (key) {
    case 'general':
      return JSON.stringify(generalForm)
    case 'agent':
      return JSON.stringify({ agent: { ...agentForm }, bot: { ...botModeForm } })
    case 'memory':
      return JSON.stringify({ memory: { ...memoryForm }, cortex: { ...cortexForm } })
    case 'server':
      return JSON.stringify(serverForm)
    case 'browser':
      return JSON.stringify(browserForm)
    case 'privacy':
      return JSON.stringify(privacyForm)
  }
}

function refreshSnapshots(): void {
  for (const key of SAVE_KEYS) {
    snapshots[key] = snapshotOf(key)
  }
}

const dirtyMap = computed<Record<SaveKey, boolean>>(() => ({
  general: snapshots.general !== snapshotOf('general'),
  agent: snapshots.agent !== snapshotOf('agent'),
  memory: snapshots.memory !== snapshotOf('memory'),
  server: snapshots.server !== snapshotOf('server'),
  browser: snapshots.browser !== snapshotOf('browser'),
  privacy: snapshots.privacy !== snapshotOf('privacy'),
}))

const dirtyCount = computed(() => SAVE_KEYS.filter(k => dirtyMap.value[k]).length)

function isSaveable(key: string): boolean {
  return SAVE_KEYS.includes(key as SaveKey)
}

function isDirty(key: string): boolean {
  if (!isSaveable(key)) return false
  return dirtyMap.value[key as SaveKey]
}

const savingKey = ref<SaveKey | null>(null)

// ---------------------------------------------------------------- 载入
function populateFromConfig(cfg: any) {
  generalForm.working_dir = cfg.working_dir || ''

  const agent = cfg.agent || {}
  // 0 means "use built-in default"; show the effective value in UI.
  const maxTurns = Number(agent.max_turns) || 0
  agentForm.max_turns = maxTurns > 0 ? maxTurns : 150
  const maxIterations = Number(agent.max_iterations) || 0
  agentForm.max_iterations = maxIterations > 0 ? maxIterations : 200
  const turnTimeout = Number(agent.turn_timeout_minutes) || 0
  agentForm.turn_timeout_minutes = turnTimeout > 0 ? turnTimeout : 30

  const botMode = cfg.bot_mode || {}
  botModeForm.enabled = botMode.enabled === true
  botModeEnabledAtLoad = botModeForm.enabled
  const hw = Number(botMode.history_window) || 0
  botModeForm.history_window = hw > 0 ? hw : 200
  const tt = Number(botMode.turn_timeout_minutes) || 0
  botModeForm.turn_timeout_minutes = tt > 0 ? tt : 30
  // Backend stores a JSON bool pointer; map to tri-state select value.
  if (botMode.inject_bot_protocol === true) botModeForm.inject_bot_protocol = 'on'
  else if (botMode.inject_bot_protocol === false) botModeForm.inject_bot_protocol = 'off'
  else botModeForm.inject_bot_protocol = ''
  botModeForm.relay_token = typeof botMode.relay_token === 'string' ? botMode.relay_token : ''

  const mem = cfg.memory || {}
  memoryForm.enabled = mem.enabled !== false

  const cortex = cfg.cortex || {}
  cortexForm.enabled = cortex.enabled !== false
  cortexForm.skill_min_pattern_freq = cortex.skill_min_pattern_freq || 3

  const server = cfg.server || {}
  serverForm.upload_url_prefix = server.upload_url_prefix || ''
  serverForm.file_strategy = server.file_strategy || 'auto'

  applyBrowserConfig(cfg)

  const privacy = cfg.privacy || {}
  privacyForm.enabled = privacy.enabled !== false
  privacyForm.redact_phone = privacy.redact_phone !== false
  privacyForm.redact_email = privacy.redact_email !== false
  privacyForm.redact_id_card = privacy.redact_id_card !== false
  privacyForm.redact_bank_card = privacy.redact_bank_card !== false
  privacyForm.redact_ip = privacy.redact_ip !== false
  privacyForm.redact_address = privacy.redact_address !== false
  // 顶层字段：凭据脱敏开关（随「隐私脱敏」页面一起保存）
  privacyForm.secret_redaction = cfg.secret_redaction || false
}

// ---------------------------------------------------------------- 浏览器设置
// 单独抽出来：保存后只需把这一小段和服务端对齐（含 `~` 被展开、null 被删键），
// 不能用整份 populateFromConfig 免得把其它分区的未保存改动冲掉。
function applyBrowserConfig(cfg: any): void {
  const raw = cfg?.browser_profile_dir
  if (typeof raw !== 'string') {
    // 键缺失（老配置 / 从未配过）或 null → 走内置默认目录
    browserForm.dir_mode = 'default'
    browserForm.dir = ''
  } else if (raw === '') {
    // 显式空串 = 每次全新临时 profile，必须和"没配"区分开
    browserForm.dir_mode = 'temp'
    browserForm.dir = ''
  } else {
    browserForm.dir_mode = 'custom'
    browserForm.dir = raw
  }
  // 缺省（键缺失）= 无头；只有显式 false 才是有头
  browserForm.headless = cfg?.browser_headless !== false
}

// ---------------------------------------------------------------- 保存
const saveActions: Record<SaveKey, () => Promise<boolean>> = {
  general: saveGeneral,
  agent: saveAgent,
  memory: saveMemory,
  server: saveServer,
  browser: saveBrowser,
  privacy: savePrivacy,
}

// 只有成功落盘才刷新快照；失败时保留"未保存"状态，避免谎报已保存
async function saveSection(key: SaveKey): Promise<void> {
  savingKey.value = key
  try {
    const ok = await saveActions[key]()
    if (ok) refreshSnapshots()
  } finally {
    savingKey.value = null
  }
}

function resetSection(key: SaveKey): void {
  const saved = JSON.parse(snapshots[key])
  switch (key) {
    case 'general':
      Object.assign(generalForm, saved)
      break
    case 'agent':
      Object.assign(agentForm, saved.agent)
      Object.assign(botModeForm, saved.bot)
      break
    case 'memory':
      Object.assign(memoryForm, saved.memory)
      Object.assign(cortexForm, saved.cortex)
      break
    case 'server':
      Object.assign(serverForm, saved)
      break
    case 'browser':
      Object.assign(browserForm, saved)
      break
    case 'privacy':
      Object.assign(privacyForm, saved)
      break
  }
  message.info(t('config.resetDone'))
}

async function saveGeneral(): Promise<boolean> {
  saving.value = true
  try {
    await configStore.updateConfig({
      working_dir: generalForm.working_dir,
    })
    // Reload to ensure sync with server
    await configStore.loadConfig()
    message.success(t('config.generalSaved'))
    return true
  } catch (e) {
    message.error(t('common.error') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
    return false
  } finally {
    saving.value = false
  }
}

async function saveAgent(): Promise<boolean> {
  saving.value = true
  try {
    await configStore.updateConfig({
      agent: {
        max_turns: agentForm.max_turns,
        max_iterations: agentForm.max_iterations,
        turn_timeout_minutes: agentForm.turn_timeout_minutes,
      },
      bot_mode: {
        enabled: botModeForm.enabled,
        history_window: botModeForm.history_window,
        turn_timeout_minutes: botModeForm.turn_timeout_minutes,
        // '' means "follow default"; send explicit bool otherwise.
        inject_bot_protocol:
          botModeForm.inject_bot_protocol === 'on' ? true :
          botModeForm.inject_bot_protocol === 'off' ? false : undefined,
        // Empty string is meaningful here: it means "only accept relay DMs
        // from localhost", so it must be persisted rather than omitted.
        relay_token: botModeForm.relay_token,
      },
    })
    await configStore.loadConfig()
    message.success(t('config.agentSaved'))
    return true
  } catch (e) {
    message.error(t('common.error') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
    return false
  } finally {
    saving.value = false
  }
}

// 记忆页统一保存：memory + cortex（Cortex AI 已并入记忆页）
async function saveMemory(): Promise<boolean> {
  saving.value = true
  try {
    await configStore.updateConfig({
      memory: { enabled: memoryForm.enabled },
      cortex: {
        enabled: cortexForm.enabled,
        skill_min_pattern_freq: cortexForm.skill_min_pattern_freq,
      },
    })
    await configStore.loadConfig()
    message.success(t('config.memorySaved'))
    return true
  } catch (e) {
    message.error(t('common.error') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
    return false
  } finally {
    saving.value = false
  }
}

async function saveServer(): Promise<boolean> {
  saving.value = true
  try {
    await configStore.updateConfig({
      server: {
        upload_url_prefix: serverForm.upload_url_prefix,
        file_strategy: serverForm.file_strategy,
      }
    })
    await configStore.loadConfig()
    message.success(t('config.serverSaved'))
    return true
  } catch (e) {
    message.error(t('common.error') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
    return false
  } finally {
    saving.value = false
  }
}

async function saveBrowser(): Promise<boolean> {
  const customDir = browserForm.dir.trim()
  if (browserForm.dir_mode === 'custom' && !customDir) {
    message.warning(t('config.browserProfileDirRequired'))
    return false
  }
  saving.value = true
  try {
    await configStore.updateConfig({
      browser_headless: browserForm.headless,
      // default 模式写 null：服务端 unmarshal 成 nil，落盘时 omitempty 把键删掉，
      // 从而回到"未配置=用内置默认目录"的原始语义（含 HOME 缺失时的兜底）。
      // temp 模式必须是空串——空串在这里是有意义的值，不能被当成"没改"。
      browser_profile_dir:
        browserForm.dir_mode === 'default' ? null :
        browserForm.dir_mode === 'temp' ? '' : customDir,
    })
    await configStore.loadConfig()
    if (configStore.config) applyBrowserConfig(configStore.config)
    message.success(t('config.browserSaved'))
    return true
  } catch (e) {
    message.error(t('common.error') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
    return false
  } finally {
    saving.value = false
  }
}

async function savePrivacy(): Promise<boolean> {
  saving.value = true
  try {
    await configStore.updateConfig({
      secret_redaction: privacyForm.secret_redaction,
      privacy: {
        enabled: privacyForm.enabled,
        redact_phone: privacyForm.redact_phone,
        redact_email: privacyForm.redact_email,
        redact_id_card: privacyForm.redact_id_card,
        redact_bank_card: privacyForm.redact_bank_card,
        redact_ip: privacyForm.redact_ip,
        redact_address: privacyForm.redact_address,
      }
    })
    await configStore.loadConfig()
    message.success(t('config.privacySaved'))
    return true
  } catch (e) {
    message.error(t('common.error') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
    return false
  } finally {
    saving.value = false
  }
}

async function resetPassword() {
  try {
    await request('/auth/reset', { method: 'POST' })
    authConfigured.value = false
    authStore.logout()
    message.success(t('config.passwordReset'))
  } catch (e) {
    message.error(t('config.failedToReset'))
  }
}

function goApproval(): void {
  router.push('/approval')
}

// ---------------------------------------------------------------- 原始 JSON
function formatJson(): void {
  try {
    const parsed = JSON.parse(rawJson.value)
    rawJson.value = JSON.stringify(parsed, null, 2)
    rawError.value = null
  } catch (e) {
    rawError.value = t('config.invalidJson') + ': ' + (e instanceof Error ? e.message : 'Parse error')
  }
}

async function saveRaw() {
  saving.value = true
  rawError.value = null
  try {
    JSON.parse(rawJson.value)
  } catch (e) {
    rawError.value = t('config.invalidJson') + ': ' + (e instanceof Error ? e.message : 'Parse error')
    saving.value = false
    return
  }
  try {
    await request('/config', {
      method: 'PUT',
      body: rawJson.value,
    })
    message.success(t('config.rawConfigSaved'))
  } catch (e) {
    message.error(t('common.error') + ': ' + (e instanceof Error ? e.message : 'Unknown error'))
  } finally {
    saving.value = false
  }
}

async function loadRaw() {
  try {
    const res: any = await request('/config')
    rawJson.value = JSON.stringify(res, null, 2)
    rawError.value = null
  } catch (e) {
    rawJson.value = '{}'
  }
}

// ---------------------------------------------------------------- 重新加载
async function doReload(): Promise<void> {
  reloading.value = true
  try {
    await configStore.loadConfig()
    if (configStore.config) {
      populateFromConfig(configStore.config)
    }
    refreshSnapshots()
    await authStore.checkStatus()
    authConfigured.value = authStore.configured
    await loadRaw()
    message.success(t('config.reloaded'))
  } finally {
    reloading.value = false
  }
}

// 有未保存修改时重新加载会丢掉改动，先让用户确认
function handleReload(): void {
  if (dirtyCount.value > 0) {
    dialog.warning({
      title: t('config.reloadConfirmTitle'),
      content: t('config.reloadConfirmContent'),
      positiveText: t('config.reloadDiscard'),
      negativeText: t('common.cancel'),
      onPositiveClick: () => {
        doReload()
      },
    })
    return
  }
  doReload()
}

// ---------------------------------------------------------------- 目录选择
const showDirPicker = ref(false)
const dirCurrentPath = ref('')
const dirEntries = ref<any[]>([])
const dirLoading = ref(false)
const showNewFolderInput = ref(false)
const newFolderName = ref('')
const newFolderInputRef = ref<{ focus: () => void } | null>(null)

// 同一个弹窗复用给「工作目录」和「浏览器 profile 目录」，用 target 决定回填到哪个字段
type DirPickerTarget = 'working' | 'browser'
const dirPickerTarget = ref<DirPickerTarget>('working')

const dirPickerTitle = computed(() =>
  dirPickerTarget.value === 'browser' ? t('config.browserProfileDirPick') : t('chat.workDirSelect')
)
const dirPickerConfirmLabel = computed(() =>
  dirPickerTarget.value === 'browser' ? t('config.browserProfileDirSet') : t('chat.workDirSet')
)

function pickedDirValue(): string {
  return dirPickerTarget.value === 'browser' ? browserForm.dir : generalForm.working_dir
}

function applyPickedDir(path: string): void {
  if (dirPickerTarget.value === 'browser') {
    browserForm.dir_mode = 'custom'
    browserForm.dir = path
  } else {
    generalForm.working_dir = path
  }
}

const dirParent = computed(() => dirEntries.value.find(e => e.name === '..')?.path || '')

async function loadDirs(path?: string) {
  dirLoading.value = true
  try {
    const res = await listDirs(path)
    dirCurrentPath.value = res.current
    dirEntries.value = res.dirs || []
  } catch (e) {
    console.error('Failed to list directories:', e)
    dirEntries.value = []
  } finally {
    dirLoading.value = false
  }
}

function openDirPicker(target: DirPickerTarget = 'working') {
  dirPickerTarget.value = target
  loadDirs(pickedDirValue() || undefined)
  loadWorkDirHistory()
  showDirPicker.value = true
}

// 统一路径分隔符并忽略 Windows 盘符大小写，用于排除当前已填写的工作目录
function normalizeDirPath(p: string): string {
  let s = (p || '').trim().replace(/[\\/]+/g, '\\').replace(/[\\]+$/, '')
  if (/^[A-Za-z]:/.test(s)) s = s.toLowerCase()
  return s
}

const workDirHistory = ref<string[]>([])

// 已使用过的目录作为推荐项；排除当前表单中已填写的工作目录。
// profile 目录与"最近使用的工作目录"无关，选 profile 时不展示推荐。
const recommendedDirs = computed(() => {
  if (dirPickerTarget.value === 'browser') return []
  const current = normalizeDirPath(generalForm.working_dir || '')
  return workDirHistory.value.filter(d => normalizeDirPath(d) !== current)
})

async function loadWorkDirHistory() {
  try {
    workDirHistory.value = await listWorkDirHistory()
  } catch (e) {
    console.error('Failed to load work dir history:', e)
    workDirHistory.value = []
  }
}

// 点击推荐目录直接填入目标字段并关闭弹窗
function applyRecommendedDir(path: string) {
  applyPickedDir(path)
  showDirPicker.value = false
}

function navigateDir(path: string) {
  if (!path) return
  showNewFolderInput.value = false
  newFolderName.value = ''
  loadDirs(path)
}

function startNewFolder() {
  showNewFolderInput.value = true
  newFolderName.value = ''
  nextTick(() => {
    newFolderInputRef.value?.focus()
  })
}

function cancelNewFolder() {
  setTimeout(() => {
    showNewFolderInput.value = false
    newFolderName.value = ''
  }, 150)
}

async function createNewFolder() {
  const name = newFolderName.value.trim()
  if (!name) {
    showNewFolderInput.value = false
    return
  }
  try {
    await createDir(dirCurrentPath.value, name)
    newFolderName.value = ''
    showNewFolderInput.value = false
    loadDirs(dirCurrentPath.value)
  } catch (e: any) {
    message.error(e?.message || t('common.operationFailed'))
  }
}

function selectWorkDir() {
  if (!dirCurrentPath.value) return
  applyPickedDir(dirCurrentPath.value)
  showDirPicker.value = false
}

onMounted(async () => {
  await configStore.loadConfig()
  if (configStore.config) {
    populateFromConfig(configStore.config)
  }
  refreshSnapshots()
  await authStore.checkStatus()
  authConfigured.value = authStore.configured
  await loadRaw()
})
</script>

<style scoped>
.config-page {
  max-width: 1180px;
  margin: 0 auto;
}

/* ---------- 页头 ---------- */
.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 20px;
}

.page-head-main {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}

.page-head-icon {
  width: 42px;
  height: 42px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
  color: #2080f0;
  background: linear-gradient(135deg, #eaf4ff, #dbeafe);
}

.page-head-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  line-height: 1.3;
  color: #111827;
}

.page-head-sub {
  margin: 3px 0 0;
  font-size: 12.5px;
  line-height: 1.5;
  color: #8a8f99;
  max-width: 620px;
}

.page-head-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.live-pill {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 6px 12px;
  border-radius: 999px;
  background: #f7f9fb;
  border: 1px solid #eceff3;
  font-size: 12px;
  color: #6b7280;
  white-space: nowrap;
}

.live-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex: none;
  background: #18a058;
  box-shadow: 0 0 0 3px rgba(24, 160, 88, 0.14);
}

.live-dot.tone-warn {
  background: #f0a020;
  box-shadow: 0 0 0 3px rgba(240, 160, 32, 0.16);
}

/* ---------- 布局 ---------- */
.config-grid {
  display: grid;
  grid-template-columns: 200px minmax(0, 1fr);
  gap: 20px;
  align-items: start;
}

.config-nav {
  position: sticky;
  top: 8px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.nav-item {
  -webkit-appearance: none;
  appearance: none;
  display: flex;
  align-items: center;
  gap: 9px;
  width: 100%;
  padding: 8px 10px;
  border: none;
  border-radius: 8px;
  background: transparent;
  font: inherit;
  font-size: 13.5px;
  color: #5b6573;
  text-align: left;
  cursor: pointer;
  transition: background 0.15s ease, color 0.15s ease;
}

.nav-item:hover {
  background: #f4f6f8;
  color: #111827;
}

.nav-item.active {
  background: #eaf4ff;
  color: #185fa5;
  font-weight: 600;
}

.nav-icon {
  flex: none;
}

.nav-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 该分区有未保存修改 */
.nav-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex: none;
  background: #f0a020;
}

.config-main {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-width: 0;
}

.section-heading {
  margin-bottom: 2px;
}

.section-title-lg {
  font-size: 16px;
  font-weight: 600;
  color: #111827;
  margin-bottom: 3px;
}

/* ---------- 卡片与配置行 ---------- */
.setting-card+.setting-card {
  margin-top: 0;
}

.section-head {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

.section-icon {
  color: #7c8899;
  margin-top: 2px;
  flex: none;
}

.section-title {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  font-weight: 600;
  color: #111827;
  line-height: 1.4;
}

.hint {
  font-size: 12px;
  line-height: 1.5;
  color: #98a2b3;
}

.hint.standalone {
  margin-bottom: 12px;
}

.row {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  flex-wrap: wrap;
  gap: 8px 16px;
  padding: 12px 0;
  border-top: 1px solid #f2f4f7;
}

.row:first-of-type {
  border-top: none;
}

/* 标签保住最小可读宽度；右侧控件（如宽度 100% 的输入框）放不下时换到下一行，
   避免窄屏下标签被挤成一列竖字 */
.row-label {
  display: flex;
  flex-direction: column;
  gap: 3px;
  flex: 1 1 220px;
  min-width: 0;
}

.row-title {
  font-size: 13.5px;
  font-weight: 500;
  color: #111827;
}

.control {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: none;
}

.unit {
  font-size: 12px;
  color: #98a2b3;
}

.inline-alert {
  margin-top: 12px;
  border-radius: 8px;
}

.inline-alert.standalone {
  margin-top: 0;
}

.sub-title {
  font-size: 12px;
  font-weight: 600;
  color: #98a2b3;
  letter-spacing: 0.04em;
  padding: 14px 0 6px;
  border-top: 1px solid #f2f4f7;
}

.toggle-grid {
  display: grid;
  grid-template-columns: repeat(2, minmax(0, 1fr));
  gap: 10px;
  padding-bottom: 4px;
}

.toggle-grid.dimmed {
  opacity: 0.55;
}

.toggle-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 9px 12px;
  border: 1px solid #eaecf0;
  border-radius: 8px;
  font-size: 13px;
  color: #111827;
}

/* ---------- 分区底部操作条 ---------- */
.section-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  position: sticky;
  bottom: 0;
  padding: 12px 16px;
  background: #fff;
  border: 1px solid #eaecf0;
  border-radius: 10px;
  box-shadow: 0 -2px 10px rgba(17, 24, 39, 0.05);
}

.section-actions .hint {
  margin-right: auto;
}

/* ---------- 原始 JSON ---------- */
.raw-editor {
  font-family: ui-monospace, SFMono-Regular, Menlo, monospace;
  font-size: 12.5px;
}

.raw-actions {
  display: flex;
  gap: 8px;
  margin-top: 12px;
}

/* ---------- 目录选择器 ---------- */
.dir-picker {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.dir-breadcrumb {
  display: flex;
  align-items: center;
  gap: 8px;
  padding-bottom: 8px;
  border-bottom: 1px solid var(--n-border-color, #eee);
}

.dir-current {
  font-size: 13px;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: monospace;
}

.dir-actions {
  display: flex;
  align-items: center;
  gap: 4px;
}

.dir-list {
  max-height: 320px;
  overflow-y: auto;
  min-height: 120px;
}

.dir-recommended {
  padding-bottom: 8px;
  border-bottom: 1px solid #f0f0f0;
  margin-bottom: 4px;
}

.dir-recommended-title {
  font-size: 12px;
  color: #999;
  padding: 4px 8px 6px;
}

.dir-recommended-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 12px;
}

.dir-recommended-item:hover {
  background: var(--n-color-hover, #f5f5f5);
}

.dir-recommended-path {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dir-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 13px;
}

.dir-item:hover {
  background: var(--n-color-hover, #f5f5f5);
}

.dir-empty,
.dir-loading {
  display: flex;
  align-items: center;
  padding: 24px;
  color: #999;
  justify-content: center;
}

@media (max-width: 900px) {
  .config-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .config-nav {
    position: static;
    flex-direction: row;
    overflow-x: auto;
    padding-bottom: 4px;
    border-bottom: 1px solid #eaecf0;
  }

  .nav-item {
    width: auto;
    white-space: nowrap;
  }

  .nav-label {
    overflow: visible;
  }

  .toggle-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}

/* 窄屏：配置行改为上下堆叠，控件占满整行 */
@media (max-width: 640px) {
  .row {
    flex-direction: column;
    align-items: stretch;
  }

  .row-label {
    flex-basis: auto;
  }

  .control {
    width: 100%;
    flex-wrap: wrap;
  }
}
</style>
