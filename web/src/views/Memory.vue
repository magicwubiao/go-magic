<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { NButton, NInput, NSpin, useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'

const { t } = useI18n()
const message = useMessage()

const loading = ref(false)
const data = ref<{
  memory?: string
  user?: string
  soul?: string
} | null>(null)
const editingSection = ref<'memory' | 'user' | 'soul' | null>(null)
const editContent = ref('')
const saving = ref(false)

async function loadMemory() {
  loading.value = true
  try {
    const res = await fetch('/api/memory')
    if (res.ok) {
      data.value = await res.json()
    } else {
      // Try CLI fallback
      const cliRes = await fetch('/api/cli', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ command: 'context', args: ['load'], profile: '' })
      })
      const cliData = await cliRes.json()
      if (cliData.output) {
        data.value = parseContextFiles(cliData.output)
      }
    }
  } catch (err: any) {
    console.error('Failed to load memory:', err)
    message.error(t('memory.loadFailed'))
  } finally {
    loading.value = false
  }
}

function parseContextFiles(output: string): { memory?: string; user?: string; soul?: string } {
  const result: { memory?: string; user?: string; soul?: string } = {}
  const sections = output.split(/#{1,3}\s+(MEMORY|USER|SOUL)/i)
  for (let i = 1; i < sections.length; i += 2) {
    const title = sections[i].toLowerCase()
    const content = sections[i + 1] || ''
    if (title === 'memory') result.memory = content.trim()
    else if (title === 'user') result.user = content.trim()
    else if (title === 'soul') result.soul = content.trim()
  }
  return result
}

function startEdit(section: 'memory' | 'user' | 'soul') {
  editingSection.value = section
  editContent.value = (data.value?.[section] || '').replace(/§/g, '\n\n')
}

function cancelEdit() {
  editingSection.value = null
  editContent.value = ''
}

async function handleSave() {
  if (!editingSection.value) return
  saving.value = true
  try {
    const content = editContent.value.replace(/\n\n+/g, '§')
    // Save via CLI
    await fetch('/api/cli', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        command: 'context',
        args: ['save', editingSection.value],
        input: content
      })
    })
    await loadMemory()
    editingSection.value = null
    editContent.value = ''
    message.success(t('common.saved'))
  } catch (err: any) {
    message.error(`${t('common.saveFailed')}: ${err.message}`)
  } finally {
    saving.value = false
  }
}

function formatTime(): string {
  return new Date().toLocaleString()
}

const displayMemory = computed(() => (data.value?.memory || '').replace(/§/g, '\n\n'))
const displayUser = computed(() => (data.value?.user || '').replace(/§/g, '\n\n'))
const displaySoul = computed(() => (data.value?.soul || '').replace(/§/g, '\n\n'))

onMounted(loadMemory)
</script>

<template>
  <div class="memory-view">
    <header class="page-header">
      <h2 class="header-title">{{ t('memory.title') }}</h2>
      <NButton size="small" quaternary @click="loadMemory" :loading="loading">
        {{ t('memory.refresh') }}
      </NButton>
    </header>

    <div class="memory-content">
      <NSpin :show="loading && !data" size="large">
        <div v-if="loading && !data" class="memory-loading">{{ t('common.loading') }}</div>
        <div v-else class="memory-sections">
          <!-- My Notes (Memory) -->
          <div class="memory-section">
            <div class="section-header">
              <div class="section-title-row">
                <span class="section-icon">
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
                    <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" />
                    <polyline points="14 2 14 8 20 8" />
                  </svg>
                </span>
                <span class="section-title">{{ t('memory.myNotes') }}</span>
              </div>
              <div class="section-actions">
                <NButton
                  size="tiny"
                  :type="editingSection === 'memory' ? 'primary' : 'default'"
                  @click="editingSection === 'memory' ? cancelEdit() : startEdit('memory')"
                >
                  {{ editingSection === 'memory' ? t('common.cancel') : t('common.edit') }}
                </NButton>
              </div>
            </div>
            <div v-if="editingSection === 'memory'" class="section-editor">
              <NInput
                v-model:value="editContent"
                type="textarea"
                :rows="10"
                :placeholder="t('memory.notesPlaceholder')"
              />
              <div class="editor-actions">
                <NButton size="small" @click="cancelEdit">{{ t('common.cancel') }}</NButton>
                <NButton size="small" type="primary" :loading="saving" @click="handleSave">
                  {{ t('common.save') }}
                </NButton>
              </div>
            </div>
            <div v-else class="section-content" :class="{ empty: !displayMemory }">
              <pre v-if="displayMemory">{{ displayMemory }}</pre>
              <span v-else class="empty-hint">{{ t('memory.notesEmpty') }}</span>
            </div>
          </div>

          <!-- User Profile -->
          <div class="memory-section">
            <div class="section-header">
              <div class="section-title-row">
                <span class="section-icon">
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
                    <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2" />
                    <circle cx="12" cy="7" r="4" />
                  </svg>
                </span>
                <span class="section-title">{{ t('memory.userProfile') }}</span>
              </div>
              <div class="section-actions">
                <NButton
                  size="tiny"
                  :type="editingSection === 'user' ? 'primary' : 'default'"
                  @click="editingSection === 'user' ? cancelEdit() : startEdit('user')"
                >
                  {{ editingSection === 'user' ? t('common.cancel') : t('common.edit') }}
                </NButton>
              </div>
            </div>
            <div v-if="editingSection === 'user'" class="section-editor">
              <NInput
                v-model:value="editContent"
                type="textarea"
                :rows="10"
                :placeholder="t('memory.userPlaceholder')"
              />
              <div class="editor-actions">
                <NButton size="small" @click="cancelEdit">{{ t('common.cancel') }}</NButton>
                <NButton size="small" type="primary" :loading="saving" @click="handleSave">
                  {{ t('common.save') }}
                </NButton>
              </div>
            </div>
            <div v-else class="section-content" :class="{ empty: !displayUser }">
              <pre v-if="displayUser">{{ displayUser }}</pre>
              <span v-else class="empty-hint">{{ t('memory.userEmpty') }}</span>
            </div>
          </div>

          <!-- Soul (Personality) -->
          <div class="memory-section">
            <div class="section-header">
              <div class="section-title-row">
                <span class="section-icon">
                  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5">
                    <path d="M12 2l3.09 6.26L22 9.27l-5 4.87 1.18 6.88L12 17.77l-6.18 3.25L7 14.14 2 9.27l6.91-1.01L12 2z" />
                  </svg>
                </span>
                <span class="section-title">{{ t('memory.soul') }}</span>
              </div>
              <div class="section-actions">
                <NButton
                  size="tiny"
                  :type="editingSection === 'soul' ? 'primary' : 'default'"
                  @click="editingSection === 'soul' ? cancelEdit() : startEdit('soul')"
                >
                  {{ editingSection === 'soul' ? t('common.cancel') : t('common.edit') }}
                </NButton>
              </div>
            </div>
            <div v-if="editingSection === 'soul'" class="section-editor">
              <NInput
                v-model:value="editContent"
                type="textarea"
                :rows="10"
                :placeholder="t('memory.soulPlaceholder')"
              />
              <div class="editor-actions">
                <NButton size="small" @click="cancelEdit">{{ t('common.cancel') }}</NButton>
                <NButton size="small" type="primary" :loading="saving" @click="handleSave">
                  {{ t('common.save') }}
                </NButton>
              </div>
            </div>
            <div v-else class="section-content" :class="{ empty: !displaySoul }">
              <pre v-if="displaySoul">{{ displaySoul }}</pre>
              <span v-else class="empty-hint">{{ t('memory.soulEmpty') }}</span>
            </div>
          </div>
        </div>
      </NSpin>
    </div>
  </div>
</template>

<style scoped lang="scss">
.memory-view {
  height: calc(100 * var(--vh));
  display: flex;
  flex-direction: column;
}

.memory-content {
  flex: 1;
  overflow-y: auto;
  padding: 20px;
}

.memory-loading {
  text-align: center;
  color: #909399;
  padding: 40px 0;
}

.memory-sections {
  display: flex;
  flex-direction: column;
  gap: 20px;
}

.memory-section {
  background: #1a1a2e;
  border: 1px solid #303133;
  border-radius: 8px;
  overflow: hidden;
}

.section-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 12px 16px;
  background: rgba(255, 255, 255, 0.03);
  border-bottom: 1px solid #303133;
}

.section-title-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.section-icon {
  color: #ffd700;
}

.section-title {
  font-size: 14px;
  font-weight: 600;
  color: #e6e6e6;
}

.section-actions {
  display: flex;
  gap: 8px;
}

.section-editor {
  padding: 16px;
}

.editor-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
  margin-top: 12px;
}

.section-content {
  padding: 16px;
  max-height: 300px;
  overflow-y: auto;

  pre {
    margin: 0;
    white-space: pre-wrap;
    word-break: break-word;
    font-family: inherit;
    font-size: 13px;
    color: #c0c4cc;
    line-height: 1.6;
  }

  &.empty {
    min-height: 60px;
  }
}

.empty-hint {
  color: #606266;
  font-style: italic;
}
</style>
