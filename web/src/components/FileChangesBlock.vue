<template>
  <div v-if="files.length > 0" class="file-changes-block">
    <!-- 头部：默认折叠的"变更的文件"入口 -->
    <div
      class="file-changes-head"
      role="button"
      tabindex="0"
      @click.stop="listExpanded = !listExpanded"
      @keydown.enter.stop="listExpanded = !listExpanded"
    >
      <n-icon size="13" class="file-changes-arrow" :class="{ 'file-changes-arrow-open': listExpanded }"><ChevronForwardOutline /></n-icon>
      <n-icon size="13"><DocumentTextOutline /></n-icon>
      <span>{{ t('chat.changedFilesTitle') }}</span>
      <span class="file-changes-count">{{ files.length }}</span>
    </div>

    <div v-show="listExpanded" class="file-changes-list">
      <div v-for="f in files" :key="f.path" class="file-change-row">
        <!-- 单文件条目：有 diff（或已落库的"无 diff"原因）时点击展开行级 diff -->
        <div
          class="file-change-item"
          :class="{
            'file-change-deleted': f.action === 'delete',
            'file-change-clickable': itemExpandable(f),
          }"
          :title="itemTitle(f)"
          :role="itemExpandable(f) ? 'button' : undefined"
          :tabindex="itemExpandable(f) ? 0 : undefined"
          @click.stop="toggleFile(f)"
          @keydown.enter.stop="toggleFile(f)"
        >
          <span class="file-change-action" :class="`action-${f.action}`">{{ actionLabel(f.action) }}</span>
          <span class="file-change-path">
            <span v-if="dirName(f.path)" class="file-change-dir">{{ dirName(f.path) }}/</span>
            <span class="file-change-base">{{ baseName(f.path) }}</span>
          </span>
          <span v-if="diffStats(f).total > 0" class="file-change-stats">
            <span class="stat-add">+{{ diffStats(f).add }}</span>
            <span class="stat-del">−{{ diffStats(f).del }}</span>
          </span>
        </div>

        <!-- 行级 diff 视图 -->
        <div v-if="expandedPath === f.path" class="file-change-diff" @click.stop>
          <div v-if="diffLines(f).length > 0" class="file-diff-body">
            <div v-for="(l, i) in diffLines(f)" :key="i" class="diff-line" :class="l.cls">
              <span class="diff-text">{{ l.text }}</span>
            </div>
          </div>
          <div v-else class="file-diff-empty">{{ t('chat.changedNoDiff') }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ChevronForwardOutline, DocumentTextOutline } from '@vicons/ionicons5'
import type { FileOp } from '@/api/sessions'

const props = defineProps<{
  // 已按路径去重、仅含变更动作（write/delete）的文件列表。
  files: FileOp[]
  // 数据源是否已落库（后端 file_ops）。为 true 时"没有 diff"是最终结论
  // （二进制/超大/空文件），点击可查看原因说明；为 false（流式聚合中）
  // 没有 diff 的条目不可展开，等 done 事件带 diff 的列表替换。
  authoritative?: boolean
}>()

const { t } = useI18n()

const listExpanded = ref(false)
const expandedPath = ref('')

function actionLabel(action: string): string {
  const key = `chat.fileActions.${action}`
  const label = t(key)
  return label === key ? action : label
}

function baseName(path: string): string {
  const norm = path.replace(/\\/g, '/')
  const idx = norm.lastIndexOf('/')
  return idx >= 0 ? norm.slice(idx + 1) : path
}

function dirName(path: string): string {
  const norm = path.replace(/\\/g, '/')
  const idx = norm.lastIndexOf('/')
  return idx > 0 ? norm.slice(0, idx) : ''
}

// 有真实 diff 文本 → 可展开看行级 diff；
// authoritative（已落库的后端结论）且确实改过文件但没有 diff
// （二进制/超大/空文件）→ 可展开看说明。
// 流式聚合中的条目（authoritative=false）没有 diff 时不可展开，
// 等 done 事件带 diff 的 file_ops 替换后即有点击能力。
function itemExpandable(f: FileOp): boolean {
  return props.authoritative === true || !!f.diff
}

function itemTitle(f: FileOp): string {
  if (f.diff) return t('chat.changedDiffClick')
  if (props.authoritative) return t('chat.changedNoDiffTitle')
  return ''
}

function toggleFile(f: FileOp) {
  if (!itemExpandable(f)) return
  expandedPath.value = expandedPath.value === f.path ? '' : f.path
}

interface DiffLine {
  cls: 'diff-add' | 'diff-del' | 'diff-ctx' | 'diff-hunk' | 'diff-meta'
  text: string
}

// 解析 unified diff 文本成带行级样式的行。行首字符决定类别：
// + 新增 / - 删除 / 空格 上下文 / @@ hunk 头 / ---、+++ 文件头（弱化）。
function diffLines(f: FileOp): DiffLine[] {
  if (!f.diff) return []
  const out: DiffLine[] = []
  for (const raw of f.diff.split('\n')) {
    if (!raw) continue
    const ch = raw[0]
    let cls: DiffLine['cls'] = 'diff-ctx'
    if (ch === '+' && !raw.startsWith('+++')) cls = 'diff-add'
    else if (ch === '-' && !raw.startsWith('---')) cls = 'diff-del'
    else if (ch === '@') cls = 'diff-hunk'
    else if (raw.startsWith('---') || raw.startsWith('+++')) cls = 'diff-meta'
    out.push({ cls, text: raw })
  }
  return out
}

// 行级 +/- 统计（不含 ---/+++ 头与 @@ 行）。
function diffStats(f: FileOp): { add: number; del: number; total: number } {
  let add = 0
  let del = 0
  for (const l of diffLines(f)) {
    if (l.cls === 'diff-add') add++
    else if (l.cls === 'diff-del') del++
  }
  return { add, del, total: add + del }
}
</script>

<style scoped>
/* 变更的文件（助手消息内嵌列表） */
.file-changes-block {
  display: flex;
  flex-direction: column;
  gap: 6px;
  margin-top: 10px;
}

.file-changes-head {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 3px 10px;
  border-radius: 12px;
  background: rgba(24, 160, 88, 0.08);
  color: #18a058;
  font-size: 12px;
  line-height: 1.4;
  width: fit-content;
  cursor: pointer;
  user-select: none;
  transition: background 0.15s;
}

.file-changes-head:hover {
  background: rgba(24, 160, 88, 0.14);
}

.file-changes-head:focus-visible {
  outline: 2px solid rgba(24, 160, 88, 0.4);
  outline-offset: 1px;
}

.file-changes-arrow {
  transition: transform 0.15s;
}

.file-changes-arrow-open {
  transform: rotate(90deg);
}

.file-changes-head .n-icon {
  opacity: 0.85;
}

.file-changes-count {
  padding: 0 6px;
  border-radius: 9px;
  background: rgba(24, 160, 88, 0.14);
  font-weight: 600;
  font-size: 11px;
  line-height: 1.6;
}

.file-changes-list {
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.file-change-row {
  display: flex;
  flex-direction: column;
  gap: 2px;
  min-width: 0;
}

.file-change-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 8px;
  border: 1px solid #ececec;
  background: #fff;
  border-radius: 8px;
  min-width: 0;
  transition: border-color 0.15s, background 0.15s;
}

.file-change-item:hover {
  border-color: #b7ebcf;
  background: #f6fffb;
}

.file-change-clickable {
  cursor: pointer;
}

.file-change-clickable:focus-visible {
  outline: 2px solid rgba(24, 160, 88, 0.4);
  outline-offset: 0;
}

.file-change-action {
  flex-shrink: 0;
  min-width: 36px;
  text-align: center;
  padding: 1px 6px;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 600;
  line-height: 1.7;
  color: #fff;
}

.file-change-action.action-write { background: #18a058; }
.file-change-action.action-delete { background: #d03050; }
.file-change-action.action-batch { background: #f0a020; }

.file-change-path {
  display: flex;
  align-items: baseline;
  gap: 2px;
  flex: 1;
  min-width: 0;
  overflow: hidden;
  font-family: 'SF Mono', 'Fira Code', Consolas, monospace;
  font-size: 12px;
}

.file-change-dir {
  color: #94a3b8;
  font-size: 11px;
  flex-shrink: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  min-width: 0;
}

.file-change-base {
  color: #1f2937;
  font-weight: 600;
  flex-shrink: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 60%;
}

.file-change-deleted .file-change-base {
  color: #d03050;
  text-decoration: line-through;
}

/* 行内 +/- 统计徽标 */
.file-change-stats {
  flex-shrink: 0;
  display: inline-flex;
  gap: 6px;
  font-family: 'SF Mono', 'Fira Code', Consolas, monospace;
  font-size: 11px;
  font-weight: 600;
}

.file-change-stats .stat-add { color: #18a058; }
.file-change-stats .stat-del { color: #d03050; }

/* 行级 diff 视图 */
.file-change-diff {
  margin: 0 0 2px 8px;
  border: 1px solid #ececec;
  border-radius: 8px;
  overflow: hidden;
  background: #fafafa;
}

.file-diff-body {
  max-height: 320px;
  overflow: auto;
  font-family: 'SF Mono', 'Fira Code', Consolas, monospace;
  font-size: 12px;
  line-height: 1.55;
}

.diff-line {
  display: flex;
  white-space: pre;
}

.diff-text {
  flex: 1;
  min-width: 0;
  overflow-x: auto;
}

.diff-ctx { background: transparent; color: #374151; }
.diff-add { background: #e6f6ec; color: #18794e; }
.diff-del { background: #fdebec; color: #b4232c; }
.diff-hunk { background: #eef1f5; color: #6e7781; font-weight: 600; }
.diff-meta { background: #f3f4f6; color: #9ca3af; font-style: italic; }

.file-diff-empty {
  padding: 8px 12px;
  color: #94a3b8;
  font-size: 12px;
}

/* ========== Dark Mode ========== */
@media (prefers-color-scheme: dark) {
  .file-changes-head {
    background: rgba(99, 226, 183, 0.12);
    color: #63e2b7;
  }

  .file-changes-count {
    background: rgba(99, 226, 183, 0.18);
  }

  .file-change-item {
    border-color: #2a2a2a;
    background: #1a1a1a;
  }

  .file-change-item:hover {
    border-color: #2f7a5a;
    background: #16241d;
  }

  .file-change-base {
    color: #e5e7eb;
  }

  .file-change-dir {
    color: #6b7280;
  }

  .file-change-deleted .file-change-base {
    color: #e88080;
  }

  .file-change-stats .stat-add { color: #3fb950; }
  .file-change-stats .stat-del { color: #f85149; }

  .file-change-diff {
    border-color: #2a2a2a;
    background: #161616;
  }

  .diff-ctx { color: #c9d1d9; }
  .diff-add { background: rgba(46, 160, 67, 0.18); color: #3fb950; }
  .diff-del { background: rgba(248, 81, 73, 0.14); color: #f85149; }
  .diff-hunk { background: #1f2630; color: #8b949e; }
  .diff-meta { background: #1b1b1b; color: #6e7681; }

  .file-diff-empty {
    color: #6b7280;
  }
}
</style>
