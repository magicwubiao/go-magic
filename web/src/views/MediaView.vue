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
              <n-select
                v-model:value="imageForm.provider"
                :options="imageProviderOptions"
                @update:value="(v: string) => onProviderChange('image', v)"
              />
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
          <n-gi span="2">
            <div v-if="imageMeta" class="provider-meta">
              <n-space align="center" size="small">
                <n-tag
                  v-for="cap in imageMeta.capabilities || []"
                  :key="cap"
                  size="small"
                  :bordered="false"
                  type="info"
                >
                  {{ capLabel(cap) }}
                </n-tag>
                <a
                  v-if="imageMeta.console_url"
                  :href="imageMeta.console_url"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="console-link"
                >
                  {{ t('media.getKey') }} ↗
                </a>
              </n-space>
              <n-text v-if="imageMeta.key_hint" depth="3" class="provider-hint">
                {{ imageMeta.key_hint }}
              </n-text>
            </div>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.image.model')" :show-feedback="false">
              <n-select
                v-model:value="imageForm.model"
                :options="modelOptions(imageMeta)"
                :placeholder="modelPlaceholder(imageMeta) || t('media.image.modelPlaceholder')"
                filterable
                tag
                clearable
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
              <n-select
                v-model:value="videoForm.provider"
                :options="videoProviderOptions"
                @update:value="(v: string) => onProviderChange('video', v)"
              />
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
          <n-gi span="2">
            <div v-if="videoMeta" class="provider-meta">
              <n-space align="center" size="small">
                <n-tag
                  v-for="cap in videoMeta.capabilities || []"
                  :key="cap"
                  size="small"
                  :bordered="false"
                  type="info"
                >
                  {{ capLabel(cap) }}
                </n-tag>
                <a
                  v-if="videoMeta.console_url"
                  :href="videoMeta.console_url"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="console-link"
                >
                  {{ t('media.getKey') }} ↗
                </a>
              </n-space>
              <n-text v-if="videoMeta.key_hint" depth="3" class="provider-hint">
                {{ videoMeta.key_hint }}
              </n-text>
            </div>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.video.model')" :show-feedback="false">
              <n-select
                v-model:value="videoForm.model"
                :options="modelOptions(videoMeta)"
                :placeholder="modelPlaceholder(videoMeta)"
                filterable
                tag
                clearable
              />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.baseUrl')" :show-feedback="false">
              <n-input v-model:value="videoForm.base_url" :placeholder="t('media.baseUrlPlaceholder')" />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item
              :label="t('media.video.duration')"
              :show-feedback="!!videoDurationHint"
              :feedback="videoDurationHint"
            >
              <n-input-number
                v-model:value="videoForm.default_duration"
                :min="0"
                :max="videoDurationMax"
                :step="1"
                style="width: 100%;"
                :placeholder="t('media.video.durationPlaceholder')"
              />
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
              <n-select
                v-model:value="voiceForm.provider"
                :options="voiceProviderOptions"
                @update:value="(v: string) => onProviderChange('voice', v)"
              />
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
          <n-gi span="2">
            <div v-if="voiceMeta" class="provider-meta">
              <n-space align="center" size="small">
                <n-tag
                  v-for="cap in voiceMeta.capabilities || []"
                  :key="cap"
                  size="small"
                  :bordered="false"
                  type="info"
                >
                  {{ capLabel(cap) }}
                </n-tag>
                <a
                  v-if="voiceMeta.console_url"
                  :href="voiceMeta.console_url"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="console-link"
                >
                  {{ t('media.getKey') }} ↗
                </a>
              </n-space>
              <n-text v-if="voiceMeta.key_hint" depth="3" class="provider-hint">
                {{ voiceMeta.key_hint }}
              </n-text>
            </div>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.voice.model')" :show-feedback="false">
              <n-select
                v-model:value="voiceForm.model"
                :options="modelOptions(voiceMeta)"
                :placeholder="modelPlaceholder(voiceMeta) || t('media.voice.modelPlaceholder')"
                filterable
                tag
                clearable
              />
            </n-form-item>
          </n-gi>
          <n-gi span="2 s:1">
            <n-form-item :label="t('media.voice.voice')" :show-feedback="false">
              <n-input
                v-model:value="voiceForm.voice"
                :placeholder="voiceMeta?.default_voice || t('media.voice.voicePlaceholder')"
              />
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
          <n-gi v-if="voiceForm.provider === 'minimax'" span="2 s:1">
            <n-form-item :label="t('media.voice.groupId')" :show-feedback="false">
              <n-input
                v-model:value="voiceForm.group_id"
                :placeholder="t('media.voice.groupIdPlaceholder')"
              />
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

const { t, te } = useI18n()
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
  model: '',
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
  group_id: '',
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
    model: c.image_gen.model || '',
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
    group_id: c.voice.group_id || '',
    speed: c.voice.speed || 1,
    pitch: c.voice.pitch || 0,
  })
  imageClearKey.value = false
  videoClearKey.value = false
  voiceClearKey.value = false
  // 重新载入后"上一家"就是当前这家：否则第一次切换会被误判成
  // "从空切到某家"，把用户已经存好的 base_url 当成旧默认值覆盖掉。
  lastProvider.image = c.image_gen.provider
  lastProvider.video = c.video_gen.provider
  lastProvider.voice = c.voice.provider
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
  const options = list.map((p) => {
    // 展示名优先用 catalog 的 label："可灵 Kling" 比 "kling" 更让人确信选对了。
    // 插件后端没有 label，回落成 name。
    const base = p.label || p.name
    const suffix = p.available ? '' : ` (${t('media.notConfigured')})`
    return { label: base + suffix, value: p.name }
  })
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

// ---- 厂商元信息与"切换即预填" ----
//
// 这一段是设置页易用性的核心：选完厂商之后，用户不必翻文档就知道该填哪个模型、
// 地址要不要改、key 长什么样、去哪拿。元信息全部来自后端 catalog（media_catalog.go），
// 前端不复制一份厂商知识——复制必然漂移。

type Section = 'image' | 'video' | 'voice'

function providerList(section: Section): MediaProviderOption[] {
  const c = config.value
  if (!c) return []
  if (section === 'image') return c.image_gen.providers
  if (section === 'video') return c.video_gen.providers
  return c.voice.providers
}

function providerMeta(section: Section, name: string): MediaProviderOption | undefined {
  return providerList(section).find((p) => p.name === name)
}

const imageMeta = computed(() => providerMeta('image', imageForm.provider))
const videoMeta = computed(() => providerMeta('video', videoForm.provider))
const voiceMeta = computed(() => providerMeta('voice', voiceForm.provider))

// 上一家的 provider 名，用来判断字段里现在是"上一家的默认值"还是用户手写的内容。
const lastProvider = reactive<Record<Section, string>>({ image: '', video: '', voice: '' })

/**
 * shouldPrefill 决定切换厂商时要不要覆盖当前值。
 *
 * 不能只用"仅当为空才预填"：从 wan 切到 kling 时 base_url 里已经有 wan 的地址，
 * 字段非空，于是新厂商的默认地址永远填不进去——这就是"切换场景下 watch 必失效"
 * 的老坑。正确判据是：空，或者仍等于上一家的默认值（说明是自动填的，可以改）。
 */
function shouldPrefill(current: string, previousDefault?: string): boolean {
  const v = (current || '').trim()
  if (v === '') return true
  return !!previousDefault && v === previousDefault
}

function onProviderChange(section: Section, next: string) {
  const previous = lastProvider[section]
  lastProvider[section] = next
  if (!next || next === previous) return

  const info = providerMeta(section, next)
  const prev = providerMeta(section, previous)
  if (!info) return

  if (section === 'image') {
    if (shouldPrefill(imageForm.base_url, prev?.default_base_url)) {
      imageForm.base_url = info.default_base_url || ''
    }
    if (shouldPrefill(imageForm.model, prev?.default_model)) {
      imageForm.model = info.default_model || ''
    }
    return
  }

  if (section === 'video') {
    if (shouldPrefill(videoForm.base_url, prev?.default_base_url)) {
      videoForm.base_url = info.default_base_url || ''
    }
    if (shouldPrefill(videoForm.model, prev?.default_model)) {
      videoForm.model = info.default_model || ''
    }
    // 带着一个该厂商必然拒绝的画幅去保存，是"配好了但生成失败"的头号原因。
    if (info.ratios?.length && !info.ratios.includes(videoForm.default_ratio)) {
      videoForm.default_ratio = info.ratios[0]
    }
    if (info.resolutions?.length && !info.resolutions.includes(videoForm.default_resolution)) {
      videoForm.default_resolution = info.resolutions[0]
    }
    // 0 的语义是"用厂商默认"，不能动；非 0 才钳到新厂商的区间。
    // 不在这里改的话，从 wan（3~15）切到 runway（5/10）会留下一个 3 秒的默认值，
    // 用户要到生成时才发现被吸附成 5 秒。
    const d = videoForm.default_duration
    if (d !== 0) {
      const min = info.min_duration || 0
      const max = info.max_duration || 15
      if (d < min) videoForm.default_duration = min
      else if (d > max) videoForm.default_duration = max
    }
    return
  }

  if (shouldPrefill(voiceForm.api_url, prev?.default_base_url)) {
    voiceForm.api_url = info.default_base_url || ''
  }
  if (shouldPrefill(voiceForm.model, prev?.default_model)) {
    voiceForm.model = info.default_model || ''
  }
  if (shouldPrefill(voiceForm.voice, prev?.default_voice)) {
    voiceForm.voice = info.default_voice || ''
  }
}

// 模型下拉：有推荐列表就让用户选（还允许手输），没有就退回普通输入框。
function modelOptions(meta?: MediaProviderOption) {
  return (meta?.models || []).map((m) => ({ label: m, value: m }))
}

function modelPlaceholder(meta?: MediaProviderOption): string {
  if (meta?.models?.length) return meta.models[0]
  if (meta?.default_model) return meta.default_model
  return ''
}

function capLabel(cap: string): string {
  const key = `media.cap.${cap}`
  return te(key) ? t(key) : cap
}

const imageSizeOptions = [
  { label: '256x256', value: '256x256' },
  { label: '512x512', value: '512x512' },
  { label: '1024x1024', value: '1024x1024' },
  { label: '1024x1792', value: '1024x1792' },
  { label: '1792x1024', value: '1792x1024' },
]

// 画幅/分辨率按当前厂商裁剪：选一个必然被拒的值，比选不到更糟。
const ratioOptions = computed(() => {
  const list = videoMeta.value?.ratios?.length ? videoMeta.value.ratios : ['16:9', '9:16', '1:1']
  return list.map((v) => ({ label: v, value: v }))
})
const resolutionOptions = computed(() => {
  const list = videoMeta.value?.resolutions?.length
    ? videoMeta.value.resolutions
    : ['480p', '720p', '1080p']
  return list.map((v) => ({ label: v, value: v }))
})
const videoDurationMax = computed(() => videoMeta.value?.max_duration || 15)
// 把该厂商实际支持的时长区间写在输入框下面。不同厂商差得很远
// （Runway 只有 5/10，其余 3~15），不写出来用户只能靠试。
const videoDurationHint = computed(() => {
  const meta = videoMeta.value
  if (!meta?.min_duration || !meta?.max_duration) return ''
  return t('media.video.durationRange', { min: meta.min_duration, max: meta.max_duration })
})

const editableImageProviders = computed(
  () =>
    (config.value?.image_gen.providers || [])
      .filter((p) => p.editable)
      .map((p) => p.label || p.name)
      .join(', ') || '-',
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
    ['provider', 'base_url', 'model', 'default_size', 'default_style', 'output_dir'],
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
    ['provider', 'model', 'voice', 'language', 'region', 'api_url', 'group_id', 'speed'],
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

/* 厂商元信息条：能力徽章 + 控制台入口 + 密钥形态提示。
   用半透明灰底而不是主题变量，是为了在明暗两套主题下都不刺眼。 */
.provider-meta {
  margin: -4px 0 12px;
  padding: 10px 12px;
  border-radius: 6px;
  background: rgba(128, 128, 128, 0.08);
  line-height: 1.6;
}

.provider-hint {
  display: block;
  margin-top: 4px;
  font-size: 12px;
  word-break: break-word;
}

.console-link {
  font-size: 12px;
  text-decoration: none;
}

.console-link:hover {
  text-decoration: underline;
}
</style>
