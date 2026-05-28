<template>
  <div class="update-manager">
    <n-card :title="t('system.update.title')" size="small">
      <div class="version-info">
        <div class="version-row">
          <span class="version-label">{{ t('system.update.currentVersion') }}</span>
          <n-tag type="info" size="small">{{ versionInfo?.version || 'unknown' }}</n-tag>
        </div>
        <div class="version-row">
          <span class="version-label">{{ t('system.platform') }}</span>
          <span>{{ versionInfo?.platform }} / {{ versionInfo?.arch }}</span>
        </div>
        <div v-if="versionInfo?.commit && versionInfo.commit !== 'unknown'" class="version-row">
          <span class="version-label">{{ t('common.version') }}</span>
          <n-code :code="versionInfo.commit.slice(0, 8)" inline />
        </div>
      </div>

      <n-divider />

      <div class="update-check">
        <n-button
          :loading="checking"
          :disabled="checking"
          type="primary"
          size="small"
          @click="checkUpdate"
        >
          <template #icon>
            <RefreshOutline />
          </template>
          {{ checking ? t('system.update.checking') : t('system.update.checkUpdate') }}
        </n-button>

        <n-alert
          v-if="updateResult"
          :type="updateResult.has_update ? 'warning' : 'success'"
          size="small"
          style="margin-top: 12px"
        >
          <template #header>
            {{ updateResult.has_update ? t('system.update.hasUpdate') : t('system.update.noUpdate') }}
          </template>
          <div v-if="updateResult.has_update" class="update-details">
            <div class="update-version">
              <span>{{ t('system.update.latestVersion') }}: <strong>{{ updateResult.latest_version }}</strong></span>
              <n-tag v-if="updateResult.prerelease" type="warning" size="tiny">{{ t('system.update.prerelease') }}</n-tag>
            </div>
            <div v-if="updateResult.published_at" class="update-date">
              {{ t('system.update.publishedAt') }}: {{ formatDate(updateResult.published_at) }}
            </div>
            <div v-if="updateResult.asset_size" class="update-size">
              {{ t('system.update.fileSize') }}: {{ formatSize(updateResult.asset_size) }}
            </div>
            <n-collapse v-if="updateResult.release_notes" style="margin-top: 8px">
              <n-collapse-item :title="t('system.update.releaseNotes')">
                <pre class="release-notes">{{ updateResult.release_notes }}</pre>
              </n-collapse-item>
            </n-collapse>
            <div class="update-actions">
              <n-button
                type="primary"
                size="small"
                tag="a"
                :href="updateResult.html_url"
                target="_blank"
              >
                {{ t('system.update.viewRelease') }}
              </n-button>
              <n-button
                v-if="updateResult.download_url"
                size="small"
                tag="a"
                :href="updateResult.download_url"
              >
                {{ t('system.update.downloadUpdate') }}
              </n-button>
            </div>
          </div>
        </n-alert>
      </div>
    </n-card>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import { RefreshOutline } from '@vicons/ionicons5'
import * as systemApi from '@/api/system'
import type { SystemVersion, VersionCheckResult } from '@/api/system'

const { t } = useI18n()
const versionInfo = ref<SystemVersion | null>(null)
const updateResult = ref<VersionCheckResult | null>(null)
const checking = ref(false)

onMounted(async () => {
  try {
    versionInfo.value = await systemApi.getSystemVersion()
  } catch (e) {
    console.error('Failed to get version:', e)
  }
})

async function checkUpdate() {
  checking.value = true
  try {
    updateResult.value = await systemApi.checkForUpdates()
  } catch (e) {
    console.error('Failed to check update:', e)
  } finally {
    checking.value = false
  }
}

function formatDate(dateStr: string): string {
  const date = new Date(dateStr)
  return date.toLocaleDateString('zh-CN', {
    year: 'numeric',
    month: 'long',
    day: 'numeric',
  })
}

function formatSize(bytes: number): string {
  if (bytes < 1024) return bytes + ' B'
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB'
  return (bytes / (1024 * 1024)).toFixed(1) + ' MB'
}
</script>

<style scoped>
.update-manager {
  max-width: 600px;
}

.version-info {
  display: flex;
  flex-direction: column;
  gap: 8px;
}

.version-row {
  display: flex;
  align-items: center;
  gap: 12px;
}

.version-label {
  color: #666;
  font-size: 13px;
  min-width: 80px;
}

.update-check {
  margin-top: 8px;
}

.update-details {
  font-size: 13px;
}

.update-version {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-bottom: 4px;
}

.update-date,
.update-size {
  color: #888;
  font-size: 12px;
}

.release-notes {
  max-height: 200px;
  overflow-y: auto;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 12px;
  line-height: 1.6;
  background: #f5f5f5;
  padding: 8px;
  border-radius: 4px;
  margin: 0;
}

.update-actions {
  display: flex;
  gap: 8px;
  margin-top: 12px;
}

/* Dark mode */
@media (prefers-color-scheme: dark) {
  .version-label {
    color: #999;
  }
  .update-date,
  .update-size {
    color: #777;
  }
  .release-notes {
    background: #2a2a2a;
  }
}
</style>