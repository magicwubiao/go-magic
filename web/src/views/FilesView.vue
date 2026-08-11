<template>
  <div class="files-container">
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>{{ t('files.title') }}</h2>
    </n-space>

    <n-tabs type="line" animated v-model:value="activeTab">
      <!-- Uploads Tab -->
      <n-tab-pane name="uploads" :tab="t('files.uploadsTab')">
        <n-space justify="space-between" style="margin-bottom: 16px;">
          <n-space>
            <n-upload
              :show-file-list="false"
              :multiple="true"
              @before-upload="handleUpload"
            >
              <n-button type="primary" size="small">
                <template #icon>
                  <n-icon><CloudUploadOutline /></n-icon>
                </template>
                {{ t('files.upload') }}
              </n-button>
            </n-upload>
          </n-space>
          <n-button size="small" @click="loadFiles" :loading="loading">
            <template #icon>
              <n-icon><RefreshOutline /></n-icon>
            </template>
          </n-button>
        </n-space>

        <n-spin :show="loading">
          <n-card :title="t('files.overview')" style="margin-bottom: 24px;" size="small">
            <n-grid :cols="3" :x-gap="16">
              <n-grid-item>
                <n-statistic :label="t('files.totalFiles')" :value="files.length">
                  <template #prefix>
                    <n-icon size="20" color="#18a058"><FolderOpenOutline /></n-icon>
                  </template>
                </n-statistic>
              </n-grid-item>
              <n-grid-item>
                <n-statistic :label="t('files.totalSize')" :value="formatSize(totalSize)">
                  <template #prefix>
                    <n-icon size="20" color="#2080f0"><SaveOutline /></n-icon>
                  </template>
                </n-statistic>
              </n-grid-item>
              <n-grid-item>
                <n-statistic :label="t('files.storagePath')" value="~/.magic">
                  <template #prefix>
                    <n-icon size="20" color="#f0a020"><FolderOutline /></n-icon>
                  </template>
                </n-statistic>
              </n-grid-item>
            </n-grid>
          </n-card>

          <n-card :title="t('files.fileList')" size="small">
            <n-data-table
              :columns="uploadColumns"
              :data="files"
              :loading="loading"
              :pagination="pagination"
              size="small"
              bordered
              striped
            />
            <n-empty v-if="!loading && files.length === 0" :description="t('files.empty')" style="margin-top: 24px;" />
          </n-card>
        </n-spin>
      </n-tab-pane>

      <!-- Workspace Tab -->
      <n-tab-pane name="workspace" :tab="t('files.workspaceTab')">
        <n-card size="small" style="margin-bottom: 12px;">
          <n-space justify="space-between" align="center">
            <n-space align="center">
              <n-text strong :depth="3" style="font-size: 13px;">{{ t('files.session') }}:</n-text>
              <n-select
                v-model:value="selectedSessionId"
                :options="sessionOptions"
                size="small"
                filterable
                :placeholder="t('files.selectSession')"
                style="width: 300px;"
                @update:value="onSessionChange"
              />
            </n-space>
            <n-space>
              <n-button size="small" @click="zipWorkspace" :disabled="!selectedSessionId">
                <template #icon><n-icon><ArchiveOutline /></n-icon></template>
                {{ t('files.zipDownload') }}
              </n-button>
              <n-button size="small" @click="createNewFolder" :disabled="!selectedSessionId">
                <template #icon><n-icon><AddOutline /></n-icon></template>
                {{ t('files.newFolder') }}
              </n-button>
              <n-button size="small" @click="loadWorkspace()" :loading="wsLoading">
                <template #icon><n-icon><RefreshOutline /></n-icon></template>
              </n-button>
            </n-space>
          </n-space>
        </n-card>

        <n-card size="small" style="margin-bottom: 12px;" v-if="selectedSessionId">
          <n-space align="center" size="small" wrap>
            <n-button v-if="showBackButton" size="tiny" text @click="goBack">
              <template #icon><n-icon><FolderOutline /></n-icon></template>
              ..
            </n-button>
            <n-text code style="font-size: 13px;">{{ currentRelativePath || '/' }}</n-text>
          </n-space>
        </n-card>

        <n-spin :show="wsLoading">
          <div class="workspace-table-container">
            <n-data-table
              :columns="workspaceColumns"
              :data="wsEntries"
              :loading="wsLoading"
              size="small"
              bordered
              striped
            />
          </div>
          <n-empty v-if="!wsLoading && selectedSessionId && wsEntries.length === 0" :description="t('files.workspaceEmpty')" style="margin-top: 24px;" />
          <n-empty v-if="!wsLoading && !selectedSessionId" :description="t('files.selectSession')" style="margin-top: 24px;" />
        </n-spin>
      </n-tab-pane>
    </n-tabs>

    <!-- File preview/editor modal -->
    <n-modal v-model:show="showPreview" preset="card" :title="previewTitle" style="max-width: 950px; width: 95vw;">
      <n-scrollbar style="max-height: 70vh;">
        <div v-if="previewType === 'image'" class="image-preview-wrapper">
          <img :src="previewImageUrl" :alt="previewTitle" style="max-width: 100%; max-height: 65vh;" />
        </div>
        <div v-else-if="previewType === 'text' && isEditing">
          <n-input
            v-model:value="editContent"
            type="textarea"
            :autosize="{ minRows: 20, maxRows: 40 }"
            style="width: 100%; font-family: monospace; font-size: 13px;"
          />
        </div>
        <pre v-else-if="previewContent" class="file-preview-content">{{ previewContent }}</pre>
        <n-empty v-else :description="t('files.noPreview')" />
      </n-scrollbar>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showPreview = false">{{ t('common.close') }}</n-button>
          <template v-if="previewType === 'text' && !isImageFile(previewTitle)">
            <n-button v-if="!isEditing" @click="startEdit">{{ t('common.edit') }}</n-button>
            <template v-else>
              <n-button @click="cancelEdit">{{ t('common.cancel') }}</n-button>
              <n-button type="primary" @click="saveEdit">{{ t('common.save') }}</n-button>
            </template>
          </template>
          <n-button type="primary" @click="downloadPreviewFile">{{ t('files.download') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Rename modal -->
    <n-modal v-model:show="showRename" preset="card" :title="t('common.edit')" style="max-width: 400px;">
      <n-input
        v-model:value="renameNewName"
        :placeholder="t('files.newFolderPlaceholder')"
        size="small"
        @keyup.enter="confirmRename"
      />
      <template #footer>
        <n-space justify="end">
          <n-button @click="showRename = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="confirmRename">{{ t('common.save') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- New folder modal -->
    <n-modal v-model:show="showNewFolder" preset="card" :title="t('files.newFolder')" style="max-width: 400px;">
      <n-input
        v-model:value="newFolderName"
        :placeholder="t('files.newFolderPlaceholder')"
        size="small"
        @keyup.enter="confirmCreateFolder"
      />
      <template #footer>
        <n-space justify="end">
          <n-button @click="showNewFolder = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="confirmCreateFolder">{{ t('common.create') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Delete session confirm modal -->
    <n-modal v-model:show="showDeleteSessionConfirm" preset="card" :title="t('files.deleteSession')" style="max-width: 450px;">
      <n-space vertical size="medium">
        <n-text>{{ t('files.deleteSessionConfirm') }}</n-text>
        <n-switch v-model:value="deleteSessionFiles" :label="t('files.deleteSessionFiles')" />
      </n-space>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showDeleteSessionConfirm = false">{{ t('common.cancel') }}</n-button>
          <n-button type="error" @click="confirmDeleteSession(deleteSessionFiles)">
            {{ t('common.delete') }}
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, h, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useChatStore } from '@/stores/chat'
import {
  NSpace,
  NUpload,
  NButton,
  NIcon,
  NDataTable,
  NEmpty,
  NPopconfirm,
  NInput,
  NGrid,
  NGridItem,
  NStatistic,
  NCard,
  NSpin,
  NTabs,
  NTabPane,
  NScrollbar,
  NText,
  NSelect,
  NFormItem,
  NSwitch,
  useMessage,
} from 'naive-ui'
import {
  CloudUploadOutline,
  TrashOutline,
  DownloadOutline,
  EyeOutline,
  CopyOutline,
  RefreshOutline,
  FolderOpenOutline,
  SaveOutline,
  FolderOutline,
  DocumentTextOutline,
  ImageOutline,
  MusicalNoteOutline,
  FilmOutline,
  CodeOutline,
  ArchiveOutline,
  AddOutline,
  CreateOutline,
} from '@vicons/ionicons5'
import * as sessionsApi from '@/api/sessions'
import type { DataTableColumns, PaginationProps } from 'naive-ui'

const { t } = useI18n()
const message = useMessage()

const activeTab = ref('workspace')

// ===== Uploads Tab =====
const files = ref<sessionsApi.FileItem[]>([])
const loading = ref(false)

const totalSize = computed(() => files.value.reduce((sum, f) => sum + f.size, 0))

const pagination = ref<PaginationProps>({
  page: 1,
  pageSize: 10,
  showSizePicker: true,
  pageSizes: [10, 20, 50],
  itemCount: 0,
  prefix: ({ itemCount }) => `${itemCount}`,
  onUpdatePage: (page: number) => {
    pagination.value.page = page
  },
  onUpdatePageSize: (pageSize: number) => {
    pagination.value.pageSize = pageSize
    pagination.value.page = 1
  },
})

watch(files, (val) => {
  pagination.value.itemCount = val.length
}, { immediate: true })

function getFileIcon(filename: string) {
  const ext = filename.split('.').pop()?.toLowerCase() || ''
  const imageExts = ['png', 'jpg', 'jpeg', 'gif', 'bmp', 'webp', 'svg']
  const codeExts = ['js', 'ts', 'go', 'py', 'java', 'c', 'cpp', 'h', 'rs', 'rb', 'php', 'sh', 'css', 'html', 'json', 'yaml', 'yml', 'xml', 'sql']
  const audioExts = ['mp3', 'wav', 'flac', 'aac', 'ogg']
  const videoExts = ['mp4', 'avi', 'mkv', 'mov', 'wmv']
  const archiveExts = ['zip', 'rar', '7z', 'tar', 'gz']
  const docExts = ['txt', 'md', 'doc', 'docx', 'pdf', 'csv']

  if (imageExts.includes(ext)) return ImageOutline
  if (codeExts.includes(ext)) return CodeOutline
  if (audioExts.includes(ext)) return MusicalNoteOutline
  if (videoExts.includes(ext)) return FilmOutline
  if (archiveExts.includes(ext)) return ArchiveOutline
  if (docExts.includes(ext)) return DocumentTextOutline
  return DocumentTextOutline
}

const uploadColumns: DataTableColumns<sessionsApi.FileItem> = [
  {
    title: '#',
    key: 'index',
    width: 50,
    align: 'center',
    render(_, index) {
      return index + 1
    },
  },
  {
    title: t('files.name'),
    key: 'filename',
    ellipsis: { tooltip: true },
    sorter: 'default',
    render(row) {
      const IconComp = getFileIcon(row.filename)
      return h(NSpace, { size: 8, align: 'center' }, {
        default: () => [
          h(NIcon, { size: 18, color: '#666' }, { default: () => h(IconComp) }),
          h('span', null, row.filename),
        ],
      })
    },
  },
  {
    title: t('files.size'),
    key: 'size',
    width: 120,
    sorter: (a, b) => a.size - b.size,
    render(row) {
      return h('span', { style: { color: '#666', fontSize: '13px' } }, formatSize(row.size))
    },
  },
  {
    title: t('files.updated'),
    key: 'updated',
    width: 170,
    sorter: (a, b) => new Date(a.updated).getTime() - new Date(b.updated).getTime(),
    render(row) {
      return h('span', { style: { color: '#999', fontSize: '13px' } }, row.updated)
    },
  },
  {
    title: t('files.actions'),
    key: 'actions',
    width: 180,
    align: 'center',
    render(row) {
      return h(NSpace, { size: 4, justify: 'center' }, {
        default: () => [
          h(NButton, {
            size: 'tiny',
            quaternary: true,
            title: t('files.preview'),
            onClick: () => previewUploadFile(row),
          }, {
            icon: () => h(NIcon, null, { default: () => h(EyeOutline) }),
          }),
          h(NButton, {
            size: 'tiny',
            quaternary: true,
            title: t('files.copyUrl'),
            onClick: () => copyUrl(row),
          }, {
            icon: () => h(NIcon, null, { default: () => h(CopyOutline) }),
          }),
          h(NButton, {
            size: 'tiny',
            quaternary: true,
            title: t('files.download'),
            onClick: () => downloadUploadFile(row),
          }, {
            icon: () => h(NIcon, null, { default: () => h(DownloadOutline) }),
          }),
          h(NPopconfirm, {
            onPositiveClick: () => handleDelete(row.filename),
          }, {
            trigger: () => h(NButton, {
              size: 'tiny',
              quaternary: true,
              type: 'error',
              title: t('files.delete'),
            }, {
              icon: () => h(NIcon, null, { default: () => h(TrashOutline) }),
            }),
            default: () => t('files.confirmDelete'),
          }),
        ],
      })
    },
  },
]

// ===== Workspace Tab =====
const wsEntries = ref<sessionsApi.FSEntry[]>([])
const wsLoading = ref(false)
const wsCurrentPath = ref('')
const selectedSessionId = ref<string>('')

const chatStore = useChatStore()

const sessionOptions = computed(() => {
  return chatStore.sessions
    .filter(s => s.work_dir && s.source === 'web')
    .map(s => ({
      label: s.title || s.id,
      value: s.id,
    }))
})

const currentRelativePath = computed(() => {
  if (!selectedSessionId.value || !wsCurrentPath.value) return '/'
  const session = chatStore.sessions.find(s => s.id === selectedSessionId.value)
  if (!session || !session.work_dir) return '/'
  const idx = wsCurrentPath.value.indexOf(session.work_dir)
  if (idx === -1) return '/'
  const rel = wsCurrentPath.value.substring(idx + session.work_dir.length)
  return rel || '/'
})

const showBackButton = computed(() => {
  if (!selectedSessionId.value || !wsCurrentPath.value) return false
  const session = chatStore.sessions.find(s => s.id === selectedSessionId.value)
  if (!session || !session.work_dir) return false
  return wsCurrentPath.value !== session.work_dir
})

async function loadSessions() {
  if (chatStore.sessions.length === 0) {
    await chatStore.loadSessions()
  }
}

function onSessionChange() {
  wsCurrentPath.value = ''
  loadWorkspace()
}

function goBack() {
  if (!wsCurrentPath.value) return
  const parts = wsCurrentPath.value.split('/').filter(Boolean)
  if (parts.length <= 1) {
    const session = chatStore.sessions.find(s => s.id === selectedSessionId.value)
    if (session && session.work_dir) {
      wsCurrentPath.value = session.work_dir
    }
  } else {
    parts.pop()
    wsCurrentPath.value = '/' + parts.join('/')
  }
  loadWorkspace(wsCurrentPath.value)
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
  return (bytes / (1024 * 1024 * 1024)).toFixed(1) + ' GB'
}

function formatDate(ts: number): string {
  if (!ts) return '-'
  return new Date(ts * 1000).toLocaleString()
}

const workspaceColumns: DataTableColumns<sessionsApi.FSEntry> = [
  {
    title: t('files.name'),
    key: 'name',
    ellipsis: { tooltip: true },
    // 文件夹优先于文件，组内按名称排序
    sorter: (a, b) => {
      if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1
      return a.name.localeCompare(b.name)
    },
    render(row) {
      const IconComp = row.is_dir ? FolderOutline : getFileIcon(row.name)
      const color = row.is_dir ? '#f0a020' : '#666'
      return h(NSpace, { size: 8, align: 'center' }, {
        default: () => [
          h(NIcon, { size: 18, color }, { default: () => h(IconComp) }),
          h('span', {
            style: {
              cursor: row.is_dir ? 'pointer' : 'default',
              color: row.is_dir ? '#2080f0' : undefined,
              fontWeight: row.is_dir ? 500 : undefined,
            },
            onClick: row.is_dir ? () => navigateWorkspace(row.path) : undefined,
          }, row.name),
        ],
      })
    },
  },
  {
    title: t('files.size'),
    key: 'size',
    width: 120,
    // 文件夹优先于文件，组内按大小排序
    sorter: (a, b) => {
      if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1
      return a.size - b.size
    },
    render(row) {
      if (row.is_dir) return h('span', { style: { color: '#999' } }, '-')
      return h('span', { style: { color: '#666', fontSize: '13px' } }, formatSize(row.size))
    },
  },
  {
    title: t('files.updated'),
    key: 'modified',
    width: 170,
    // 文件夹优先于文件，组内按修改时间排序
    sorter: (a, b) => {
      if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1
      return a.modified - b.modified
    },
    render(row) {
      return h('span', { style: { color: '#999', fontSize: '13px' } }, formatDate(row.modified))
    },
  },
  {
    title: t('files.actions'),
    key: 'actions',
    width: 180,
    align: 'center',
    render(row) {
      return h(NSpace, { size: 4, justify: 'center' }, {
        default: () => [
          h(NButton, {
            size: 'tiny',
            quaternary: true,
            title: t('files.preview'),
            onClick: () => previewWorkspaceFile(row),
          }, {
            icon: () => h(NIcon, null, { default: () => h(EyeOutline) }),
          }),
          h(NButton, {
            size: 'tiny',
            quaternary: true,
            title: t('common.edit'),
            onClick: () => startRename(row),
          }, {
            icon: () => h(NIcon, null, { default: () => h(CreateOutline) }),
          }),
          h(NButton, {
            size: 'tiny',
            quaternary: true,
            title: t('files.download'),
            onClick: () => downloadWorkspaceFile(row),
            disabled: row.is_dir,
          }, {
            icon: () => h(NIcon, null, { default: () => h(DownloadOutline) }),
          }),
          h(NPopconfirm, {
            onPositiveClick: () => deleteWorkspaceItem(row),
          }, {
            trigger: () => h(NButton, {
              size: 'tiny',
              quaternary: true,
              type: 'error',
              title: t('files.delete'),
            }, {
              icon: () => h(NIcon, null, { default: () => h(TrashOutline) }),
            }),
            default: () => t('files.confirmDelete'),
          }),
        ],
      })
    },
  },
]

// ===== Uploads Actions =====
async function loadFiles() {
  loading.value = true
  try {
    files.value = await sessionsApi.listFiles()
  } catch (e) {
    message.error(t('files.loadError'))
  } finally {
    loading.value = false
  }
}

async function handleUpload({ file }: { file: { file: File } }) {
  const nativeFile = file.file
  if (!nativeFile) return false
  try {
    await sessionsApi.uploadFile(nativeFile)
    message.success(t('files.uploadSuccess'))
    await loadFiles()
  } catch (e) {
    message.error(t('files.uploadError') + ': ' + (e as Error).message)
  }
  return false
}

async function handleDelete(filename: string) {
  try {
    await sessionsApi.deleteFile(filename)
    message.success(t('files.deleteSuccess'))
    await loadFiles()
  } catch (e) {
    message.error(t('files.deleteError'))
  }
}

function downloadUploadFile(file: sessionsApi.FileItem) {
  const link = document.createElement('a')
  link.href = file.url
  link.download = file.filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
}

async function previewUploadFile(file: sessionsApi.FileItem) {
  previewPath.value = ''
  previewTitle.value = file.filename
  previewType.value = 'none'
  previewContent.value = ''
  previewImageUrl.value = ''
  previewDownloadUrl.value = file.url

  if (isImageFile(file.filename)) {
    previewType.value = 'image'
    previewImageUrl.value = file.url
    showPreview.value = true
    return
  }

  try {
    const res = await fetch(file.url)
    if (!res.ok) {
      throw new Error(`Failed to preview file: ${res.statusText}`)
    }
    const content = await res.text()
    previewContent.value = content
    editContent.value = content
    previewType.value = 'text'
    showPreview.value = true
  } catch (e) {
    message.error(t('files.previewError') || 'Failed to preview file')
    console.error(e)
  }
}

async function copyUrl(file: sessionsApi.FileItem) {
  try {
    await navigator.clipboard.writeText(window.location.origin + file.url)
    message.success(t('files.copyUrlSuccess'))
  } catch {
    message.error(t('files.copyUrlError'))
  }
}

// ===== Workspace Actions =====
async function loadWorkspace(path?: string) {
  wsLoading.value = true
  try {
    const sessionId = selectedSessionId.value || undefined
    const session = sessionId ? chatStore.sessions.find(s => s.id === sessionId) : undefined
    const loadPath = path || session?.work_dir
    const res = await sessionsApi.listFSEntries(loadPath, sessionId)
    wsCurrentPath.value = res.current
    // 文件夹优先，组内按名称排序
    wsEntries.value = (res.entries || [])
      .filter(e => e.name !== '..')
      .sort((a, b) => {
        if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1
        return a.name.localeCompare(b.name)
      })
  } catch (e) {
    message.error(t('files.workspaceLoadError') || 'Failed to load workspace files')
    console.error(e)
  } finally {
    wsLoading.value = false
  }
}

function navigateWorkspace(path: string) {
  if (!path) return
  loadWorkspace(path)
}

// Preview/Editor
const showPreview = ref(false)
const previewTitle = ref('')
const previewContent = ref('')
const previewPath = ref('')
const previewType = ref<'text' | 'image' | 'none'>('none')
const previewImageUrl = ref('')
const previewDownloadUrl = ref('')
const isEditing = ref(false)
const editContent = ref('')

// Rename
const showRename = ref(false)
const renamePath = ref('')
const renameNewName = ref('')

function isImageFile(name: string): boolean {
  const ext = name.split('.').pop()?.toLowerCase() || ''
  return ['png', 'jpg', 'jpeg', 'gif', 'bmp', 'webp', 'svg'].includes(ext)
}

async function previewWorkspaceFile(row: sessionsApi.FSEntry) {
  if (row.is_dir) {
    navigateWorkspace(row.path)
    return
  }
  const sessionId = selectedSessionId.value || undefined
  previewPath.value = row.path
  previewTitle.value = row.name
  previewType.value = 'none'
  previewContent.value = ''
  previewImageUrl.value = ''
  previewDownloadUrl.value = sessionsApi.getFSDownloadUrl(row.path, sessionId)
  isEditing.value = false

  if (isImageFile(row.name)) {
    previewType.value = 'image'
    previewImageUrl.value = sessionsApi.getFSReadUrl(row.path, sessionId)
    showPreview.value = true
    return
  }

  try {
    const content = await sessionsApi.readFSFile(row.path, sessionId)
    previewContent.value = content
    editContent.value = content
    previewType.value = 'text'
    showPreview.value = true
  } catch (e) {
    message.error(t('files.previewError') || 'Failed to preview file')
  }
}

function startEdit() {
  isEditing.value = true
}

function cancelEdit() {
  isEditing.value = false
  editContent.value = previewContent.value
}

async function saveEdit() {
  if (!previewPath.value) return
  try {
    const sessionId = selectedSessionId.value || undefined
    await sessionsApi.writeFSFile(previewPath.value, editContent.value, sessionId)
    previewContent.value = editContent.value
    isEditing.value = false
    message.success(t('common.success'))
    await loadWorkspace(wsCurrentPath.value)
  } catch (e) {
    message.error(t('files.uploadError') || 'Save failed')
    console.error(e)
  }
}

async function downloadWithAuth(url: string, filename: string) {
  try {
    const res = await fetch(url, { headers: sessionsApi.getFSAuthHeaders() })
    if (!res.ok) {
      throw new Error(`Download failed: ${res.statusText}`)
    }
    const blob = await res.blob()
    const blobUrl = URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = blobUrl
    link.download = filename
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    URL.revokeObjectURL(blobUrl)
  } catch (e) {
    message.error(t('files.downloadError') || 'Download failed')
    console.error(e)
  }
}

function downloadWorkspaceFile(row: sessionsApi.FSEntry) {
  const sessionId = selectedSessionId.value || undefined
  const url = sessionsApi.getFSDownloadUrl(row.path, sessionId)
  downloadWithAuth(url, row.name)
}

function downloadPreviewFile() {
  if (!previewDownloadUrl.value) return
  downloadWithAuth(previewDownloadUrl.value, previewTitle.value)
}

// Rename
function startRename(row: sessionsApi.FSEntry) {
  renamePath.value = row.path
  renameNewName.value = row.name
  showRename.value = true
}

async function confirmRename() {
  if (!renamePath.value || !renameNewName.value.trim()) return
  try {
    const sessionId = selectedSessionId.value || undefined
    await sessionsApi.renameFSPath(renamePath.value, renameNewName.value.trim(), sessionId)
    message.success(t('common.success'))
    showRename.value = false
    await loadWorkspace(wsCurrentPath.value)
  } catch (e) {
    message.error((e as Error).message || t('common.error'))
    console.error(e)
  }
}

// New folder
const showNewFolder = ref(false)
const newFolderName = ref('')

function createNewFolder() {
  newFolderName.value = ''
  showNewFolder.value = true
}

async function confirmCreateFolder() {
  if (!newFolderName.value.trim()) return
  try {
    const sessionId = selectedSessionId.value || undefined
    await sessionsApi.createDir(wsCurrentPath.value || '', newFolderName.value.trim(), sessionId)
    message.success(t('files.folderCreated'))
    showNewFolder.value = false
    await loadWorkspace(wsCurrentPath.value)
  } catch (e) {
    message.error((e as Error).message || t('files.folderCreateFailed'))
    console.error(e)
  }
}

// Delete
async function deleteWorkspaceItem(row: sessionsApi.FSEntry) {
  try {
    const sessionId = selectedSessionId.value || undefined
    await sessionsApi.deleteFSPath(row.path, sessionId)
    message.success(t('files.deleteSuccess'))
    await loadWorkspace(wsCurrentPath.value)
  } catch (e) {
    message.error((e as Error).message || t('files.deleteError'))
    console.error(e)
  }
}

// ===== Zip =====
function zipWorkspace() {
  if (!wsCurrentPath.value) return
  const sessionId = selectedSessionId.value || undefined
  const url = sessionsApi.getFSZipUrl(wsCurrentPath.value, sessionId)
  const filename = (wsCurrentPath.value.split('/').filter(Boolean).pop() || 'workspace') + '.zip'
  downloadWithAuth(url, filename)
}

// Delete Session with confirmation
const showDeleteSessionConfirm = ref(false)
const deleteSessionId = ref('')
const deleteSessionFiles = ref(false)

function deleteSessionWithConfirm(sessionId: string) {
  deleteSessionId.value = sessionId
  showDeleteSessionConfirm.value = true
}

async function confirmDeleteSession(deleteFiles: boolean) {
  if (!deleteSessionId.value) return
  try {
    await sessionsApi.deleteSession(deleteSessionId.value)
    if (deleteFiles) {
      const session = chatStore.sessions.find(s => s.id === deleteSessionId.value)
      if (session && session.work_dir) {
        await sessionsApi.deleteFSPath(session.work_dir, deleteSessionId.value)
      }
    }
    await chatStore.loadSessions()
    message.success(t('common.success'))
    showDeleteSessionConfirm.value = false
    deleteSessionId.value = ''
    selectedSessionId.value = ''
    wsEntries.value = []
    wsCurrentPath.value = ''
  } catch (e) {
    message.error((e as Error).message || t('common.error'))
    console.error(e)
  }
}

watch(() => chatStore.sessions, () => {
  if (selectedSessionId.value && !chatStore.sessions.find(s => s.id === selectedSessionId.value)) {
    selectedSessionId.value = ''
    wsCurrentPath.value = ''
    loadWorkspace()
  }
}, { deep: true })

watch(selectedSessionId, (val) => {
  if (val) {
    localStorage.setItem('files_last_session_id', val)
  }
})

onMounted(async () => {
  loadFiles()
  await loadSessions()
  const lastSessionId = localStorage.getItem('files_last_session_id')
  if (lastSessionId && chatStore.sessions.find(s => s.id === lastSessionId && s.work_dir)) {
    selectedSessionId.value = lastSessionId
  }
  loadWorkspace()
})
</script>

<style scoped>
.files-container {
  height: 100%;
}

.workspace-table-container {
  max-height: calc(100vh - 280px);
  overflow-y: auto;
}

.file-preview-content {
  background: #f5f5f5;
  padding: 16px;
  border-radius: 4px;
  font-family: monospace;
  font-size: 13px;
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 70vh;
  overflow: auto;
}

.image-preview-wrapper {
  display: flex;
  justify-content: center;
  align-items: center;
}
</style>
