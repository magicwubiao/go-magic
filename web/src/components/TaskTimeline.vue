<template>
  <div class="task-timeline">
    <div class="timeline-body">
      <div
        v-for="(step, idx) in steps"
        :key="idx"
        class="timeline-step"
        :class="[`status-${step.status}`]"
      >
        <div class="step-marker">
          <div class="step-dot" :class="[`status-${step.status}`]">
            <span v-if="step.status === 'completed'">✓</span>
            <span v-else-if="step.status === 'failed'">✗</span>
            <span v-else-if="step.status === 'running'" class="spinner"></span>
            <span v-else>{{ idx + 1 }}</span>
          </div>
          <div v-if="idx < steps.length - 1" class="step-line" :class="[`status-${step.status}`]"></div>
        </div>
        <div class="step-content">
          <div class="step-title">{{ step.title }}</div>
          <div v-if="step.description" class="step-desc">{{ step.description }}</div>
          <div v-if="step.status === 'running' && step.detail" class="step-detail">{{ step.detail }}</div>
          <div v-if="step.duration" class="step-meta">{{ step.duration }}</div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import type { PropType } from 'vue'

export interface TimelineStep {
  title: string
  description?: string
  status: 'pending' | 'running' | 'completed' | 'failed' | 'skipped'
  detail?: string
  duration?: string
}

defineProps({
  steps: { type: Array as PropType<TimelineStep[]>, required: true },
  title: { type: String, default: '' },
  overallPercent: { type: Number, default: undefined },
})
</script>

<style scoped>
.task-timeline {
  padding: 4px 0;
  margin: 4px 0;
}
.timeline-body {
  display: flex;
  flex-direction: column;
  gap: 0;
}
.timeline-step {
  display: flex;
  gap: 8px;
  padding: 3px 0;
}
.step-marker {
  display: flex;
  flex-direction: column;
  align-items: center;
  width: 16px;
  flex-shrink: 0;
}
.step-dot {
  width: 14px;
  height: 14px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 9px;
  background: transparent;
  color: #ccc;
  border: 1px solid #ddd;
  transition: all 0.3s;
}
.step-dot.status-pending {
  color: #ccc;
  border-color: #ddd;
}
.step-dot.status-running {
  color: #999;
  border-color: #bbb;
}
.step-dot.status-completed {
  color: #aaa;
  border-color: #ccc;
}
.step-dot.status-failed {
  color: #d9a0a0;
  border-color: #e0c0c0;
}
.step-dot.status-skipped {
  color: #ddd;
  border-color: #e8e8e8;
}
.step-line {
  width: 1px;
  flex: 1;
  min-height: 12px;
  background: #eee;
  margin: 2px 0;
}
.step-line.status-completed {
  background: #ddd;
}
.step-line.status-running {
  background: #e0e0e0;
}
.step-line.status-failed {
  background: #f0d0d0;
}
.step-content {
  flex: 1;
  padding-top: 0;
}
.step-title {
  font-size: 12px;
  font-weight: 400;
  color: #888;
}
.step-desc {
  font-size: 10px;
  color: #bbb;
  margin-top: 1px;
}
.step-detail {
  font-size: 10px;
  color: #aaa;
  margin-top: 2px;
}
.step-meta {
  font-size: 9px;
  color: #ccc;
  margin-top: 1px;
}
.spinner {
  width: 6px;
  height: 6px;
  border: 1px solid #bbb;
  border-top-color: transparent;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
}
@keyframes spin {
  to { transform: rotate(360deg); }
}
/* Dark mode */
@media (prefers-color-scheme: dark) {
  .step-dot {
    color: #555;
    border-color: #444;
  }
  .step-dot.status-completed {
    color: #666;
    border-color: #555;
  }
  .step-dot.status-running {
    color: #777;
    border-color: #666;
  }
  .step-dot.status-failed {
    color: #855;
    border-color: #644;
  }
  .step-line {
    background: #333;
  }
  .step-line.status-completed {
    background: #444;
  }
  .step-title {
    color: #777;
  }
  .step-desc {
    color: #555;
  }
  .step-detail {
    color: #666;
  }
  .step-meta {
    color: #444;
  }
}
</style>
