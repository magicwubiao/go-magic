<template>
  <div>
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
            <n-input
              v-model:value="searchQuery"
              :placeholder="t('files.search')"
              size="small"
              clearable
              style="width: 200px"
            >
              <template #prefix>
                <n-icon><SearchOutline /></n-icon>
              </template>
            </n-input>
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
              :data="filteredFiles"
              :loading="loading"
              :pagination="pagination"
              size="small"
              bordered
              striped
            />
            <n-empty v-if="!loading && filteredFiles.length === 0" :description="searchQuery ? t('files.noSearchResult') : t('files.empty')" style="margin-top: 24px;" />
          </n-card>
        </n-spin>
      </n-tab-pane>

      <!-- Workspace Tab -->
      <n-tab-pane name="workspace" :tab="t('files.workspaceTab')">
        <n-space justify="space-between" style="margin-bottom: 16px;">
          <n-space>
            <n-input
              v-model:value="wsSearchQuery"
              :placeholder="t('files.search')"
              size="small"
              clearable
              style="width: 240px"
            >
              <template #prefix>
                <n-icon><SearchOutline /></n-icon>
              </template>
            </n-input>
          </n-space>
          <n-space>
            <n-button size="small" @click="zipWorkspace" :disabled="!wsCurrentPath">
              <template #icon><n-icon><ArchiveOutline /></n-icon></template>
              {{ t('files.zipDownload') }}
            </n-button>
            <n-button size="small" type="primary" @click="shareWorkspace" :disabled="!wsCurrentPath">
              <template #icon><n-icon><LinkOutline /></n-icon></template>
              {{ t('files.shareLink') }}
            </n-button>
            <n-button size="small" @click="loadWorkspace(wsCurrentPath)" :loading="wsLoading">
              <template #icon>
                <n-icon><RefreshOutline /></n-icon>
              </template>
            </n-button>
          </n-space>
        </n-space>

        <!-- Breadcrumb -->
        <n-card size="small" style="margin-bottom: 12px;">
          <n-space align="center" size="small" wrap>
            <n-button size="tiny" quaternary :disabled="!wsParentPath" @click="navigateWorkspace(wsParentPath)">
              <template #icon><n-icon><FolderOpenOutline /></n-icon></template>
              ..
            </n-button>
            <n-text code style="font-size: 13px;">{{ wsCurrentPath || '/' }}</n-text>
          </n-space>
        </n-card>

        <n-spin :show="wsLoading">
          <n-card :title="t('files.workspaceList')" size="small">
            <n-data-table
              :columns="workspaceColumns"
              :data="filteredWorkspaceEntries"
              :loading="wsLoading"
              :pagination="wsPagination"
              size="small"
              bordered
              striped
            />
            <n-empty v-if="!wsLoading && filteredWorkspaceEntries.length === 0" :description="t('files.workspaceEmpty')" style="margin-top: 24px;" />
          </n-card>
        </n-spin>
      </n-tab-pane>
    </n-tabs>

    <!-- File preview modal -->
    <n-modal v-model:show="showPreview" preset="card" :title="previewTitle" style="max-width: 900px; width: 90vw;">
      <n-scrollbar style="max-height: 70vh;">
        <div v-if="previewType === 'image'" class="image-preview-wrapper">
          <img :src="previewImageUrl" :alt="previewTitle" style="max-width: 100%; max-height: 65vh;" />
        </div>
        <pre v-else-if="previewContent" class="file-preview-content">{{ previewContent }}</pre>
        <n-empty v-else :description="t('files.noPreview')" />
      </n-scrollbar>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showPreview = false">{{ t('common.close') }}</n-button>
          <n-button type="primary" @click="downloadPreviewFile">{{ t('files.download') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Share link modal -->
    <n-modal v-model:show="showShare" preset="card" :title="t('files.shareLinkTitle')" style="max-width: 600px; width: 90vw;">
      <n-space vertical size="medium">
        <n-space align="center" size="small">
          <n-text>{{ t('files.sharePath') }}:</n-text>
          <n-text code style="font-size: 12px; word-break: break-all;">{{ sharePath }}</n-text>
        </n-space>

        <n-form-item :label="t('files.shareExpiresIn')" label-placement="left">
          <n-select
            v-model:value="shareTTL"
            :options="shareTTLOptions"
            size="small"
            style="width: 200px"
          />
        </n-form-item>

        <n-input
          v-model:value="shareURL"
          readonly
          size="small"
          :placeholder="t('files.sharePlaceholder')"
        />

        <n-text v-if="shareExpiry" depth="3" style="font-size: 12px;">
          {{ t('files.shareExpiresAt') }}: {{ shareExpiry }}
        </n-text>
      </n-space>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showShare = false">{{ t('common.close') }}</n-button>
          <n-button :disabled="!shareURL" @click="copyShareURL">
            <template #icon><n-icon><CopyOutline /></n-icon></template>
            {{ t('files.copyUrl') }}
          </n-button>
          <n-button :disabled="!shareURL" @click="openShareURL">
            {{ t('files.openInNewTab') }}
          </n-button>
          <n-button type="primary" :loading="shareLoading" @click="createWorkspaceShare">
            {{ t('files.createShare') }}
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, h, computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
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
  useMessage,
} from 'naive-ui'
import {
  CloudUploadOutline,
  TrashOutline,
  DownloadOutline,
  SearchOutline,
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
  LinkOutline,
} from '@vicons/ionicons5'
import * as sessionsApi from '@/api/sessions'
import type { DataTableColumns, PaginationProps } from 'naive-ui'

const { t } = useI18n()
const message = useMessage()

const activeTab = ref('uploads')

// ===== Uploads Tab =====
const files = ref<sessionsApi.FileItem[]>([])
const loading = ref(false)
const searchQuery = ref('')

const totalSize = computed(() => files.value.reduce((sum, f) => sum + f.size, 0))

const filteredFiles = computed(() => {
  if (!searchQuery.value) return files.value
  const q = searchQuery.value.toLowerCase()
  return files.value.filter(f => f.filename.toLowerCase().includes(q))
})

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

watch(filteredFiles, (val) => {
  pagination.value.itemCount = val.length
}, { immediate: true })

watch(searchQuery, () => {
  pagination.value.page = 1
})

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
const wsParentPath = ref('')
const wsSearchQuery = ref('')

const filteredWorkspaceEntries = computed(() => {
  let entries = wsEntries.value
  if (wsSearchQuery.value) {
    const q = wsSearchQuery.value.toLowerCase()
    entries = entries.filter(e => e.name.toLowerCase().includes(q))
  }
  return entries
})

const wsPagination = ref<PaginationProps>({
  page: 1,
  pageSize: 20,
  showSizePicker: true,
  pageSizes: [10, 20, 50, 100],
  itemCount: 0,
  prefix: ({ itemCount }) => `${itemCount}`,
  onUpdatePage: (page: number) => {
    wsPagination.value.page = page
  },
  onUpdatePageSize: (pageSize: number) => {
    wsPagination.value.pageSize = pageSize
    wsPagination.value.page = 1
  },
})

watch(filteredWorkspaceEntries, (val) => {
  wsPagination.value.itemCount = val.length
}, { immediate: true })

watch(wsSearchQuery, () => {
  wsPagination.value.page = 1
})

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
    sorter: 'default',
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
    sorter: (a, b) => a.size - b.size,
    render(row) {
      if (row.is_dir) return h('span', { style: { color: '#999' } }, '-')
      return h('span', { style: { color: '#666', fontSize: '13px' } }, formatSize(row.size))
    },
  },
  {
    title: t('files.updated'),
    key: 'modified',
    width: 170,
    sorter: (a, b) => a.modified - b.modified,
    render(row) {
      return h('span', { style: { color: '#999', fontSize: '13px' } }, formatDate(row.modified))
    },
  },
  {
    title: t('files.actions'),
    key: 'actions',
    width: 140,
    align: 'center',
    render(row) {
      if (row.is_dir) return null
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
            title: t('files.download'),
            onClick: () => downloadWorkspaceFile(row),
          }, {
            icon: () => h(NIcon, null, { default: () => h(DownloadOutline) }),
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
    const res = await sessionsApi.listFSEntries(path)
    wsCurrentPath.value = res.current
    wsEntries.value = res.entries || []
    // Find parent path from ".." entry
    const parentEntry = res.entries?.find(e => e.name === '..')
    wsParentPath.value = parentEntry ? parentEntry.path : ''
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

const showPreview = ref(false)
const previewTitle = ref('')
const previewContent = ref('')
const previewPath = ref('')
const previewType = ref<'text' | 'image' | 'none'>('none')
const previewImageUrl = ref('')
const previewDownloadUrl = ref('')

function isImageFile(name: string): boolean {
  const ext = name.split('.').pop()?.toLowerCase() || ''
  return ['png', 'jpg', 'jpeg', 'gif', 'bmp', 'webp', 'svg'].includes(ext)
}

async function previewWorkspaceFile(row: sessionsApi.FSEntry) {
  if (row.is_dir) {
    navigateWorkspace(row.path)
    return
  }
  previewPath.value = row.path
  previewTitle.value = row.name
  previewType.value = 'none'
  previewContent.value = ''
  previewImageUrl.value = ''
  previewDownloadUrl.value = sessionsApi.getFSDownloadUrl(row.path)

  if (isImageFile(row.name)) {
    previewType.value = 'image'
    previewImageUrl.value = sessionsApi.getFSReadUrl(row.path)
    showPreview.value = true
    return
  }

  try {
    const content = await sessionsApi.readFSFile(row.path)
    previewContent.value = content
    previewType.value = 'text'
    showPreview.value = true
  } catch (e) {
    message.error(t('files.previewError') || 'Failed to preview file')
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
  const url = sessionsApi.getFSDownloadUrl(row.path)
  downloadWithAuth(url, row.name)
}

function downloadPreviewFile() {
  if (!previewDownloadUrl.value) return
  downloadWithAuth(previewDownloadUrl.value, previewTitle.value)
}

// ===== Zip and share =====
const showShare = ref(false)
const sharePath = ref('')
const shareURL = ref('')
const shareLoading = ref(false)
const shareExpiry = ref('')
const shareTTL = ref(3600)

const shareTTLOptions = computed(() => [
  { label: t('files.ttl1h'), value: 3600 },
  { label: t('files.ttl24h'), value: 86400 },
  { label: t('files.ttl7d'), value: 604800 },
])

function zipWorkspace() {
  if (!wsCurrentPath.value) return
  const url = sessionsApi.getFSZipUrl(wsCurrentPath.value)
  const filename = (wsCurrentPath.value.split('/').filter(Boolean).pop() || 'workspace') + '.zip'
  downloadWithAuth(url, filename)
}

function shareWorkspace() {
  if (!wsCurrentPath.value) return
  sharePath.value = wsCurrentPath.value
  shareURL.value = ''
  shareExpiry.value = ''
  shareTTL.value = 3600
  showShare.value = true
}

async function createWorkspaceShare() {
  if (!sharePath.value) return
  shareLoading.value = true
  try {
    const res = await sessionsApi.createShare(sharePath.value, shareTTL.value)
    shareURL.value = res.url
    shareExpiry.value = new Date(res.expires_at * 1000).toLocaleString()
    message.success(t('files.shareCreated') || 'Share link created')
  } catch (e) {
    message.error(t('files.shareFailed') || `Failed to create share link: ${(e as Error).message}`)
    console.error(e)
  } finally {
    shareLoading.value = false
  }
}

async function copyShareURL() {
  if (!shareURL.value) return
  try {
    await navigator.clipboard.writeText(shareURL.value)
    message.success(t('files.copyUrlSuccess'))
  } catch {
    message.error(t('files.copyUrlError'))
  }
}

function openShareURL() {
  if (!shareURL.value) return
  window.open(shareURL.value, '_blank', 'noopener')
}

onMounted(() => {
  loadFiles()
  loadWorkspace()
})
</script>

<style scoped>
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
</style>
