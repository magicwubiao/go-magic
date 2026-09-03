<template>
  <!-- 单个工具调用：简洁可折叠卡片。 -->
  <!-- 头部：状态徽标 + 工具名 + 成败文字 + 耗时 + 折叠箭头（始终可见成败）。 -->
  <!-- 展开体：仅参数预览（命令/路径/JSON），不再展示执行结果。 -->
  <div class="tool-call-card" :class="`status-${statusClass}`">
    <button
      class="tool-call-header"
      type="button"
      @click="expanded = !expanded"
      :aria-expanded="expanded"
    >
      <span class="tool-call-icon">
        <span v-if="tool.status === 'running'" class="tool-call-spinner" aria-label="运行中"></span>
        <svg v-else-if="isSuccess" class="tool-call-svg" viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
          <circle cx="8" cy="8" r="7" fill="none" stroke="currentColor" stroke-width="1.4" />
          <path d="M4.5 8.5l2.5 2.5 4.5-5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
        <svg v-else-if="isError" class="tool-call-svg" viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
          <circle cx="8" cy="8" r="7" fill="none" stroke="currentColor" stroke-width="1.4" />
          <path d="M5.5 5.5l5 5M10.5 5.5l-5 5" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" />
        </svg>
        <svg v-else class="tool-call-svg" viewBox="0 0 16 16" width="13" height="13" aria-hidden="true">
          <circle cx="8" cy="8" r="7" fill="none" stroke="currentColor" stroke-width="1.4" />
          <circle cx="8" cy="8" r="2.5" fill="currentColor" />
        </svg>
      </span>
      <span class="tool-call-name">{{ toolLabel }}</span>
      <span class="tool-call-status-text" :class="statusClass">{{ statusLabel }}</span>
      <span v-if="tool.duration" class="tool-call-duration">{{ tool.duration }}</span>
      <span class="tool-call-chevron" :class="{ open: expanded }">
        <svg viewBox="0 0 16 16" width="11" height="11" aria-hidden="true">
          <path d="M4 6l4 4 4-4" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </span>
    </button>
    <!-- 参数区：仅当有可展示参数时渲染；n-collapse-transition 保证展开/收起平滑不跳版。
         展开体本身高度有限（见 .tool-call-code max-height），超长内容内部滚动。 -->
    <n-collapse-transition v-if="argsPreview" :show="expanded">
      <div class="tool-call-body">
        <div class="tool-call-section-head">
          <span class="tool-call-section-lang">{{ argsLang }}</span>
          <span class="tool-call-section-label">{{ t('chat.toolCallArgs') }}</span>
        </div>
        <pre class="tool-call-code"><code :class="`language-${argsLang}`">{{ argsPreview }}</code></pre>
      </div>
    </n-collapse-transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { ToolCallEvent } from '@/stores/chat'

const props = defineProps<{
  tool: ToolCallEvent
}>()

const { t } = useI18n()

// 默认折叠。用户手动 toggle 后保持该状态——不再因 status 变化被自动改回，
// 避免"刚折叠又被自动展开"造成的交互失灵感。
const expanded = ref(false)

const isSuccess = computed(
  () => props.tool.status !== 'running' && props.tool.status !== 'error' && props.tool.success !== false
)
const isError = computed(
  () => props.tool.status === 'error' || props.tool.success === false
)

const statusClass = computed(() => {
  if (props.tool.status === 'running') return 'running'
  if (isError.value) return 'error'
  return 'success'
})

const statusLabel = computed(() => {
  if (props.tool.status === 'running') return t('chat.runRunning')
  if (isError.value) return t('chat.runFailed')
  return t('chat.runSuccess')
})

// 工具中文标签
const TOOL_LABELS: Record<string, string> = {
  bash: '运行命令',
  shell: '运行命令',
  exec: '运行命令',
  Read: '读取文件',
  read_file: '读取文件',
  Write: '写入文件',
  write_file: '写入文件',
  Edit: '编辑文件',
  edit_file: '编辑文件',
  MultiEdit: '批量编辑',
  Glob: '搜索文件',
  glob_files: '搜索文件',
  Grep: '搜索内容',
  grep_files: '搜索内容',
  WebFetch: '抓取网页',
  web_fetch: '抓取网页',
  WebSearch: '搜索网页',
  web_search: '搜索网页',
  TodoWrite: '更新待办',
  todo_write: '更新待办',
  ListDir: '列出目录',
  list_dir: '列出目录',
}

const toolLabel = computed(() => {
  const name = props.tool.name || ''
  return TOOL_LABELS[name] || name || t('chat.toolCall')
})

// ---- 解析 args ----
function tryParseArgs(raw: string): Record<string, unknown> | null {
  if (!raw) return null
  try {
    const v = JSON.parse(raw)
    return v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : null
  } catch {
    return null
  }
}

function pickMainArg(parsed: Record<string, unknown> | null, raw: string): { text: string; lang: string } {
  if (parsed) {
    if (typeof parsed.command === 'string') return { text: parsed.command, lang: 'bash' }
    if (typeof parsed.cmd === 'string') return { text: parsed.cmd, lang: 'bash' }
    const path = (parsed.file_path ?? parsed.path ?? parsed.filepath ?? parsed.file) as string | undefined
    const pattern = parsed.pattern as string | undefined
    if (path && pattern) return { text: `${path}\n${pattern}`, lang: 'text' }
    if (path) return { text: String(path), lang: 'text' }
    if (pattern) return { text: String(pattern), lang: 'text' }
    if (typeof parsed.old_string === 'string' || typeof parsed.new_string === 'string') {
      const o = parsed.old_string ?? ''
      const n = parsed.new_string ?? ''
      return { text: `- ${String(o)}\n+ ${String(n)}`, lang: 'diff' }
    }
    try {
      return { text: JSON.stringify(parsed, null, 2), lang: 'json' }
    } catch {
      /* fallthrough */
    }
  }
  return { text: raw, lang: 'text' }
}

// ---- 截断保护 ----
// 工具 args 里常带大 payload（Write 整文件内容、批量工具的长数组等），
// 全量展示会把卡片撑得极长、页面跳动。超过上限即截断并标注。
const MAX_PREVIEW_CHARS = 1200

function truncatePreview(text: string): string {
  if (!text || text.length <= MAX_PREVIEW_CHARS) return text
  const head = text.slice(0, MAX_PREVIEW_CHARS)
  const nl = head.lastIndexOf('\n')
  const cut = nl > MAX_PREVIEW_CHARS * 0.6 ? nl : MAX_PREVIEW_CHARS
  return `${head.slice(0, cut)}\n… ${t('chat.toolCallTruncated')} (${text.length} chars)`
}

const argsView = computed(() => {
  const raw = props.tool.args || ''
  const parsed = tryParseArgs(raw)
  const { text, lang } = pickMainArg(parsed, raw)
  return { lang: lang || 'text', text: truncatePreview(text) }
})

const argsPreview = computed(() => argsView.value.text)
const argsLang = computed(() => argsView.value.lang)
</script>

<style scoped>
.tool-call-card {
  border-radius: 6px;
  background: #f7f8fa;
  border: 1px solid #ececec;
  margin: 4px 0;
  overflow: hidden;
  transition: border-color 0.15s, background 0.15s;
}
.tool-call-card:hover {
  border-color: #e0e0e0;
}
.tool-call-card.status-running {
  border-color: #e6c47a;
  background: #fffbef;
}
.tool-call-card.status-error {
  border-color: #e8b8b8;
  background: #fef6f6;
}

.tool-call-header {
  display: flex;
  align-items: center;
  gap: 8px;
  width: 100%;
  padding: 6px 10px;
  background: transparent;
  border: none;
  cursor: pointer;
  font-size: 13px;
  color: #4b5563;
  text-align: left;
  user-select: none;
}
.tool-call-header:hover {
  background: rgba(0, 0, 0, 0.025);
}

.tool-call-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 16px;
  height: 16px;
  flex-shrink: 0;
  color: #6b7280;
}
.tool-call-card.status-running .tool-call-icon { color: #d4a52e; }
.tool-call-card.status-error .tool-call-icon { color: #c14a4a; }
.tool-call-card.status-success .tool-call-icon { color: #4a8a5a; }

.tool-call-spinner {
  width: 11px;
  height: 11px;
  border: 1.5px solid #e6c47a;
  border-top-color: #d4a52e;
  border-radius: 50%;
  animation: tool-call-spin 0.85s linear infinite;
}
@keyframes tool-call-spin {
  to { transform: rotate(360deg); }
}

.tool-call-name {
  flex: 1;
  font-weight: 500;
  color: #374151;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.tool-call-card.status-error .tool-call-name {
  color: #8a3a3a;
}

.tool-call-status-text {
  font-size: 11.5px;
  flex-shrink: 0;
  font-family: 'SF Mono', 'Consolas', monospace;
  font-variant-numeric: tabular-nums;
}
.tool-call-status-text.success { color: #4a8a5a; }
.tool-call-status-text.error { color: #c14a4a; }
.tool-call-status-text.running { color: #b48a26; }

.tool-call-duration {
  font-size: 11px;
  color: #9ca3af;
  font-family: 'SF Mono', 'Consolas', monospace;
  font-variant-numeric: tabular-nums;
  flex-shrink: 0;
}

.tool-call-chevron {
  color: #9ca3af;
  flex-shrink: 0;
  display: inline-flex;
  transition: transform 0.18s;
}
.tool-call-chevron.open {
  transform: rotate(180deg);
}

.tool-call-body {
  padding: 6px 10px 8px;
}

.tool-call-section-head {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 3px;
  font-size: 11px;
  color: #9ca3af;
  font-family: 'SF Mono', 'Consolas', monospace;
}
.tool-call-section-lang {
  background: #e5e7eb;
  color: #4b5563;
  padding: 1px 6px;
  border-radius: 3px;
  font-size: 10.5px;
  line-height: 1.4;
}
.tool-call-section-label {
  font-family: inherit;
}

.tool-call-code {
  margin: 0;
  padding: 8px 10px;
  background: #fafbfc;
  border: 1px solid #ececec;
  border-radius: 4px;
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 12px;
  line-height: 1.55;
  color: #1f2937;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 240px;
  overflow: auto;
}
.tool-call-code code {
  font-family: inherit;
  background: transparent;
  padding: 0;
}

/* ============ 暗色模式 ============ */
@media (prefers-color-scheme: dark) {
  .tool-call-card {
    background: #1f2024;
    border-color: #2c2d31;
  }
  .tool-call-card:hover { border-color: #3a3b3f; }
  .tool-call-card.status-running {
    background: #2b261a;
    border-color: #5a4a20;
  }
  .tool-call-card.status-error {
    background: #2c1f1f;
    border-color: #5a3030;
  }

  .tool-call-header { color: #c8cbd1; }
  .tool-call-header:hover { background: rgba(255, 255, 255, 0.03); }

  .tool-call-icon { color: #8a8d96; }
  .tool-call-card.status-running .tool-call-icon { color: #e6c47a; }
  .tool-call-card.status-error .tool-call-icon { color: #e89292; }
  .tool-call-card.status-success .tool-call-icon { color: #63c98e; }

  .tool-call-name { color: #d6d9df; }
  .tool-call-card.status-error .tool-call-name { color: #f0a4a4; }

  .tool-call-status-text.success { color: #63c98e; }
  .tool-call-status-text.error { color: #e89292; }
  .tool-call-status-text.running { color: #e6c47a; }

  .tool-call-duration { color: #6b6e76; }
  .tool-call-chevron { color: #6b6e76; }

  .tool-call-section-head { color: #6b6e76; }
  .tool-call-section-lang {
    background: #2c2d31;
    color: #a0a3ab;
  }

  .tool-call-code {
    background: #15161a;
    border-color: #2c2d31;
    color: #d6d9df;
  }
}
</style>