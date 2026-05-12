<template>
  <div class="file-upload">
    <input
      ref="fileInput"
      type="file"
      :accept="accept"
      :multiple="multiple"
      @change="handleFileSelect"
      class="file-input"
    />
    <div
      class="upload-area"
      :class="{ dragging: isDragging, 'has-files': files.length > 0 }"
      @dragover.prevent="isDragging = true"
      @dragleave="isDragging = false"
      @drop.prevent="handleDrop"
      @click="triggerFileSelect"
    >
      <div v-if="files.length === 0" class="upload-placeholder">
        <span class="upload-icon">📎</span>
        <span class="upload-text">Drop files here or click to upload</span>
        <span class="upload-hint">{{ accept }} files up to {{ formatSize(maxSize) }}</span>
      </div>
      <div v-else class="file-list">
        <div v-for="(file, index) in files" :key="index" class="file-item">
          <span class="file-icon">{{ getFileIcon(file.type) }}</span>
          <span class="file-name">{{ file.name }}</span>
          <span class="file-size">{{ formatSize(file.size) }}</span>
          <button @click.stop="removeFile(index)" class="remove-btn">×</button>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'

const props = withDefaults(defineProps<{
  accept?: string
  multiple?: boolean
  maxSize?: number
}>(), {
  accept: '*/*',
  multiple: true,
  maxSize: 10 * 1024 * 1024, // 10MB
})

const emit = defineEmits(['filesSelected'])

const fileInput = ref<HTMLInputElement | null>(null)
const isDragging = ref(false)
const files = ref<Array<{name: string; size: number; type: string; data?: string}>>([])

function triggerFileSelect() {
  fileInput.value?.click()
}

function handleFileSelect(e: Event) {
  const target = e.target as HTMLInputElement
  processFiles(Array.from(target.files || []))
}

function handleDrop(e: DragEvent) {
  isDragging.value = false
  processFiles(Array.from(e.dataTransfer?.files || []))
}

async function processFiles(fileList: File[]) {
  const validFiles: Array<{name: string; size: number; type: string; data?: string}> = []
  
  for (const file of fileList) {
    if (file.size > props.maxSize) {
      alert(`File ${file.name} is too large (max ${formatSize(props.maxSize)})`)
      continue
    }
    
    const data = await readFileAsDataURL(file)
    validFiles.push({
      name: file.name,
      size: file.size,
      type: file.type,
      data,
    })
  }
  
  files.value = props.multiple ? [...files.value, ...validFiles] : validFiles
  emit('filesSelected', files.value)
}

function readFileAsDataURL(file: File): Promise<string> {
  return new Promise((resolve) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result as string)
    reader.readAsDataURL(file)
  })
}

function removeFile(index: number) {
  files.value.splice(index, 1)
  emit('filesSelected', files.value)
}

function getFileIcon(type: string): string {
  if (type.startsWith('image/')) return '🖼️'
  if (type.startsWith('video/')) return '🎬'
  if (type.startsWith('audio/')) return '🎵'
  if (type.includes('pdf')) return '📄'
  if (type.includes('word') || type.includes('document')) return '📝'
  if (type.includes('sheet') || type.includes('excel')) return '📊'
  if (type.includes('zip') || type.includes('archive')) return '📦'
  return '📎'
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
}
</script>

<style scoped>
.file-upload {
  margin-bottom: 16px;
}
.file-input {
  display: none;
}
.upload-area {
  border: 2px dashed var(--border-color);
  border-radius: 12px;
  padding: 24px;
  text-align: center;
  cursor: pointer;
  transition: all 0.2s;
}
.upload-area:hover,
.upload-area.dragging {
  border-color: var(--primary-color);
  background: var(--hover-bg);
}
.upload-placeholder {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
}
.upload-icon {
  font-size: 32px;
}
.upload-text {
  font-size: 14px;
  color: var(--text-primary);
}
.upload-hint {
  font-size: 12px;
  color: var(--text-secondary);
}
.file-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  text-align: left;
}
.file-item {
  display: flex;
  align-items: center;
  padding: 8px 12px;
  background: var(--bg-secondary);
  border-radius: 8px;
}
.file-icon {
  margin-right: 8px;
}
.file-name {
  flex: 1;
  font-size: 13px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.file-size {
  margin: 0 12px;
  font-size: 12px;
  color: var(--text-secondary);
}
.remove-btn {
  background: none;
  border: none;
  font-size: 18px;
  color: var(--text-secondary);
  cursor: pointer;
}
.remove-btn:hover {
  color: var(--error-color);
}
</style>
