<template>
  <div class="office-preview-wrapper">
    <n-spin v-if="rendering" size="large" class="office-preview-spin" />
    <FileViewer
      v-if="file"
      :key="fileKey"
      :file="file"
      :filename="filename"
      :name="name"
      :type="type"
      :size="file.size"
      :options="viewerOptions"
      @load-complete="onLoaded"
      @error="onError"
    />
  </div>
</template>

<script setup lang="ts">
// Office 文档预览包装（docx/xlsx/pptx/odt/ods/odp）。
//
// 刻意做成独立组件由 FilePreviewDialog 通过 defineAsyncComponent 引入：
// Flyfish 渲染器（含 xlsx 解析、docx 排版等）体积不小，只有真的预览到
// office 文件时才值得下载这一块代码，主包保持精简。
//
// 渲染器按需裁剪：不引 preset-office（会把 pdf.js 和带水印的 .ppt 老格式
// 运行时一起拖进来），只组合 word / spreadsheet / presentation 三个插件，
// 且 presentation 只留 pptx 定义、word 排除 .doc 老二进制——这两类在
// FilePreviewDialog 里本来就走「下载查看」回落。
// OOXML 渲染全部在主线程完成，不依赖 worker/wasm 等外链资产，离线可用。
import { computed, ref } from 'vue'
import { NSpin } from 'naive-ui'
import { FileViewer } from '@file-viewer/vue3'
import '@file-viewer/vue3/dist/file-viewer3.css'
import {
  renderFileViewerOpenDocument,
  renderFileViewerWordDocx,
  wordRendererDefinitions,
} from '@file-viewer/renderer-word'
import {
  renderFileViewerSpreadsheet,
  spreadsheetRendererDefinition,
} from '@file-viewer/renderer-spreadsheet'
import {
  presentationRendererDefinition,
  renderFileViewerPresentation,
} from '@file-viewer/renderer-presentation'
import type { FileViewerOptions } from '@file-viewer/core'

interface Props {
  /** 待预览的文件内容（FilePreviewDialog 已通过票据拉好的完整二进制） */
  file: File
  /** 真实文件名，渲染器靠扩展名选择解析管线 */
  filename?: string
  /** 展示名 */
  name?: string
  /** 小写扩展名 */
  type?: string
}

const props = withDefaults(defineProps<Props>(), {
  filename: '',
  name: '',
  type: '',
})

const emit = defineEmits<{
  (e: 'loaded'): void
  (e: 'failed', message: string): void
}>()

const rendering = ref(true)

// 与 wordRenderer / spreadsheetRenderer / presentationRenderer 同构，
// 但只保留本应用需要的定义：
//  - word：docx（openxml）+ odt（open-document），去掉 .doc 老二进制
//  - spreadsheet：xlsx/ods（openxml），去掉 dbf
//  - presentation：仅 pptx，去掉 .ppt（老格式运行时带水印且需要外链资产）
//
// 收口 as：各渲染器包把 handler 的 target 标注成 HTMLDivElement，而 core 的
// options.renderers 期望 HTMLElement —— 函数参数逆变导致结构化类型不兼容，
// 实际调用方传的正是渲染器自己创建的 div 容器，语义安全。
const officeRenderers = [
  {
    id: 'go-magic-renderer-word',
    label: 'Word renderer (docx/odt)',
    definitions: wordRendererDefinitions.filter(d => d.id !== 'office-word-binary'),
    handlers: [
      { rendererId: 'office-word-openxml', handler: renderFileViewerWordDocx },
      { rendererId: 'open-document', handler: renderFileViewerOpenDocument },
    ],
  },
  {
    id: 'go-magic-renderer-spreadsheet',
    label: 'Spreadsheet renderer (xlsx/ods)',
    definitions: [spreadsheetRendererDefinition],
    handlers: [{ rendererId: spreadsheetRendererDefinition.id, handler: renderFileViewerSpreadsheet }],
  },
  {
    id: 'go-magic-renderer-presentation',
    label: 'Presentation renderer (pptx)',
    definitions: [presentationRendererDefinition],
    handlers: [{ rendererId: presentationRendererDefinition.id, handler: renderFileViewerPresentation }],
  },
] as unknown as NonNullable<FileViewerOptions['renderers']>

// 工具栏交给外层 FilePreviewDialog（缩放/下载/全屏都在那），水印不需要，
// 主题跟随应用浅色主题。
const viewerOptions = computed<FileViewerOptions>(() => ({
  renderers: officeRenderers,
  toolbar: false,
  watermark: false,
  search: false,
  theme: 'light',
}))

const fileKey = computed(() => `${props.filename}|${props.file.size}`)

function onLoaded() {
  rendering.value = false
  emit('loaded')
}

function onError(message: string) {
  rendering.value = false
  emit('failed', message || '')
}
</script>

<style scoped>
/* 撑满外层 .preview-content-container，滚动交给渲染器内部 */
.office-preview-wrapper {
  position: relative;
  width: 100%;
  height: 70vh;
  min-height: 320px;
  background: #fff;
}

.office-preview-spin {
  position: absolute;
  top: 50%;
  left: 50%;
  transform: translate(-50%, -50%);
  z-index: 1;
}

/* 全屏预览时占满退出条以外的全部空间（与 .preview-image 等分支同一约定） */
.preview-content-container.preview-fullscreen .office-preview-wrapper {
  height: 100%;
  min-height: 0;
}

/* 并排面板/浮窗：外层 .preview-content-container 由 flex 定高，wrapper 直接
   撑满容器、滚动交给渲染器内部（FileViewer 根元素自带 height: 100%）。
   固定 70vh 两头不讨好：面板比 70vh 矮时工具栏+预览一起把外层容器撑出
   滚动条；面板比 70vh 高时正文下方留一大块空白。
   dialog 模式不能照搬——容器高度是 auto（只有 max-height），百分比高度
   无法解析，保持 70vh 估算值。 */
.file-preview-panel--panel .office-preview-wrapper {
  height: 100%;
  min-height: 0;
}
</style>
