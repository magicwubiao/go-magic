<template>
  <!-- 单段思考：过程区内的一个独立可折叠块。默认展开（过程区本身就是展开才可见），
       用户点头部可单独收起这一段。 -->
  <div class="thought-block">
    <button class="thought-toggle" type="button" :aria-expanded="expanded" @click="expanded = !expanded">
      <span class="thought-title">{{ t('chat.thinking') }}</span>
      <span class="thought-chevron" :class="{ open: expanded }">
        <svg viewBox="0 0 16 16" width="11" height="11" aria-hidden="true">
          <path d="M4 6l4 4 4-4" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </span>
    </button>
    <!-- 收起时的单行预览 -->
    <button
      v-if="!expanded && preview"
      class="thought-preview"
      type="button"
      @click="expanded = true"
    >{{ preview }}</button>
    <n-collapse-transition :show="expanded">
      <div class="thought-body">
        <ReasoningBody :text="text" />
      </div>
    </n-collapse-transition>
  </div>
</template>

<script setup lang="ts">
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { NCollapseTransition } from 'naive-ui'
import { stripZeroWidth } from '@/utils/text'
import ReasoningBody from './ReasoningBody.vue'

const props = defineProps<{ text: string }>()

const { t } = useI18n()
const expanded = ref(true)

// 收起时显示首行纯文本预览
const preview = computed(() => {
  const plain = stripZeroWidth(props.text || '')
    .replace(/```[\s\S]*?```/g, ' ')
    .replace(/[#*`>\-]+/g, ' ')
    .replace(/\s+/g, ' ')
    .trim()
  return plain.length > 100 ? `${plain.slice(0, 100)}…` : plain
})
</script>

<style scoped>
.thought-block {
  border-left: 2px solid #eef0f3;
  padding-left: 10px;
  margin: 2px 0;
}

.thought-toggle {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  padding: 0;
  background: none;
  border: none;
  cursor: pointer;
  user-select: none;
  color: #9ca3af;
  font-size: 12px;
  text-align: left;
}
.thought-toggle:hover {
  color: #6b7280;
}

.thought-title {
  flex-shrink: 0;
}

.thought-chevron {
  display: inline-flex;
  color: #b0b5bd;
  transition: transform 0.18s;
}
.thought-chevron.open {
  transform: rotate(180deg);
}
.thought-toggle:hover .thought-chevron {
  color: #6b7280;
}

.thought-preview {
  display: block;
  width: 100%;
  margin: 2px 0 0;
  padding: 0;
  background: none;
  border: none;
  cursor: pointer;
  text-align: left;
  font-size: 12px;
  line-height: 1.5;
  color: #b0b5bd;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  user-select: none;
}
.thought-preview:hover {
  color: #8b919a;
}

.thought-body {
  padding: 6px 0 2px 0;
}

@media (prefers-color-scheme: dark) {
  .thought-block {
    border-left-color: #2c2d31;
  }
  .thought-toggle {
    color: #6b7280;
  }
  .thought-toggle:hover {
    color: #9ca3af;
  }
  .thought-chevron {
    color: #565b64;
  }
  .thought-preview {
    color: #565b64;
  }
  .thought-preview:hover {
    color: #8b919a;
  }
}
</style>
