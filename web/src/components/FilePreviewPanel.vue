<template>
  <div ref="panelRoot" class="file-preview-panel" :class="`file-preview-panel--${variant}`">
    <div v-if="loading" class="preview-state" @mousedown="onChromeMouseDown">
      <!-- 加载 / 错误两态没有工具栏（在 preview-ready 里才渲染），但浮窗模式
           下用户仍需要拖动窗口与最小化的入口，因此：
           1) 整块状态区按空白处可拖动（onChromeMouseDown 排除按钮）；
           2) 右上角除了关闭按钮，还渲染调用方注入的 #actions（最小化）。 -->
      <div class="preview-state-top">
        <slot name="actions" />
        <n-button
          class="preview-state-close"
          size="small"
          quaternary
          :title="t('common.close')"
          @click="closeFromToolbar"
        >
          <template #icon><n-icon :component="CloseOutline" /></template>
        </n-button>
      </div>
      <n-spin size="large" />
      <n-text depth="3" class="preview-state-text">Loading...</n-text>
    </div>

    <div v-else-if="error" class="preview-state" @mousedown="onChromeMouseDown">
      <div class="preview-state-top">
        <slot name="actions" />
        <n-button
          class="preview-state-close"
          size="small"
          quaternary
          :title="t('common.close')"
          @click="closeFromToolbar"
        >
          <template #icon><n-icon :component="CloseOutline" /></template>
        </n-button>
      </div>
      <n-text type="error">{{ error }}</n-text>
      <!-- 读取失败（含「超过 2MB 无法预览」）时，用户最需要的还是能拿到文件，
           所以错误态也保留下载出口 -->
      <div class="preview-state-actions">
        <!-- 票据过期、网络抖动、文件刚被写入还没落盘，都可能是一次性的失败；
             错误态里没有工具栏，重试入口只能放在这里。 -->
        <n-button size="small" :loading="refreshing" @click="refresh">
          <template #icon><n-icon :component="ReloadOutline" /></template>
          {{ t('common.refresh') }}
        </n-button>
        <n-button v-if="downloadUrl" size="small" @click="download">
          <template #icon><n-icon :component="DownloadOutline" /></template>
          {{ t('chat.download') }}
        </n-button>
      </div>
    </div>

    <div v-else class="preview-ready">
      <!-- 紧凑头部：一行放下 文件名 + 大小 + 全部操作按钮。
           旧版是「卡片标题 + 带文字工具条」两层堆叠，头部占掉 100px+；
           改成单行后正文可视高度显著提升。按钮一律纯图标 + title 提示。
           浮窗模式下这条工具栏兼作标题栏：按空白处可以拖动整个窗口。 -->
      <div
        class="preview-toolbar"
        :class="{ 'preview-toolbar--drag': variant === 'panel' }"
        @mousedown="onChromeMouseDown"
      >
        <span class="preview-title" :title="name">{{ name }}</span>
        <n-tag v-if="typeof size === 'number'" size="small" type="info">{{ formatSize(size) }}</n-tag>

        <div class="preview-actions">
          <!-- 预览是只读的：这里刻意不提供任何写入口。
               改文件请用编辑器 / agent，预览一旦能写回，误触就会把文件内容冲掉。 -->

          <!-- 视图控制：按当前类型出现，不堆一排永远禁用的按钮 -->
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
            <span class="zoom-label">{{ zoomLabel }}</span>
          </template>
          <n-button
            v-if="(kind === 'text' || htmlSource) && content"
            size="small"
            quaternary
            :title="wrapLines ? t('chat.unwrapLines') : t('chat.wrapLines')"
            @click="wrapLines = !wrapLines"
          >
            <template #icon><n-icon :component="TextOutline" /></template>
          </n-button>
          <n-button
            v-if="isMarkdown && content"
            size="small"
            quaternary
            :title="markdownRendered ? t('chat.markdownSource') : t('chat.markdownPreview')"
            @click="markdownRendered = !markdownRendered"
          >
            <template #icon><n-icon :component="markdownRendered ? CodeSlashOutline : EyeOutline" /></template>
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
          </n-button>
          <n-button
            v-if="openableUrl"
            size="small"
            quaternary
            :title="t('chat.openInNewTab')"
            @click="openInNewTab"
          >
            <template #icon><n-icon :component="OpenOutline" /></template>
          </n-button>

          <span class="toolbar-divider" />

          <!-- 刷新：文件在磁盘上被改过（agent 刚写完、外部编辑器保存、页面依赖的
               数据变了）时，预览不会自己更新——iframe / 图片 / 正文都是加载一次
               就停住。这是重新拉取的唯一入口。 -->
          <n-button
            size="small"
            quaternary
            :loading="refreshing"
            :title="t('common.refresh')"
            @click="refresh"
          >
            <template #icon><n-icon :component="ReloadOutline" /></template>
          </n-button>
          <n-button
            size="small"
            quaternary
            :disabled="!content"
            :title="t('chat.copyContent')"
            @click="copyContent"
          >
            <template #icon><n-icon :component="CopyOutline" /></template>
          </n-button>
          <n-button
            size="small"
            quaternary
            :disabled="!downloadUrl"
            :title="t('chat.download')"
            @click="download"
          >
            <template #icon><n-icon :component="DownloadOutline" /></template>
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
          </n-button>
          <!-- 浮窗/面板额外动作（最小化等）由调用方注入，放在关闭按钮之前 -->
          <slot name="actions" />
          <!-- 卡片标题栏已隐藏，关闭按钮收进工具栏最右侧 -->
          <n-button size="small" quaternary :title="t('common.close')" @click="closeFromToolbar">
            <template #icon><n-icon :component="CloseOutline" /></template>
          </n-button>
        </div>
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

        <div
          v-if="kind === 'image'"
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
            :src="displayImageUrl"
            :alt="name"
            class="preview-image"
            :class="{ 'preview-image-zoomed': imageZoom > 1 }"
            :style="imageDisplayStyle"
            :title="t('chat.zoomHint')"
            @dblclick="resetImageZoom"
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
              :src="frameSrc"
              class="preview-media-frame"
              controls
              playsinline
              @loadeddata="onWebLoaded"
              @error="onWebError"
            ></video>
            <audio
              v-else-if="mediaType === 'audio'"
              :src="frameSrc"
              class="preview-audio-frame"
              controls
              @loadeddata="onWebLoaded"
              @error="onWebError"
            ></audio>
            <iframe
              v-else
              :src="frameSrc"
              class="html-preview-frame"
              :sandbox="frameSandbox"
              title="HTML Preview"
              @load="onWebLoaded"
              @error="onWebError"
            ></iframe>
          </template>
        </div>

        <!-- Office 文档（docx/xlsx/pptx/odt/ods/odp）：Flyfish 渲染器在浏览器端
             解析。渲染失败或解析出错时整体切到错误态（含下载出口）。 -->
        <OfficeFilePreview
          v-else-if="kind === 'office' && officeFile"
          :key="refreshStamp"
          :file="officeFile"
          :filename="officeFile.name"
          :name="name"
          :type="ext"
          @failed="onOfficeFailed"
        />

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
        <div v-else-if="isMarkdown && markdownRendered && content && markdownHtml" class="markdown-preview">
          <iframe
            class="markdown-preview-frame"
            sandbox=""
            :srcdoc="markdownHtml"
            title="Markdown Preview"
          ></iframe>
        </div>

        <!-- 行号栏 + 代码区：行号 sticky 在左侧，横向滚动时保持可见；
             仅不折行时显示（折行会破坏行号与内容行的对齐）。
             行号是**一个**文本节点（整块字符串 + white-space: pre），
             不是每行一个 <span>：2 万行文件曾经一次生成 2 万个 DOM 节点，
             打开预览的那一帧直接把主线程占满，正在流式输出的对话跟着卡住。 -->
        <div v-else-if="content" class="preview-body">
          <div v-if="!wrapLines && lineCount <= MAX_GUTTER_LINES" class="line-gutter" aria-hidden="true">
            <span class="line-gutter-text">{{ lineNumbers }}</span>
          </div>
          <pre
            class="preview-content"
            :class="{ 'preview-wrap': wrapLines }"
            v-html="highlighted"
          ></pre>
        </div>

        <div v-else class="preview-state">
          <n-text depth="3">{{ t('files.noPreview') }}</n-text>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, onUnmounted, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import { NButton, NIcon, NSpin, NTag, NText } from 'naive-ui'
import {
  AddOutline,
  CloseOutline,
  CodeSlashOutline,
  ContractOutline,
  CopyOutline,
  DocumentOutline,
  DownloadOutline,
  ExpandOutline,
  EyeOutline,
  OpenOutline,
  RefreshOutline,
  ReloadOutline,
  RemoveOutline,
  TextOutline,
} from '@vicons/ionicons5'
import { marked } from 'marked'
import * as sessionsApi from '@/api/sessions'

// Office 文档预览（Flyfish 渲染器）按需加载：体积不小，只有真的预览到
// docx/xlsx/pptx 等格式时才拉取这一块代码。
const OfficeFilePreview = defineAsyncComponent(() => import('./OfficeFilePreview.vue'))

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
 * 通用文件预览面板（纯内容，不含任何弹窗外壳）。
 *
 * 两种承载方式：
 *  - FilePreviewDialog：包一层 n-modal，用于 FilesView / BotsView 等独立页面
 *  - ChatView：直接并排挂在对话区右侧，预览时对话照常可用（打字、发送、
 *    看流式输出都不受影响）
 *
 * 支持两种数据来源，二选一：
 *  - fsPath (+ sessionId)：工作区/会话文件，走 sessionsApi 读写与托管
 *  - url                 ：直接可用的地址（如上传文件），内部会补 token
 *
 * 自动按扩展名分派：图片 / HTML·PDF / 视频 / 音频 / 二进制占位 / 文本（带高亮）。
 *
 * 只读：预览不提供写回能力，任何“改文件”的诉求都走编辑器或 agent。
 */
interface Props {
  /** 是否处于展示状态：变为 true 时加载内容，false 时清空定时器与全屏态 */
  active: boolean
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
  /** panel：跟随父容器高度（并排面板）；dialog：沿用弹窗的 85vh 限高 */
  variant?: 'dialog' | 'panel'
}

const props = withDefaults(defineProps<Props>(), {
  name: '',
  typeName: '',
  url: '',
  fsPath: '',
  variant: 'dialog',
})

const emit = defineEmits<{
  (e: 'close'): void
  /** 浮窗模式：工具栏空白处按下，调用方据此开始拖动整个窗口 */
  (e: 'dragstart', ev: MouseEvent): void
}>()

const { t } = useI18n()
const message = useMessage()

// 面板根节点：滚轮隔离监听挂在这里（覆盖工具栏 + 正文，两种承载方式共用）
const panelRoot = ref<HTMLElement | null>(null)
let wheelHost: HTMLElement | null = null

// 行号只渲染到这个行数；再多就没必要看行号了（也避免生成几万行的字符串）
const MAX_GUTTER_LINES = 20000

type Kind = 'none' | 'text' | 'image' | 'web' | 'office' | 'binary'

// 图片：浏览器可直接渲染（ico 归入图片，与既有行为一致）
const IMAGE_EXTS = ['png', 'jpg', 'jpeg', 'gif', 'bmp', 'webp', 'svg', 'ico']
// 浏览器原生可预览：HTML / PDF / 视频 / 音频，走托管地址由 iframe 或原生控件加载
const WEB_EXTS = ['html', 'htm', 'pdf', 'mp4', 'webm', 'mov', 'm4v', 'mpg', 'mpeg', 'ogv', 'mp3', 'wav', 'm4a', 'flac', 'aac', 'oga', 'ogg']
// Office Open XML / OpenDocument：交给 Flyfish 渲染器在浏览器端解析预览。
// 老 .doc/.xls/.ppt 二进制格式不在其中——保真度差（.ppt 公开版还带水印），
// 保持走 binary 分支给出明确的下载出口更诚实。
const OFFICE_EXTS = ['docx', 'xlsx', 'pptx', 'odt', 'ods', 'odp']
// Office 预览的体积护栏：渲染器在前端解析整个文件，几十 MB 的表格会拖垮
// 标签页。超过这个值直接提示下载，而不是让用户对着白屏等崩溃。
const MAX_OFFICE_PREVIEW_BYTES = 64 * 1024 * 1024
// 无法在浏览器渲染、仅提供下载的类型
const BINARY_EXTS = ['exe', 'dll', 'so', 'dylib', 'zip', 'rar', '7z', 'tar', 'gz', 'bz2', 'xz', 'doc', 'xls', 'ppt', 'avi', 'mkv', 'wmv', 'woff', 'woff2', 'ttf', 'eot', 'class', 'jar', 'war', 'pyc', 'o', 'a', 'lib', 'bin', 'dat', 'db', 'sqlite', 'wasm']

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

// 高亮预算：超过这个字符数直接转义输出纯文本。highlight.js 是同步的，
// 500KB 的文件在 highlightAuto 下能占住主线程 1 秒以上——期间正在流式输出的
// 对话会明显掉帧，这正是「预览卡住对话」的主因之一。
const MAX_HIGHLIGHT_CHARS = 120_000
// 未知扩展名才做自动语言探测，且只在很小的文件上做（探测要遍历全部语言）
const MAX_AUTODETECT_CHARS = 48_000
// Markdown 渲染（marked 同步解析）的大文档阈值，超过就走源码视图
const MAX_MARKDOWN_RENDER_CHARS = 300_000

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
const kind = ref<Kind>('none')
// office 分支的完整二进制：Flyfish 渲染器需要整个文件，不能走 2MB 上限的
// 正文读取接口，用下载票据拉 blob 后交给 OfficeFilePreview。
const officeFile = ref<File | null>(null)
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
// 折行与 Markdown 渲染视图是用户的阅读偏好，跨文件、跨会话记忆下来：
// 偏好存在 localStorage，load() 时的 reset() 不再覆盖这两个值。
const PREFS_STORAGE_KEY = 'file-preview-prefs'
interface PreviewPrefs {
  wrapLines?: boolean
  markdownRendered?: boolean
}
function loadPreviewPrefs(): PreviewPrefs {
  try {
    return JSON.parse(localStorage.getItem(PREFS_STORAGE_KEY) || '{}') as PreviewPrefs
  } catch {
    return {}
  }
}
function persistPreviewPrefs(p: PreviewPrefs) {
  try {
    localStorage.setItem(PREFS_STORAGE_KEY, JSON.stringify({ ...loadPreviewPrefs(), ...p }))
  } catch {
    // 隐私模式等写不进去就算了，偏好记忆是锦上添花
  }
}
const savedPrefs = loadPreviewPrefs()
const wrapLines = ref(savedPrefs.wrapLines === true)
// Markdown：默认看源码（带高亮），可切到渲染视图（记住上次的选择）
const markdownRendered = ref(savedPrefs.markdownRendered === true)
watch([wrapLines, markdownRendered], ([w, m]) => {
  persistPreviewPrefs({ wrapLines: w, markdownRendered: m })
})
// HTML：默认渲染 iframe（上传的 HTML 也照样渲染，服务端现在按 inline 应答，
// 隔离由响应 CSP 的 sandbox 承担），可切到源码视图（见 toggleHtmlSource）
const htmlSource = ref(false)
const sourceLoading = ref(false)

const ext = computed(() => (props.typeName || props.name || '').split('.').pop()?.toLowerCase() || '')

const usesFsPath = computed(() => !!props.fsPath)

// 三种地址：读取正文 / iframe·媒体托管 / 下载。
//
// 三者现在都是服务端签发的**票据**，换取需要一次带 Authorization 头的 fetch，
// 因此都改成 ref + 异步填充，不能再是 computed。
const readUrl = ref('')
const serveUrl = ref('')
const downloadUrl = ref('')

const imageUrl = computed(() => readUrl.value)

// ===== 刷新 =====
// 文件在磁盘上被改过（agent 刚写完、外部编辑器保存、页面依赖的数据变了）时，
// 预览不会自己更新：iframe / <img> / 正文都只加载一次。刷新 = 重新换票据 +
// 重新加载。
const refreshing = ref(false)
// 票据地址刷新前后常常一模一样，仅靠 :src 变化不会触发重新导航（浏览器还会
// 直接命中 HTTP 缓存）；追一个查询参数才能真正打穿缓存——服务端只按路径里的
// 签名校验，未知 query 一律忽略，所以这个参数是安全的。
const refreshStamp = ref(0)
function withStamp(url: string): string {
  if (!url || !refreshStamp.value) return url
  return `${url}${url.includes('?') ? '&' : '?'}_r=${refreshStamp.value}`
}
// iframe / 音视频 / 图片都走带戳的地址
const frameSrc = computed(() => withStamp(serveUrl.value))
const displayImageUrl = computed(() => withStamp(imageUrl.value))

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

// marked 不做消毒，因此渲染结果放进 sandbox="" 的 iframe（禁止脚本执行）。
//
// 不再用 computed 同步产出：marked.parse 是同步的，长文档会在真正需要渲染的
// 那一刻卡住主线程（比如刚打开预览、或从源码视图切过来）。这里改成 watcher +
// 短延迟，把解析放到当前渲染帧之后，并记录序号丢弃过期结果。
const markdownHtml = ref('')
let markdownTimer: ReturnType<typeof setTimeout> | null = null
let markdownSeq = 0
function scheduleMarkdownRender() {
  if (markdownTimer) {
    clearTimeout(markdownTimer)
    markdownTimer = null
  }
  const text = content.value || ''
  // 超大文档不渲染：marked 是同步的，几百 KB 的 Markdown 一次性解析会把
  // 主线程按住，正在流式输出的对话立刻掉帧。这里直接留空，模板会落回源码视图
  // （不改用户的「渲染视图」偏好，换回小文件仍然按偏好渲染）。
  if (!markdownRendered.value || !text || text.length > MAX_MARKDOWN_RENDER_CHARS) {
    markdownSeq++
    markdownHtml.value = ''
    return
  }
  markdownSeq++
  const seq = markdownSeq
  markdownTimer = setTimeout(() => {
    markdownTimer = null
    if (seq !== markdownSeq) return
    const body = marked.parse(text, { async: false }) as string
    if (seq !== markdownSeq) return
    markdownHtml.value = `<!doctype html><html><head><meta charset="utf-8"><style>${MARKDOWN_PREVIEW_CSS}</style></head><body>${body}</body></html>`
  }, 60)
}
watch([content, markdownRendered], scheduleMarkdownRender, { immediate: true })

const mediaType = computed<'video' | 'audio' | 'none'>(() => {
  if (VIDEO_EXTS.includes(ext.value)) return 'video'
  if (AUDIO_EXTS.includes(ext.value)) return 'audio'
  return 'none'
})

// 行号数量（按换行切分；空文件按 1 行算）
const lineCount = computed(() => (content.value ? content.value.split('\n').length : 0))

// 整块行号字符串：单个文本节点 + white-space: pre，行高与 pre 一致即自然对齐。
// 比每行一个 <span> 少掉几万个 DOM 节点，长文件的打开速度差别非常明显。
const lineNumbers = computed(() => {
  const n = lineCount.value
  if (!n || n > MAX_GUTTER_LINES) return ''
  const out = new Array(n)
  for (let i = 0; i < n; i++) out[i] = i + 1
  return out.join('\n')
})

// 图片缩放比例显示（120% 这种整数最直观）
const zoomLabel = computed(() => `${Math.round(imageZoom.value * 100)}%`)

function formatSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  if (bytes < 1024 * 1024 * 1024) return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
  return (bytes / (1024 * 1024 * 1024)).toFixed(1) + ' GB'
}

function escapeHtml(s: string): string {
  return s.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}

// 预览高亮：按扩展名选语言，未识别时（且文件很小）才自动探测；
// 超长文件直接跳过高亮（避免主线程被占住），异常时回退纯转义文本。
const highlighted = computed(() => {
  const text = content.value
  if (!text) return ''
  if (text.length > MAX_HIGHLIGHT_CHARS) return escapeHtml(text)
  const lang = extLanguageMap[ext.value]
  try {
    if (lang && hljs.getLanguage(lang)) {
      return hljs.highlight(text, { language: lang, ignoreIllegals: true }).value
    }
    // highlightAuto 会遍历全部已注册语言，成本随文本长度超线性增长，
    // 只在「不知道是什么语言 + 文件很小」时才值得
    if (text.length <= MAX_AUTODETECT_CHARS) {
      return hljs.highlightAuto(text).value
    }
    return escapeHtml(text)
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
  kind.value = 'none'
  officeFile.value = null
  fullscreen.value = false
  webLoading.value = false
  webError.value = ''
  serveUrl.value = ''
  imageError.value = false
  imageZoom.value = 1
  imageNaturalWidth.value = 0
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
//
// 注意：拿到新地址后才赋值，绝不能入口就 serveUrl.value = ''。刷新走的正是
// 这条路径，清空会让 <video>/<audio> 的 src 变成空串，浏览器立刻抛 media
// error → onWebError → 模板切到错误态并卸载视频元素，结果就是「视频刷新一次
// 就变成预览文件失败」，而且错误态里视频已不存在，再刷新也恢复不了。
async function prepareServeUrl(): Promise<boolean> {
  let next = ''
  if (usesFsPath.value) {
    try {
      next = await sessionsApi.createFSServeUrl(props.fsPath, props.sessionId)
    } catch (err: unknown) {
      error.value = err instanceof Error ? err.message : t('files.previewError')
      return false
    }
  } else if (props.url) {
    // 非工作区文件（props.url 指向 /api/uploads/...）：换一张 uploads 票据。
    // 与 fsPath 一样必须异步——浏览器发不出 Authorization 头，凭据只能签进 URL。
    try {
      next = await sessionsApi.resolveAttachmentSrc(props.url)
    } catch (err: unknown) {
      error.value = err instanceof Error ? err.message : t('files.previewError')
      return false
    }
  }
  if (!next) return false
  serveUrl.value = next
  return true
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
    if (OFFICE_EXTS.includes(e)) {
      if (typeof props.size === 'number' && props.size > MAX_OFFICE_PREVIEW_BYTES) {
        error.value = t('chat.tooLargeToPreview', { size: formatSize(MAX_OFFICE_PREVIEW_BYTES) })
        return
      }
      // 渲染器要整个文件：下载票据没有 2MB 限制，一次拉全量
      const res = await fetch(downloadUrl.value)
      if (seq !== loadSeq) return
      if (!res.ok) {
        error.value = `${t('files.previewError')}: ${res.statusText}`
        return
      }
      const blob = await res.blob()
      if (seq !== loadSeq) return
      officeFile.value = new File([blob], props.name || props.typeName || `file.${e}`, {
        type: blob.type || 'application/octet-stream',
      })
      kind.value = 'office'
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

// iframe / 媒体加载失败：给出错误提示。
// src 为空（例如刷新过程中地址暂时不可用）不算失败——空 src 一定会触发
// media error，把它当错误会把预览推进无法自愈的错误态。
function onWebError() {
  if (!serveUrl.value) return
  clearWebTimer()
  webLoading.value = false
  webError.value = t('files.previewError')
}

// Office 渲染失败：切到错误态。error 分支自带下载出口，用户不至于卡死。
function onOfficeFailed(message: string) {
  error.value = message || t('files.previewError')
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

// 浮窗模式的拖动句柄：工具栏空白处与 loading/error 状态区空白处按下，
// 都通知父组件拖动整个窗口。
// 排除按钮/输入类元素，否则点"下载""刷新"会连带把窗口拖走。
function onChromeMouseDown(e: MouseEvent) {
  if (props.variant !== 'panel' || e.button !== 0) return
  const el = e.target as HTMLElement | null
  if (el?.closest('button, a, input, textarea, select, .n-base-selection, .n-tag')) return
  emit('dragstart', e)
}

// 刷新当前预览。
// 预览只读，没有“未保存改动”要保护，直接重读即可。
async function refresh() {
  if (refreshing.value) return
  await doRefresh()
}

async function doRefresh() {
  refreshing.value = true
  try {
    // HTML 源码视图：只重读正文，保持视图不跳。
    // 走 load() 会把 htmlSource 重置成渲染视图，用户视角上等于「刷新把我的视图切走了」。
    if (isHtmlDoc.value && htmlSource.value) {
      const text = await readTextBody()
      content.value = text
      return
    }
    // 网页/媒体：只重签托管地址，不闪加载态。
    // 旧文档留在 iframe 里直到新文档加载完成，视觉上是一次原地刷新。
    // 音视频一旦进过错误态，元素已被 v-if 摘掉，刷新成功必须显式清掉
    // webError 才能重新挂载（否则停在「预览文件失败」不动）。
    if (kind.value === 'web') {
      const ok = await prepareServeUrl()
      if (!ok) return
      // 先换戳再清错误：错误态下视频元素是随后才重新挂载的，
      // 这样它一挂出来就是最终地址，不会先按旧地址加载一次再跳。
      refreshStamp.value = Date.now()
      webError.value = ''
      webLoading.value = false
      return
    }
    // 文本/图片/Office：走完整加载流程（重新换票据、重新读正文/二进制）
    await load()
    if (error.value) return
    refreshStamp.value = Date.now()
  } catch (err: unknown) {
    message.error(err instanceof Error ? err.message : t('files.previewError'))
  } finally {
    refreshing.value = false
  }
}

async function copyContent() {
  const text = content.value
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

// 工具栏上的关闭按钮
function closeFromToolbar() {
  emit('close')
}

// 捕获阶段拦截：全屏下 ESC 先退出全屏（iframe 里的 ESC 冒不到父页）
function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape' && fullscreen.value) {
    e.preventDefault()
    e.stopPropagation()
    fullscreen.value = false
  }
}

/**
 * 判断元素是否还能沿本次滚轮的主方向继续滚动。
 * 只看真正可滚的容器（overflow auto/scroll），且必须还没到边界；
 * overflow:hidden 的渲染器画布不算——它是虚拟滚动，不靠浏览器默认行为。
 */
function canScrollInDirection(el: HTMLElement, e: WheelEvent): boolean {
  const vertical = Math.abs(e.deltaY) >= Math.abs(e.deltaX)
  // 先读滚动尺寸再读 computed style：滚轮很密集，绝大多数节点在这一步
  // 就排除了，避免每一下滚轮都对路径上十几个节点做样式解析。
  if (vertical) {
    if (e.deltaY === 0 || el.scrollHeight <= el.clientHeight + 1) return false
    if (!/(auto|scroll|overlay)/.test(getComputedStyle(el).overflowY)) return false
    return e.deltaY > 0
      ? el.scrollTop + el.clientHeight < el.scrollHeight - 1
      : el.scrollTop > 1
  }
  if (e.deltaX === 0 || el.scrollWidth <= el.clientWidth + 1) return false
  if (!/(auto|scroll|overlay)/.test(getComputedStyle(el).overflowX)) return false
  return e.deltaX > 0
    ? el.scrollLeft + el.clientWidth < el.scrollWidth - 1
    : el.scrollLeft > 1
}

/**
 * 滚轮只在预览内部消化，不许外溢到对话区。
 *
 * 复现（xlsx 最明显）：e-virt-table 的虚拟表格自己按 deltaY 改 scrollY，
 * 但只有「表格还有余量」时才 preventDefault；滚到顶/底、或表格压根没有
 * 滚动条时，这一下 wheel 的默认行为会沿滚动链落到外层容器，把对话一起
 * 带着滚——所以是「有时候」才出现。图片/文本预览滚到边界同理。
 *
 * 在面板根节点用捕获阶段兜底：从事件目标沿 composedPath 往上找还有余量的
 * 滚动容器（渲染器挂在 shadow root 里，普通冒泡路径看不到内部节点）；只要
 * 预览自己还能滚就放行，全都滚不动了就掐掉默认行为。
 *
 * 绝不能 stopPropagation：表格的 wheel 监听还要收事件，否则表格反而不滚了。
 * Ctrl/Cmd + 滚轮是 FileViewer 的缩放手势（它会检查 defaultPrevented），放行。
 */
function onPanelWheel(e: WheelEvent) {
  if (e.ctrlKey || e.metaKey || (e.deltaY === 0 && e.deltaX === 0)) return
  const root = panelRoot.value
  if (!root) return
  for (const node of e.composedPath()) {
    if (node === root) break
    // 内部还能滚的容器（正文、放大后的图片、表格标签栏…）优先自己滚
    if (node instanceof HTMLElement && canScrollInDirection(node, e)) return
  }
  e.preventDefault()
}

// 打开/关闭，以及**在面板保持打开时切换文件**，都要重新加载。
//
// 这几个来源必须合并在一个 watcher 里：父组件打开预览时是「文件路径 + active」
// 一起变的，分成两个 watcher 会在同一帧触发两次 load——两次并发请求谁先返回
// 不确定，旧文件的内容可能盖到新文件上。合并后 Vue 会批处理成一次触发。
watch(
  () => [props.active, props.fsPath, props.url, props.typeName, props.name],
  () => {
    if (props.active) {
      void load()
    } else {
      fullscreen.value = false
      clearWebTimer()
    }
  },
  { immediate: true },
)

onMounted(() => {
  window.addEventListener('keydown', onKeydown, true)
  // 捕获阶段 + 显式非 passive：必须早于渲染器的 wheel 监听拿到事件，
  // 且要能 preventDefault（passive 监听里调用会被浏览器忽略）。
  wheelHost = panelRoot.value
  wheelHost?.addEventListener('wheel', onPanelWheel, { capture: true, passive: false })
})

onUnmounted(() => {
  window.removeEventListener('keydown', onKeydown, true)
  wheelHost?.removeEventListener('wheel', onPanelWheel, true)
  wheelHost = null
  clearWebTimer()
  if (markdownTimer) {
    clearTimeout(markdownTimer)
    markdownTimer = null
  }
})
</script>

<style scoped>
/* 面板根节点：两种承载方式共用一个根类。
   - dialog：无高度约束，尺寸由 n-modal 卡片决定
   - panel ：纵向撑满父容器，正文自己滚 */
.file-preview-panel {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.file-preview-panel--panel {
  height: 100%;
}

.preview-ready {
  display: flex;
  flex-direction: column;
  min-height: 0;
}

.file-preview-panel--panel .preview-ready {
  flex: 1 1 auto;
}

.preview-state {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 40px 16px;
  text-align: center;
}

/* 无工具栏的状态（加载中 / 读取失败）：右上角一行放 调用方注入的动作
   （浮窗模式下的最小化按钮）+ 关闭按钮 */
.preview-state-top {
  position: absolute;
  top: 4px;
  right: 4px;
  display: flex;
  align-items: center;
  gap: 2px;
}

/* 关闭按钮在行内布局里取消自身的绝对定位（保留类是为了独立复用时的兜底样式） */
.preview-state-top .preview-state-close {
  position: static;
}

/* 独立使用时（无 preview-state-top 包裹）仍钉在右上角 */
.preview-state-close {
  position: absolute;
  top: 4px;
  right: 4px;
}

/* 浮窗模式：状态区空白处可拖动窗口，光标与工具栏拖动区保持一致 */
.file-preview-panel--panel .preview-state {
  cursor: grab;
}

.file-preview-panel--panel .preview-state:active {
  cursor: grabbing;
}

.preview-state-text {
  display: block;
  margin-top: 12px;
}

/* 错误态底部动作区：重试 + 下载并排，窄面板下换行也不挤 */
.preview-state-actions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 8px;
  margin-top: 12px;
}

/* 紧凑头部：文件名 + 大小徽标 + 图标按钮组一行放下 */
.preview-toolbar {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 8px;
  padding: 6px 8px;
  margin-bottom: 8px;
  border-bottom: 1px solid #e8e8e8;
}

/* 浮窗模式：整条工具栏可拖动。按钮自己会覆盖回 pointer，不影响点击 */
.preview-toolbar--drag {
  cursor: grab;
  user-select: none;
}

.preview-toolbar--drag:active {
  cursor: grabbing;
}

.preview-toolbar--drag .preview-actions {
  cursor: default;
}

.preview-title {
  flex: 0 1 auto;
  min-width: 0;
  max-width: 40%;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
  font-size: 14px;
  font-weight: 600;
}

.preview-actions {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 2px;
  margin-left: auto;
}

.toolbar-divider {
  width: 1px;
  height: 16px;
  margin: 0 4px;
  background: #e0e0e0;
}

/* 弹窗里：卡片被 .modal-scroll 限高 85vh，正文必须再让出「卡片内边距 39px（上 19 + 下 20）
   + 工具栏 49px（28 按钮 + 上下 14 内边距 + 1 边框 + 8 下间距）」。
   原来只减 60px，总和是 85vh + 28px，长文本撑满时会超出卡片，
   卡片跟着冒出第二根滚动条——就是「只多几十像素、只有长文本才触发」那根。
   92 是 88 再留 4px 余量，防止字体渲染差异。 */
.file-preview-panel--dialog .preview-content-container {
  max-height: calc(85vh - 92px);
}

/* 并排面板里：高度由父容器给，正文吃掉剩余空间后自己滚。
   这里绝不能再写 max-height，否则面板比 85vh 高时正文下方会留一大块空白。 */
.file-preview-panel--panel .preview-content-container {
  flex: 1 1 auto;
  min-height: 0;
  max-height: none;
}

.preview-content-container {
  overflow: auto;
  /* 滚到边界时不再把滚动链交还给外层（对话/弹窗卡片）。
     与 onPanelWheel 的 JS 兜底互为保险：CSS 管原生滚动链，JS 管
     xlsx 这类虚拟滚动「根本没滚动容器」时直接落到外层的 wheel 默认行为。 */
  overscroll-behavior: contain;
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
/* 与下方 panel 模式同理：必须用 :not() 排除放大态。放大时宽度由内联样式
   按比例定死，max-height 再钳高度会破坏宽高比（全屏放大变形就是这条漏了
   排除导致的——3 类权重压过 .preview-image-zoomed 的 max-height: none）。 */
.preview-content-container.preview-fullscreen .preview-image:not(.preview-image-zoomed) {
  max-height: 100%;
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
  max-height: 70vh;
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
  height: 70vh;
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
  max-height: 70vh;
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
  height: 70vh;
  border: none;
  display: block;
}

.preview-media-frame {
  width: 100%;
  max-height: 70vh;
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
/* 行号 + 代码并排：外层横向滚动由 .preview-content-container 承担，
   行号 sticky 钉在左缘，横向滚动时始终可见 */
.preview-body {
  display: flex;
  align-items: stretch;
  min-width: fit-content;
}

.line-gutter {
  position: sticky;
  left: 0;
  z-index: 1;
  flex: 0 0 auto;
  min-width: 3.5em;
  padding: 16px 10px;
  text-align: right;
  color: #a8abb0;
  background: #fafafa;
  border-right: 1px solid #e8e8e8;
  font-family: Consolas, Monaco, 'Andale Mono', 'Ubuntu Mono', monospace;
  font-size: 13px;
  line-height: 1.6;
  user-select: none;
}

/* 整块行号：单个文本节点 + pre 换行，行高必须与 .preview-content 完全一致
   （1.6 / 13px），否则行号会和代码逐行错位。 */
.line-gutter-text {
  display: block;
  white-space: pre;
  font-variant-numeric: tabular-nums;
}

/* 有行号时代码区贴住行号栏；pre 不得收缩，长行才能撑出横向滚动 */
.preview-body .preview-content {
  flex: 1 0 auto;
  padding-left: 12px;
}

.zoom-label {
  display: inline-flex;
  align-items: center;
  min-width: 44px;
  justify-content: center;
  font-size: 12px;
  color: #666;
  font-variant-numeric: tabular-nums;
}

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

/* 浮窗/面板：媒体与网页预览按**容器**高度铺开，而不是按视口的 70vh。
   关键在于容器自身必须定死高度（height: 100% 或绝对定位）：
   父级 .preview-content-container 在 panel 模式下是 flex 撑开的定高块，
   百分比可以解析；而只给 iframe 写 100%、父容器还是 auto 高度时，
   百分比退化成 auto，iframe 缩回浏览器默认的 150px —— 表现就是
   「只显示页头一条、下面全是空白」。这里绝不能再写 flex: 1 1 auto
   了事：这些容器的父级不是 flex 容器，那条声明是空操作。 */
.file-preview-panel--panel .html-preview-container,
.file-preview-panel--panel .markdown-preview {
  height: 100%;
  min-height: 0;
  max-height: none;
  box-sizing: border-box;
}
.file-preview-panel--panel .html-preview-frame,
.file-preview-panel--panel .markdown-preview-frame {
  height: 100%;
}
.file-preview-panel--panel .preview-media-frame {
  max-height: 100%;
  height: 100%;
}
/* 图片：容器定高后让图片在剩余空间里居中，而不是按 70vh 顶在顶部 */
.file-preview-panel--panel .image-preview-wrapper {
  height: 100%;
  min-height: 0;
  max-height: none;
}
/* 只约束未放大状态：放大后图片必须能超出容器、由外层滚动查看。
   不加 :not() 的话，这条（权重 0,3,0）会压掉 .preview-image-zoomed 的
   max-height: none（0,2,0），放大后图片还是被裁在容器高度里。 */
.file-preview-panel--panel .preview-image:not(.preview-image-zoomed) {
  max-height: 100%;
}
/* 加载中/失败态在浮窗里也铺满，避免只占左上角一小块 */
.file-preview-panel--panel .web-preview-loading,
.file-preview-panel--panel .web-preview-error {
  height: 100%;
  min-height: 0;
}

/* 小屏适配：文件名放宽占满整行，按钮组折行；内容高度加大 */
@media (max-width: 768px) {
  .preview-toolbar {
    padding: 4px 4px;
    margin-bottom: 6px;
  }

  .preview-title {
    max-width: 100%;
    flex-basis: 100%;
  }

  .preview-actions {
    width: 100%;
    margin-left: 0;
  }

  .file-preview-panel--dialog .preview-content-container {
    max-height: 66vh;
  }

  .preview-content {
    padding: 10px;
    font-size: 12px;
  }

  .line-gutter {
    padding: 10px 6px;
    font-size: 12px;
  }

  .preview-body .preview-content {
    padding-left: 8px;
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
