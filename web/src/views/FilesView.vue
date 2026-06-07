<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>{{ t('files.title') }}</h2>
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
        <n-button size="small" @click="loadFiles" :loading="loading">
          <template #icon>
            <n-icon><RefreshOutline /></n-icon>
          </template>
        </n-button>
      </n-space>
    </n-space>

    <n-spin :show="loading">
      <!-- Stats cards -->
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

      <!-- File list -->
      <n-card :title="t('files.fileList')" size="small">
        <n-data-table
          :columns="columns"
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
} from '@vicons/ionicons5'
import * as sessionsApi from '@/api/sessions'
import type { DataTableColumns, PaginationProps } from 'naive-ui'

const { t } = useI18n()
const message = useMessage()

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

// Update itemCount when filtered files change
watch(filteredFiles, (val) => {
  pagination.value.itemCount = val.length
}, { immediate: true })

// Reset page when search query changes
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

const columns: DataTableColumns<sessionsApi.FileItem> = [
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
            onClick: () => previewFile(row),
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
            onClick: () => downloadFile(row),
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

function formatSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
  return (bytes / (1024 * 1024 * 1024)).toFixed(1) + ' GB'
}

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

function downloadFile(file: sessionsApi.FileItem) {
  const link = document.createElement('a')
  link.href = file.url
  link.download = file.filename
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
}

function previewFile(file: sessionsApi.FileItem) {
  window.open(file.url, '_blank')
}

async function copyUrl(file: sessionsApi.FileItem) {
  try {
    await navigator.clipboard.writeText(window.location.origin + file.url)
    message.success(t('files.copyUrlSuccess'))
  } catch {
    message.error(t('files.copyUrlError'))
  }
}

onMounted(loadFiles)
</script>
