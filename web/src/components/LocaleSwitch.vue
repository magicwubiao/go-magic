<script setup lang="ts">
import { computed } from 'vue'
import { NDropdown } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { locales, setLocale, getLocale } from '../locales'

const { t } = useI18n()

const currentLocale = computed(() => getLocale())

function handleSelect(code: string) {
  setLocale(code)
}
</script>

<template>
  <n-dropdown
    :options="locales.map(l => ({ key: l.code, label: l.name }))"
    @select="handleSelect"
    trigger="click"
  >
    <div class="locale-switch">
      <svg class="locale-icon" viewBox="0 0 512 512" fill="currentColor">
        <path d="M256 48C141.13 48 48 141.13 48 256s93.13 208 208 208 208-93.13 208-208S370.87 48 256 48zm-88 208c0-25.34 5.56-49.36 15.49-71.12-21.64 15.14-36.49 38.46-41.33 65.12h25.84zm88 168c-28.06-27.72-47.68-64.92-54.43-104h108.86c-6.75 39.08-26.37 76.28-54.43 104zm-54.43-136c6.75-39.08 26.37-76.28 54.43-104 28.06 27.72 47.68 64.92 54.43 104H201.57zm12-136c13.68 17.2 24.91 36.21 33.43 56h66c8.52-19.79 19.75-38.8 33.43-56-21.64 15.14-36.49 38.46-41.33 65.12h25.84c4.84-26.66 19.69-49.98 41.33-65.12C378.44 206.64 384 230.66 384 256h-25.84c-4.84-26.66-19.69-49.98-41.33-65.12 13.68 17.2 24.91 36.21 33.43 56h66c8.52-19.79 19.75-38.8 33.43-56-21.64 15.14-36.49 38.46-41.33 65.12H464c0-25.34-5.56-49.36-15.49-71.12 21.64 15.14 36.49 38.46 41.33 65.12h25.84c0-114.87-93.13-208-208-208-28.06 27.72-47.68 64.92-54.43 104h108.86c6.75-39.08 26.37-76.28 54.43-104z"/>
      </svg>
      <span class="locale-name">{{ locales.find(l => l.code === currentLocale)?.name }}</span>
    </div>
  </n-dropdown>
</template>

<style scoped>
.locale-switch {
  display: flex;
  align-items: center;
  justify-content: flex-start;
  gap: 12px;
  padding: 10px 20px;
  border-radius: 6px;
  cursor: pointer;
  transition: background-color 0.2s;
  color: rgb(51, 54, 57);
  width: 100%;
  height: 44px;
}

.locale-switch:hover {
  background-color: rgba(0, 0, 0, 0.05);
  color: #18a058;
}

.locale-icon {
  width: 22px;
  height: 22px;
  flex-shrink: 0;
}

.locale-name {
  font-size: 14px;
  font-weight: 500;
}
</style>
