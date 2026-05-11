<template>
  <div class="message-bubble" :class="{ 'is-own': isOwn, 'is-agent': message.type === 'agent' }">
    <n-avatar v-if="!isOwn" :size="32" round class="avatar">
      {{ message.username.charAt(0).toUpperCase() }}
    </n-avatar>
    
    <div class="message-content">
      <div class="message-header">
        <span class="username">{{ message.username }}</span>
        <n-tag v-if="message.type === 'agent'" type="warning" size="small">Agent</n-tag>
        <span class="timestamp">{{ formatTime(message.timestamp) }}</span>
      </div>
      
      <div class="message-body" v-html="renderedContent" />
    </div>
    
    <n-avatar v-if="isOwn" :size="32" round class="avatar">
      {{ message.username.charAt(0).toUpperCase() }}
    </n-avatar>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { NAvatar, NTag } from 'naive-ui'
import type { Message } from '../api/groupchat'

const props = defineProps<{
  message: Message
  isOwn: boolean
}>()

const renderedContent = computed(() => {
  // Simple markdown rendering
  let content = props.message.content
  // Escape HTML
  content = content.replace(/&/g, '&amp;')
                   .replace(/</g, '&lt;')
                   .replace(/>/g, '&gt;')
  // Bold
  content = content.replace(/\*\*(.*?)\*\*/g, '<strong>$1</strong>')
  // Italic
  content = content.replace(/\*(.*?)\*/g, '<em>$1</em>')
  // Code
  content = content.replace(/`(.*?)`/g, '<code>$1</code>')
  // Code block
  content = content.replace(/```([\s\S]*?)```/g, '<pre><code>$1</code></pre>')
  // Line breaks
  content = content.replace(/\n/g, '<br>')
  return content
})

function formatTime(timestamp: string): string {
  const date = new Date(timestamp)
  return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}
</script>

<style scoped>
.message-bubble {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
  padding: 8px;
  border-radius: 8px;
}

.message-bubble.is-own {
  flex-direction: row-reverse;
}

.message-bubble.is-agent {
  background: rgba(255, 193, 7, 0.1);
  border-left: 3px solid #ffc107;
}

.avatar {
  flex-shrink: 0;
}

.message-content {
  flex: 1;
  max-width: 70%;
}

.message-header {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}

.username {
  font-weight: 600;
  font-size: 13px;
}

.timestamp {
  font-size: 11px;
  color: var(--text-color-3);
}

.message-body {
  padding: 10px 14px;
  background: var(--card-color);
  border-radius: 12px;
  font-size: 14px;
  line-height: 1.5;
  word-wrap: break-word;
}

.is-own .message-body {
  background: var(--primary-color);
  color: white;
}

.message-body code {
  background: rgba(0, 0, 0, 0.1);
  padding: 2px 4px;
  border-radius: 4px;
  font-family: 'Fira Code', monospace;
}

.is-own .message-body code {
  background: rgba(255, 255, 255, 0.2);
}

.message-body pre {
  background: rgba(0, 0, 0, 0.1);
  padding: 8px;
  border-radius: 8px;
  overflow-x: auto;
  margin-top: 8px;
}

.is-own .message-body pre {
  background: rgba(255, 255, 255, 0.2);
}
</style>
