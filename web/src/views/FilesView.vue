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

    <!-- 文件预览：公共组件（与 RightSidebar 复用），内置高亮/编辑/全屏与移动端适配 -->
    <FilePreviewDialog
      v-model:show="showPreview"
      :name="previewFile?.filename || ''"
      :type-name="previewFile?.disk || previewFile?.filename || ''"
      :size="previewFile?.size"
      :url="previewFile?.url || ''"
    />
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
  NGrid,
  NGridItem,
  NStatistic,
  NCard,
  NSpin,
  useMessage,
} from 'naive-ui'
import {
  CloudUploadOutline,
  TrashOutline,
  DownloadOutline,
  EyeOutline,
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

// 文件预览（含高亮）已抽为公共组件，FilesView 与 RightSidebar 复用
import FilePreviewDialog from '@/components/FilePreviewDialog.vue'

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
            title: t('files.download'),
            onClick: () => downloadUploadFile(row),
          }, {
            icon: () => h(NIcon, null, { default: () => h(DownloadOutline) }),
          }),
          h(NPopconfirm, {
            onPositiveClick: () => handleDelete(row),
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
    ? uploadColumns.filter((c) => (c as { key?: string }).key !== 'updated')
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

async function handleDelete(file: sessionsApi.FileItem) {
  // Delete by the on-disk uuid name (not the readable display name) so the
  // backend can find the real file under its session directory. Fall back to
  // the legacy filename when the server did not return a disk field.
  const disk = file.disk || file.filename
  try {
    await sessionsApi.deleteFile(file.session_id, disk)
    message.success(t('files.deleteSuccess'))
    await loadFiles()
  } catch (e) {
    message.error(t('files.deleteError'))
  }
}

async function downloadUploadFile(file: sessionsApi.FileItem) {
  try {
    // 先换一张 uploads 票据再交给浏览器下载：<a href> 带不上 Authorization 头，
    // 而 FileItem.url 现在是裸地址。旧实现把登录凭据拼进 url（下面那个函数
    // 名叫 downloadWithAuth，其实并不带任何认证头），那正是本次要消除的做法。
    const url = await sessionsApi.resolveAttachmentSrc(file.url)
    const link = document.createElement('a')
    link.href = url
    link.download = file.filename
    link.style.display = 'none'
    document.body.appendChild(link)
    link.click()
    document.body.removeChild(link)
    message.success(t('common.downloadStarted'))
  } catch (e) {
    message.error(t('common.operationFailed'))
  }
}

// ===== Preview（预览 UI 收敛到公共组件 FilePreviewDialog） =====
const showPreview = ref(false)
const previewFile = ref<sessionsApi.FileItem | null>(null)

function previewUploadFile(file: sessionsApi.FileItem) {
  previewFile.value = file
  showPreview.value = true
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