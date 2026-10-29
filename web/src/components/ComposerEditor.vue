<template>
  <!-- 富文本输入框（contenteditable）：技能/连接器胶囊是**行内原子 token**，
       可以和正文穿插、随换行自然折行，退格/点击 × 直接摘掉。 -->
  <div class="composer-editor-wrap">
    <div
      ref="editorRef"
      class="composer-editor"
      contenteditable="true"
      role="textbox"
      aria-multiline="true"
      :aria-label="placeholder"
      spellcheck="false"
      @input="onInput"
      @keydown="onKeydown"
      @paste="onPaste"
      @compositionstart="onCompositionStart"
      @compositionend="onCompositionEnd"
      @click="onEditorClick"
    ></div>
    <div v-if="showPlaceholder" class="composer-editor-placeholder" aria-hidden="true">{{ placeholder }}</div>
  </div>
</template>

<script setup lang="ts">
/**
 * ComposerEditor —— 支持行内胶囊的 contenteditable 输入框。
 *
 * 为什么不用 textarea：胶囊要"嵌在文字中间"（WorkBuddy 的 mention 交互），
 * 而 textarea 里只能有纯文本，胶囊只能单独占一行。contenteditable 允许把
 * contenteditable=false 的 <span> 当成原子 token：光标可以前后移动、正文可以
 * 绕在它两侧，退格能整颗删掉。
 *
 * 数据契约（刻意保持"文本与胶囊分离"）：
 *   - v-model          → 只含**正文**（胶囊不参与），供候选/命令判断使用；
 *   - chips prop       → 胶囊列表（父组件是唯一数据源，这里只负责 DOM 呈现）；
 *   - chip-remove      → 用户在框内删掉某颗胶囊时通知父组件；
 *   - getPayload()     → 发送时调用：按 DOM 顺序把胶囊还原成 `/skill x` 指令行，
 *                        后面接正文。服务端逐行解析这些指令（chat_directives.go）。
 */
import { computed, nextTick, onMounted, ref, watch } from 'vue'

interface Chip {
  id: string
  /** 完整指令文本，如 `/skill translation`、`/mcp github search` */
  text: string
  /** 胶囊上显示的名字 */
  name: string
  /** skills | mcp | commands | files —— 只影响头像配色 */
  group: string
  avatar: string
  /**
   * 胶囊种类：directive = 斜杠指令（发送时序列化成指令行）；
   * file = 上传的文件附件（发送时在原位置留下 [[att:file:<id>]] 占位标记，
   * 由 ChatView.send 改写成最终下标——文件本体仍走 files/images 通道）。
   * 缺省按 directive 处理。
   */
  kind?: 'directive' | 'file'
  /** 文件胶囊附带的大小文案（如 "1.2 MB"），只用于展示与 title */
  sizeLabel?: string
}

const props = defineProps<{
  modelValue: string
  chips: Chip[]
  placeholder?: string
  removeLabel?: string
}>()

const emit = defineEmits<{
  (e: 'update:modelValue', value: string): void
  (e: 'change'): void
  (e: 'send'): void
  (e: 'nav', delta: number): void
  (e: 'escape'): void
  (e: 'chip-remove', id: string): void
}>()

const editorRef = ref<HTMLDivElement | null>(null)
// 正文的本地镜像：占位符显隐、与 modelValue 的比较都靠它（读 DOM 成本更高）。
const textValue = ref('')
const composing = ref(false)
// 已经渲染进 DOM 的胶囊 id，用于增量对账。
const renderedIds = new Set<string>()

// 组字（composition）期间 textValue 尚未同步——onInput 在 composing 时直接
// return，要等 compositionend 才落值。若只看 textValue，打拼音的整段时间里
// placeholder 都会和组字文本叠在一起（用户截图报过"输入消息…"+"ni"重影）。
// 因此 composing 一置位就立刻把 placeholder 藏掉。
const showPlaceholder = computed(() =>
  textValue.value.length === 0 && props.chips.length === 0 && !composing.value
)

// ---------- DOM 读取 ----------

// 读正文：只取文本节点，跳过胶囊；<br> 当一个换行（输入法/浏览器兜底路径可能出现）。
function readText(): string {
  const editor = editorRef.value
  if (!editor) return ''
  let out = ''
  const walk = (node: Node) => {
    if (node.nodeType === Node.TEXT_NODE) {
      out += node.nodeValue ?? ''
      return
    }
    if (node.nodeType !== Node.ELEMENT_NODE) return
    const el = node as HTMLElement
    if (el.dataset.chipId) return
    if (el.tagName === 'BR') {
      out += '\n'
      return
    }
    el.childNodes.forEach(walk)
  }
  editor.childNodes.forEach(walk)
  return out
}

function syncText() {
  const v = readText()
  if (v === textValue.value) return
  textValue.value = v
  emit('update:modelValue', v)
}

// ---------- 胶囊 DOM ----------

function createChipEl(chip: Chip): HTMLElement {
  // 用 createElement/textContent 而不是 innerHTML：技能名来自用户上传的文件，
  // 拼 HTML 等于给自己开一个 XSS 口子。
  const el = document.createElement('span')
  el.className = 'composer-chip'
  el.contentEditable = 'false'
  el.dataset.chipId = chip.id
  el.dataset.chipKind = chip.kind || 'directive'
  // 指令胶囊才带 directive 序列化文本；文件胶囊不进 getPayload（见下）。
  if (chip.kind !== 'file') el.dataset.directive = chip.text
  el.title = chip.sizeLabel ? `${chip.name} (${chip.sizeLabel})` : chip.text

  const avatar = document.createElement('span')
  avatar.className = `composer-chip-avatar av-${chip.group}`
  avatar.textContent = chip.avatar

  const name = document.createElement('span')
  name.className = 'composer-chip-name'
  name.textContent = chip.name

  el.append(avatar, name)
  if (chip.sizeLabel) {
    const size = document.createElement('span')
    size.className = 'composer-chip-size'
    size.textContent = chip.sizeLabel
    el.append(size)
  }

  const remove = document.createElement('span')
  remove.className = 'composer-chip-remove'
  remove.setAttribute('role', 'button')
  remove.setAttribute('aria-label', props.removeLabel || 'remove')
  remove.title = props.removeLabel || 'remove'
  remove.textContent = '×'
  // mousedown 必须拦住：否则点 × 会先把光标放到胶囊旁边，再触发一次光标定位。
  remove.addEventListener('mousedown', (e) => {
    e.preventDefault()
    e.stopPropagation()
  })
  remove.addEventListener('click', (e) => {
    e.preventDefault()
    e.stopPropagation()
    removeChip(chip.id)
  })

  el.append(avatar, name, remove)
  return el
}

function insertChipAtCaret(chip: Chip) {
  const editor = editorRef.value
  if (!editor) return
  const el = createChipEl(chip)
  const sel = window.getSelection()
  const inEditor = !!sel && sel.rangeCount > 0 && editor.contains(sel.anchorNode)

  if (inEditor && sel) {
    const range = sel.getRangeAt(0)
    range.deleteContents()
    range.insertNode(el)
    // 胶囊后面补一个空格：否则紧接着输入的文字会贴在胶囊边上。
    const afterChip = document.createRange()
    afterChip.setStartAfter(el)
    afterChip.collapse(true)
    const space = document.createTextNode(' ')
    afterChip.insertNode(space)
    afterChip.setStartAfter(space)
    afterChip.collapse(true)
    sel.removeAllRanges()
    sel.addRange(afterChip)
  } else {
    editor.append(el, document.createTextNode(' '))
  }
  renderedIds.add(chip.id)
  syncText()
}

function removeChip(id: string) {
  const editor = editorRef.value
  if (!editor) return
  editor.querySelectorAll<HTMLElement>('[data-chip-id]').forEach((el) => {
    if (el.dataset.chipId === id) el.remove()
  })
  renderedIds.delete(id)
  syncText()
  emit('chip-remove', id)
}

// 父组件的胶囊列表变了 → 增删对账（已有胶囊不重建，避免光标跳动）。
function reconcileChips() {
  const editor = editorRef.value
  if (!editor) return
  const wanted = new Set(props.chips.map((c) => c.id))
  editor.querySelectorAll<HTMLElement>('[data-chip-id]').forEach((el) => {
    const id = el.dataset.chipId || ''
    if (!wanted.has(id)) {
      el.remove()
      renderedIds.delete(id)
    }
  })
  for (const chip of props.chips) {
    if (!renderedIds.has(chip.id)) insertChipAtCaret(chip)
  }
  syncText()
}

// ---------- 外部写入 ----------

// 整块重建：胶囊按列表顺序排在前面，正文跟在后面。
// 只在"父组件直接改文本"（如选中候选命令、回填排队消息）时发生；
// 用户正常打字走的是 DOM 增量更新，不会触发这里。
function renderAll(value: string) {
  const editor = editorRef.value
  if (!editor) return
  editor.replaceChildren()
  renderedIds.clear()
  for (const chip of props.chips) {
    editor.appendChild(createChipEl(chip))
    editor.appendChild(document.createTextNode(' '))
    renderedIds.add(chip.id)
  }
  if (value) editor.appendChild(document.createTextNode(value))
  textValue.value = value
}

// ---------- 事件 ----------

function onInput() {
  if (composing.value) return
  syncText()
  emit('change')
}

function onCompositionStart() {
  composing.value = true
}

function onCompositionEnd() {
  composing.value = false
  // 中文输入法：compositionend 之后紧接着才是最终 input，这里再兜一次。
  nextTick(() => {
    syncText()
    emit('change')
  })
}

function onKeydown(e: KeyboardEvent) {
  if (e.isComposing || composing.value) return
  switch (e.key) {
    case 'Enter':
      if (e.shiftKey) {
        e.preventDefault()
        insertPlainText('\n')
        return
      }
      e.preventDefault()
      emit('send')
      return
    case 'Escape':
      emit('escape')
      return
    case 'ArrowUp':
      emit('nav', -1)
      return
    case 'ArrowDown':
      emit('nav', 1)
      return
    case 'Backspace':
      if (removeChipAtCaret(-1)) e.preventDefault()
      return
    case 'Delete':
      if (removeChipAtCaret(1)) e.preventDefault()
      return
  }
}

// 粘贴：纯文本直接插入（不要浏览器带来的 HTML 样式）；带文件的粘贴不做处理，
// 交给父组件的上传逻辑（它自己会 preventDefault）。
function onPaste(e: ClipboardEvent) {
  const items = e.clipboardData?.items
  if (items) {
    for (let i = 0; i < items.length; i++) {
      if (items[i].kind === 'file') return
    }
  }
  const text = e.clipboardData?.getData('text/plain') ?? ''
  if (!text) return
  e.preventDefault()
  insertPlainText(text)
}

function onEditorClick(e: MouseEvent) {
  const chip = (e.target as HTMLElement | null)?.closest<HTMLElement>('[data-chip-id]')
  if (!chip) return
  // 点胶囊本体：把光标放在它后面，接着打字不会卡在胶囊内部。
  e.preventDefault()
  editorRef.value?.focus()
  const range = document.createRange()
  range.setStartAfter(chip)
  range.collapse(true)
  const sel = window.getSelection()
  sel?.removeAllRanges()
  sel?.addRange(range)
}

// ---------- 光标工具 ----------

function insertPlainText(text: string) {
  const editor = editorRef.value
  if (!editor) return
  const sel = window.getSelection()
  if (!sel || !sel.rangeCount || !editor.contains(sel.anchorNode)) {
    editor.appendChild(document.createTextNode(text))
    placeCaretAtEnd()
    syncText()
    return
  }
  const range = sel.getRangeAt(0)
  range.deleteContents()
  const node = document.createTextNode(text)
  range.insertNode(node)
  const after = document.createRange()
  after.setStartAfter(node)
  after.collapse(true)
  sel.removeAllRanges()
  sel.addRange(after)
  syncText()
}

function placeCaretAtEnd() {
  const editor = editorRef.value
  if (!editor) return
  const range = document.createRange()
  range.selectNodeContents(editor)
  range.collapse(false)
  const sel = window.getSelection()
  sel?.removeAllRanges()
  sel?.addRange(range)
}

// 光标紧邻胶囊时，退格/删除整颗摘掉（浏览器默认只会把原子元素删成半截）。
function removeChipAtCaret(direction: -1 | 1): boolean {
  const sel = window.getSelection()
  if (!sel || !sel.isCollapsed || !sel.rangeCount) return false
  const range = sel.getRangeAt(0)
  const node = range.startContainer
  let target: ChildNode | null = null

  if (node.nodeType === Node.TEXT_NODE) {
    const offset = range.startOffset
    if (direction === -1) {
      if (offset !== 0) return false
      target = node.previousSibling
    } else {
      if (offset !== (node.nodeValue?.length ?? 0)) return false
      target = node.nextSibling
    }
  } else if (node.nodeType === Node.ELEMENT_NODE) {
    const offset = range.startOffset
    target = direction === -1 ? node.childNodes[offset - 1] ?? null : node.childNodes[offset] ?? null
  }

  if (!target || target.nodeType !== Node.ELEMENT_NODE) return false
  const el = target as HTMLElement
  const id = el.dataset.chipId
  if (!id) return false
  removeChip(id)
  return true
}

// ---------- 对外 API ----------

function focus() {
  const editor = editorRef.value
  if (!editor) return
  if (document.activeElement === editor) return
  // 光标原本就在编辑器里（例如刚从面板点选胶囊回来）时保留位置：
  // 强行 placeCaretAtEnd 会把刚插到正文中间的胶囊"跳"到末尾。
  const sel = window.getSelection()
  const keepCaret = !!sel && sel.rangeCount > 0 && editor.contains(sel.anchorNode)
  editor.focus()
  if (!keepCaret) placeCaretAtEnd()
}

// 发送用：按 DOM 顺序序列化——正文原样保留，胶囊在原位置变成独占一行的
// `/xxx args` 指令（服务端只认行首的指令，所以前后没有换行时要补一个）。
// 这样"胶囊A 正文A 胶囊B 正文B"发出去就是「指令A ⏎ 正文A ⏎ 指令B ⏎ 正文B」，
// 服务端能按位置把每段正文绑到它前面的那颗胶囊上（chat_directives.go）。
function getPayload(): string {
  const editor = editorRef.value
  if (!editor) return ''
  let out = ''
  const walk = (node: Node) => {
    if (node.nodeType === Node.TEXT_NODE) {
      out += node.nodeValue ?? ''
      return
    }
    if (node.nodeType !== Node.ELEMENT_NODE) return
    const el = node as HTMLElement
    if (el.dataset.chipId) {
      // 文件附件胶囊：不序列化成指令，但在**原位置**留下一个占位标记。
      // 发送端（ChatView.send）把它改写成 `[[att:<n>]]`（n = 附件在最终附件
      // 列表里的下标），渲染端据此把附件胶囊画回文字流原位置——否则输入框里
      // "文字A 图片 文字B" 到了对话气泡里会退化成"附件块 + 文字A 文字B"。
      if (el.dataset.chipKind === 'file') {
        out += `[[att:${el.dataset.chipId}]]`
        return
      }
      const directive = el.dataset.directive || ''
      if (!directive) return
      if (out && !out.endsWith('\n')) out += '\n'
      out += directive + '\n'
      return
    }
    el.childNodes.forEach(walk)
  }
  editor.childNodes.forEach(walk)
  return out.trim()
}

function clear() {
  const editor = editorRef.value
  if (!editor) return
  editor.replaceChildren()
  renderedIds.clear()
  if (textValue.value !== '') {
    textValue.value = ''
    emit('update:modelValue', '')
  }
}

defineExpose({ focus, getPayload, clear })

// ---------- 同步 ----------

watch(
  () => props.modelValue,
  (v) => {
    if (v === readText()) return
    renderAll(v)
  },
)

// 只看 id 序列：父组件 push/splice 数组时能触发，又不至于每次渲染都深度比较。
watch(
  () => props.chips.map((c) => c.id).join('|'),
  () => reconcileChips(),
)

onMounted(() => {
  renderAll(props.modelValue || '')
})
</script>

<style scoped>
.composer-editor-wrap {
  position: relative;
  min-width: 0;
}

.composer-editor {
  min-height: 24px;
  max-height: 168px;
  overflow-y: auto;
  outline: none;
  font-size: 14px;
  line-height: 1.6;
  color: #1f2937;
  white-space: pre-wrap;
  word-break: break-word;
  padding: 1px 0;
}

.composer-editor-placeholder {
  position: absolute;
  inset: 1px auto auto 0;
  height: 22px;
  line-height: 22px;
  font-size: 14px;
  color: #c2c6cc;
  pointer-events: none;
  user-select: none;
}

/* ---- 行内胶囊：vertical-align: middle 让它和文字排在同一基线，随文字折行 ---- */
.composer-editor :deep(.composer-chip) {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  height: 24px;
  padding: 0 4px 0 3px;
  margin: 0 3px;
  vertical-align: middle;
  background: #f2f3f5;
  border: 1px solid #e5e7eb;
  border-radius: 999px;
  max-width: 260px;
  user-select: none;
  cursor: default;
}

.composer-editor :deep(.composer-chip-avatar) {
  width: 17px;
  height: 17px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 50%;
  font-size: 10px;
  font-weight: 600;
  flex: 0 0 auto;
  background: #eef2ff;
  color: #4338ca;
}

.composer-editor :deep(.composer-chip-avatar.av-skills) {
  background: #eef2ff;
  color: #4338ca;
}

.composer-editor :deep(.composer-chip-avatar.av-mcp) {
  background: #ecfeff;
  color: #0e7490;
}

.composer-editor :deep(.composer-chip-avatar.av-commands) {
  background: #f1f2f4;
  color: #6b7280;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}

/* 文件附件胶囊：琥珀色调，与技能（蓝）/连接器（青）区分 */
.composer-editor :deep(.composer-chip-avatar.av-files) {
  background: #fef3c7;
  color: #b45309;
}

.composer-editor :deep(.composer-chip-size) {
  font-size: 11px;
  color: #9ca3af;
  flex: 0 0 auto;
}

.composer-editor :deep(.composer-chip-name) {
  font-size: 12px;
  color: #374151;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.composer-editor :deep(.composer-chip-remove) {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 15px;
  height: 15px;
  border-radius: 50%;
  color: #9ca3af;
  font-size: 13px;
  line-height: 1;
  cursor: pointer;
  flex: 0 0 auto;
}

.composer-editor :deep(.composer-chip-remove:hover) {
  color: #4b5563;
  background: #e5e7eb;
}

/* 暗色：与聊天区其它暗色样式保持一致 */
@media (prefers-color-scheme: dark) {
  .composer-editor {
    color: #e5e7eb;
  }
  .composer-editor :deep(.composer-chip) {
    background: #2b2c30;
    border-color: #3a3b40;
  }
  .composer-editor :deep(.composer-chip-name) {
    color: #d6d9df;
  }
  .composer-editor :deep(.composer-chip-avatar.av-skills) {
    background: #2c3350;
    color: #a5b4fc;
  }
  .composer-editor :deep(.composer-chip-avatar.av-mcp) {
    background: #173f47;
    color: #67e8f9;
  }
  .composer-editor :deep(.composer-chip-avatar.av-commands) {
    background: #33353a;
    color: #a0a3ab;
  }
  .composer-editor :deep(.composer-chip-avatar.av-files) {
    background: #453a1f;
    color: #fcd34d;
  }
  .composer-editor :deep(.composer-chip-size) {
    color: #6b7280;
  }
  .composer-editor-placeholder {
    color: #5b5f66;
  }
}
</style>
