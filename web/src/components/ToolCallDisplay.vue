<template>
  <div class="tool-call-display" v-if="toolCalls && toolCalls.length > 0">
    <div class="tool-header">
      <span class="tool-icon">🛠️</span>
      <span class="tool-title">Tool Calls</span>
      <span class="tool-count">{{ toolCalls.length }}</span>
    </div>
    <div class="tool-list">
      <div
        v-for="(tool, index) in toolCalls"
        :key="index"
        class="tool-item"
        :class="{ expanded: expandedIndex === index }"
        @click="toggleExpand(index)"
      >
        <div class="tool-main">
          <span class="tool-icon-small">{{ getToolIcon(tool.name) }}</span>
          <span class="tool-name">{{ tool.name }}</span>
          <span v-if="tool.status" class="tool-status" :class="tool.status">
            {{ tool.status }}
          </span>
          <span class="expand-icon">{{ expandedIndex === index ? '▼' : '▶' }}</span>
        </div>
        <div v-if="expandedIndex === index" class="tool-details">
          <div v-if="tool.arguments" class="tool-args">
            <div class="args-header">Arguments:</div>
            <pre class="args-content">{{ formatJson(tool.arguments) }}</pre>
          </div>
          <div v-if="tool.result" class="tool-result">
            <div class="result-header">Result:</div>
            <pre class="result-content">{{ truncate(tool.result, 500) }}</pre>
          </div>
          <div v-if="tool.error" class="tool-error">
            <div class="error-header">Error:</div>
            <pre class="error-content">{{ tool.error }}</pre>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'

interface ToolCall {
  name: string
  arguments?: string | object
  result?: string
  error?: string
  status?: 'pending' | 'success' | 'error'
}

defineProps<{
  toolCalls: ToolCall[]
}>()

const expandedIndex = ref<number | null>(null)

function toggleExpand(index: number) {
  expandedIndex.value = expandedIndex.value === index ? null : index
}

function getToolIcon(name: string): string {
  const icons: Record<string, string> = {
    web_search: '🌐',
    web_extract: '🔍',
    read_file: '📄',
    write_file: '✏️',
    edit_file: '📝',
    list_files: '📁',
    search_in_files: '🔎',
    execute_command: '⚡',
    terminal: '💻',
    execute_code: '💻',
    memory_store: '💾',
    memory_recall: '🧠',
    delegate_task: '🎭',
    ha_call_service: '🏠',
    json: '📋',
    yaml: '📋',
    uuid: '🔑',
    random: '🎲',
    time: '⏰',
  }
  return icons[name] || '🛠️'
}

function formatJson(obj: string | object): string {
  try {
    const parsed = typeof obj === 'string' ? JSON.parse(obj) : obj
    return JSON.stringify(parsed, null, 2)
  } catch {
    return String(obj)
  }
}

function truncate(text: string, length: number): string {
  if (text.length <= length) return text
  return text.substring(0, length) + '...'
}
</script>

<style scoped>
.tool-call-display {
  margin: 16px 0;
  border: 1px solid var(--border-color);
  border-radius: 12px;
  overflow: hidden;
  background: var(--bg-secondary);
}
.tool-header {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 12px 16px;
  background: var(--bg-tertiary);
  border-bottom: 1px solid var(--border-color);
}
.tool-icon {
  font-size: 16px;
}
.tool-title {
  font-weight: 500;
  font-size: 14px;
}
.tool-count {
  margin-left: auto;
  padding: 2px 8px;
  background: var(--primary-color);
  color: white;
  border-radius: 10px;
  font-size: 12px;
}
.tool-list {
  max-height: 300px;
  overflow-y: auto;
}
.tool-item {
  border-bottom: 1px solid var(--border-color);
  cursor: pointer;
}
.tool-item:last-child {
  border-bottom: none;
}
.tool-main {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  transition: background 0.2s;
}
.tool-item:hover {
  background: var(--hover-bg);
}
.tool-icon-small {
  font-size: 16px;
}
.tool-name {
  font-size: 14px;
  font-weight: 500;
}
.tool-status {
  padding: 2px 8px;
  border-radius: 10px;
  font-size: 11px;
  text-transform: uppercase;
}
.tool-status.pending {
  background: #f59e0b;
  color: white;
}
.tool-status.success {
  background: #10b981;
  color: white;
}
.tool-status.error {
  background: #ef4444;
  color: white;
}
.expand-icon {
  margin-left: auto;
  font-size: 12px;
  color: var(--text-secondary);
}
.tool-details {
  padding: 12px 16px;
  background: var(--bg-primary);
  border-top: 1px solid var(--border-color);
}
.args-header, .result-header, .error-header {
  font-size: 12px;
  color: var(--text-secondary);
  margin-bottom: 8px;
}
.error-header {
  color: #ef4444;
}
.args-content, .result-content, .error-content {
  margin: 0;
  padding: 12px;
  background: var(--bg-tertiary);
  border-radius: 8px;
  font-size: 12px;
  font-family: monospace;
  overflow-x: auto;
  white-space: pre-wrap;
  word-break: break-all;
}
.error-content {
  color: #ef4444;
  background: rgba(239, 68, 68, 0.1);
}
</style>
