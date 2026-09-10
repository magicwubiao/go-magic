<template>
  <div>
    <h2 style="margin-bottom: 24px;">{{ t('system.title') }}</h2>
    <n-spin v-if="systemStore.loading" />
    <n-grid v-else :cols="2" :x-gap="16" :y-gap="16">
      <n-gi>
        <n-card :title="t('system.systemInformation')">
          <n-descriptions :column="1">
            <n-descriptions-item :label="t('system.version')">
              {{ systemStore.info?.version }}
            </n-descriptions-item>
            <n-descriptions-item :label="t('system.platform')">
              {{ systemStore.info?.platform }}
            </n-descriptions-item>
            <n-descriptions-item :label="t('system.architecture')">
              {{ systemStore.info?.arch }}
            </n-descriptions-item>
            <n-descriptions-item :label="t('system.goVersion')">
              {{ systemStore.info?.go_version }}
            </n-descriptions-item>
          </n-descriptions>
        </n-card>
      </n-gi>

      <n-gi>
        <n-card :title="t('system.systemStatus')">
          <n-descriptions :column="1">
            <n-descriptions-item :label="t('system.health')">
              <n-tag :type="systemStore.health?.status === 'healthy' ? 'success' : 'error'">
                {{ systemStore.health?.status || 'unknown' }}
              </n-tag>
            </n-descriptions-item>
            <n-descriptions-item :label="t('system.uptime')">
              {{ formatUptime(systemStore.stats?.uptime) }}
            </n-descriptions-item>
            <n-descriptions-item :label="t('system.memoryUsage')">
              {{ formatBytes(systemStore.stats?.memory_usage) }}
            </n-descriptions-item>
            <n-descriptions-item :label="t('system.goroutines')">
              {{ systemStore.stats?.goroutines }}
            </n-descriptions-item>
          </n-descriptions>
        </n-card>
      </n-gi>

      <n-gi :span="2">
        <n-card :title="t('system.links.title')">
          <div class="link-grid">
            <a
              v-for="link in projectLinks"
              :key="link.href"
              class="link-item"
              :href="link.href"
              target="_blank"
              rel="noopener noreferrer"
            >
              <n-icon :component="link.icon" :size="22" class="link-icon" />
              <div class="link-text">
                <div class="link-name">{{ link.name }}</div>
                <div class="link-desc">{{ link.desc }}</div>
                <div class="link-href">{{ link.href }}</div>
              </div>
              <n-icon :component="OpenOutline" :size="16" class="link-arrow" />
            </a>
          </div>
        </n-card>
      </n-gi>

      <n-gi :span="2">
        <UpdateManager />
      </n-gi>
    </n-grid>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Component } from 'vue'
import {
  BookOutline,
  GlobeOutline,
  LogoGithub,
  OpenOutline,
} from '@vicons/ionicons5'
import { useSystemStore } from '@/stores/system'
import UpdateManager from '@/components/UpdateManager.vue'

const { t } = useI18n()
const systemStore = useSystemStore()

interface ProjectLink {
  name: string
  desc: string
  href: string
  icon: Component
}

const projectLinks = computed<ProjectLink[]>(() => [
  {
    name: t('system.links.website'),
    desc: t('system.links.websiteDesc'),
    href: 'https://magictech.cc/',
    icon: GlobeOutline,
  },
  {
    name: t('system.links.docs'),
    desc: t('system.links.docsDesc'),
    href: 'https://magictech.cc/docs.html',
    icon: BookOutline,
  },
  {
    name: t('system.links.github'),
    desc: t('system.links.githubDesc'),
    href: 'https://github.com/magicwubiao/go-magic.git',
    icon: LogoGithub,
  },
])

function formatUptime(seconds?: number): string {
  if (seconds === undefined || seconds === null || seconds < 0) return t('system.na')
  const days = Math.floor(seconds / 86400)
  const hours = Math.floor((seconds % 86400) / 3600)
  const minutes = Math.floor((seconds % 3600) / 60)
  const secs = Math.floor(seconds % 60)

  const parts: string[] = []
  if (days > 0) parts.push(`${days}d`)
  if (hours > 0) parts.push(`${hours}h`)
  if (minutes > 0) parts.push(`${minutes}m`)
  if (secs > 0 && parts.length === 0) parts.push(`${secs}s`)
  return parts.join(' ') || '0s'
}

function formatBytes(bytes?: number): string {
  if (bytes === undefined || bytes === null) return t('system.na')
  const mb = bytes / 1024 / 1024
  return `${mb.toFixed(2)} MB`
}

onMounted(() => {
  systemStore.loadAll()
})
</script>

<style scoped>
.link-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 12px;
}

.link-item {
  display: flex;
  align-items: flex-start;
  gap: 12px;
  padding: 12px 14px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  text-decoration: none;
  color: inherit;
  transition: border-color 0.2s, box-shadow 0.2s, transform 0.2s;
}

.link-item:hover {
  border-color: #2080f0;
  box-shadow: 0 2px 8px rgba(32, 128, 240, 0.15);
  transform: translateY(-1px);
}

.link-icon {
  color: #2080f0;
  flex-shrink: 0;
  margin-top: 2px;
}

.link-text {
  flex: 1;
  min-width: 0;
}

.link-name {
  font-size: 14px;
  font-weight: 500;
  line-height: 1.4;
}

.link-desc {
  margin-top: 2px;
  font-size: 12px;
  color: #888;
}

.link-href {
  margin-top: 4px;
  font-size: 12px;
  color: #2080f0;
  overflow-wrap: anywhere;
}

.link-arrow {
  color: #bbb;
  flex-shrink: 0;
  margin-top: 2px;
}

.link-item:hover .link-arrow {
  color: #2080f0;
}

@media (max-width: 768px) {
  .link-grid {
    grid-template-columns: minmax(0, 1fr);
  }
}

@media (prefers-color-scheme: dark) {
  .link-item {
    border-color: #3a3a3a;
  }
  .link-desc {
    color: #777;
  }
  .link-arrow {
    color: #666;
  }
}
</style>
