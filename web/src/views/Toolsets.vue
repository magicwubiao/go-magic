<template>
  <div class="toolsets-view">
    <n-space vertical :size="16">
      <h2>{{ $t('toolsets.title') }}</h2>

      <n-grid :cols="2" :x-gap="16" :y-gap="16">
        <n-gi v-for="toolset in toolsets" :key="toolset.name">
          <n-card>
            <template #header>
              <n-space justify="space-between" align="center">
                <n-space align="center">
                  <n-icon :component="Construct" size="20" />
                  <span class="toolset-name">{{ toolset.name }}</span>
                </n-space>
                <n-switch
                  :value="toolset.enabled"
                  @update:value="(v) => toggleToolset(toolset.name, v)"
                />
              </n-space>
            </template>
            <template #default>
              <p class="toolset-description">{{ toolset.description }}</p>
              <n-tag
                v-for="tag in toolset.tags"
                :key="tag"
                size="small"
                type="info"
              >
                {{ tag }}
              </n-tag>
              <div class="toolset-tools">
                <n-text depth="3">{{ toolset.tools.length }} tools</n-text>
              </div>
            </template>
          </n-card>
        </n-gi>
      </n-grid>
    </n-space>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { NIcon } from 'naive-ui'
import { Construct } from '@vicons/ionicons5'
import { toolsetApi } from '@/api'
import type { Toolset } from '@/types'

const toolsets = ref<Toolset[]>([])

async function loadToolsets() {
  try {
    const response = await toolsetApi.list()
    toolsets.value = response.data
  } catch (e) {
    console.error('Failed to load toolsets:', e)
  }
}

async function toggleToolset(name: string, enabled: boolean) {
  try {
    await toolsetApi.enable(name)
    const toolset = toolsets.value.find((t) => t.name === name)
    if (toolset) toolset.enabled = enabled
  } catch (e) {
    console.error('Failed to toggle toolset:', e)
  }
}

onMounted(() => {
  loadToolsets()
})
</script>

<style lang="scss" scoped>
.toolset-name {
  font-weight: 600;
}

.toolset-description {
  margin: 0 0 12px;
  color: var(--text-color-3);
}

.toolset-tools {
  margin-top: 12px;
}
</style>
