<template>
  <div>
    <n-space justify="space-between" style="margin-bottom: 16px;">
      <h2>Plugins</h2>
      <n-space>
        <n-button @click="pluginsStore.rescan()">🔄 Rescan</n-button>
        <n-button type="primary" @click="showInstall = true">+ Install</n-button>
      </n-space>
    </n-space>

    <n-spin v-if="pluginsStore.loading" />
    <n-list v-else bordered>
      <n-list-item v-for="plugin in pluginsStore.plugins" :key="plugin.id">
        <n-thing :title="plugin.name">
          <template #description>
            <n-space vertical>
              <n-text depth="3">{{ plugin.description }}</n-text>
              <n-space>
                <n-tag size="small">{{ plugin.version }}</n-tag>
                <n-tag size="small">{{ plugin.type }}</n-tag>
                <n-tag size="small">{{ plugin.author }}</n-tag>
              </n-space>
            </n-space>
          </template>
          <template #header-extra>
            <n-tag :type="plugin.enabled ? 'success' : 'default'">
              {{ plugin.enabled ? 'Enabled' : 'Disabled' }}
            </n-tag>
          </template>
          <template #action>
            <n-space>
              <n-button v-if="!plugin.enabled" size="small" type="primary" @click="pluginsStore.enablePlugin(plugin.id)">Enable</n-button>
              <n-button v-else size="small" @click="pluginsStore.disablePlugin(plugin.id)">Disable</n-button>
              <n-button size="small" type="error" @click="deletePlugin(plugin.id)">Delete</n-button>
            </n-space>
          </template>
        </n-thing>
      </n-list-item>
    </n-list>

    <!-- Install Modal -->
    <n-modal v-model:show="showInstall" title="Install Plugin">
      <n-card style="width: 500px;">
        <n-form>
          <n-form-item label="Plugin URL">
            <n-input v-model:value="installUrl" placeholder="https://github.com/user/plugin" />
          </n-form-item>
        </n-form>
        <template #footer>
          <n-space justify="end">
            <n-button @click="showInstall = false">Cancel</n-button>
            <n-button type="primary" @click="install">Install</n-button>
          </n-space>
        </template>
      </n-card>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { usePluginsStore } from '@/stores/plugins'

const message = useMessage()
const pluginsStore = usePluginsStore()
const showInstall = ref(false)
const installUrl = ref('')

async function install() {
  if (!installUrl.value) return
  await pluginsStore.installPlugin(installUrl.value)
  installUrl.value = ''
  showInstall.value = false
  message.success('Plugin installed')
}

async function deletePlugin(id: string) {
  await pluginsStore.deletePlugin(id)
  message.success('Plugin deleted')
}

onMounted(() => pluginsStore.loadPlugins())
</script>
