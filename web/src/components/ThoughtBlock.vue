<template>
  <!-- 单个文本切片：内部再按 <think> 标签拆分。
       - think 标签内的内容 = 真思考 → 可折叠块
       - 标签外的内容 = 模型的正常叙述/回答 → 直接平铺展示，不折叠
       （工具之间的叙述性文本会被时间线切成独立切片，它们不是思考，
         之前整片塞进折叠块导致"回答被渲染到思考中"。） -->
  <div class="thought-block">
    <template v-for="(part, i) in parts" :key="i">
      <!-- 平铺文本段 -->
      <div v-if="!part.isThink && part.text.trim()" class="thought-plain">
        <ReasoningBody :text="part.text" />
      </div>
      <!-- 思考段：可折叠 -->
      <div v-else-if="part.isThink && part.text.trim()" class="thought-collapse">
        <button class="thought-toggle" type="button" :aria-expanded="open" @click="open = !open">
          <span class="thought-label">{{ t('chat.thinking') }}</span>
          <span class="thought-chevron" :class="{ open }">
            <svg viewBox="0 0 16 16" width="11" height="11" aria-hidden="true">
              <path d="M4 6l4 4 4-4" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" />
            </svg>
          </span>
        </button>
        <n-collapse-transition :show="open">
          <div class="thought-body">
            <ReasoningBody :text="part.text" />
          </div>
        </n-collapse-transition>
      </div>
    </template>
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
const open = ref(true)

// 按 <think>...</think> 拆分（大小写不敏感；未闭合的 think 归思考——流式窗口）。
const parts = computed<Array<{ isThink: boolean; text: string }>>(() => {
  const content = props.text || ''
  const low = content.toLowerCase()
  const OPEN = '<think>'
  const CLOSE = '</think>'
  const out: Array<{ isThink: boolean; text: string }> = []
  let cursor = 0
  let searchFrom = 0
  const visible = (s: string) => stripZeroWidth(s).trim() !== ''

  while (true) {
    const oi = low.indexOf(OPEN, searchFrom)
    if (oi === -1) break
    if (oi > cursor) {
      const plain = content.substring(cursor, oi)
      if (visible(plain)) out.push({ isThink: false, text: plain.trim() })
    }
    const ci = low.indexOf(CLOSE, oi + OPEN.length)
    if (ci === -1) {
      // 未闭合：剩余全部归思考（流式中或模型漏闭合）
      const think = content.substring(oi + OPEN.length)
      if (visible(think)) out.push({ isThink: true, text: think.trim() })
      return out
    }
    const think = content.substring(oi + OPEN.length, ci)
    if (visible(think)) out.push({ isThink: true, text: think.trim() })
    cursor = ci + CLOSE.length
    searchFrom = cursor
  }
  if (cursor < content.length) {
    const plain = content.substring(cursor)
    if (visible(plain)) out.push({ isThink: false, text: plain.trim() })
  }
  return out
})
</script>

<style scoped>
.thought-block {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

/* 平铺的叙述文本：正常回答样式 */
.thought-plain {
  font-size: 14px;
  line-height: 1.7;
  color: #1f2937;
}

/* 思考折叠块：左侧细线 */
.thought-collapse {
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
  max-height: 50vh;
  overflow-y: auto;
  overscroll-behavior: contain;
}

@media (prefers-color-scheme: dark) {
  .thought-plain {
    color: #e5e7eb;
  }
  .thought-collapse {
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
