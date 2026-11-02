<template>
  <!-- 目录选择弹窗：Chat / Kanban / Cron / Config / Media 共用。
       样式写在 slot 内容里（scoped 仍生效——slot 由父组件编译，
       带 data-v），宽度等落在卡片 class 上的样式走全局 App.vue。 -->
  <n-modal
    :show="show"
    :title="title"
    preset="card"
    class="modal-responsive"
    style="width: 520px; max-width: 96vw;"
    @update:show="(v: boolean) => emit('update:show', v)"
  >
    <div v-if="recommendedDirs.length" class="dir-picker-recommended">
      <div class="dir-picker-recommended-title">{{ t('dirPicker.recommended') }}</div>
      <div
        v-for="d in recommendedDirs"
        :key="d"
        class="dir-picker-recommended-item"
        :title="d"
        @click="applyRecommendedDir(d)"
      >
        <n-icon size="16"><FolderOutline /></n-icon>
        <span class="dir-picker-recommended-path">{{ d }}</span>
      </div>
    </div>

    <div class="dir-picker-breadcrumb">
      <n-text class="dir-picker-current" :title="dirCurrentPath">{{ dirCurrentPath }}</n-text>
      <n-button size="tiny" quaternary :title="t('dirPicker.newFolder')" @click="startNewFolder">
        <template #icon><n-icon><AddOutline /></n-icon></template>
      </n-button>
    </div>

    <div v-if="showNewFolderInput" class="dir-picker-new-folder">
      <n-input
        v-model:value="newFolderName"
        size="small"
        :placeholder="t('dirPicker.newFolder')"
        @keyup.enter="createNewFolder"
        @blur="cancelNewFolder"
        ref="newFolderInputRef"
      />
    </div>

    <div class="dir-picker-list">
      <div v-if="dirLoading" class="dir-picker-loading">
        <n-spin size="small" />
      </div>
      <div v-else-if="dirEntries.length === 0" class="dir-picker-empty">
        <n-text depth="3">{{ t('dirPicker.empty') }}</n-text>
      </div>
      <div
        v-for="entry in dirEntries"
        v-else
        :key="entry.path"
        class="dir-picker-item"
        @click="navigateDir(entry.path)"
      >
        <n-icon size="16"><FolderOutline /></n-icon>
        <span>{{ entry.name }}</span>
      </div>
    </div>

    <template #footer>
      <n-space justify="end">
        <n-button @click="emit('update:show', false)">{{ t('common.cancel') }}</n-button>
        <n-button type="primary" @click="applyDirCurrent" :disabled="!dirCurrentPath">
          {{ t('dirPicker.select') }}
        </n-button>
      </n-space>
    </template>
  </n-modal>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import { NButton, NIcon, NInput, NModal, NSpace, NSpin, NText } from 'naive-ui'
import { AddOutline, FolderOutline } from '@vicons/ionicons5'

import * as sessionsApi from '@/api/sessions'
import { useConfigStore } from '@/stores/config'

// props / 事件
const props = defineProps<{
  show: boolean
  title?: string
  /** 当前已选目录：作为起点与推荐项过滤依据，可为空 */
  current?: string
}>()
const emit = defineEmits<{
  (e: 'update:show', v: boolean): void
  (e: 'select', path: string): void
}>()

const { t } = useI18n()
const message = useMessage()
const configStore = useConfigStore()

const dirCurrentPath = ref('')
const dirEntries = ref<sessionsApi.DirEntry[]>([])
const dirLoading = ref(false)
const showNewFolderInput = ref(false)
const newFolderName = ref('')
const workDirHistory = ref<string[]>([])
const newFolderInputRef = ref<{ focus: () => void } | null>(null)

// 配置页「通用」中设置的全局工作目录（后端 cfg.WorkingDir），作为默认起点
const globalWorkDir = computed(() => configStore.config?.working_dir || '')

// 配置按需拉取（首次打开选择器时），避免页面每次进入都多一个请求。
async function ensureConfigLoaded(): Promise<void> {
  if (configStore.config) return
  try {
    await configStore.loadConfig()
  } catch {
    // 拉取失败不影响选择器：起点退回后端默认
  }
}

function normalizeDirPath(p: string): string {
  let s = (p || '').trim().replace(/[\\/]+/g, '\\').replace(/[\\]+$/, '')
  if (/^[A-Za-z]:/.test(s)) s = s.toLowerCase()
  return s
}

// 已使用过的目录作为推荐项；排除当前已选中的目录
const recommendedDirs = computed(() => {
  const current = normalizeDirPath(props.current || '')
  return workDirHistory.value.filter((d) => normalizeDirPath(d) !== current)
})

async function loadWorkDirHistory(): Promise<void> {
  try {
    workDirHistory.value = await sessionsApi.listWorkDirHistory()
  } catch {
    workDirHistory.value = []
  }
}

async function loadDirs(path?: string): Promise<boolean> {
  dirLoading.value = true
  try {
    const res = await sessionsApi.listDirs(path)
    dirCurrentPath.value = res.current
    dirEntries.value = res.dirs || []
    return true
  } catch {
    dirEntries.value = []
    return false
  } finally {
    dirLoading.value = false
  }
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
    await sessionsApi.createDir(dirCurrentPath.value, name)
    newFolderName.value = ''
    showNewFolderInput.value = false
    loadDirs(dirCurrentPath.value)
  } catch (e: unknown) {
    const msg = e instanceof Error ? e.message : String(e)
    message.error(msg || t('common.operationFailed'))
  }
}

// 打开时定位起点：当前已选目录 > 全局工作目录 > 后端默认
watch(
  () => props.show,
  async (open) => {
    if (!open) return
    await Promise.all([loadWorkDirHistory(), ensureConfigLoaded()])
    const prefer = (props.current || '').trim() || globalWorkDir.value
    if (prefer && (await loadDirs(prefer))) return
    await loadDirs(undefined)
  },
  { immediate: true },
)

function applyRecommendedDir(path: string) {
  emit('select', path)
  emit('update:show', false)
}

function applyDirCurrent() {
  if (!dirCurrentPath.value) return
  emit('select', dirCurrentPath.value)
  emit('update:show', false)
}
</script>

<style scoped>
/* ===== 目录选择器（与 chat/cron/kanban/config 各页同款样式） ===== */
.dir-picker-recommended {
  padding: 4px 0 8px;
  border-bottom: 1px solid #f0f0f0;
  margin-bottom: 4px;
}

.dir-picker-recommended-title {
  font-size: 12px;
  color: #999;
  padding: 4px 12px 6px;
}

.dir-picker-recommended-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 12px;
  transition: background 0.15s;
}

.dir-picker-recommended-item:hover {
  background: #f0f0f0;
}

.dir-picker-recommended-path {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dir-picker-breadcrumb {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 0;
  border-bottom: 1px solid #f0f0f0;
}

.dir-picker-current {
  font-size: 12px;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: monospace;
  color: #666;
}

.dir-picker-new-folder {
  padding: 8px 0;
  border-bottom: 1px solid #f0f0f0;
}

.dir-picker-list {
  max-height: 300px;
  overflow-y: auto;
  padding: 8px 0;
}

.dir-picker-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 13px;
  transition: background 0.15s;
}

.dir-picker-item:hover {
  background: #f0f0f0;
}

.dir-picker-empty,
.dir-picker-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}
</style>
