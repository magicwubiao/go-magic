<template>
  <div class="files-container">
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>{{ t('files.title') }}</h2>
    </n-space>

    <n-space justify="space-between" style="margin-bottom: 16px;">
      <n-space>
        <n-upload
          :show-file-list="false"
          :multiple="true"
          :custom-request="handleUpload"
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
          :columns="uploadColumnsComputed"
          :data="files"
          :loading="loading"
          :pagination="pagination"
          :scroll-x="isMobile ? 400 : 720"
          size="small"
          bordered
          striped
        />
        <n-empty v-if="!loading && files.length === 0" :description="t('files.empty')" style="margin-top: 24px;" />
      </n-card>
    </n-spin>

    <!-- File preview/editor modal -->
    <n-modal v-model:show="showPreview" preset="card" class="modal-responsive modal-scroll" :title="previewTitle" style="width: min(950px, 95vw);">
      <n-scrollbar style="max-height: 70vh;">
        <div v-if="previewType === 'image'" class="image-preview-wrapper">
          <img :src="previewImageUrl" :alt="previewTitle" style="max-width: 100%; max-height: 65vh;" />
        </div>
        <div v-else-if="previewType === 'binary'" class="binary-preview-wrapper">
          <n-empty :description="t('chat.binaryFilePreview') || 'Binary file, preview not supported'" />
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
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted, h, computed, watch } from 'vue'
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
  NScrollbar,
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
} from '@vicons/ionicons5'
import * as sessionsApi from '@/api/sessions'
import type { DataTableColumns, PaginationProps, UploadCustomRequestOptions } from 'naive-ui'

const { t } = useI18n()
const message = useMessage()

// ===== Uploads =====
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

// ===== Responsive =====
const isMobile = ref(window.innerWidth <= 768)
const updateIsMobile = () => {
  isMobile.value = window.innerWidth <= 768
}

const uploadColumns: DataTableColumns<sessionsApi.FileItem> = [
  {
    title: '#',
    key: 'index',
    width: isMobile.value ? 44 : 50,
    align: 'center',
    render(_, index) {
      return index + 1
    },
  },
  {
    title: t('files.name'),
    key: 'filename',
    minWidth: isMobile.value ? 110 : 200,
    ellipsis: { tooltip: true },
    sorter: 'default',
    render(row) {
      const IconComp = getFileIcon(row.filename)
      return h('div', {
        style: {
          display: 'flex',
          alignItems: 'center',
          gap: '8px',
          overflow: 'hidden',
        },
      }, {
        default: () => [
          h(NIcon, { size: 18, color: '#666', style: { flexShrink: 0 } }, { default: () => h(IconComp) }),
          h('span', {
            style: {
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            },
          }, row.filename),
        ],
      })
    },
  },
  {
    title: t('files.size'),
    key: 'size',
    width: isMobile.value ? 84 : 120,
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
    width: isMobile.value ? 148 : 180,
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

// 移动端隐藏“更新时间”列,避免固定列宽超出屏幕导致文件列表显示不全
const uploadColumnsComputed = computed<DataTableColumns<sessionsApi.FileItem>>(() =>
  isMobile.value
    ? uploadColumns.filter((c) => c.key !== 'updated')
    : uploadColumns
)

// ===== Upload Actions =====
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

function handleUpload({ file, onFinish, onError }: UploadCustomRequestOptions) {
  const nativeFile = file.file
  if (!nativeFile) {
    onError()
    return
  }
  sessionsApi.uploadFile(nativeFile)
    .then(() => {
      message.success(t('files.uploadSuccess'))
      loadFiles()
      onFinish()
    })
    .catch((e) => {
      message.error(t('files.uploadError') + ': ' + (e as Error).message)
      onError()
    })
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
  downloadWithAuth(file.url, file.filename)
}

function copyUrl(file: sessionsApi.FileItem) {
  navigator.clipboard.writeText(file.url).then(() => {
    message.success(t('common.copied') || 'Copied!')
  }).catch(() => {
    message.error(t('common.copyFailed') || 'Copy failed')
  })
}

// ===== Preview/Editor =====
const showPreview = ref(false)
const previewTitle = ref('')
const previewContent = ref('')
const previewPath = ref('')
const previewType = ref<'text' | 'image' | 'binary' | 'none'>('none')
const previewImageUrl = ref('')
const previewDownloadUrl = ref('')
const isEditing = ref(false)
const editContent = ref('')

function isImageFile(name: string): boolean {
  const ext = name.split('.').pop()?.toLowerCase() || ''
  return ['png', 'jpg', 'jpeg', 'gif', 'bmp', 'webp', 'svg', 'ico'].includes(ext)
}

function isBinaryFile(name: string): boolean {
  const ext = name.split('.').pop()?.toLowerCase() || ''
  const binaryExts = ['exe', 'dll', 'so', 'dylib', 'zip', 'rar', '7z', 'tar', 'gz', 'bz2', 'xz', 'pdf', 'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx', 'odt', 'ods', 'odp', 'mp3', 'mp4', 'avi', 'mkv', 'mov', 'wmv', 'flac', 'aac', 'ogg', 'wav', 'ico', 'woff', 'woff2', 'ttf', 'eot', 'class', 'jar', 'war', 'pyc', 'o', 'a', 'lib', 'bin', 'dat', 'db', 'sqlite', 'wasm']
  return binaryExts.includes(ext)
}

async function previewUploadFile(file: sessionsApi.FileItem) {
  previewPath.value = ''
  previewTitle.value = file.filename
  previewType.value = 'none'
  previewContent.value = ''
  previewImageUrl.value = ''
  previewDownloadUrl.value = file.url
  isEditing.value = false

  if (isImageFile(file.filename)) {
    previewType.value = 'image'
    previewImageUrl.value = file.url
    showPreview.value = true
    return
  }

  if (isBinaryFile(file.filename)) {
    previewType.value = 'binary'
    showPreview.value = true
    return
  }

  try {
    const res = await fetch(file.url)
    if (!res.ok) {
      throw new Error(`Failed to preview file: ${res.statusText}`)
    }
    const content = await res.text()
    if (content.includes('"binary":true') && content.includes('"error"')) {
      try {
        const parsed = JSON.parse(content)
        if (parsed.binary) {
          previewType.value = 'binary'
          showPreview.value = true
          return
        }
      } catch {}
    }
    previewContent.value = content
    editContent.value = content
    previewType.value = 'text'
    showPreview.value = true
  } catch (e) {
    message.error(t('files.previewError') || 'Failed to preview file')
    console.error(e)
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
    await sessionsApi.writeFSFile(previewPath.value, editContent.value, undefined)
    previewContent.value = editContent.value
    isEditing.value = false
    message.success(t('common.success'))
    await loadFiles()
  } catch (e) {
    message.error(t('files.uploadError') || 'Save failed')
    console.error(e)
  }
}

async function downloadWithAuth(url: string, filename: string) {
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.style.display = 'none'
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  message.success(t('common.downloadStarted'))
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
  return (bytes / (1024 * 1024 * 1024)).toFixed(1) + ' GB'
}

onMounted(() => {
  loadFiles()
  window.addEventListener('resize', updateIsMobile)
})

onUnmounted(() => {
  window.removeEventListener('resize', updateIsMobile)
})
</script>

<style scoped>
.files-container {
  height: 100%;
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

.binary-preview-wrapper {
  display: flex;
  justify-content: center;
  align-items: center;
  padding: 40px 0;
}

/* Responsive: Mobile devices */
@media (max-width: 768px) {
  .files-container {
    padding: 0 8px;
  }
  
  .files-container :deep(.n-grid) {
    grid-template-columns: 1fr !important;
  }
  
  .files-container :deep(.n-data-table) {
    font-size: 12px;
  }
  
  .files-container :deep(.n-card) {
    margin-bottom: 12px;
  }
  
  .files-container :deep(.n-space) {
    flex-wrap: wrap;
  }
}
</style>