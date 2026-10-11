<template>
  <!-- 单段思考：过程区内的一个独立可折叠块。
       折叠态：一行预览文本（预览即标题，不再重复"思考过程"字样）。
       展开态：小标题 + 完整内容。 -->
  <div class="thought-block">
    <button class="thought-toggle" type="button" :aria-expanded="expanded" @click="expanded = !expanded">
      <span class="thought-label">{{ t('chat.thinking') }}</span>
      <span class="thought-chevron" :class="{ open: expanded }">
        <svg viewBox="0 0 16 16" width="11" height="11" aria-hidden="true">
          <path d="M4 6l4 4 4-4" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
        </svg>
      </span>
    </button>
    <n-collapse-transition :show="expanded">
      <div class="thought-body">
        <ReasoningBody :text="text" />
      </div>
    </n-collapse-transition>
  </div>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { NCollapseTransition } from 'naive-ui'
import ReasoningBody from './ReasoningBody.vue'

const props = defineProps<{ text: string }>()

const { t } = useI18n()
const expanded = ref(true)
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

.thought-label {
  flex-shrink: 0;
}

.thought-chevron {
  display: inline-flex;
  flex-shrink: 0;
  color: #b0b5bd;
  transition: transform 0.18s;
}
.thought-chevron.open {
  transform: rotate(180deg);
}
.thought-toggle:hover .thought-chevron {
  color: #6b7280;
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
}
</style>
