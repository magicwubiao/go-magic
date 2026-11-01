<template>
  <div>
    <div class="page-head">
      <div>
        <h2 style="margin: 0;">{{ t('media.title') }}</h2>
        <n-text depth="3" style="font-size: 12px;">{{ t('media.subtitle') }}</n-text>
      </div>
      <n-space>
        <n-button text @click="copyConfigPath" v-if="config?.config_path">
          <template #icon><n-icon><CopyOutline /></n-icon></template>
          {{ t('media.copyConfigPath') }}
        </n-button>
        <n-button :loading="loading" @click="load">
          <template #icon><n-icon><RefreshOutline /></n-icon></template>
        </n-button>
      </n-space>
    </div>

    <n-alert v-if="loadError" type="error" style="margin-bottom: 16px;" :title="t('media.loadFailed')">
      <n-space align="center">
        <n-text>{{ loadError }}</n-text>
        <n-button size="small" @click="load">{{ t('media.retry') }}</n-button>
      </n-space>
    </n-alert>

    <n-spin v-if="loading && !config" />
    <template v-else-if="config">
      <!-- 图片生成 -->
      <n-card :title="t('media.image.title')" style="margin-bottom: 16px;">
        <template #header-extra>
          <n-space align="center">
            <n-tag :type="config.image_gen.available ? 'success' : 'default'" size="small">
              {{ config.image_gen.available ? t('media.available') : t('media.notConfigured') }}
            </n-tag>
          </n-space>
        </template>

        <n-alert v-if="config.image_gen.env_overrides.length" type="warning" style="margin-bottom: 12px;" :show-icon="true">
          {{ t('media.envOverride', { keys: config.image_gen.env_overrides.join(', ') }) }}
        </n-alert>

        <n-grid :cols="2" :x-gap="16" :y-gap="4" responsive="screen" item-responsive>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.provider')" :show-feedback="false">
              <n-select v-model:value="imageForm.provider" :options="imageProviderOptions" />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.apiKey')" :show-feedback="false">
              <n-input
                v-model:value="imageForm.api_key"
                type="password"
                show-password-on="click"
                :disabled="imageClearKey"
                :placeholder="keyPlaceholder(config.image_gen)"
              />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.baseUrl')" :show-feedback="false">
              <n-input v-model:value="imageForm.base_url" :placeholder="t('media.baseUrlPlaceholder')" />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.image.size')" :show-feedback="false">
              <n-select v-model:value="imageForm.default_size" :options="imageSizeOptions" clearable />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.image.style')" :show-feedback="false">
              <n-input v-model:value="imageForm.default_style" :placeholder="t('media.image.stylePlaceholder')" />
            </n-form-item>
          </n-gi>
          <n-gi span="2">
            <n-form-item :label="t('media.outputDir')" :show-feedback="false">
              <n-input v-model:value="imageForm.output_dir" :placeholder="t('media.outputDirPlaceholder')" />
            </n-form-item>
          </n-gi>
        </n-grid>

        <n-space align="center" justify="space-between" style="margin-top: 12px;">
          <n-space align="center">
            <n-tag v-if="imageClearKey" type="warning" size="small">{{ t('media.keyWillBeCleared') }}</n-tag>
            <n-button v-else-if="config.image_gen.api_key_set" text size="small" @click="imageClearKey = true">
              {{ t('media.clearKey') }}
            </n-button>
            <n-button v-if="imageClearKey" text size="small" @click="imageClearKey = false">
              {{ t('media.undoClear') }}
            </n-button>
            <n-text depth="3" style="font-size: 12px;">
              {{ t('media.image.editHint', { providers: editableImageProviders }) }}
            </n-text>
          </n-space>
          <n-button type="primary" :loading="saving === 'image'" @click="saveImage">
            {{ t('common.save') }}
          </n-button>
        </n-space>
      </n-card>

      <!-- 视频生成 -->
      <n-card :title="t('media.video.title')" style="margin-bottom: 16px;">
        <template #header-extra>
          <n-tag :type="config.video_gen.available ? 'success' : 'default'" size="small">
            {{ config.video_gen.available ? t('media.available') : t('media.notConfigured') }}
          </n-tag>
        </template>

        <n-alert v-if="config.video_gen.env_overrides.length" type="warning" style="margin-bottom: 12px;" :show-icon="true">
          {{ t('media.envOverride', { keys: config.video_gen.env_overrides.join(', ') }) }}
        </n-alert>

        <n-grid :cols="2" :x-gap="16" :y-gap="4" responsive="screen" item-responsive>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.provider')" :show-feedback="false">
              <n-select v-model:value="videoForm.provider" :options="videoProviderOptions" />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.apiKey')" :show-feedback="false">
              <n-input
                v-model:value="videoForm.api_key"
                type="password"
                show-password-on="click"
                :disabled="videoClearKey"
                :placeholder="keyPlaceholder(config.video_gen)"
              />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.video.model')" :show-feedback="false">
              <n-input v-model:value="videoForm.model" :placeholder="videoModelPlaceholder" />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.baseUrl')" :show-feedback="false">
              <n-input v-model:value="videoForm.base_url" :placeholder="t('media.baseUrlPlaceholder')" />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.video.duration')" :show-feedback="false">
              <n-input-number v-model:value="videoForm.default_duration" :min="0" :max="15" :step="1" style="width: 100%;" :placeholder="t('media.video.durationPlaceholder')" />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.video.ratio')" :show-feedback="false">
              <n-select v-model:value="videoForm.default_ratio" :options="ratioOptions" clearable />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.video.resolution')" :show-feedback="false">
              <n-select v-model:value="videoForm.default_resolution" :options="resolutionOptions" clearable />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.outputDir')" :show-feedback="false">
              <n-input v-model:value="videoForm.output_dir" :placeholder="t('media.outputDirPlaceholder')" />
            </n-form-item>
          </n-gi>
        </n-grid>

        <n-space align="center" justify="space-between" style="margin-top: 12px;">
          <n-space align="center">
            <n-tag v-if="videoClearKey" type="warning" size="small">{{ t('media.keyWillBeCleared') }}</n-tag>
            <n-button v-else-if="config.video_gen.api_key_set" text size="small" @click="videoClearKey = true">
              {{ t('media.clearKey') }}
            </n-button>
            <n-button v-if="videoClearKey" text size="small" @click="videoClearKey = false">
              {{ t('media.undoClear') }}
            </n-button>
            <n-text depth="3" style="font-size: 12px;">{{ t('media.video.hint') }}</n-text>
          </n-space>
          <n-button type="primary" :loading="saving === 'video'" @click="saveVideo">
            {{ t('common.save') }}
          </n-button>
        </n-space>
      </n-card>

      <!-- 语音 -->
      <n-card :title="t('media.voice.title')">
        <template #header-extra>
          <n-tag :type="config.voice.available ? 'success' : 'default'" size="small">
            {{ config.voice.available ? t('media.available') : t('media.notConfigured') }}
          </n-tag>
        </template>

        <n-alert v-if="config.voice.env_overrides.length" type="warning" style="margin-bottom: 12px;" :show-icon="true">
          {{ t('media.envOverride', { keys: config.voice.env_overrides.join(', ') }) }}
        </n-alert>

        <n-grid :cols="2" :x-gap="16" :y-gap="4" responsive="screen" item-responsive>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.provider')" :show-feedback="false">
              <n-select v-model:value="voiceForm.provider" :options="voiceProviderOptions" />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.apiKey')" :show-feedback="false">
              <n-input
                v-model:value="voiceForm.api_key"
                type="password"
                show-password-on="click"
                :disabled="voiceClearKey"
                :placeholder="keyPlaceholder(config.voice)"
              />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.voice.model')" :show-feedback="false">
              <n-input v-model:value="voiceForm.model" :placeholder="t('media.voice.modelPlaceholder')" />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.voice.voice')" :show-feedback="false">
              <n-input v-model:value="voiceForm.voice" :placeholder="t('media.voice.voicePlaceholder')" />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.voice.language')" :show-feedback="false">
              <n-input v-model:value="voiceForm.language" :placeholder="t('media.voice.languagePlaceholder')" />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.voice.region')" :show-feedback="false">
              <n-input v-model:value="voiceForm.region" :placeholder="t('media.voice.regionPlaceholder')" />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.voice.apiUrl')" :show-feedback="false">
              <n-input v-model:value="voiceForm.api_url" :placeholder="t('media.baseUrlPlaceholder')" />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.voice.speed')" :show-feedback="false">
              <n-input-number v-model:value="voiceForm.speed" :min="0.5" :max="2" :step="0.1" style="width: 100%;" />
            </n-form-item>
          </n-gi>
        </n-grid>

        <n-space align="center" justify="space-between" style="margin-top: 12px;">
          <n-space align="center">
            <n-tag v-if="voiceClearKey" type="warning" size="small">{{ t('media.keyWillBeCleared') }}</n-tag>
            <n-button v-else-if="config.voice.api_key_set" text size="small" @click="voiceClearKey = true">
              {{ t('media.clearKey') }}
            </n-button>
            <n-button v-if="voiceClearKey" text size="small" @click="voiceClearKey = false">
              {{ t('media.undoClear') }}
            </n-button>
            <n-text depth="3" style="font-size: 12px;">{{ t('media.voice.hint') }}</n-text>
          </n-space>
          <n-button type="primary" :loading="saving === 'voice'" @click="saveVoice">
            {{ t('common.save') }}
          </n-button>
        </n-space>
      </n-card>
    </template>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage } from 'naive-ui'
import {
  NAlert,
  NButton,
  NCard,
  NFormItem,
  NGi,
  NGrid,
  NIcon,
  NInput,
  NInputNumber,
  NSelect,
  NSpace,
  NSpin,
  NTag,
  NText,
} from 'naive-ui'
import { CopyOutline, RefreshOutline } from '@vicons/ionicons5'

import {
  getMediaConfig,
  updateMediaConfig,
  type ImageGenPatch,
  type MediaConfig,
  type MediaProviderOption,
  type VideoGenPatch,
  type VoicePatch,
} from '@/api/media'

const { t } = useI18n()
const message = useMessage()

const loading = ref(false)
const loadError = ref('')
const saving = ref<'' | 'image' | 'video' | 'voice'>('')
const config = ref<MediaConfig | null>(null)

// 表单里不放"已保存的密钥"：后端只给掩码，前端也不可能还原明文。
// 输入框空 = 不改；只有用户真的敲了新 key 才发 api_key 字段。
const imageForm = reactive({
  provider: '',
  api_key: '',
  base_url: '',
  default_size: '',
  default_style: '',
  output_dir: '',
})
const videoForm = reactive({
  provider: '',
  api_key: '',
  base_url: '',
  model: '',
  default_duration: 0,
  default_ratio: '',
  default_resolution: '',
  output_dir: '',
})
const voiceForm = reactive({
  provider: '',
  api_key: '',
  model: '',
  voice: '',
  language: '',
  region: '',
  api_url: '',
  speed: 1,
  pitch: 0,
})

const imageClearKey = ref(false)
const videoClearKey = ref(false)
const voiceClearKey = ref(false)

function syncForms() {
  const c = config.value
  if (!c) return
  Object.assign(imageForm, {
    provider: c.image_gen.provider,
    api_key: '',
    base_url: c.image_gen.base_url,
    default_size: c.image_gen.default_size,
    default_style: c.image_gen.default_style,
    output_dir: c.image_gen.output_dir,
  })
  Object.assign(videoForm, {
    provider: c.video_gen.provider,
    api_key: '',
    base_url: c.video_gen.base_url,
    model: c.video_gen.model,
    default_duration: c.video_gen.default_duration,
    default_ratio: c.video_gen.default_ratio,
    default_resolution: c.video_gen.default_resolution,
    output_dir: c.video_gen.output_dir,
  })
  Object.assign(voiceForm, {
    provider: c.voice.provider,
    api_key: '',
    model: c.voice.model,
    voice: c.voice.voice,
    language: c.voice.language,
    region: c.voice.region,
    api_url: c.voice.api_url,
    speed: c.voice.speed || 1,
    pitch: c.voice.pitch || 0,
  })
  imageClearKey.value = false
  videoClearKey.value = false
  voiceClearKey.value = false
}

async function load() {
  loading.value = true
  loadError.value = ''
  try {
    config.value = await getMediaConfig()
    syncForms()
  } catch (err) {
    // 不静默置空：页面必须说明"读不到配置"，否则用户会以为配置丢了。
    loadError.value = err instanceof Error ? err.message : String(err)
    config.value = null
  } finally {
    loading.value = false
  }
}

function keyPlaceholder(section: { api_key_set: boolean; api_key_hint: string }): string {
  return section.api_key_set
    ? t('media.keyConfigured', { hint: section.api_key_hint })
    : t('media.keyEmpty')
}

function providerOptions(list: MediaProviderOption[], current: string) {
  const options = list.map((p) => ({
    label: p.available ? p.name : `${p.name} (${t('media.notConfigured')})`,
    value: p.name,
  }))
  // 配置里指着一个当前没注册的 provider（插件被禁用/已卸载）时，naive-ui 的
  // select 会显示空值，看起来像"没配"。把它补进选项，明确标成 unavailable。
  if (current && !options.some((o) => o.value === current)) {
    options.unshift({
      label: `${current} (${t('media.providerUnavailable')})`,
      value: current,
    })
  }
  return options
}

const imageProviderOptions = computed(() =>
  providerOptions(config.value?.image_gen.providers || [], config.value?.image_gen.provider || ''),
)
const videoProviderOptions = computed(() =>
  providerOptions(config.value?.video_gen.providers || [], config.value?.video_gen.provider || ''),
)
const voiceProviderOptions = computed(() =>
  providerOptions(config.value?.voice.providers || [], config.value?.voice.provider || ''),
)

const imageSizeOptions = [
  { label: '256x256', value: '256x256' },
  { label: '512x512', value: '512x512' },
  { label: '1024x1024', value: '1024x1024' },
  { label: '1024x1792', value: '1024x1792' },
  { label: '1792x1024', value: '1792x1024' },
]
const ratioOptions = ['16:9', '9:16', '1:1'].map((v) => ({ label: v, value: v }))
const resolutionOptions = ['480p', '720p', '1080p'].map((v) => ({ label: v, value: v }))

const editableImageProviders = computed(
  () =>
    (config.value?.image_gen.providers || [])
      .filter((p) => p.editable)
      .map((p) => p.name)
      .join(', ') || '-',
)

const videoModelPlaceholder = computed(() =>
  videoForm.provider === 'volcengine' ? 'doubao-seedance-1-0-pro-250528' : 'wan2.2-t2v-plus',
)

function keyPatch(form: { api_key: string }, cleared: boolean): string | undefined {
  if (cleared) return ''
  const v = form.api_key.trim()
  return v === '' ? undefined : v
}

// 只发送真正改动过的字段：后端把"字段缺省"理解为"保持不变"，
// 全量提交会把用户没碰的项一起覆盖（尤其是密钥，空串 = 清除）。
function assignIfChanged(
  patch: Record<string, unknown>,
  form: Record<string, unknown>,
  current: Record<string, unknown>,
  fields: string[],
) {
  for (const f of fields) {
    if (form[f] !== current[f]) patch[f] = form[f]
  }
}

// section → config.json 里的段名（voice 不带 _gen，这里必须映射，
// 拼字符串会把语音配置发到 voice_gen 这个不存在的段上）。
const SECTION_KEY = { image: 'image_gen', video: 'video_gen', voice: 'voice' } as const

async function applyPatch(
  section: 'image' | 'video' | 'voice',
  patch: Record<string, unknown>,
) {
  if (Object.keys(patch).length === 0) {
    message.info(t('media.noChanges'))
    return
  }
  saving.value = section
  try {
    const body = { [SECTION_KEY[section]]: patch }
    config.value = await updateMediaConfig(body as never)
    syncForms()
    message.success(t('media.saved'))
  } catch (err) {
    message.error(err instanceof Error ? err.message : String(err))
  } finally {
    saving.value = ''
  }
}

async function saveImage() {
  const c = config.value
  if (!c) return
  const patch: ImageGenPatch = {}
  const key = keyPatch(imageForm, imageClearKey.value)
  if (key !== undefined) patch.api_key = key
  assignIfChanged(
    patch as Record<string, unknown>,
    imageForm as unknown as Record<string, unknown>,
    c.image_gen as unknown as Record<string, unknown>,
    ['provider', 'base_url', 'default_size', 'default_style', 'output_dir'],
  )
  await applyPatch('image', patch as Record<string, unknown>)
}

async function saveVideo() {
  const c = config.value
  if (!c) return
  const patch: VideoGenPatch = {}
  const key = keyPatch(videoForm, videoClearKey.value)
  if (key !== undefined) patch.api_key = key
  assignIfChanged(
    patch as Record<string, unknown>,
    videoForm as unknown as Record<string, unknown>,
    c.video_gen as unknown as Record<string, unknown>,
    [
      'provider',
      'base_url',
      'model',
      'default_duration',
      'default_ratio',
      'default_resolution',
      'output_dir',
    ],
  )
  await applyPatch('video', patch as Record<string, unknown>)
}

async function saveVoice() {
  const c = config.value
  if (!c) return
  const patch: VoicePatch = {}
  const key = keyPatch(voiceForm, voiceClearKey.value)
  if (key !== undefined) patch.api_key = key
  assignIfChanged(
    patch as Record<string, unknown>,
    voiceForm as unknown as Record<string, unknown>,
    c.voice as unknown as Record<string, unknown>,
    ['provider', 'model', 'voice', 'language', 'region', 'api_url', 'speed'],
  )
  await applyPatch('voice', patch as Record<string, unknown>)
}

async function copyConfigPath() {
  const p = config.value?.config_path
  if (!p) return
  try {
    await navigator.clipboard.writeText(p)
    message.success(t('media.copied'))
  } catch {
    message.info(p)
  }
}

onMounted(load)
</script>

<style scoped>
.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  margin-bottom: 24px;
}
</style>
