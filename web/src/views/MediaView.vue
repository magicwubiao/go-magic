<template>
  <div class="media-page">
    <!-- 页头：标题 + 未保存状态 + 重新加载（与配置页同款） -->
    <div class="page-head">
      <div class="page-head-main">
        <div class="page-head-icon">
          <n-icon :component="ImagesOutline" :size="22" />
        </div>
        <div>
          <h2 class="page-head-title">{{ t('media.title') }}</h2>
          <p class="page-head-sub">{{ t('media.subtitle') }}</p>
        </div>
      </div>
      <div class="page-head-actions">
        <div class="live-pill">
          <span class="live-dot" :class="dirtyCount ? 'tone-warn' : 'tone-ok'" />
          <span class="live-label">
            {{ dirtyCount ? t('config.unsavedTitle', { count: dirtyCount }) : t('config.allSaved') }}
          </span>
        </div>
        <n-button text @click="copyConfigPath" v-if="config?.config_path">
          <template #icon><n-icon><CopyOutline /></n-icon></template>
          {{ t('media.copyConfigPath') }}
        </n-button>
        <n-button secondary :loading="loading" @click="handleReload">
          <template #icon><n-icon><RefreshOutline /></n-icon></template>
          {{ t('config.reload') }}
        </n-button>
      </div>
    </div>

    <n-alert v-if="loadError" type="error" style="margin-bottom: 16px;" :title="t('media.loadFailed')">
      <n-space align="center">
        <n-text>{{ loadError }}</n-text>
        <n-button size="small" @click="load">{{ t('media.retry') }}</n-button>
      </n-space>
    </n-alert>

    <n-spin v-if="loading && !config" />
    <div v-else-if="config" class="media-grid">
      <!-- 侧边分区导航 -->
      <nav class="media-nav">
        <button
          v-for="s in sectionList"
          :key="s.key"
          type="button"
          class="nav-item"
          :class="{ active: active === s.key }"
          @click="active = s.key"
        >
          <n-icon :component="s.icon" :size="17" class="nav-icon" />
          <span class="nav-label">{{ s.label }}</span>
          <span v-if="sectionDirty(s.key)" class="nav-dot" />
        </button>
      </nav>

      <!-- 分区内容 -->
      <div class="media-main">
        <!-- 图片生成 -->
        <template v-if="active === 'image'">
          <n-card size="small" class="setting-card">
            <template #header>
              <div class="section-head">
                <n-icon :component="ImageOutline" :size="18" class="section-icon" />
                <div>
                  <div class="section-title">{{ t('media.image.title') }}</div>
                  <div class="hint">{{ t('media.sectionDesc.image') }}</div>
                </div>
              </div>
            </template>
            <template #header-extra>
              <n-space align="center">
                <n-tag :type="config.image_gen.available ? 'success' : 'default'" size="small">
                  {{ config.image_gen.available ? t('media.available') : t('media.notConfigured') }}
                </n-tag>
                <n-button size="small" :loading="testState.image.loading" :title="t('media.testHint')" @click="runTest('image')">
                  <template #icon><n-icon><FlaskOutline /></n-icon></template>
                  {{ t('media.test') }}
                </n-button>
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
                  <n-input-group>
                    <n-input v-model:value="imageForm.output_dir" :placeholder="t('media.outputDirPlaceholder')" />
                    <n-button :title="t('media.browse')" @click="openDirPicker('image')">
                      <template #icon><n-icon><FolderOpenOutline /></n-icon></template>
                      {{ t('media.browse') }}
                    </n-button>
                  </n-input-group>
                </n-form-item>
              </n-gi>
            </n-grid>

            <!-- 试运行结果：真实调用一次后的小样预览 -->
            <div v-if="testState.image.error" class="test-result">
              <n-alert type="error" :show-icon="true">{{ testState.image.error }}</n-alert>
            </div>
            <div v-else-if="testState.image.result" class="test-result">
              <n-alert v-if="testState.image.result.status === 'not_configured'" type="warning" :show-icon="true">
                {{ testState.image.result.message || t('media.testNotConfigured') }}
              </n-alert>
              <template v-else>
                <img
                  v-if="testState.image.previewUrl"
                  :src="testState.image.previewUrl"
                  class="test-image"
                  :alt="t('media.test')"
                />
                <div class="test-meta">
                  <n-text depth="3" style="font-size: 12px;">
                    {{ t('media.testSavedTo') }}: {{ testState.image.result.file_path }}
                  </n-text>
                  <n-button text size="tiny" @click="copyText(testState.image.result?.file_path || '')">
                    <template #icon><n-icon><CopyOutline /></n-icon></template>
                    {{ t('media.copyPath') }}
                  </n-button>
                </div>
              </template>
            </div>
          </n-card>

          <!-- 分区底部操作条 -->
          <div class="section-actions">
            <n-space align="center" size="small" class="actions-left">
              <n-popconfirm
                :positive-text="t('media.clear')"
                :negative-text="t('common.cancel')"
                @positive-click="clearSection('image')"
              >
                <template #trigger>
                  <n-button
                    text
                    size="small"
                    type="error"
                    :disabled="!config.image_gen.api_key_set && !config.image_gen.provider"
                  >
                    {{ t('media.clear') }}
                  </n-button>
                </template>
                {{ imageClearConfirm }}
              </n-popconfirm>
              <n-tag v-if="imageClearKey" type="warning" size="small">{{ t('media.keyWillBeCleared') }}</n-tag>
              <n-button v-else-if="config.image_gen.api_key_set" text size="small" @click="imageClearKey = true">
                {{ t('media.clearKey') }}
              </n-button>
              <n-button v-if="imageClearKey" text size="small" @click="imageClearKey = false">
                {{ t('media.undoClear') }}
              </n-button>
            </n-space>
            <span class="hint">
              {{ imageDirty ? t('config.unsavedHint') : t('config.allSavedHint') }}
            </span>
            <n-button quaternary :disabled="!imageDirty" @click="resetSection('image')">
              {{ t('config.reset') }}
            </n-button>
            <n-button type="primary" :disabled="!imageDirty" :loading="saving === 'image'" @click="saveImage">
              <template #icon><n-icon :component="SaveOutline" /></template>
              {{ t('config.saveSection') }}
            </n-button>
          </div>
          <div class="actions-note">
            <n-text depth="3" style="font-size: 12px;">
              {{ t('media.image.editHint', { providers: editableImageProviders }) }}
            </n-text>
          </div>
        </template>

        <!-- 视频生成 -->
        <template v-if="active === 'video'">
          <n-card size="small" class="setting-card">
            <template #header>
              <div class="section-head">
                <n-icon :component="FilmOutline" :size="18" class="section-icon" />
                <div>
                  <div class="section-title">{{ t('media.video.title') }}</div>
                  <div class="hint">{{ t('media.sectionDesc.video') }}</div>
                </div>
              </div>
            </template>
            <template #header-extra>
              <n-space align="center">
                <n-tag :type="config.video_gen.available ? 'success' : 'default'" size="small">
                  {{ config.video_gen.available ? t('media.available') : t('media.notConfigured') }}
                </n-tag>
              </n-space>
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
                  <n-input-group>
                    <n-input v-model:value="videoForm.output_dir" :placeholder="t('media.outputDirPlaceholder')" />
                    <n-button :title="t('media.browse')" @click="openDirPicker('video')">
                      <template #icon><n-icon><FolderOpenOutline /></n-icon></template>
                      {{ t('media.browse') }}
                    </n-button>
                  </n-input-group>
                </n-form-item>
              </n-gi>
            </n-grid>
          </n-card>

          <div class="section-actions">
            <n-space align="center" size="small" class="actions-left">
              <n-popconfirm
                :positive-text="t('media.clear')"
                :negative-text="t('common.cancel')"
                @positive-click="clearSection('video')"
              >
                <template #trigger>
                  <n-button
                    text
                    size="small"
                    type="error"
                    :disabled="!config.video_gen.api_key_set && !config.video_gen.provider"
                  >
                    {{ t('media.clear') }}
                  </n-button>
                </template>
                {{ videoClearConfirm }}
              </n-popconfirm>
              <n-tag v-if="videoClearKey" type="warning" size="small">{{ t('media.keyWillBeCleared') }}</n-tag>
              <n-button v-else-if="config.video_gen.api_key_set" text size="small" @click="videoClearKey = true">
                {{ t('media.clearKey') }}
              </n-button>
              <n-button v-if="videoClearKey" text size="small" @click="videoClearKey = false">
                {{ t('media.undoClear') }}
              </n-button>
            </n-space>
            <span class="hint">
              {{ videoDirty ? t('config.unsavedHint') : t('config.allSavedHint') }}
            </span>
            <n-button quaternary :disabled="!videoDirty" @click="resetSection('video')">
              {{ t('config.reset') }}
            </n-button>
            <n-button type="primary" :disabled="!videoDirty" :loading="saving === 'video'" @click="saveVideo">
              <template #icon><n-icon :component="SaveOutline" /></template>
              {{ t('config.saveSection') }}
            </n-button>
          </div>
          <div class="actions-note">
            <n-text depth="3" style="font-size: 12px;">{{ t('media.video.hint') }}</n-text>
          </div>
        </template>

        <!-- 语音 -->
        <template v-if="active === 'voice'">
          <n-card size="small" class="setting-card">
            <template #header>
              <div class="section-head">
                <n-icon :component="MusicalNotesOutline" :size="18" class="section-icon" />
                <div>
                  <div class="section-title">{{ t('media.voice.title') }}</div>
                  <div class="hint">{{ t('media.sectionDesc.voice') }}</div>
                </div>
              </div>
            </template>
            <template #header-extra>
              <n-space align="center">
                <n-tag :type="config.voice.available ? 'success' : 'default'" size="small">
                  {{ config.voice.available ? t('media.available') : t('media.notConfigured') }}
                </n-tag>
                <n-button size="small" :loading="testState.voice.loading" :title="t('media.testHint')" @click="runTest('voice')">
                  <template #icon><n-icon><FlaskOutline /></n-icon></template>
                  {{ t('media.test') }}
                </n-button>
              </n-space>
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
                  <n-slider v-model:value="voiceForm.speed" :min="0.5" :max="2" :step="0.1" style="width: 100%;" />
                </n-form-item>
              </n-gi>
              <n-gi span="2 s:1">
                <n-form-item :label="t('media.voice.pitch')" :show-feedback="false">
                  <n-slider v-model:value="voiceForm.pitch" :min="-10" :max="10" :step="1" style="width: 100%;" />
                </n-form-item>
              </n-gi>
            </n-grid>

            <!-- 试运行结果：真实合成一句的试听 -->
            <div v-if="testState.voice.error" class="test-result">
              <n-alert type="error" :show-icon="true">{{ testState.voice.error }}</n-alert>
            </div>
            <div v-else-if="testState.voice.result" class="test-result">
              <n-alert v-if="testState.voice.result.status === 'not_configured'" type="warning" :show-icon="true">
                {{ testState.voice.result.message || t('media.testNotConfigured') }}
              </n-alert>
              <template v-else>
                <audio v-if="testState.voice.previewUrl" controls :src="testState.voice.previewUrl" class="test-audio"></audio>
                <div class="test-meta">
                  <n-text depth="3" style="font-size: 12px;">
                    {{ t('media.testSavedTo') }}: {{ testState.voice.result.file_path }}
                  </n-text>
                  <n-button text size="tiny" @click="copyText(testState.voice.result?.file_path || '')">
                    <template #icon><n-icon><CopyOutline /></n-icon></template>
                    {{ t('media.copyPath') }}
                  </n-button>
                </div>
              </template>
            </div>
          </n-card>

          <div class="section-actions">
            <n-space align="center" size="small" class="actions-left">
              <n-popconfirm
                :positive-text="t('media.clear')"
                :negative-text="t('common.cancel')"
                @positive-click="clearSection('voice')"
              >
                <template #trigger>
                  <n-button
                    text
                    size="small"
                    type="error"
                    :disabled="!config.voice.api_key_set && !config.voice.provider"
                  >
                    {{ t('media.clear') }}
                  </n-button>
                </template>
                {{ voiceClearConfirm }}
              </n-popconfirm>
              <n-tag v-if="voiceClearKey" type="warning" size="small">{{ t('media.keyWillBeCleared') }}</n-tag>
              <n-button v-else-if="config.voice.api_key_set" text size="small" @click="voiceClearKey = true">
                {{ t('media.clearKey') }}
              </n-button>
              <n-button v-if="voiceClearKey" text size="small" @click="voiceClearKey = false">
                {{ t('media.undoClear') }}
              </n-button>
            </n-space>
            <span class="hint">
              {{ voiceDirty ? t('config.unsavedHint') : t('config.allSavedHint') }}
            </span>
            <n-button quaternary :disabled="!voiceDirty" @click="resetSection('voice')">
              {{ t('config.reset') }}
            </n-button>
            <n-button type="primary" :disabled="!voiceDirty" :loading="saving === 'voice'" @click="saveVoice">
              <template #icon><n-icon :component="SaveOutline" /></template>
              {{ t('config.saveSection') }}
            </n-button>
          </div>
          <div class="actions-note">
            <n-text depth="3" style="font-size: 12px;">{{ t('media.voice.hint') }}</n-text>
          </div>
        </template>
      </div>
    </div>

    <!-- 输出目录选择（图片 / 视频共用一个弹窗，按 target 写回对应表单） -->
    <DirPickerModal
      v-model:show="showDirPicker"
      :title="t('media.outputDir')"
      :current="dirPickerTarget === 'image' ? imageForm.output_dir : videoForm.output_dir"
      @select="applyPickedDir"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useDialog, useMessage } from 'naive-ui'
import {
  NAlert,
  NButton,
  NCard,
  NFormItem,
  NGi,
  NGrid,
  NIcon,
  NInput,
  NInputGroup,
  NInputNumber,
  NPopconfirm,
  NSelect,
  NSlider,
  NSpace,
  NSpin,
  NTag,
  NText,
} from 'naive-ui'
import {
  CopyOutline,
  FilmOutline,
  FlaskOutline,
  FolderOpenOutline,
  ImageOutline,
  ImagesOutline,
  MusicalNotesOutline,
  RefreshOutline,
  SaveOutline,
} from '@vicons/ionicons5'

import DirPickerModal from '@/components/DirPickerModal.vue'

import {
  getMediaConfig,
  resetMediaConfig,
  testMediaConfig,
  updateMediaConfig,
  type ImageGenPatch,
  type MediaConfig,
  type MediaProviderOption,
  type MediaTestResult,
  type VideoGenPatch,
  type VoicePatch,
} from '@/api/media'
import { getFSReadUrl } from '@/api/sessions'

const { t, te } = useI18n()
const message = useMessage()
const dialog = useDialog()

const loading = ref(false)
const loadError = ref('')
const saving = ref<'' | 'image' | 'video' | 'voice'>('')
const config = ref<MediaConfig | null>(null)

// ---------------------------------------------------------------- 分区导航
type Section = 'image' | 'video' | 'voice'
const active = ref<Section>('image')

const sectionList = computed(() => [
  { key: 'image' as Section, label: t('media.image.title'), icon: ImageOutline },
  { key: 'video' as Section, label: t('media.video.title'), icon: FilmOutline },
  { key: 'voice' as Section, label: t('media.voice.title'), icon: MusicalNotesOutline },
])

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

// ---- 输出目录选择器（与 chat/cron/kanban 各页同款交互） ----
type DirPickerTarget = 'image' | 'video'
const showDirPicker = ref(false)
const dirPickerTarget = ref<DirPickerTarget>('image')

function openDirPicker(target: DirPickerTarget) {
  dirPickerTarget.value = target
  showDirPicker.value = true
}

function applyPickedDir(path: string) {
  if (dirPickerTarget.value === 'image') imageForm.output_dir = path
  else videoForm.output_dir = path
}

// ---- 试运行：配置完当场验证，不用去对话里碰运气 ----
//
// 试运行用的是**已保存**的配置，所以点按钮前先把未保存的改动落盘——
// 否则"页面上看到的"和"试的"是两份配置，结果只会让人更困惑。
const testState = reactive<
  Record<'image' | 'voice', { loading: boolean; error: string; result: MediaTestResult | null; previewUrl: string }>
>({
  image: { loading: false, error: '', result: null, previewUrl: '' },
  voice: { loading: false, error: '', result: null, previewUrl: '' },
})

async function runTest(section: 'image' | 'voice') {
  try {
    if (section === 'image' && imageDirty.value) await saveImage()
    else if (section === 'voice' && voiceDirty.value) await saveVoice()
  } catch {
    // 保存失败时 saveXxx 已经弹过错误，别拿旧配置继续试
    return
  }
  const st = testState[section]
  st.loading = true
  st.error = ''
  st.result = null
  st.previewUrl = ''
  try {
    const res = await testMediaConfig(section)
    st.result = res
    if (res.status === 'success' && res.file_path) {
      // <img>/<audio> 发不出 Authorization 头，走 read 票据预览
      st.previewUrl = await getFSReadUrl(res.file_path)
    }
  } catch (err) {
    st.error = err instanceof Error ? err.message : String(err)
  } finally {
    st.loading = false
  }
}

async function copyText(v: string) {
  if (!v) return
  try {
    await navigator.clipboard.writeText(v)
    message.success(t('media.copied'))
  } catch {
    message.info(v)
  }
}

// ---- 未保存检测 ----
//
// 三个分区各自独立保存，改动只在前端表单里；分区导航/页头胶囊按这里的结果
// 提示，保存/还原按钮也按它禁用。逐字段与最近一次服务端返回比对
// （api_key 除外：表单里本来就只放新 key）。
const imageDirty = computed(() => {
  const c = config.value?.image_gen
  if (!c) return false
  return (
    imageForm.provider !== c.provider ||
    imageForm.base_url !== c.base_url ||
    imageForm.model !== (c.model || '') ||
    imageForm.default_size !== c.default_size ||
    imageForm.default_style !== c.default_style ||
    imageForm.output_dir !== c.output_dir ||
    imageClearKey.value
  )
})
const videoDirty = computed(() => {
  const c = config.value?.video_gen
  if (!c) return false
  return (
    videoForm.provider !== c.provider ||
    videoForm.base_url !== c.base_url ||
    videoForm.model !== c.model ||
    videoForm.default_duration !== c.default_duration ||
    videoForm.default_ratio !== c.default_ratio ||
    videoForm.default_resolution !== c.default_resolution ||
    videoForm.output_dir !== c.output_dir ||
    videoClearKey.value
  )
})
const voiceDirty = computed(() => {
  const c = config.value?.voice
  if (!c) return false
  return (
    voiceForm.provider !== c.provider ||
    voiceForm.model !== c.model ||
    voiceForm.voice !== c.voice ||
    voiceForm.language !== c.language ||
    voiceForm.region !== c.region ||
    voiceForm.api_url !== c.api_url ||
    voiceForm.group_id !== (c.group_id || '') ||
    voiceForm.speed !== (c.speed || 1) ||
    voiceForm.pitch !== (c.pitch || 0) ||
    voiceClearKey.value
  )
})

const dirtyMap = computed<Record<Section, boolean>>(() => ({
  image: imageDirty.value,
  video: videoDirty.value,
  voice: voiceDirty.value,
}))
const dirtyCount = computed(() => (['image', 'video', 'voice'] as Section[]).filter((k) => dirtyMap.value[k]).length)

function sectionDirty(key: Section): boolean {
  return dirtyMap.value[key]
}

function onBeforeUnload(e: BeforeUnloadEvent) {
  if (!dirtyCount.value) return
  e.preventDefault()
  e.returnValue = ''
}

// 有未保存修改时重新加载会丢掉改动，先让用户确认（与配置页同款交互；
// 必须用 naive-ui 的 dialog：桌面 WebView 里原生 confirm/alert 不弹会卡死）。
function handleReload(): void {
  if (dirtyCount.value > 0) {
    dialog.warning({
      title: t('config.reloadConfirmTitle'),
      content: t('config.reloadConfirmContent'),
      positiveText: t('config.reloadDiscard'),
      negativeText: t('common.cancel'),
      onPositiveClick: () => {
        load()
      },
    })
    return
  }
  load()
}

// ---------------------------------------------------------------- 表单同步
function syncImageForm(c: MediaConfig) {
  Object.assign(imageForm, {
    provider: c.image_gen.provider,
    api_key: '',
    base_url: c.image_gen.base_url,
    model: c.image_gen.model || '',
    default_size: c.image_gen.default_size,
    default_style: c.image_gen.default_style,
    output_dir: c.image_gen.output_dir,
  })
  imageClearKey.value = false
  // 重新载入后"上一家"就是当前这家：否则第一次切换会被误判成
  // "从空切到某家"，把用户已经存好的 base_url 当成旧默认值覆盖掉。
  lastProvider.image = c.image_gen.provider
}

function syncVideoForm(c: MediaConfig) {
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
  videoClearKey.value = false
  lastProvider.video = c.video_gen.provider
}

function syncVoiceForm(c: MediaConfig) {
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
  voiceClearKey.value = false
  lastProvider.voice = c.voice.provider
}

function syncForms() {
  const c = config.value
  if (!c) return
  syncImageForm(c)
  syncVideoForm(c)
  syncVoiceForm(c)
}

// 还原单个分区：把该分区表单恢复到服务端已保存的值，其它分区不动
function resetSection(section: Section) {
  const c = config.value
  if (!c) return
  if (section === 'image') syncImageForm(c)
  else if (section === 'video') syncVideoForm(c)
  else syncVoiceForm(c)
  message.info(t('config.resetDone'))
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
  section: Section,
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
    ['provider', 'model', 'voice', 'language', 'region', 'api_url', 'group_id', 'speed', 'pitch'],
  )
  await applyPatch('voice', patch as Record<string, unknown>)
}

// ---- 清除配置（整段复位） ----
//
// 与「清除密钥」的区别：那个只清 key（保存后生效，其它字段不动）；这个是整段
// 归零——密钥、模型、接口地址、分厂商凭据（如 minimax 的 group_id）一起清掉，
// 并立即落盘。用 DELETE 而不是 PUT 一堆空串：整段语义更明确，也不会漏字段。
const imageClearConfirm = computed(() => clearConfirmText('image'))
const videoClearConfirm = computed(() => clearConfirmText('video'))
const voiceClearConfirm = computed(() => clearConfirmText('voice'))

function sectionEnvOverrides(section: Section): string[] {
  const c = config.value
  if (!c) return []
  if (section === 'image') return c.image_gen.env_overrides
  if (section === 'video') return c.video_gen.env_overrides
  return c.voice.env_overrides
}

function clearConfirmText(section: Section): string {
  let txt = t('media.clearConfirm')
  const keys = sectionEnvOverrides(section)
  // 环境变量不在配置文件里、清不掉；不说清楚，用户会以为"清了还能用"是 bug。
  if (keys.length) txt += ' ' + t('media.clearEnvNote', { keys: keys.join(', ') })
  return txt
}

async function clearSection(section: Section) {
  saving.value = section
  try {
    config.value = await resetMediaConfig(section)
    // 试运行小样对应的是刚被清掉的配置，留着会让人以为"清除了还在跑"。
    if (section !== 'video') {
      testState[section].result = null
      testState[section].error = ''
      testState[section].previewUrl = ''
    }
    syncForms()
    message.success(t('media.cleared'))
  } catch (err) {
    message.error(err instanceof Error ? err.message : String(err))
  } finally {
    saving.value = ''
  }
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

onMounted(() => {
  load()
  window.addEventListener('beforeunload', onBeforeUnload)
})
onUnmounted(() => {
  window.removeEventListener('beforeunload', onBeforeUnload)
})
</script>

<style scoped>
.media-page {
  max-width: 1180px;
  margin: 0 auto;
}

/* ---------- 页头 ---------- */
.page-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  margin-bottom: 20px;
}

.page-head-main {
  display: flex;
  align-items: center;
  gap: 12px;
  min-width: 0;
}

.page-head-icon {
  width: 42px;
  height: 42px;
  border-radius: 12px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
  color: #2080f0;
  background: linear-gradient(135deg, #eaf4ff, #dbeafe);
}

.page-head-title {
  margin: 0;
  font-size: 20px;
  font-weight: 600;
  line-height: 1.3;
  color: #111827;
}

.page-head-sub {
  margin: 3px 0 0;
  font-size: 12.5px;
  line-height: 1.5;
  color: #8a8f99;
  max-width: 620px;
}

.page-head-actions {
  display: flex;
  align-items: center;
  gap: 10px;
}

.live-pill {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 6px 12px;
  border-radius: 999px;
  background: #f7f9fb;
  border: 1px solid #eceff3;
  font-size: 12px;
  color: #6b7280;
  white-space: nowrap;
}

.live-dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex: none;
  background: #18a058;
  box-shadow: 0 0 0 3px rgba(24, 160, 88, 0.14);
}

.live-dot.tone-warn {
  background: #f0a020;
  box-shadow: 0 0 0 3px rgba(240, 160, 32, 0.16);
}

/* ---------- 布局 ---------- */
.media-grid {
  display: grid;
  grid-template-columns: 200px minmax(0, 1fr);
  gap: 20px;
  align-items: start;
}

.media-nav {
  position: sticky;
  top: 8px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.nav-item {
  -webkit-appearance: none;
  appearance: none;
  display: flex;
  align-items: center;
  gap: 9px;
  width: 100%;
  padding: 8px 10px;
  border: none;
  border-radius: 8px;
  background: transparent;
  font: inherit;
  font-size: 13.5px;
  color: #5b6573;
  text-align: left;
  cursor: pointer;
  transition: background 0.15s ease, color 0.15s ease;
}

.nav-item:hover {
  background: #f4f6f8;
  color: #111827;
}

.nav-item.active {
  background: #eaf4ff;
  color: #185fa5;
  font-weight: 600;
}

.nav-icon {
  flex: none;
}

.nav-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 该分区有未保存修改 */
.nav-dot {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  flex: none;
  background: #f0a020;
}

.media-main {
  display: flex;
  flex-direction: column;
  gap: 16px;
  min-width: 0;
}

/* ---------- 卡片与厂商元信息 ---------- */
.section-head {
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

.section-icon {
  color: #7c8899;
  margin-top: 2px;
  flex: none;
}

.section-title {
  font-size: 14px;
  font-weight: 600;
  color: #111827;
  line-height: 1.4;
}

.hint {
  font-size: 12px;
  line-height: 1.5;
  color: #98a2b3;
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

/* ---------- 分区底部操作条 ---------- */
.section-actions {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 10px;
  flex-wrap: wrap;
  position: sticky;
  bottom: 0;
  padding: 12px 16px;
  background: #fff;
  border: 1px solid #eaecf0;
  border-radius: 10px;
  box-shadow: 0 -2px 10px rgba(17, 24, 39, 0.05);
}

.actions-left {
  margin-right: auto;
}

/* 操作条下面一行的小字说明（编辑范围 / 各分区提示） */
.actions-note {
  margin-top: -6px;
  padding: 0 4px;
}

/* ===== 试运行结果 ===== */
.test-result {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px dashed rgba(128, 128, 128, 0.3);
}

.test-image {
  display: block;
  max-width: 240px;
  max-height: 240px;
  border-radius: 6px;
}

.test-audio {
  display: block;
  width: 100%;
  max-width: 420px;
}

.test-meta {
  display: flex;
  align-items: center;
  gap: 8px;
  margin-top: 6px;
  word-break: break-all;
}

/* ===== 移动端 ===== */
@media (max-width: 900px) {
  .media-grid {
    grid-template-columns: minmax(0, 1fr);
  }

  .media-nav {
    position: static;
    flex-direction: row;
    overflow-x: auto;
    padding-bottom: 4px;
    border-bottom: 1px solid #eaecf0;
  }

  .nav-item {
    width: auto;
    white-space: nowrap;
  }

  .nav-label {
    overflow: visible;
  }
}

@media (max-width: 768px) {
  .page-head {
    flex-direction: column;
    gap: 8px;
  }

  .test-image {
    max-width: 100%;
    max-height: 200px;
  }
}
</style>
