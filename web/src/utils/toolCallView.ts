/**
 * 工具调用的展示层公共逻辑。
 *
 * 背景：同一份「工具名 → 中文标签」「args → 主参数摘要」的推导，原本在
 * ToolCallCard.vue 与 ToolCallBlock.vue 里各写了一份；底部执行坞（TaskTimeline）
 * 要展示同样的信息，若再抄第三份，三处必然逐渐不一致（同一个工具在对话流里
 * 叫「运行命令」、在坞里叫 `bash`，是典型的廉价 bug 来源）。
 *
 * 因此把纯函数抽到这里，三个组件共用。只做文本推导，不碰 Vue 响应式。
 */
import type { ToolCallEvent } from '@/stores/chat'
import { i18n } from '@/locales'

/**
 * 取某命名空间下 `key` 的译文；缺失时返回空串（由调用方回退）。
 *
 * 用 `te` 先探测存在性，避免 vue-i18n 在缺 key 时把整条 key 路径（如
 * `toolLabels.bash`）当作译文返回 —— 那样调用方拿到的就不是空串，回退
 * 逻辑会被绕过，界面上直接漏出 `toolLabels.bash` 这种字样。
 */
function tr(ns: 'toolLabels' | 'toolShortLabels' | 'toolActions', key: string): string {
  const path = `${ns}.${key}`
  if (!i18n.global.te(path)) return ''
  return i18n.global.t(path)
}

/** 工具名 → 人类可读标签。未命中时回退为原始 name（由调用方决定兜底文案）。 */
export function toolDisplayName(name: string): string {
  return tr('toolLabels', name) || name || ''
}

/**
 * 工具名 → 短标签（坞里空间紧张，用比卡片更短的中文/英文措辞）。
 * 例如 `Read` 在卡片里是「读取文件」，在坞里只占「读取」。
 */
export function toolShortName(name: string): string {
  return tr('toolShortLabels', name) || tr('toolLabels', name) || name || ''
}

/** 文件变更行里的动作短标（read/write/delete/...）。 */
export function toolActionLabel(action: string): string {
  return tr('toolActions', action) || action
}

/** 用 emoji 做类型标识：坞里没有空间放图标组件，emoji 是最省事且跨平台一致的做法。 */
export function toolEmoji(name: string): string {
  return TOOL_EMOJIS[name] || '🔧'
}

/**
 * 反查：短标签 → emoji。
 *
 * 坞里的步骤 title 存的是**短标签**（「读取」/「Read」）而不是工具名（`Read`），
 * 因此不能直接拿 title 去查 TOOL_EMOJIS —— 那样每个图标都会退化成 🔧。
 * 这里把短标签反向映射回一个代表性工具名，再取 emoji。
 * 多个工具共享同一短标签（Read/read_file 都叫「读取」）时，取第一个即可，
 * 它们本来就该用同一个图标。
 *
 * 短标签现在来自 i18n（随语言变化），因此缓存需要按当前语言失效重建：
 * 记录构建时用的语言，语言一变就重建。否则切换语言后图标会沿用旧语言的映射。
 */
let shortLabelToEmoji: { locale: string; map: Record<string, string> } | null = null

function getShortLabelToEmoji(): Record<string, string> {
  const locale = String(i18n.global.locale.value)
  if (shortLabelToEmoji && shortLabelToEmoji.locale === locale) return shortLabelToEmoji.map
  const m: Record<string, string> = {}
  for (const tool of Object.keys(TOOL_EMOJIS)) {
    const short = toolShortName(tool)
    if (short && !(short in m)) m[short] = TOOL_EMOJIS[tool] || '🔧'
  }
  shortLabelToEmoji = { locale, map: m }
  return m
}

/** 按坞里的步骤标题（短标签或工具名）取图标。 */
export function toolEmojiByTitle(title: string): string {
  return getShortLabelToEmoji()[title] || TOOL_EMOJIS[title] || '🔧'
}

export function tryParseArgs(raw: string): Record<string, unknown> | null {
  if (!raw) return null
  try {
    const v = JSON.parse(raw)
    return v && typeof v === 'object' && !Array.isArray(v) ? (v as Record<string, unknown>) : null
  } catch {
    return null
  }
}

export interface MainArg {
  text: string
  lang: string
}

/**
 * 从 args 中挑出「最能说明这次调用在干什么」的那一个参数。
 * 命令优先，其次是路径 / 匹配模式，再其次是 diff，最后退回整段 JSON。
 */
export function pickMainArg(parsed: Record<string, unknown> | null, raw: string): MainArg {
  if (parsed) {
    if (typeof parsed.command === 'string') return { text: parsed.command, lang: 'bash' }
    if (typeof parsed.cmd === 'string') return { text: parsed.cmd, lang: 'bash' }
    const path = (parsed.file_path ?? parsed.path ?? parsed.filepath ?? parsed.file) as
      | string
      | undefined
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
      return { text: JSON.stringify(parsed), lang: 'json' }
    } catch {
      /* fallthrough */
    }
  }
  return { text: raw, lang: 'text' }
}

/**
 * 单行参数摘要：合并换行、折叠空白、按字符数截断。
 * 用于坞 / 列表这类只能给一行的位置（卡片里走多行 pre，不走这里）。
 */
export function oneLineArgSummary(rawArgs: string, max = 96): string {
  const parsed = tryParseArgs(rawArgs)
  const { text } = pickMainArg(parsed, rawArgs)
  const flat = text.replace(/\s+/g, ' ').trim()
  if (flat.length <= max) return flat
  return flat.slice(0, max) + '…'
}

/**
 * 从工具调用里提取「这次调用碰了哪个文件/命令」，供底部坞的一行摘要展示。
 * 没解析出实质性参数时返回空串（坞就只显示工具名）。
 */
export function toolCallSummary(tc: Pick<ToolCallEvent, 'args' | 'args_text'>, max = 96): string {
  const raw = tc.args_text && tc.args_text !== tc.args ? tc.args_text : tc.args
  return oneLineArgSummary(raw || '', max)
}

const TOOL_EMOJIS: Record<string, string> = {
  bash: '⌘',
  shell: '⌘',
  exec: '⌘',
  execute_command: '⌘',
  execute_code: '⌘',
  Read: '📄',
  read_file: '📄',
  Write: '✏️',
  write_file: '✏️',
  Edit: '📝',
  edit_file: '📝',
  file_edit: '📝',
  MultiEdit: '📝',
  Glob: '🔍',
  glob_files: '🔍',
  Grep: '🔎',
  grep_files: '🔎',
  search_in_files: '🔎',
  ListDir: '📁',
  list_dir: '📁',
  list_files: '📁',
  directory_tree: '📂',
  WebFetch: '🌐',
  web_fetch: '🌐',
  WebSearch: '🔍',
  web_search: '🔍',
  web_select: '🖱️',
  browser_navigate: '🌍',
  TodoWrite: '✅',
  todo_write: '✅',
  todo: '✅',
  delegate_task: '🎭',
  memory_store: '💾',
  memory_recall: '🧠',
  session_search: '🔎',
  cronjob: '⏰',
  skill: '📚',
  clarify: '❓',
  image_gen: '🎨',
  image_edit: '🖼️',
  tts: '🔊',
  asr: '🎤',
  send_message: '💬',
  gitignore: '📋',
  batch_file_ops: '📦',
  project_analyze: '📊',
  diff_patch: '🔀',
  present_files: '📎',
}
