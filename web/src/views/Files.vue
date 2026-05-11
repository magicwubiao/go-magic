<template>
  <div class="files-view">
    <n-grid :cols="4" :x-gap="16">
      <!-- File Browser -->
      <n-gi :span="3">
        <n-card title="File Browser" class="file-browser-card">
          <template #header-extra>
            <n-space>
              <n-select
                v-model:value="currentBackend"
                :options="backendOptions"
                style="width: 140px"
                @update:value="changeBackend"
              />
              <n-button size="small" @click="refreshFiles">
                <template #icon>
                  <n-icon :component="Refresh" />
                </template>
              </n-button>
              <n-button size="small" @click="uploadFile">
                <template #icon>
                  <n-icon :component="CloudUpload" />
                </template>
                Upload
              </n-button>
            </n-space>
          </template>

          <!-- Breadcrumb -->
          <n-breadcrumb style="margin-bottom: 12px">
            <n-breadcrumb-item
              v-for="(dir, index) in breadcrumb"
              :key="dir.path"
              @click="navigateTo(dir.path)"
              :clickable="index < breadcrumb.length - 1"
            >
              <n-icon :component="Folder" v-if="index < breadcrumb.length - 1" />
              {{ dir.name }}
            </n-breadcrumb-item>
          </n-breadcrumb>

          <!-- File List -->
          <n-data-table
            :columns="fileColumns"
            :data="files"
            :bordered="false"
            :row-key="(row: FileItem) => row.name"
          />
        </n-card>
      </n-gi>

      <!-- File Details -->
      <n-gi :span="1">
        <n-card title="Details" v-if="selectedFile">
          <n-descriptions :column="1" label-placement="top">
            <n-descriptions-item label="Name">
              {{ selectedFile.name }}
            </n-descriptions-item>
            <n-descriptions-item label="Type">
              <n-tag size="small">{{ selectedFile.type }}</n-tag>
            </n-descriptions-item>
            <n-descriptions-item label="Size">
              {{ formatSize(selectedFile.size) }}
            </n-descriptions-item>
            <n-descriptions-item label="Modified">
              {{ formatTime(selectedFile.modified) }}
            </n-descriptions-item>
            <n-descriptions-item label="Permissions">
              <n-tag size="small">{{ selectedFile.permissions }}</n-tag>
            </n-descriptions-item>
          </n-descriptions>

          <n-divider />

          <n-space vertical>
            <n-button block @click="downloadSelected" v-if="selectedFile.type === 'file'">
              <template #icon>
                <n-icon :component="Download" />
              </template>
              Download
            </n-button>
            <n-button block @click="renameFile">
              <template #icon>
                <n-icon :component="Create" />
              </template>
              Rename
            </n-button>
            <n-button block @click="copyFile">
              <template #icon>
                <n-icon :component="Copy" />
              </template>
              Copy
            </n-button>
            <n-button block type="error" @click="deleteFile">
              <template #icon>
                <n-icon :component="Trash" />
              </template>
              Delete
            </n-button>
          </n-space>
        </n-card>

        <n-card title="Details" v-else>
          <n-empty description="Select a file to view details" />
        </n-card>

        <!-- Upload Progress -->
        <n-card title="Uploads" style="margin-top: 16px" v-if="uploads.length > 0">
          <n-list>
            <n-list-item v-for="upload in uploads" :key="upload.name">
              <n-thing :title="upload.name">
                <template #description>
                  <n-progress
                    :percentage="upload.progress"
                    :status="upload.progress === 100 ? 'success' : 'default'"
                  />
                </template>
              </n-thing>
            </n-list-item>
          </n-list>
        </n-card>
      </n-gi>
    </n-grid>

    <!-- Create Folder Modal -->
    <n-modal v-model:show="showCreateFolder" preset="card" title="Create Folder" style="width: 400px">
      <n-input v-model:value="newFolderName" placeholder="Folder name" />
      <template #footer>
        <n-space justify="end">
          <n-button @click="showCreateFolder = false">Cancel</n-button>
          <n-button type="primary" @click="createFolder">Create</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Rename Modal -->
    <n-modal v-model:show="showRename" preset="card" title="Rename" style="width: 400px">
      <n-input v-model:value="newFileName" placeholder="New name" />
      <template #footer>
        <n-space justify="end">
          <n-button @click="showRename = false">Cancel</n-button>
          <n-button type="primary" @click="confirmRename">Rename</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Copy Modal -->
    <n-modal v-model:show="showCopy" preset="card" title="Copy to..." style="width: 400px">
      <n-input v-model:value="copyDestination" placeholder="Destination path" />
      <template #footer>
        <n-space justify="end">
          <n-button @click="showCopy = false">Cancel</n-button>
          <n-button type="primary" @click="confirmCopy">Copy</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- File Preview Modal -->
    <n-modal v-model:show="showPreview" preset="card" :title="selectedFile?.name || 'Preview'" style="width: 800px">
      <n-spin v-if="loadingPreview" />
      <n-code v-else-if="previewContent" :code="previewContent" language="auto" />
      <n-empty v-else description="Cannot preview this file" />
    </n-modal>

    <!-- Hidden file input for upload -->
    <input
      type="file"
      ref="fileInputRef"
      style="display: none"
      @change="handleFileSelect"
      multiple
    />
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import {
  NCard,
  NGrid,
  NGi,
  NButton,
  NIcon,
  NSpace,
  NSelect,
  NBreadcrumb,
  NBreadcrumbItem,
  NDataTable,
  NTag,
  NText,
  NEmpty,
  NDescriptions,
  NDescriptionsItem,
  NDivider,
  NProgress,
  NList,
  NListItem,
  NThing,
  NModal,
  NInput,
  NCode,
  NSpin,
} from 'naive-ui'
import {
  Refresh,
  CloudUpload,
  Folder,
  Document,
  Download,
  Create,
  Copy,
  Trash,
  Image,
  Film,
  MusicalNotes,
  CodeSlash,
} from '@vicons/ionicons5'

interface FileItem {
  name: string
  type: 'file' | 'directory' | 'symlink'
  size: number
  modified: number
  permissions: string
  path: string
}

interface BreadcrumbItem {
  name: string
  path: string
}

interface Upload {
  name: string
  progress: number
}

const currentBackend = ref('local')
const currentPath = ref('/')
const breadcrumb = ref<BreadcrumbItem[]>([{ name: 'Root', path: '/' }])
const files = ref<FileItem[]>([])
const selectedFile = ref<FileItem | null>(null)
const loadingPreview = ref(false)
const previewContent = ref('')

const showCreateFolder = ref(false)
const showRename = ref(false)
const showCopy = ref(false)
const showPreview = ref(false)

const newFolderName = ref('')
const newFileName = ref('')
const copyDestination = ref('')

const uploads = ref<Upload[]>([])
const fileInputRef = ref<HTMLInputElement>()

const backendOptions = [
  { label: 'Local', value: 'local' },
  { label: 'Docker', value: 'docker' },
  { label: 'SSH', value: 'ssh' },
]

const fileColumns = [
  {
    title: 'Name',
    key: 'name',
    render: (row: FileItem) =>
      h('div', { style: { display: 'flex', alignItems: 'center', gap: '8px' } }, [
        h(NIcon, { component: getFileIcon(row), size: 20 }),
        row.type === 'directory'
          ? h('a', { href: '#', onClick: () => navigateTo(row.path) }, row.name)
          : row.name,
      ]),
  },
  {
    title: 'Size',
    key: 'size',
    render: (row: FileItem) => (row.type === 'directory' ? '-' : formatSize(row.size)),
  },
  {
    title: 'Modified',
    key: 'modified',
    render: (row: FileItem) => formatTime(row.modified),
  },
  {
    title: 'Permissions',
    key: 'permissions',
    render: (row: FileItem) => h(NTag, { size: 'small' }, { default: () => row.permissions }),
  },
]

function getFileIcon(file: FileItem) {
  if (file.type === 'directory') return Folder

  const ext = file.name.split('.').pop()?.toLowerCase()
  if (['jpg', 'jpeg', 'png', 'gif', 'svg', 'webp'].includes(ext || '')) return Image
  if (['mp4', 'avi', 'mov', 'mkv'].includes(ext || '')) return Film
  if (['mp3', 'wav', 'ogg', 'flac'].includes(ext || '')) return MusicalNotes
  if (['js', 'ts', 'py', 'go', 'rs', 'java', 'c', 'cpp', 'h'].includes(ext || '')) return CodeSlash
  return Document
}

function formatSize(bytes: number): string {
  if (bytes === 0) return '-'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / Math.pow(k, i)).toFixed(1)} ${sizes[i]}`
}

function formatTime(timestamp: number): string {
  return new Date(timestamp * 1000).toLocaleString()
}

function navigateTo(path: string) {
  currentPath.value = path
  updateBreadcrumb(path)
  loadFiles()
}

function updateBreadcrumb(path: string) {
  const parts = path.split('/').filter(Boolean)
  breadcrumb.value = [{ name: 'Root', path: '/' }]
  let current = ''
  for (const part of parts) {
    current += `/${part}`
    breadcrumb.value.push({ name: part, path: current })
  }
}

async function loadFiles() {
  try {
    const res = await fetch(`/api/files/list?backend=${currentBackend.value}&path=${encodeURIComponent(currentPath.value)}`)
    if (res.ok) {
      files.value = await res.json()
    }
  } catch (e) {
    console.error('Failed to load files:', e)
  }
}

async function refreshFiles() {
  await loadFiles()
}

function changeBackend() {
  currentPath.value = '/'
  updateBreadcrumb('/')
  loadFiles()
}

function uploadFile() {
  fileInputRef.value?.click()
}

async function handleFileSelect(e: Event) {
  const input = e.target as HTMLInputElement
  if (!input.files?.length) return

  for (const file of input.files) {
    const upload: Upload = { name: file.name, progress: 0 }
    uploads.value.push(upload)

    const formData = new FormData()
    formData.append('file', file)
    formData.append('path', currentPath.value)
    formData.append('backend', currentBackend.value)

    try {
      const xhr = new XMLHttpRequest()
      xhr.upload.onprogress = (e) => {
        if (e.lengthComputable) {
          upload.progress = Math.round((e.loaded / e.total) * 100)
        }
      }
      xhr.onload = () => {
        upload.progress = 100
        setTimeout(() => {
          uploads.value = uploads.value.filter((u) => u.name !== file.name)
        }, 1000)
        refreshFiles()
      }
      xhr.open('POST', '/api/files/upload')
      xhr.send(formData)
    } catch (e) {
      console.error('Upload failed:', e)
    }
  }

  input.value = ''
}

async function downloadSelected() {
  if (!selectedFile.value) return
  window.open(`/api/files/download?backend=${currentBackend.value}&path=${encodeURIComponent(selectedFile.value.path)}`)
}

async function createFolder() {
  if (!newFolderName.value) return
  try {
    await fetch('/api/files/mkdir', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        backend: currentBackend.value,
        path: currentPath.value,
        name: newFolderName.value,
      }),
    })
    showCreateFolder.value = false
    newFolderName.value = ''
    refreshFiles()
  } catch (e) {
    console.error('Failed to create folder:', e)
  }
}

function renameFile() {
  if (!selectedFile.value) return
  newFileName.value = selectedFile.value.name
  showRename.value = true
}

async function confirmRename() {
  if (!selectedFile.value || !newFileName.value) return
  try {
    await fetch('/api/files/rename', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        backend: currentBackend.value,
        oldPath: selectedFile.value.path,
        newPath: currentPath.value + '/' + newFileName.value,
      }),
    })
    showRename.value = false
    refreshFiles()
  } catch (e) {
    console.error('Failed to rename:', e)
  }
}

function copyFile() {
  if (!selectedFile.value) return
  copyDestination.value = currentPath.value + '/' + selectedFile.value.name
  showCopy.value = true
}

async function confirmCopy() {
  if (!selectedFile.value || !copyDestination.value) return
  try {
    await fetch('/api/files/copy', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        backend: currentBackend.value,
        source: selectedFile.value.path,
        destination: copyDestination.value,
      }),
    })
    showCopy.value = false
    refreshFiles()
  } catch (e) {
    console.error('Failed to copy:', e)
  }
}

async function deleteFile() {
  if (!selectedFile.value) return
  if (!confirm(`Delete ${selectedFile.value.name}?`)) return

  try {
    await fetch('/api/files/delete', {
      method: 'DELETE',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        backend: currentBackend.value,
        path: selectedFile.value.path,
      }),
    })
    selectedFile.value = null
    refreshFiles()
  } catch (e) {
    console.error('Failed to delete:', e)
  }
}

async function previewFile() {
  if (!selectedFile.value || selectedFile.value.type !== 'file') return
  loadingPreview.value = true
  showPreview.value = true

  try {
    const res = await fetch(`/api/files/read?backend=${currentBackend.value}&path=${encodeURIComponent(selectedFile.value.path)}`)
    if (res.ok) {
      previewContent.value = await res.text()
    }
  } catch (e) {
    console.error('Failed to preview:', e)
  } finally {
    loadingPreview.value = false
  }
}

onMounted(() => {
  loadFiles()
})
</script>

<style lang="scss" scoped>
.files-view {
  padding: 16px;
  height: calc(100vh - 84px);
}

.file-browser-card {
  height: 100%;
}
</style>
