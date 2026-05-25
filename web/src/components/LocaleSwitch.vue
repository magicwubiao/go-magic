<script setup lang="ts">
import { computed } from 'vue'
import { NDropdown, NIcon } from 'naive-ui'
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
      <span class="locale-icon">🌐</span>
      <span class="locale-name">{{ locales.find(l => l.code === currentLocale)?.name }}</span>
    </div>
  </n-dropdown>
</template>

<style scoped>
.locale-switch {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 12px;
  border-radius: 6px;
  cursor: pointer;
  transition: background-color 0.2s;
  color: var(--text-color, #fff);
}

.locale-switch:hover {
  background-color: rgba(255, 255, 255, 0.1);
}

.locale-icon {
  font-size: 16px;
}

.locale-name {
  font-size: 14px;
  font-weight: 500;
}
</style>
