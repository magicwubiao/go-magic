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

/** 工具名 → 人类可读标签。未命中时回退为原始 name（由调用方决定兜底文案）。 */
export function toolDisplayName(name: string): string {
  return TOOL_LABELS[name] || name || ''
}

/**
 * 工具名 → 短标签（坞里空间紧张，用比卡片更短的措辞）。
 * 例如 `Read` 在卡片里是「读取文件」，在坞里只占「读取」。
 */
export function toolShortName(name: string): string {
  return TOOL_SHORT_LABELS[name] || TOOL_LABELS[name] || name || ''
}

/** 用 emoji 做类型标识：坞里没有空间放图标组件，emoji 是最省事且跨平台一致的做法。 */
export function toolEmoji(name: string): string {
  return TOOL_EMOJIS[name] || '🔧'
}

/**
 * 反查：短标签 → emoji。
 *
 * 坞里的步骤 title 存的是**短标签**（「读取」）而不是工具名（`Read`），
 * 因此不能直接拿 title 去查 TOOL_EMOJIS —— 那样每个图标都会退化成 🔧。
 * 这里把短标签反向映射回一个代表性工具名，再取 emoji。
 * 多个工具共享同一短标签（Read/read_file 都叫「读取」）时，取第一个即可，
 * 它们本来就该用同一个图标。
 *
 * 注意：必须**惰性**构建。写成模块顶层的立即执行函数会踩 TDZ —— 它会在
 * TOOL_SHORT_LABELS / TOOL_EMOJIS 这两个 const 初始化之前就读取它们，
 * 生产构建里表现为整个前端白屏（`ReferenceError: Cannot access 'Am'
 * before initialization`），因为 ESM 的 const 在声明前处于暂时性死区。
 * 缓存一份即可，不必每次重建。
 */
let shortLabelToEmoji: Record<string, string> | null = null

function getShortLabelToEmoji(): Record<string, string> {
  if (shortLabelToEmoji) return shortLabelToEmoji
  const m: Record<string, string> = {}
  for (const [tool, short] of Object.entries(TOOL_SHORT_LABELS)) {
    if (!(short in m)) m[short] = TOOL_EMOJIS[tool] || '🔧'
  }
  shortLabelToEmoji = m
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

const TOOL_LABELS: Record<string, string> = {
  bash: '运行命令',
  shell: '运行命令',
  exec: '运行命令',
  execute_command: '运行命令',
  execute_code: '执行代码',
  Read: '读取文件',
  read_file: '读取文件',
  Write: '写入文件',
  write_file: '写入文件',
  Edit: '编辑文件',
  edit_file: '编辑文件',
  file_edit: '编辑文件',
  MultiEdit: '批量编辑',
  Glob: '搜索文件',
  glob_files: '搜索文件',
  list_files: '列出文件',
  directory_tree: '目录树',
  Grep: '搜索内容',
  grep_files: '搜索内容',
  search_in_files: '搜索内容',
  WebFetch: '抓取网页',
  web_fetch: '抓取网页',
  WebSearch: '搜索网页',
  web_search: '搜索网页',
  web_select: '网页选择',
  browser_navigate: '打开网页',
  TodoWrite: '更新待办',
  todo_write: '更新待办',
  todo: '更新待办',
  ListDir: '列出目录',
  list_dir: '列出目录',
  delegate_task: '派发子任务',
  memory_store: '写入记忆',
  memory_recall: '检索记忆',
  session_search: '检索会话',
  cronjob: '定时任务',
  skill: '加载技能',
  clarify: '请求澄清',
  image_gen: '生成图片',
  image_edit: '编辑图片',
  tts: '语音合成',
  asr: '语音识别',
  send_message: '发送消息',
  gitignore: '忽略规则',
  batch_file_ops: '批量文件操作',
  project_analyze: '分析项目',
  diff_patch: '应用补丁',
  present_files: '展示文件',
}

/** 坞专用短标签：比卡片更省字。 */
const TOOL_SHORT_LABELS: Record<string, string> = {
  bash: '运行命令',
  shell: '运行命令',
  exec: '运行命令',
  execute_command: '运行命令',
  execute_code: '执行代码',
  Read: '读取',
  read_file: '读取',
  Write: '写入',
  write_file: '写入',
  Edit: '编辑',
  edit_file: '编辑',
  file_edit: '编辑',
  MultiEdit: '批量编辑',
  Glob: '找文件',
  glob_files: '找文件',
  Grep: '搜内容',
  grep_files: '搜内容',
  search_in_files: '搜内容',
  WebFetch: '抓网页',
  web_fetch: '抓网页',
  WebSearch: '搜网页',
  web_search: '搜网页',
  ListDir: '列目录',
  list_dir: '列目录',
  list_files: '列文件',
  TodoWrite: '更新待办',
  todo_write: '更新待办',
  present_files: '展示文件',
  delegate_task: '派发任务',
  memory_store: '写记忆',
  memory_recall: '读记忆',
  session_search: '搜会话',
  cronjob: '定时任务',
  image_gen: '生成图片',
  image_edit: '编辑图片',
  send_message: '发消息',
  browser_navigate: '开网页',
  web_select: '选网页',
  project_analyze: '分析项目',
  diff_patch: '应用补丁',
  batch_file_ops: '批量操作',
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
