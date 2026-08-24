<template>
  <div class="tool-call-compact" :class="[`status-${toolCall.status}`, { expanded }]">
    <div class="tool-header" @click="expanded = !expanded">
      <span class="tool-emoji">{{ emoji }}</span>
      <span class="tool-name">{{ toolCall.name }}</span>
      <span v-if="toolCall.file_ops && toolCall.file_ops.length" class="tool-file-badge" :title="fileOpsTitle">
        📄{{ toolCall.file_ops.length }}
      </span>
      <span v-if="toolCall.status === 'running'" class="tool-indicator running"></span>
      <span v-else-if="toolCall.status === 'completed'" class="tool-indicator completed">✓</span>
      <span v-else class="tool-indicator error">!</span>
      <span class="expand-icon">{{ expanded ? '▾' : '▸' }}</span>
    </div>

    <div v-if="expanded" class="tool-details">
      <div v-if="toolCall.file_ops && toolCall.file_ops.length" class="tool-file-ops">
        <div class="detail-label">操作文件：</div>
        <div class="file-op-list">
          <div v-for="(op, idx) in toolCall.file_ops" :key="idx" class="file-op-item">
            <span class="file-op-action" :class="`action-${op.action}`">{{ actionLabel(op.action) }}</span>
            <span class="file-op-path" :title="op.path">{{ op.path }}</span>
          </div>
        </div>
      </div>

      <div v-if="prettyArgs" class="tool-args">
        <div class="detail-label">参数：</div>
        <pre class="args-content">{{ prettyArgs }}</pre>
      </div>

      <div v-if="toolCall.status !== 'running' && toolCall.content" class="tool-content">
        <div class="detail-label">
          <span>{{ toolCall.status === 'error' ? '错误信息' : '返回结果' }}：</span>
          <span v-if="toolCall.status === 'error'" class="content-status error">失败</span>
          <span v-else class="content-status success">成功</span>
        </div>
        <pre class="content-preview" @click.stop>{{ contentPreview }}</pre>
        <button v-if="contentTooLong && !showFullContent" class="expand-more-btn" @click.stop="showFullContent = true">
          展开全部 ({{ contentLength }} 字符)
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import type { ToolCallEvent, FileOp } from '@/stores/chat'

const props = defineProps<{
  toolCall: ToolCallEvent
}>()

const expanded = ref(false)
const showFullContent = ref(false)

const emojiMap: Record<string, string> = {
  web_search: '🔍', web_fetch: '🌐', web_select: '🖱️',
  read_file: '📄', write_file: '✏️', file_edit: '📝', list_files: '📁',
  directory_tree: '📂', search_in_files: '🔎', execute_command: '⚡',
  execute_code: '💻', browser_navigate: '🌍', delegate_task: '🎭',
  memory_store: '💾', memory_recall: '🧠', session_search: '🔎',
  cronjob: '⏰', skill: '📚', clarify: '❓', image_gen: '🎨',
  image_edit: '🖼️', tts: '🔊', asr: '🎤', send_message: '💬',
  todo: '✅', gitignore: '📋', batch_file_ops: '📦',
  project_analyze: '📊', diff_patch: '🔀', ha: '🏠',
  kanban_show: '📋', kanban_complete: '✅', kanban_block: '🚫',
  kanban_heartbeat: '💓', kanban_comment: '💬', kanban_create: '➕', kanban_link: '🔗',
}

const emoji = computed(() => emojiMap[props.toolCall.name] || '🔧')

const prettyArgs = computed(() => {
  const raw = props.toolCall.args_text || props.toolCall.args
  if (!raw) return ''
  try {
    if (typeof raw === 'object') {
      return JSON.stringify(raw, null, 2)
    }
    if (raw.startsWith('{') || raw.startsWith('[')) {
      return JSON.stringify(JSON.parse(raw), null, 2)
    }
  } catch {}
  return typeof raw === 'string' ? raw : JSON.stringify(raw, null, 2)
})

const fileOpsTitle = computed(() => {
  if (!props.toolCall.file_ops) return ''
  return props.toolCall.file_ops.map(f => `${actionLabel(f.action)} ${f.path}`).join('\n')
})

const contentLength = computed(() => (props.toolCall.content || '').length)
const contentTooLong = computed(() => contentLength.value > 800)
const contentPreview = computed(() => {
  if (!props.toolCall.content) return ''
  if (!contentTooLong.value || showFullContent.value) return props.toolCall.content
  return props.toolCall.content.slice(0, 800) + '...'
})

function actionLabel(a: string): string {
  const map: Record<string, string> = {
    read: '读',
    write: '写',
    delete: '删',
    list: '列',
    search: '搜',
    batch: '批',
    access: '访',
  }
  return map[a] || a
}
</script>

<style scoped>
.tool-call-compact {
  display: block;
  padding: 4px 8px;
  background: #fafafa;
  border: 1px solid #eee;
  border-radius: 8px;
  font-size: 11px;
  color: #555;
  margin: 2px 0;
  transition: background 0.2s, border-color 0.2s;
  cursor: pointer;
}

.tool-call-compact:hover {
  background: #f3f7ff;
  border-color: #d0dcff;
}

.tool-call-compact.status-error {
  background: #fff5f5;
  border-color: #ffd4d4;
}

.tool-call-compact.expanded {
  background: #f9faff;
  border-color: #cfe0ff;
}

.tool-header {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  width: 100%;
}

.tool-emoji {
  font-size: 12px;
  opacity: 0.8;
}

.tool-name {
  font-family: 'SF Mono', 'Fira Code', monospace;
  font-size: 11px;
  font-weight: 600;
}

.tool-file-badge {
  font-size: 10px;
  padding: 1px 5px;
  background: #eaf3ff;
  color: #4a89dc;
  border-radius: 8px;
  margin-left: 2px;
}

.tool-indicator {
  font-size: 9px;
  margin-left: 2px;
}

.tool-indicator.running {
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: #666;
  animation: pulse 1.2s ease-in-out infinite;
}

@keyframes pulse {
  0%, 100% { opacity: 0.3; transform: scale(0.8); }
  50% { opacity: 1; transform: scale(1); }
}

.tool-indicator.completed {
  color: #888;
  font-family: 'SF Mono', monospace;
}

.tool-indicator.error {
  color: #d66;
  font-weight: bold;
}

.expand-icon {
  margin-left: auto;
  font-size: 9px;
  color: #999;
}

.tool-details {
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px dashed #e0e0e0;
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.detail-label {
  font-size: 10px;
  color: #888;
  font-weight: 600;
  margin-bottom: 4px;
  display: flex;
  justify-content: space-between;
  align-items: center;
}

.content-status {
  padding: 1px 6px;
  border-radius: 6px;
  font-size: 9px;
  font-weight: 600;
}
.content-status.success { background: #e8f7e8; color: #3a8a3a; }
.content-status.error { background: #ffecec; color: #c23b3b; }

.tool-file-ops .file-op-list {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.file-op-item {
  display: flex;
  gap: 6px;
  align-items: center;
  padding: 2px 4px;
  background: #fff;
  border-radius: 4px;
  font-family: 'SF Mono', monospace;
  font-size: 10px;
}
.file-op-action {
  flex-shrink: 0;
  padding: 1px 5px;
  border-radius: 4px;
  font-size: 9px;
  font-weight: 700;
  color: #fff;
}
.file-op-action.action-read { background: #6aa9ff; }
.file-op-action.action-write { background: #ffb24d; }
.file-op-action.action-delete { background: #ff6b6b; }
.file-op-action.action-list { background: #7bc97b; }
.file-op-action.action-search { background: #b17dff; }
.file-op-action.action-batch { background: #4fc9c9; }
.file-op-action.action-access { background: #aaa; }

.file-op-path {
  color: #444;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
  min-width: 0;
}

.tool-args .args-content,
.tool-content .content-preview {
  margin: 0;
  padding: 6px 8px;
  background: #fff;
  border: 1px solid #eee;
  border-radius: 4px;
  font-family: 'SF Mono', 'Fira Code', monospace;
  font-size: 10px;
  color: #333;
  white-space: pre-wrap;
  word-break: break-all;
  max-height: 200px;
  overflow: auto;
  cursor: text;
  user-select: text;
}

.expand-more-btn {
  margin-top: 4px;
  padding: 3px 8px;
  font-size: 10px;
  background: #f0f4ff;
  border: 1px solid #d8e3ff;
  color: #4a7ad2;
  border-radius: 5px;
  cursor: pointer;
}
.expand-more-btn:hover { background: #e0eaff; }

@media (prefers-color-scheme: dark) {
  .tool-call-compact {
    background: #1c1c1e;
    border-color: #2a2a2c;
    color: #bbb;
  }
  .tool-call-compact:hover {
    background: #242a36;
    border-color: #3a4a70;
  }
  .tool-call-compact.status-error {
    background: #2a1c1c;
    border-color: #5c3333;
  }
  .tool-call-compact.expanded {
    background: #23262d;
    border-color: #3a4a70;
  }
  .tool-file-badge {
    background: #2a3a58;
    color: #7aa6ff;
  }
  .tool-details { border-top-color: #333; }
  .detail-label { color: #888; }
  .file-op-item {
    background: #151517;
  }
  .file-op-path { color: #ccc; }
  .tool-args .args-content,
  .tool-content .content-preview {
    background: #151517;
    border-color: #2a2a2c;
    color: #ccc;
  }
  .expand-more-btn {
    background: #2a3a58;
    border-color: #3a4a70;
    color: #7aa6ff;
  }
}
</style>