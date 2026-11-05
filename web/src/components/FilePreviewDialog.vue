<template>
  <n-modal
    v-model:show="visible"
    preset="card"
    class="file-preview-modal modal-responsive modal-scroll"
    :style="modalStyle"
    :bordered="false"
    :header-style="{ display: 'none' }"
  >
    <!-- 内容全部在 FilePreviewPanel 里：它同时被 ChatView 用作并排预览面板
         （预览时不挡对话），这里只负责提供弹窗外壳，保持四处调用点零改动。 -->
    <FilePreviewPanel
      :active="visible"
      variant="dialog"
      :name="name"
      :type-name="typeName"
      :size="size"
      :url="url"
      :fs-path="fsPath"
      :session-id="sessionId"
      @close="visible = false"
    />
  </n-modal>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { NModal } from 'naive-ui'
import FilePreviewPanel from '@/components/FilePreviewPanel.vue'

/**
 * 文件预览弹窗（弹窗外壳）。
 *
 * 内容体与全部预览逻辑在 FilePreviewPanel 里，本组件只做两件事：
 * 1. 用 n-modal 提供遮罩/居中的独立页预览外观（FilesView / BotsView 等页面用）
 * 2. 把 v-model:show 映射成面板的 active
 *
 * 预览是只读的，没有「未保存改动」要拦截，关闭可以直接落到 v-model 上。
 *
 * 聊天页要的是「预览不挡对话」，那边直接用 FilePreviewPanel 并排挂载。
 */
interface Props {
  show: boolean
  /** 显示名（标题与下载文件名） */
  name?: string
  /** 真实文件名，用于扩展名判定（上传场景显示名可能与磁盘名不一致） */
  typeName?: string
  /** 字节数，用于标题右侧体积标签，也用于预览前的体积预判 */
  size?: number
  /** 直接可用地址（上传文件场景） */
  url?: string
  /** 工作区相对路径（文件树场景） */
  fsPath?: string
  sessionId?: string
}

const props = withDefaults(defineProps<Props>(), {
  name: '',
  typeName: '',
  url: '',
  fsPath: '',
})

const emit = defineEmits<{
  (e: 'update:show', value: boolean): void
}>()

const visible = computed({
  get: () => props.show,
  set: (value: boolean) => emit('update:show', value),
})

const isMobile = ref(typeof window !== 'undefined' && window.innerWidth <= 768)
const updateIsMobile = () => {
  isMobile.value = window.innerWidth <= 768
}

const modalStyle = computed(() =>
  isMobile.value ? { width: '96vw', maxWidth: '96vw' } : { width: 'min(960px, 92vw)' },
)

onMounted(() => {
  window.addEventListener('resize', updateIsMobile)
})

onUnmounted(() => {
  window.removeEventListener('resize', updateIsMobile)
})
</script>
