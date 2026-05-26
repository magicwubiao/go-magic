<template>
  <div>
    <h2 style="margin-bottom: 24px;">{{ t('tools.title') }}</h2>
    <n-spin v-if="toolsStore.loading" />
    <template v-else>
      <!-- Toolsets -->
      <n-card :title="t('tools.toolsets')" style="margin-bottom: 24px;">
        <n-grid :cols="3" :x-gap="12" :y-gap="12">
          <n-gi v-for="toolset in toolsStore.toolsets" :key="toolset.id">
            <n-card size="small">
              <template #header>
                <n-space align="center">
                  <span style="font-weight: 500;">{{ toolset.name }}</span>
                  <n-tag :type="toolset.enabled ? 'success' : 'default'" size="small">
                    {{ toolset.enabled ? t('tools.enabled') : t('tools.disabled') }}
                  </n-tag>
                </n-space>
              </template>
              <template #header-extra>
                <n-switch v-model:value="toolset.enabled" size="small" @update:value="toggleToolset(toolset.id, $event)" />
              </template>
              <n-text depth="3">{{ toolset.description || t('tools.noDescription') }}</n-text>
              <template #footer>
                <n-text depth="3" style="font-size: 12px;">
                  {{ t('tools.toolsCount', { count: toolset.tools?.length || 0 }) }}
                </n-text>
              </template>
            </n-card>
          </n-gi>
        </n-grid>
        <n-empty v-if="!toolsStore.toolsets.length" :description="t('tools.noToolsets')" />
      </n-card>

      <!-- Tools -->
      <n-card :title="t('tools.allTools')">
        <n-grid :cols="3" :x-gap="12" :y-gap="12">
          <n-gi v-for="tool in toolsStore.tools" :key="tool.id">
            <n-card size="small">
              <template #header>
                <span style="font-weight: 500;">{{ tool.name }}</span>
              </template>
              <n-space vertical size="small">
                <n-text depth="3">{{ tool.description || t('tools.noDescription') }}</n-text>
                <n-text depth="3" style="font-size: 12px;">{{ t('tools.category') }}: {{ tool.category || t('tools.general') }}</n-text>
              </n-space>
            </n-card>
          </n-gi>
        </n-grid>
        <n-empty v-if="!toolsStore.tools.length" :description="t('tools.noTools')" />
      </n-card>
    </template>
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useToolsStore } from '@/stores/tools'

const { t } = useI18n()
const message = useMessage()
const toolsStore = useToolsStore()

async function toggleToolset(id: string, enabled: boolean): Promise<void> {
  try {
    await toolsStore.toggleToolset(id, enabled)
    await toolsStore.loadToolsets()
    message.success(enabled ? t('tools.toolsetEnabled') : t('tools.toolsetDisabled'))
  } catch (e) {
    message.error(t('tools.failedToToggle'))
  }
}

onMounted(() => {
  toolsStore.loadToolsets()
  toolsStore.loadTools()
})
</script>
