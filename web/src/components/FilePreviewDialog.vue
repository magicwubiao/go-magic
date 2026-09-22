<template>
  <n-modal
    v-model:show="visible"
    preset="card"
    class="file-preview-modal modal-responsive modal-scroll"
    :title="name"
    :style="modalStyle"
    :mask-closable="!unsavedChanges"
    :close-on-esc="!unsavedChanges"
    @close="handleCloseAttempt"
  >
    <template #header-extra>
      <n-tag v-if="typeof size === 'number'" size="small" type="info">{{ formatSize(size) }}</n-tag>
    </template>

    <div v-if="loading" class="preview-state">
      <n-spin size="large" />
      <n-text depth="3" class="preview-state-text">Loading...</n-text>
    </div>

    <div v-else-if="error" class="preview-state">
      <n-text type="error">{{ error }}</n-text>
      <!-- 读取失败（含「超过 2MB 无法预览」）时，用户最需要的还是能拿到文件，
           所以错误态也保留下载出口 -->
      <n-button v-if="downloadUrl" size="small" class="binary-preview-action" @click="download">
        <template #icon><n-icon :component="DownloadOutline" /></template>
        {{ t('chat.download') }}
      </n-button>
    </div>

    <div v-else>
      <!-- 工具条：左侧编辑/保存，右侧复制/下载/全屏（统一入口，移动端自动折行） -->
      <div class="preview-toolbar">
        <n-space :size="8" :wrap="true" class="preview-toolbar-group">
          <n-button
            v-if="canEdit"
            size="small"
            :type="isEditing ? 'warning' : 'default'"
            @click="toggleEdit"
          >
            <template #icon>
              <n-icon :component="isEditing ? CloseOutline : PencilOutline" />
            </template>
            <span v-if="!isMobile" class="btn-label">{{ isEditing ? t('common.cancel') : t('common.edit') }}</span>
          </n-button>
          <n-button
            v-if="canEdit && isEditing"
            size="small"
            type="primary"
            :loading="saving"
            :disabled="!unsavedChanges"
            @click="save"
          >
            <template #icon>
              <n-icon :component="SaveOutline" />
            </template>
            <span v-if="!isMobile" class="btn-label">{{ t('common.save') }}</span>
          </n-button>
        </n-space>

        <!-- 视图控制：缩放 / 折行 / Markdown 渲染 / 新标签打开。
             按当前类型出现，不堆一排永远禁用的按钮。 -->
        <n-space :size="8" :wrap="true" class="preview-toolbar-group">
          <template v-if="kind === 'image' && !imageError">
            <n-button size="small" quaternary :title="t('chat.zoomOut')" @click="zoomBy(1 / 1.25)">
              <template #icon><n-icon :component="RemoveOutline" /></template>
            </n-button>
            <n-button size="small" quaternary :title="t('chat.zoomIn')" @click="zoomBy(1.25)">
              <template #icon><n-icon :component="AddOutline" /></template>
            </n-button>
            <n-button size="small" quaternary :title="t('chat.zoomReset')" @click="resetImageZoom">
              <template #icon><n-icon :component="RefreshOutline" /></template>
            </n-button>
          </template>
          <n-button
            v-if="(kind === 'text' || htmlSource) && content"
            size="small"
            quaternary
            :title="wrapLines ? t('chat.unwrapLines') : t('chat.wrapLines')"
            @click="wrapLines = !wrapLines"
          >
            <template #icon><n-icon :component="TextOutline" /></template>
            <span v-if="!isMobile" class="btn-label">{{ wrapLines ? t('chat.unwrapLines') : t('chat.wrapLines') }}</span>
          </n-button>
          <n-button
            v-if="isMarkdown && content"
            size="small"
            quaternary
            :title="markdownRendered ? t('chat.markdownSource') : t('chat.markdownPreview')"
            @click="markdownRendered = !markdownRendered"
          >
            <template #icon><n-icon :component="markdownRendered ? CodeSlashOutline : EyeOutline" /></template>
            <span v-if="!isMobile" class="btn-label">{{ markdownRendered ? t('chat.markdownSource') : t('chat.markdownPreview') }}</span>
          </n-button>
          <!-- HTML：默认渲染页面，可切到源码。没有这个入口时，HTML 预览是一
               个只能看渲染结果的黑盒——排版/脚本一出问题就无从下手。
               注意源码视图对**单文件**页面最有用：iframe 里的相对子资源
               （<img src="logo.png">）会按票据 URL 解析，取不到同目录的别的
               上传文件，多文件站点在这里只会渲染出一部分。 -->
          <n-button
            v-if="isHtmlDoc"
            size="small"
            quaternary
            :loading="sourceLoading"
            :title="htmlSource ? t('chat.htmlPreview') : t('chat.htmlSource')"
            @click="toggleHtmlSource"
          >
            <template #icon>
              <n-icon :component="htmlSource ? EyeOutline : CodeSlashOutline" />
            </template>
            <span v-if="!isMobile" class="btn-label">
              {{ htmlSource ? t('chat.htmlPreview') : t('chat.htmlSource') }}
            </span>
          </n-button>
          <n-button
            v-if="openableUrl"
            size="small"
            quaternary
            :title="t('chat.openInNewTab')"
            @click="openInNewTab"
          >
            <template #icon><n-icon :component="OpenOutline" /></template>
            <span v-if="!isMobile" class="btn-label">{{ t('chat.openInNewTab') }}</span>
          </n-button>
        </n-space>

        <n-space :size="8" :wrap="true" class="preview-toolbar-group">
          <n-button
            size="small"
            quaternary
            :disabled="!content"
            :title="t('chat.copyContent')"
            @click="copyContent"
          >
            <template #icon>
              <n-icon :component="CopyOutline" />
            </template>
            <span v-if="!isMobile" class="btn-label">{{ t('chat.copyContent') }}</span>
          </n-button>
          <n-button
            size="small"
            quaternary
            :disabled="!downloadUrl"
            :title="t('chat.download')"
            @click="download"
          >
            <template #icon>
              <n-icon :component="DownloadOutline" />
            </template>
            <span v-if="!isMobile" class="btn-label">{{ t('chat.download') }}</span>
          </n-button>
          <n-button
            size="small"
            quaternary
            :title="fullscreen ? t('chat.exitFullscreen') : t('chat.fullscreen')"
            @click="toggleFullscreen"
          >
            <template #icon>
              <n-icon :component="fullscreen ? ContractOutline : ExpandOutline" />
            </template>
            <span v-if="!isMobile" class="btn-label">
              {{ fullscreen ? t('chat.exitFullscreen') : t('chat.fullscreen') }}
            </span>
          </n-button>
        </n-space>
      </div>

      <div v-if="unsavedChanges" class="unsaved-hint">
        <n-icon size="14" color="#f0a020" style="margin-right: 6px;">
          <AlertCircleOutline />
        </n-icon>
        <n-text depth="2" style="font-size: 12px;">{{ t('chat.unsavedChanges') }}</n-text>
      </div>

      <div class="preview-content-container" :class="{ 'preview-fullscreen': fullscreen }">
        <!-- 全屏时这一层是 fixed inset:0，会把上面的工具条整条盖住；而 iframe 里
             按 ESC 不会冒泡到父页面，触屏设备更没有 ESC。因此全屏必须自带来路
             明确的退出入口，否则用户会被困在预览里。 -->
        <div v-if="fullscreen" class="preview-fullscreen-bar">
          <n-text class="preview-fullscreen-name" :title="name">{{ name }}</n-text>
          <n-button
            size="small"
            secondary
            :title="t('chat.exitFullscreen')"
            @click="toggleFullscreen"
          >
            <template #icon>
              <n-icon :component="ContractOutline" />
            </template>
            <span class="btn-label">{{ t('chat.exitFullscreen') }}</span>
          </n-button>
        </div>

        <textarea
          v-if="isEditing"
          v-model="editingContent"
          class="preview-textarea"
          spellcheck="false"
        ></textarea>

        <div
          v-else-if="kind === 'image'"
          class="image-preview-wrapper"
          :class="{ 'image-preview-wrapper--zoomed': imageZoom > 1 }"
        >
          <!-- 图片加载失败（文件被删/无权限/托管地址失效）以前是静默空白，
               现在给出明确提示并保留下载出口 -->
          <div v-if="imageError" class="preview-state">
            <n-text type="error">{{ t('chat.imageLoadFailed') }}</n-text>
            <n-button v-if="downloadUrl" size="small" class="binary-preview-action" @click="download">
              <template #icon><n-icon :component="DownloadOutline" /></template>
              {{ t('chat.download') }}
            </n-button>
          </div>
          <img
            v-else
            :src="imageUrl"
            :alt="name"
            class="preview-image"
            :class="{ 'preview-image-zoomed': imageZoom > 1 }"
            :style="imageDisplayStyle"
            :title="t('chat.zoomHint')"
            @load="onImageLoad"
            @error="onImageError"
            @click="toggleImageZoom"
            @wheel="onImageWheel"
          />
        </div>

        <div v-else-if="kind === 'web' && !htmlSource" class="html-preview-container">
          <div v-if="webLoading" class="web-preview-loading">
            <n-spin size="large" />
          </div>
          <template v-else>
            <div v-if="webError" class="web-preview-error">
              <n-text type="error">{{ webError }}</n-text>
            </div>
            <video
              v-else-if="mediaType === 'video'"
              :src="serveUrl"
              class="preview-media-frame"
              controls
              playsinline
              @loadeddata="onWebLoaded"
              @error="onWebError"
            ></video>
            <audio
              v-else-if="mediaType === 'audio'"
              :src="serveUrl"
              class="preview-audio-frame"
              controls
              @loadeddata="onWebLoaded"
              @error="onWebError"
            ></audio>
            <iframe
              v-else
              :src="serveUrl"
              class="html-preview-frame"
              :sandbox="frameSandbox"
              title="HTML Preview"
              @load="onWebLoaded"
              @error="onWebError"
            ></iframe>
          </template>
        </div>

        <div v-else-if="kind === 'binary'" class="binary-preview-wrapper">
          <n-icon size="48" depth="3"><DocumentOutline /></n-icon>
          <n-text depth="3" class="binary-preview-text">{{ t('chat.binaryFilePreview') }}</n-text>
          <n-button size="small" class="binary-preview-action" @click="download">
            <template #icon><n-icon :component="DownloadOutline" /></template>
            {{ t('chat.download') }}
          </n-button>
        </div>

        <!-- Markdown 渲染视图：包在 sandbox="" 的 iframe 里。
             marked 不做消毒，正文里的 <script>/onerror 会原样进入文档；
             sandbox="" 禁止脚本执行，因此即使渲染了恶意 Markdown 也无法
             碰到主站的 DOM 与 localStorage。 -->
        <div v-else-if="isMarkdown && markdownRendered && content" class="markdown-preview">
          <iframe
            class="markdown-preview-frame"
            sandbox=""
            :srcdoc="markdownHtml"
            title="Markdown Preview"
          ></iframe>
        </div>

        <pre
          v-else-if="content"
          class="preview-content"
          :class="{ 'preview-wrap': wrapLines }"
          v-html="highlighted"
        ></pre>

        <div v-else class="preview-state">
          <n-text depth="3">{{ t('files.noPreview') }}</n-text>
        </div>
      </div>
    </div>
  </n-modal>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import {
  AddOutline,
  AlertCircleOutline,
  CloseOutline,
  CodeSlashOutline,
  ContractOutline,
  CopyOutline,
  DocumentOutline,
  DownloadOutline,
  ExpandOutline,
  EyeOutline,
  OpenOutline,
  PencilOutline,
  RefreshOutline,
  RemoveOutline,
  SaveOutline,
  TextOutline,
} from '@vicons/ionicons5'
import { marked } from 'marked'
import * as sessionsApi from '@/api/sessions'

// ===== Syntax highlighting (highlight.js, GitHub Light palette) =====
import hljs from 'highlight.js/lib/core'
import javascript from 'highlight.js/lib/languages/javascript'
import typescript from 'highlight.js/lib/languages/typescript'
import python from 'highlight.js/lib/languages/python'
import go from 'highlight.js/lib/languages/go'
import bash from 'highlight.js/lib/languages/bash'
import json from 'highlight.js/lib/languages/json'
import xml from 'highlight.js/lib/languages/xml'
import css from 'highlight.js/lib/languages/css'
import markdown from 'highlight.js/lib/languages/markdown'
import yaml from 'highlight.js/lib/languages/yaml'
import sql from 'highlight.js/lib/languages/sql'
import ini from 'highlight.js/lib/languages/ini'

hljs.registerLanguage('javascript', javascript)
hljs.registerLanguage('typescript', typescript)
hljs.registerLanguage('python', python)
hljs.registerLanguage('go', go)
hljs.registerLanguage('bash', bash)
hljs.registerLanguage('json', json)
hljs.registerLanguage('xml', xml)
hljs.registerLanguage('css', css)
hljs.registerLanguage('markdown', markdown)
hljs.registerLanguage('yaml', yaml)
hljs.registerLanguage('sql', sql)
hljs.registerLanguage('ini', ini)

/**
 * 通用文件预览弹窗。
 *
 * 支持两种数据来源，二选一：
 *  - fsPath (+ sessionId)：工作区/会话文件，走 sessionsApi 读写与托管
 *  - url                 ：直接可用的地址（如上传文件），内部会补 token
 *
 * 自动按扩展名分派：图片 / HTML·PDF / 视频 / 音频 / 二进制占位 / 文本（含高亮与编辑）。
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
  /** 是否允许编辑（还需同时提供 fsPath 才能真正保存） */
  editable?: boolean
}

const props = withDefaults(defineProps<Props>(), {
  name: '',
  typeName: '',
  url: '',
  fsPath: '',
  editable: true,
})

const emit = defineEmits<{
  (e: 'update:show', value: boolean): void
  (e: 'saved'): void
}>()

const { t } = useI18n()
const message = useMessage()

const visible = computed({
  get: () => props.show,
  set: (value: boolean) => emit('update:show', value),
})

type Kind = 'none' | 'text' | 'image' | 'web' | 'binary'

// 图片：浏览器可直接渲染（ico 归入图片，与既有行为一致）
const IMAGE_EXTS = ['png', 'jpg', 'jpeg', 'gif', 'bmp', 'webp', 'svg', 'ico']
// 浏览器原生可预览：HTML / PDF / 视频 / 音频，走托管地址由 iframe 或原生控件加载
const WEB_EXTS = ['html', 'htm', 'pdf', 'mp4', 'webm', 'mov', 'm4v', 'mpg', 'mpeg', 'ogv', 'mp3', 'wav', 'm4a', 'flac', 'aac', 'oga', 'ogg']
// 无法在浏览器渲染、仅提供下载的类型
const BINARY_EXTS = ['exe', 'dll', 'so', 'dylib', 'zip', 'rar', '7z', 'tar', 'gz', 'bz2', 'xz', 'doc', 'docx', 'xls', 'xlsx', 'ppt', 'pptx', 'odt', 'ods', 'odp', 'avi', 'mkv', 'wmv', 'woff', 'woff2', 'ttf', 'eot', 'class', 'jar', 'war', 'pyc', 'o', 'a', 'lib', 'bin', 'dat', 'db', 'sqlite', 'wasm']

// 可被 <video> 播放的容器。mkv 已从列表移除：Chrome 不支持 Matroska 容器，
// 命中它只会渲染出一个永远播不起来的播放器（死条目），不如落到 BINARY_EXTS
// 给出明确的下载出口。
const VIDEO_EXTS = ['mp4', 'webm', 'mov', 'm4v', 'mpg', 'mpeg', 'ogv']
const AUDIO_EXTS = ['mp3', 'wav', 'm4a', 'flac', 'aac', 'oga', 'ogg']
const MARKDOWN_EXTS = ['md', 'markdown']

// 「按网页文档渲染」的类型，必须严格是 WEB_EXTS 里走 iframe 的那几个。
// 只有它们需要「渲染预览 ↔ 查看源码」这一对视图：音视频/PDF 没有可读源码，
// xhtml 落在文本预览分支里本来就直接显示源码，都不该再挂一个切换按钮。
const HTML_DOC_EXTS = ['html', 'htm']

// HTML 预览的 sandbox：必须允许脚本（否则预览页跑不起来），但绝不能加
// allow-same-origin —— 那会让被预览的 HTML 与主站同源，从而能读到
// localStorage.auth_token，并直接拿它调用后端 API（被预览页面即可接管账号）。
// 实测：带 allow-same-origin 时父页能拿到 iframe.contentDocument；去掉后
// contentDocument 为 null、访问 contentWindow 抛 SecurityError，token 拿不到。
const HTML_FRAME_SANDBOX = 'allow-scripts allow-forms'

// 与后端 /api/fs/read 的 2MB 上限一致。后端超限会返回 413，但等到那时才报错
// 太晚（要先把 2MB 下下来才发现看不了），所以这里先用 props.size 拦一道。
const MAX_TEXT_PREVIEW_BYTES = 2 * 1024 * 1024

// Markdown 渲染视图的样式，作用在 sandbox="" 的 iframe 内
const MARKDOWN_PREVIEW_CSS = `
  body { margin: 0; padding: 16px; color: #333; word-wrap: break-word;
    font: 14px/1.7 -apple-system, "Segoe UI", Roboto, "Helvetica Neue", Arial, "PingFang SC", "Microsoft YaHei", sans-serif; }
  h1, h2, h3, h4 { margin: 1.2em 0 .6em; line-height: 1.3; }
  h1 { font-size: 1.6em; border-bottom: 1px solid #eaecef; padding-bottom: .3em; }
  h2 { font-size: 1.35em; border-bottom: 1px solid #eaecef; padding-bottom: .3em; }
  code { background: #f6f8fa; padding: .2em .4em; border-radius: 4px; font-family: Consolas, Monaco, monospace; font-size: 85%; }
  pre { background: #f6f8fa; padding: 12px; border-radius: 6px; overflow: auto; }
  pre code { background: none; padding: 0; }
  blockquote { margin: 0; padding: 0 1em; color: #6a737d; border-left: .25em solid #dfe2e5; }
  table { border-collapse: collapse; display: block; overflow: auto; }
  th, td { border: 1px solid #dfe2e5; padding: 6px 13px; }
  img { max-width: 100%; }
  a { color: #0366d6; }
`

// 扩展名 → highlight.js 语言映射
const extLanguageMap: Record<string, string> = {
  js: 'javascript', mjs: 'javascript', cjs: 'javascript', jsx: 'javascript',
  ts: 'typescript', tsx: 'typescript', mts: 'typescript', cts: 'typescript',
  py: 'python', go: 'go',
  sh: 'bash', bash: 'bash', zsh: 'bash',
  json: 'json', xml: 'xml', svg: 'xml', html: 'xml', htm: 'xml', vue: 'xml',
  css: 'css', scss: 'css', less: 'css',
  md: 'markdown', markdown: 'markdown',
  yml: 'yaml', yaml: 'yaml',
  sql: 'sql',
  ini: 'ini', conf: 'ini', cfg: 'ini', toml: 'ini', properties: 'ini', env: 'ini',
}

const loading = ref(false)
const error = ref('')
const content = ref('')
const originalContent = ref('')
const kind = ref<Kind>('none')
const isEditing = ref(false)
const editingContent = ref('')
const saving = ref(false)
const fullscreen = ref(false)
const webLoading = ref(false)
const webError = ref('')
let webTimer: ReturnType<typeof setTimeout> | null = null

// 图片：加载失败提示 + 缩放
const imageError = ref(false)
const imageZoom = ref(1)
const imageNaturalWidth = ref(0)
const IMAGE_ZOOM_MIN = 0.1
const IMAGE_ZOOM_MAX = 8

// 文本视图：是否折行。默认不折，保留代码/长行的原始形态，改用横向滚动。
const wrapLines = ref(false)
// Markdown：默认看源码（带高亮），可切到渲染视图
const markdownRendered = ref(false)
// HTML：默认渲染 iframe（上传的 HTML 也照样渲染，服务端现在按 inline 应答，
// 隔离由响应 CSP 的 sandbox 承担），可切到源码视图（见 toggleHtmlSource）
const htmlSource = ref(false)
const sourceLoading = ref(false)

const isMobile = ref(typeof window !== 'undefined' && window.innerWidth <= 768)
const updateIsMobile = () => {
  isMobile.value = window.innerWidth <= 768
}

const modalStyle = computed(() =>
  isMobile.value ? { width: '96vw', maxWidth: '96vw' } : { width: 'min(960px, 92vw)' },
)

const ext = computed(() => (props.typeName || props.name || '').split('.').pop()?.toLowerCase() || '')

const usesFsPath = computed(() => !!props.fsPath)

// 只有真拿到正文的文本类文件才可写回：二进制与音视频/PDF 根本没有正文，
// 旧实现下它们照样显示「编辑」，点开是一个空文本域——一旦误保存就把文件清空。
// HTML 在读出来源之后（htmlSource 分支）同样可编辑，kind 仍是 'web'，
// 所以这里按「有没有正文」判，而不是只认 kind === 'text'。
const canEdit = computed(
  () =>
    props.editable
    && usesFsPath.value
    && (kind.value === 'text' || (kind.value === 'web' && !!content.value)),
)

const unsavedChanges = computed(
  () => isEditing.value && editingContent.value !== originalContent.value,
)

// 三种地址：读取正文 / iframe·媒体托管 / 下载。
//
// 三者现在都是服务端签发的**票据**，换取需要一次带 Authorization 头的 fetch，
// 因此都改成 ref + 异步填充，不能再是 computed。
const readUrl = ref('')
const serveUrl = ref('')
const downloadUrl = ref('')

const imageUrl = computed(() => readUrl.value)

// PDF 必须**不用** sandbox：Chrome 内置阅读器是以同源扩展文档实现的，文档被
// sandbox 变成不透明源后它无法初始化。实测同一份 PDF：sandbox 下 iframe 区域
// 97.9% 是纯灰 #dddddd、没有任何内容；不加 sandbox 则正常渲染出阅读器工具栏与
// 纸张。PDF 里没有能访问主站 DOM/storage 的 HTML 或脚本，这个取舍可接受。
const frameSandbox = computed(() => (ext.value === 'pdf' ? undefined : HTML_FRAME_SANDBOX))

const isMarkdown = computed(() => MARKDOWN_EXTS.includes(ext.value))

// 可切换「渲染预览 / 源码」的网页文档（HTML）
const isHtmlDoc = computed(() => HTML_DOC_EXTS.includes(ext.value))

// 能在新标签页独立打开的地址（图片/媒体/PDF/HTML 用托管地址）
const openableUrl = computed(() => {
  if (kind.value === 'image') return imageUrl.value
  if (kind.value === 'web') return serveUrl.value
  return ''
})

// 缩放用显式像素宽度实现，而不是 transform: scale —— transform 不改变元素的
// 布局尺寸，容器算不出滚动区域，放大后就只能看到左上角。
const imageDisplayStyle = computed(() => {
  if (imageError.value) return {}
  if (!imageNaturalWidth.value || imageZoom.value === 1) return {}
  return { width: `${Math.round(imageNaturalWidth.value * imageZoom.value)}px` }
})

// marked 不做消毒，因此渲染结果放进 sandbox="" 的 iframe（禁止脚本执行）
const markdownHtml = computed(() => {
  const body = marked.parse(content.value || '', { async: false }) as string
  return `<!doctype html><html><head><meta charset="utf-8"><style>${MARKDOWN_PREVIEW_CSS}</style></head><body>${body}</body></html>`
})

const mediaType = computed<'video' | 'audio' | 'none'>(() => {
  if (VIDEO_EXTS.includes(ext.value)) return 'video'
  if (AUDIO_EXTS.includes(ext.value)) return 'audio'
  return 'none'
})

function formatSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
  return (bytes / (1024 * 1024 * 1024)).toFixed(1) + ' GB'
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

// 预览高亮：按扩展名选语言，未识别时自动探测；
// 超长文件跳过高亮（避免卡顿），异常时回退纯转义文本。
const highlighted = computed(() => {
  const text = content.value
  if (!text) return ''
  if (text.length > 500000) return escapeHtml(text)
  const lang = extLanguageMap[ext.value]
  try {
    if (lang && hljs.getLanguage(lang)) {
      return hljs.highlight(text, { language: lang, ignoreIllegals: true }).value
    }
    return hljs.highlightAuto(text).value
  } catch {
    return escapeHtml(text)
  }
})

function clearWebTimer() {
  if (webTimer) {
    clearTimeout(webTimer)
    webTimer = null
  }
}

function reset() {
  loading.value = true
  error.value = ''
  content.value = ''
  originalContent.value = ''
  kind.value = 'none'
  isEditing.value = false
  editingContent.value = ''
  saving.value = false
  fullscreen.value = false
  webLoading.value = false
  webError.value = ''
  serveUrl.value = ''
  imageError.value = false
  imageZoom.value = 1
  imageNaturalWidth.value = 0
  markdownRendered.value = false
  htmlSource.value = false
  sourceLoading.value = false
  clearWebTimer()
}

/**
 * 按探测到的编码解码文本。
 *
 * 后端统一按 UTF-8 应答，但工作区里的中文文件大量是 GBK/GB18030，直接
 * text() 会整篇乱码。顺序：BOM → 响应声明的 charset → 严格 UTF-8 →
 * GB18030（GBK / GB2312 的超集）。严格 UTF-8 解码能通过就一定不是 GBK，
 * 所以这个判定是可靠的，不必为此再引一个编码探测库。
 */
function decodeText(buffer: ArrayBuffer, contentType: string): string {
  const bytes = new Uint8Array(buffer)
  if (bytes.length >= 3 && bytes[0] === 0xef && bytes[1] === 0xbb && bytes[2] === 0xbf) {
    return new TextDecoder('utf-8').decode(bytes.subarray(3))
  }
  if (bytes.length >= 2 && bytes[0] === 0xff && bytes[1] === 0xfe) {
    return new TextDecoder('utf-16le').decode(bytes.subarray(2))
  }
  if (bytes.length >= 2 && bytes[0] === 0xfe && bytes[1] === 0xff) {
    return new TextDecoder('utf-16be').decode(bytes.subarray(2))
  }

  const declared = /charset=["']?([\w-]+)/i.exec(contentType || '')?.[1]?.toLowerCase()
  if (declared && declared !== 'utf-8' && declared !== 'utf8') {
    try {
      return new TextDecoder(declared).decode(bytes)
    } catch {
      // 浏览器不认这个编码名，落到下面的探测
    }
  }

  try {
    return new TextDecoder('utf-8', { fatal: true }).decode(bytes)
  } catch {
    // 不是合法 UTF-8 → 按中文环境最常见的 GB18030 解码
  }
  try {
    return new TextDecoder('gb18030').decode(bytes)
  } catch {
    return new TextDecoder('utf-8').decode(bytes)
  }
}

/**
 * 读出文件正文（文本预览与 HTML 源码视图共用）。
 *
 * 体积预判放在这里而不是调用点：后端超限会返回 413，但不必先把 2MB 下下来
 * 才发现看不了。命中时抛错，由调用方展示——错误提示本身已含「请下载后查看」。
 */
async function readTextBody(): Promise<string> {
  if (typeof props.size === 'number' && props.size > MAX_TEXT_PREVIEW_BYTES) {
    throw new Error(t('chat.tooLargeToPreview', { size: formatSize(MAX_TEXT_PREVIEW_BYTES) }))
  }
  if (usesFsPath.value) {
    const { buffer, contentType } = await sessionsApi.readFSFileBytes(props.fsPath, props.sessionId)
    return decodeText(buffer, contentType)
  }
  const res = await fetch(readUrl.value)
  if (!res.ok) throw new Error(`${t('files.previewError')}: ${res.statusText}`)
  const contentType = res.headers.get('Content-Type') || ''
  return decodeText(await res.arrayBuffer(), contentType)
}

// 换取托管签名地址（凭据在路径里，见 sessionsApi.createFSServeUrl）。
// 失败时把原因写进 error 并返回 false。
async function prepareServeUrl(): Promise<boolean> {
  serveUrl.value = ''
  if (usesFsPath.value) {
    try {
      serveUrl.value = await sessionsApi.createFSServeUrl(props.fsPath, props.sessionId)
      return true
    } catch (err: unknown) {
      error.value = err instanceof Error ? err.message : t('files.previewError')
      return false
    }
  }
  // 非工作区文件（props.url 指向 /api/uploads/...）：换一张 uploads 票据。
  // 与 fsPath 一样必须异步——浏览器发不出 Authorization 头，凭据只能签进 URL。
  if (!props.url) return false
  try {
    serveUrl.value = await sessionsApi.resolveAttachmentSrc(props.url)
    return !!serveUrl.value
  } catch (err: unknown) {
    error.value = err instanceof Error ? err.message : t('files.previewError')
    return false
  }
}

// prepareTicketUrls 换取「读取」与「下载」两张票据。
//
// <img src> 与 <a href> 都带不上请求头，票据是它们唯一的凭据，所以必须在对应
// 预览分支动起来之前就绪。工作区文件按 fsPath 签；上传附件走 uploads 票据。
async function prepareTicketUrls(): Promise<boolean> {
  readUrl.value = ''
  downloadUrl.value = ''
  try {
    if (usesFsPath.value) {
      const [read, download] = await Promise.all([
        sessionsApi.getFSReadUrl(props.fsPath, props.sessionId),
        sessionsApi.getFSDownloadUrl(props.fsPath, props.sessionId),
      ])
      readUrl.value = read
      downloadUrl.value = download
    } else if (props.url) {
      const signed = await sessionsApi.resolveAttachmentSrc(props.url)
      readUrl.value = signed
      downloadUrl.value = signed
    }
  } catch (err: unknown) {
    error.value = err instanceof Error ? err.message : t('files.previewError')
    return false
  }
  if (!readUrl.value) {
    // 既没有 fsPath 也没有 url：根本拿不到文件地址。给出可见错误，而不是
    // 留下一个静默的空对话框。
    error.value = t('files.previewError')
    return false
  }
  return true
}

// 每次加载递增。异步过程中若又发起了新的加载，旧请求的结果一律丢弃——
// 否则「先发的请求后返回」会把上一个文件的内容盖到当前文件上。
let loadSeq = 0

async function load() {
  reset()
  const seq = ++loadSeq
  const e = ext.value
  try {
    // 图片/网页/文本三类预览、以及错误态里的下载入口，都要用到票据地址。
    // 统一先换好，免得各分支各自处理异步与失败。
    if (!(await prepareTicketUrls())) return
    if (seq !== loadSeq) return

    if (IMAGE_EXTS.includes(e)) {
      kind.value = 'image'
      return
    }
    if (WEB_EXTS.includes(e)) {
      // 先换到签名地址再渲染 iframe，否则会先闪一次空 src 的空白页
      if (!(await prepareServeUrl())) return
      if (seq !== loadSeq) return
      kind.value = 'web'
      webLoading.value = true
      webError.value = ''
      // 兜底：若 iframe/媒体 load 事件未触发，6 秒后强制结束 loading
      webTimer = setTimeout(() => {
        webLoading.value = false
      }, 6000)
      return
    }
    if (BINARY_EXTS.includes(e)) {
      kind.value = 'binary'
      return
    }
    if (!readUrl.value) {
      error.value = t('files.previewError')
      return
    }

    const text = await readTextBody()
    if (seq !== loadSeq) return
    content.value = text
    originalContent.value = text
    kind.value = 'text'
  } catch (err: unknown) {
    if (seq !== loadSeq) return
    // 二进制（415）降级为占位 + 下载入口，而不是把错误 JSON 当正文渲染出来
    if (err instanceof sessionsApi.FSPreviewError && err.kind === 'binary') {
      kind.value = 'binary'
      content.value = ''
      return
    }
    error.value = err instanceof Error ? err.message : t('files.previewError')
    content.value = ''
  } finally {
    // 旧请求不要顺手把新请求的 loading 关掉
    if (seq === loadSeq) loading.value = false
  }
}

// iframe / 媒体加载成功：结束 loading
function onWebLoaded() {
  clearWebTimer()
  webLoading.value = false
}

// iframe / 媒体加载失败：给出错误提示
function onWebError() {
  clearWebTimer()
  webLoading.value = false
  webError.value = t('files.previewError')
}

// ===== 图片缩放 =====
function onImageLoad(e: Event) {
  const el = e.target as HTMLImageElement
  imageNaturalWidth.value = el.naturalWidth
  imageError.value = false
}

function onImageError() {
  imageError.value = true
}

function zoomBy(factor: number) {
  const next = imageZoom.value * factor
  imageZoom.value = Math.min(IMAGE_ZOOM_MAX, Math.max(IMAGE_ZOOM_MIN, Number(next.toFixed(3))))
}

function resetImageZoom() {
  imageZoom.value = 1
}

// 点击图片在「适应窗口」与 2 倍之间切换
function toggleImageZoom() {
  imageZoom.value = imageZoom.value === 1 ? 2 : 1
}

// Ctrl/Cmd + 滚轮缩放；不按修饰键时不拦截，交给容器正常滚动
function onImageWheel(e: WheelEvent) {
  if (!e.ctrlKey && !e.metaKey) return
  e.preventDefault()
  zoomBy(e.deltaY < 0 ? 1.1 : 1 / 1.1)
}

/**
 * 切换 HTML 的「渲染预览 / 查看源码」。
 *
 * 源码按需读取：HTML 走 iframe 分支，正文从未进过 content，首次切换时才去取
 * （并留在 content 里缓存），否则每预览一个 HTML 都要白读一遍文件。
 * 读失败就不切视图，用提示说明原因（过大 / 接口报错），免得留一个空白框。
 */
async function toggleHtmlSource() {
  if (htmlSource.value) {
    htmlSource.value = false
    return
  }
  if (!content.value) {
    sourceLoading.value = true
    try {
      const text = await readTextBody()
      content.value = text
      originalContent.value = text
    } catch (err: unknown) {
      message.error(err instanceof Error ? err.message : t('files.previewError'))
      return
    } finally {
      sourceLoading.value = false
    }
  }
  htmlSource.value = true
}

// 在独立标签页打开当前预览内容。
//
// 这条路径的隔离程度取决于内容从哪来：
//  - 上传附件：服务端按 inline 应答并带上 sandbox CSP，顶层文档因此跑在不透明
//    源里，读不到 localStorage.auth_token，请求也不带任何凭据（本服务不用
//    cookie），所以「打开看看」不会把账号交出去。
//  - 工作区文件（scope=read/serve）：与主站同源，被打开的页面能读到
//    localStorage。这是本地项目自己的文件，且打开是用户主动点击的动作，
//    所以维持现状；要彻底消除得把预览放到独立源（另一个端口/域名）。
function openInNewTab() {
  const url = openableUrl.value
  if (!url) return
  window.open(url, '_blank', 'noopener,noreferrer')
}

// 切换预览内容全屏（撑满视口）
function toggleFullscreen() {
  fullscreen.value = !fullscreen.value
}

function toggleEdit() {
  if (isEditing.value) {
    if (unsavedChanges.value && !window.confirm(t('chat.discardChanges'))) return
    isEditing.value = false
    editingContent.value = ''
    return
  }
  editingContent.value = content.value
  originalContent.value = content.value
  isEditing.value = true
}

async function save() {
  if (!props.fsPath) return
  saving.value = true
  try {
    await sessionsApi.writeFSFile(
      props.fsPath,
      editingContent.value,
      props.sessionId,
    )
    content.value = editingContent.value
    originalContent.value = editingContent.value
    isEditing.value = false
    message.success(t('common.success'))
    emit('saved')
  } catch (e: unknown) {
    message.error(e instanceof Error ? e.message : t('common.operationFailed'))
  } finally {
    saving.value = false
  }
}

async function copyContent() {
  const text = isEditing.value ? editingContent.value : content.value
  try {
    await navigator.clipboard.writeText(text || '')
    message.success(t('chat.contentCopied'))
  } catch {
    const textarea = document.createElement('textarea')
    textarea.value = text || ''
    document.body.appendChild(textarea)
    textarea.select()
    try {
      document.execCommand('copy')
      message.success(t('chat.contentCopied'))
    } catch {
      message.error(t('common.operationFailed'))
    }
    document.body.removeChild(textarea)
  }
}

function download() {
  const url = downloadUrl.value
  if (!url) return
  const link = document.createElement('a')
  link.href = url
  link.download = props.name
  link.style.display = 'none'
  document.body.appendChild(link)
  link.click()
  document.body.removeChild(link)
  message.success(t('common.downloadStarted'))
}

// 有未保存修改时拦截关闭（naive-ui 的 on-close 是 thenable 守卫：
// 返回 false 即中止关闭；右上角 X 点击、以及 mask/ESC 之外的程序化关闭都会走这里）
function handleCloseAttempt(): boolean {
  if (!unsavedChanges.value) return true
  if (!window.confirm(t('chat.discardChanges'))) return false
  isEditing.value = false
  editingContent.value = ''
  return true
}

// 捕获阶段拦截：全屏下 ESC 先退出全屏；编辑态下 Ctrl/Cmd+S 保存
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && fullscreen.value) {
    e.preventDefault()
    e.stopPropagation()
    fullscreen.value = false
    return
  }
  if ((e.ctrlKey || e.metaKey) && (e.key === 's' || e.key === 'S')) {
    // 只在编辑态接管；否则放行浏览器默认行为
    if (!isEditing.value) return
    e.preventDefault()
    if (canEdit.value && unsavedChanges.value && !saving.value) void save()
  }
}

// 打开/关闭，以及**在弹窗保持打开时切换文件**，都要重新加载。
//
// 这几个来源必须合并在一个 watcher 里：父组件打开预览时是「文件路径 + show」
// 一起变的，分成两个 watcher 会在同一帧触发两次 load——两次并发请求谁先返回
// 不确定，旧文件的内容可能盖到新文件上。合并后 Vue 会批处理成一次触发。
watch(
  () => [props.show, props.fsPath, props.url, props.typeName, props.name],
  () => {
    if (props.show) {
      void load()
    } else {
      fullscreen.value = false
      clearWebTimer()
    }
  },
  { immediate: true },
)

onMounted(() => {
  window.addEventListener('resize', updateIsMobile)
  window.addEventListener('keydown', onKeydown, true)
})

onUnmounted(() => {
  window.removeEventListener('resize', updateIsMobile)
  window.removeEventListener('keydown', onKeydown, true)
  clearWebTimer()
})
</script>

<style scoped>
.preview-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 40px 16px;
  text-align: center;
}

.preview-state-text {
  display: block;
  margin-top: 12px;
}

.preview-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  padding: 12px 16px;
  background: #fafafa;
  border: 1px solid #e8e8e8;
  border-radius: 8px;
  margin-bottom: 12px;
}

.preview-content-container {
  max-height: 60vh;
  overflow: auto;
  border: 1px solid #e8e8e8;
  border-radius: 8px;
}

/* 全屏预览：撑满视口。采用纵向 flex 布局，顶部固定一条退出条，
   预览主体占满剩余空间（否则 100vh 的 iframe 会把退出条顶出视口）。 */
.preview-content-container.preview-fullscreen {
  position: fixed;
  inset: 0;
  z-index: 3000;
  max-height: none;
  overflow: hidden;
  border: none;
  border-radius: 0;
  background: #fff;
  display: flex;
  flex-direction: column;
}

.preview-fullscreen-bar {
  flex: 0 0 auto;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 6px 12px;
  background: #fafafa;
  border-bottom: 1px solid #e8e8e8;
}

.preview-fullscreen-name {
  font-size: 13px;
  font-weight: 600;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

/* 预览主体占满退出条之外的高度 */
.preview-content-container.preview-fullscreen > *:not(.preview-fullscreen-bar) {
  flex: 1 1 auto;
  min-height: 0;
  max-height: none;
  overflow: auto;
}

.preview-content-container.preview-fullscreen .html-preview-container {
  overflow: hidden;
  border: none;
  border-radius: 0;
}
.preview-content-container.preview-fullscreen .html-preview-frame {
  height: 100%;
}
.preview-content-container.preview-fullscreen .markdown-preview-frame {
  height: 100%;
}
.preview-content-container.preview-fullscreen .preview-media-frame {
  height: 100%;
  max-height: 100%;
}
.preview-content-container.preview-fullscreen .preview-image {
  max-height: 100%;
}
.preview-content-container.preview-fullscreen .preview-textarea {
  height: 100%;
  min-height: 0;
  border-radius: 0;
  resize: none;
}
.preview-content-container.preview-fullscreen .binary-preview-wrapper,
.preview-content-container.preview-fullscreen .preview-state {
  display: flex;
  align-items: center;
  justify-content: center;
}

.image-preview-wrapper {
  display: flex;
  justify-content: center;
  align-items: center;
  padding: 16px;
  overflow: auto;
}

/* 放大后内容会超出容器，需要兼顾“居中显示”和“能看全”：
   - 图片比容器小时：margin:auto 会吸收两侧多余空间，把图片居中。
   - 图片比容器大时：auto 边距退化为 0，此时由 flex-start(左上角对齐) 兜底，
     overflow 全部落在正方向，滚动条可访问整张图（若用 justify-content/align-items:
     center 居中，溢出部分会被挤到负坐标、滚动条够不到，看不全）。 */
.image-preview-wrapper--zoomed {
  justify-content: flex-start;
  align-items: flex-start;
}

.image-preview-wrapper--zoomed .preview-image {
  margin: auto;
}

.preview-image {
  max-width: 100%;
  max-height: 60vh;
  border-radius: 4px;
  cursor: zoom-in;
}

.preview-image-zoomed {
  max-width: none;
  max-height: none;
  cursor: zoom-out;
}

/* Markdown 渲染视图（内容在 sandbox="" 的 iframe 里） */
.markdown-preview {
  display: block;
}

.markdown-preview-frame {
  width: 100%;
  height: 60vh;
  border: none;
  display: block;
}

.binary-preview-wrapper {
  display: flex;
  flex-direction: column;
  justify-content: center;
  align-items: center;
  padding: 40px 16px;
  text-align: center;
}

.binary-preview-text {
  display: block;
  margin-top: 16px;
}

.binary-preview-action {
  margin-top: 12px;
}

.html-preview-container {
  position: relative;
  max-height: 60vh;
  overflow: hidden;
  border: 1px solid #e8e8e8;
  border-radius: 8px;
  background: #fff;
}

.web-preview-loading {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 40vh;
}

.web-preview-error {
  display: flex;
  justify-content: center;
  align-items: center;
  min-height: 30vh;
  padding: 24px;
  text-align: center;
}

.html-preview-frame {
  width: 100%;
  height: 60vh;
  border: none;
  display: block;
}

.preview-media-frame {
  width: 100%;
  max-height: 60vh;
  display: block;
  background: #000;
}

.preview-audio-frame {
  width: 100%;
  display: block;
  margin: 24px 0;
}

/* 默认不折行：代码与长行保持原始形态，改用横向滚动。
   原先固定 word-break: break-all 会把 URL、长标识符从中间劈开，
   既难读又无法关闭；现在折行是可选项，且折行时用 break-word
   而不是 break-all，尽量不在词中间断开。 */
.preview-content {
  white-space: pre;
  overflow-x: auto;
  word-break: normal;
  overflow-wrap: normal;
  font-size: 13px;
  line-height: 1.6;
  margin: 0;
  padding: 16px;
  font-family: Consolas, Monaco, 'Andale Mono', 'Ubuntu Mono', monospace;
}

.preview-content.preview-wrap {
  white-space: pre-wrap;
  overflow-x: hidden;
  word-break: break-word;
  overflow-wrap: anywhere;
}

.preview-textarea {
  width: 100%;
  min-height: 400px;
  font-family: Consolas, Monaco, 'Andale Mono', 'Ubuntu Mono', monospace;
  font-size: 13px;
  line-height: 1.6;
  padding: 16px;
  border: none;
  border-radius: 8px;
  resize: vertical;
  box-sizing: border-box;
  outline: none;
}

.preview-textarea:focus {
  box-shadow: inset 0 0 0 2px #2080f0;
}

/* 预览高亮：GitHub Light 配色（全局已加载 github-dark.css，这里是浅色底，
   必须用 :deep 覆盖 token 颜色，避免深色主题文字在浅色背景上不可读） */
.preview-content :deep(.hljs-keyword),
.preview-content :deep(.hljs-selector-tag),
.preview-content :deep(.hljs-doctag) {
  color: #d73a49;
}
.preview-content :deep(.hljs-string),
.preview-content :deep(.hljs-regexp) {
  color: #032f62;
}
.preview-content :deep(.hljs-comment),
.preview-content :deep(.hljs-quote) {
  color: #6a737d;
  font-style: italic;
}
.preview-content :deep(.hljs-number),
.preview-content :deep(.hljs-literal),
.preview-content :deep(.hljs-symbol),
.preview-content :deep(.hljs-bullet) {
  color: #005cc5;
}
.preview-content :deep(.hljs-title),
.preview-content :deep(.hljs-title.function_),
.preview-content :deep(.hljs-title.class_),
.preview-content :deep(.hljs-function) {
  color: #6f42c1;
}
.preview-content :deep(.hljs-attr),
.preview-content :deep(.hljs-attribute),
.preview-content :deep(.hljs-variable),
.preview-content :deep(.hljs-template-variable),
.preview-content :deep(.hljs-name) {
  color: #22863a;
}
.preview-content :deep(.hljs-built_in),
.preview-content :deep(.hljs-type),
.preview-content :deep(.hljs-class),
.preview-content :deep(.hljs-params) {
  color: #e36209;
}
.preview-content :deep(.hljs-meta),
.preview-content :deep(.hljs-link),
.preview-content :deep(.hljs-selector-attr),
.preview-content :deep(.hljs-selector-pseudo),
.preview-content :deep(.hljs-selector-id),
.preview-content :deep(.hljs-selector-class) {
  color: #005cc5;
}
.preview-content :deep(.hljs-section) {
  color: #005cc5;
  font-weight: 600;
}
.preview-content :deep(.hljs-emphasis) {
  font-style: italic;
}
.preview-content :deep(.hljs-strong) {
  font-weight: 600;
}
.preview-content :deep(.hljs-addition) {
  color: #22863a;
  background: #f0fff4;
}
.preview-content :deep(.hljs-deletion) {
  color: #b31d28;
  background: #ffeef0;
}

.unsaved-hint {
  display: flex;
  align-items: center;
  padding: 8px 12px;
  margin-bottom: 12px;
  background: #fffbe6;
  border: 1px solid #ffe58f;
  border-radius: 6px;
}

/* 小屏适配：给工具条留白、压缩内容高度，避免按钮被挤出视口 */
@media (max-width: 768px) {
  .preview-toolbar {
    padding: 8px 10px;
    margin-bottom: 8px;
  }

  .preview-toolbar-group {
    width: 100%;
  }

  .preview-toolbar-group :deep(.n-space) {
    width: 100%;
  }

  .preview-content-container {
    max-height: 66vh;
  }

  .preview-content {
    padding: 10px;
    font-size: 12px;
  }

  .preview-textarea {
    min-height: 50vh;
    padding: 10px;
    font-size: 12px;
  }

  .preview-image {
    max-height: 55vh;
  }

  .html-preview-frame {
    height: 58vh;
  }

  .preview-media-frame {
    max-height: 58vh;
  }

  .preview-audio-frame {
    margin: 12px 0;
  }
}
</style>
