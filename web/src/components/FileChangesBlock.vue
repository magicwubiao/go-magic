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

    <!-- 文件列表：仅展示变更动作与路径，不提供行级 diff -->
    <div v-show="listExpanded" class="file-changes-list">
      <div
        v-for="f in files"
        :key="f.path"
        class="file-change-item"
        :class="{ 'file-change-deleted': f.action === 'delete' }"
      >
        <span class="file-change-action" :class="`action-${f.action}`">{{ actionLabel(f.action) }}</span>
        <span class="file-change-path">
          <span v-if="dirName(f.path)" class="file-change-dir">{{ dirName(f.path) }}/</span>
          <span class="file-change-base">{{ baseName(f.path) }}</span>
        </span>
        <!-- 行数统计（来自 diff，无 diff 时隐藏），仅展示不可展开 -->
        <span v-if="diffStats(f).total > 0" class="file-change-stats">
          <span class="stat-add">+{{ diffStats(f).add }}</span>
          <span class="stat-del">−{{ diffStats(f).del }}</span>
        </span>
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
  // 数据源是否已落库（后端 file_ops）。纯列表展示下不参与 UI 逻辑，保留以兼容调用方。
  authoritative?: boolean
}>()

const { t } = useI18n()

const listExpanded = ref(false)

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

interface DiffLine {
  cls: 'diff-add' | 'diff-del' | 'diff-ctx' | 'diff-hunk' | 'diff-meta'
  text: string
}

// 解析 unified diff 文本成带行级类别的行（仅用于统计 +/- 行数，不渲染）。
// 行首字符决定类别：+ 新增 / - 删除 / 空格 上下文 / @@ hunk 头 / ---、+++ 文件头。
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

// 行级 +/- 统计（不含 ---/+++ 头与 @@ 行），供徽标展示。
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

.file-change-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 8px;
  border: 1px solid #ececec;
  background: #fff;
  border-radius: 8px;
  min-width: 0;
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

/* 行内 +/- 数量徽标 */
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
}
</style>
