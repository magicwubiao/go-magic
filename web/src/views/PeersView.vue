<template>
  <div>
    <n-space justify="space-between" align="center" style="margin-bottom: 16px;">
      <div>
        <h2 style="margin: 0;">{{ t('peers.title') }}</h2>
        <n-text depth="3" style="font-size: 12px;">{{ t('peers.subtitle') }}</n-text>
      </div>
      <n-space>
        <n-button :loading="loading" @click="load">
          <template #icon><n-icon><RefreshOutline /></n-icon></template>
        </n-button>
        <n-button type="primary" @click="openAdd">
          <template #icon><n-icon><AddOutline /></n-icon></template>
          {{ t('peers.add') }}
        </n-button>
      </n-space>
    </n-space>

    <!-- This machine's identity: what a remote operator needs to register us. -->
    <n-card :title="t('peers.identityTitle')" size="small" style="margin-bottom: 16px;">
      <n-descriptions :column="1" label-placement="left" size="small">
        <n-descriptions-item :label="t('peers.instanceId')">
          <n-space align="center" :size="8">
            <n-text code>{{ overview.instance_id || '—' }}</n-text>
            <n-button v-if="overview.instance_id" size="tiny" quaternary @click="copy(overview.instance_id)">
              <template #icon><n-icon><CopyOutline /></n-icon></template>
              {{ t('peers.copy') }}
            </n-button>
          </n-space>
        </n-descriptions-item>
        <n-descriptions-item :label="t('peers.magicHome')">
          <n-text code>{{ overview.magic_home || '—' }}</n-text>
        </n-descriptions-item>
      </n-descriptions>
      <n-text class="peer-hint" depth="3" style="font-size: 12px;">{{ t('peers.identityHint') }}</n-text>
    </n-card>

    <n-card :title="t('peers.listTitle')" size="small">
      <n-empty v-if="!peers.length" :description="t('peers.empty')">
        <template #extra>
          <n-text depth="3" style="font-size: 12px;">{{ t('peers.emptyHint') }}</n-text>
        </template>
      </n-empty>

      <n-list v-else hoverable clickable>
        <n-list-item v-for="p in peers" :key="p.name">
          <n-thing>
            <template #header>
              <n-space align="center" :size="8">
                <n-text strong>{{ p.name }}</n-text>
                <n-tag :type="p.has_token ? 'success' : 'default'" size="small" round>
                  {{ p.has_token ? t('peers.hasToken') : t('peers.noToken') }}
                </n-tag>
              </n-space>
            </template>
            <template #description>
              <n-text code style="font-size: 12px;">{{ p.base_url }}</n-text>
            </template>
          </n-thing>
          <template #suffix>
            <n-space :size="8">
              <n-button size="small" @click="openDM(p)">{{ t('peers.dm') }}</n-button>
              <n-popconfirm @positive-click="removePeer(p)">
                <template #trigger>
                  <n-button size="small" type="error" quaternary>
                    <template #icon><n-icon><TrashOutline /></n-icon></template>
                  </n-button>
                </template>
                {{ t('peers.deleteConfirm', { name: p.name }) }}
              </n-popconfirm>
            </n-space>
          </template>
        </n-list-item>
      </n-list>
    </n-card>

    <!-- Add peer -->
    <n-modal
      v-model:show="showAdd"
      preset="card"
      class="modal-responsive"
      style="width: 520px; max-width: 96vw;"
      :title="t('peers.addTitle')"
    >
      <n-form label-placement="top">
        <n-form-item :label="t('peers.name')">
          <n-input v-model:value="form.name" :placeholder="t('peers.namePlaceholder')" />
          <template #feedback>
            <n-text depth="3" style="font-size: 12px;">{{ t('peers.nameHint') }}</n-text>
          </template>
        </n-form-item>
        <n-form-item :label="t('peers.baseUrl')">
          <n-input v-model:value="form.base_url" :placeholder="t('peers.baseUrlPlaceholder')" />
          <template #feedback>
            <n-text depth="3" style="font-size: 12px;">{{ t('peers.baseUrlHint') }}</n-text>
          </template>
        </n-form-item>
        <n-form-item :label="t('peers.token')">
          <n-input
            v-model:value="form.token"
            type="password"
            show-password-on="click"
            :placeholder="t('peers.tokenPlaceholder')"
          />
          <template #feedback>
            <n-text depth="3" style="font-size: 12px;">{{ t('peers.tokenHint') }}</n-text>
          </template>
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showAdd = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="saving" @click="savePeer">{{ t('common.save') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Send a DM to a bot on the selected peer -->
    <n-modal
      v-model:show="showDM"
      preset="card"
      class="modal-responsive"
      style="width: 560px; max-width: 96vw;"
      :title="t('peers.dmTitle', { name: dmPeer?.name || '' })"
    >
      <n-form label-placement="top">
        <n-form-item :label="t('peers.dmBot')">
          <n-input v-model:value="dmForm.bot" :placeholder="t('peers.dmBotPlaceholder')" />
        </n-form-item>
        <n-form-item :label="t('peers.dmMessage')">
          <n-input
            v-model:value="dmForm.message"
            type="textarea"
            :rows="3"
            :placeholder="t('peers.dmMessagePlaceholder')"
          />
        </n-form-item>
      </n-form>
      <n-alert v-if="dmReply" type="success" :title="t('peers.dmSent')" style="margin-bottom: 12px;">
        <pre class="peer-reply">{{ dmReply }}</pre>
      </n-alert>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showDM = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="sending" @click="sendDM">
            {{ sending ? t('peers.sending') : t('peers.send') }}
          </n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useMessage } from 'naive-ui'
import {
  NAlert, NButton, NCard, NDescriptions, NDescriptionsItem, NEmpty, NForm, NFormItem,
  NIcon, NInput, NList, NListItem, NModal, NPopconfirm, NSpace, NTag, NText, NThing,
} from 'naive-ui'
import { AddOutline, CopyOutline, RefreshOutline, TrashOutline } from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import { addPeer, deletePeer, getPeers, sendPeerDM, type PeerInfo } from '@/api/peers'

const { t } = useI18n()
const message = useMessage()

const loading = ref(false)
const saving = ref(false)
const sending = ref(false)
const overview = ref<{ instance_id: string; magic_home: string }>({ instance_id: '', magic_home: '' })
const peers = ref<PeerInfo[]>([])

const showAdd = ref(false)
const form = reactive({ name: '', base_url: '', token: '' })

const showDM = ref(false)
const dmPeer = ref<PeerInfo | null>(null)
const dmForm = reactive({ bot: '', message: '' })
const dmReply = ref('')

async function load() {
  loading.value = true
  try {
    const data = await getPeers()
    overview.value = { instance_id: data.instance_id, magic_home: data.magic_home }
    peers.value = data.peers || []
  } catch (e) {
    message.error(e instanceof Error ? e.message : t('common.error'))
  } finally {
    loading.value = false
  }
}

function openAdd() {
  form.name = ''
  form.base_url = ''
  form.token = ''
  showAdd.value = true
}

async function savePeer() {
  if (!form.name.trim() || !form.base_url.trim()) {
    message.warning(t('common.error'))
    return
  }
  saving.value = true
  try {
    await addPeer({
      name: form.name.trim(),
      base_url: form.base_url.trim(),
      token: form.token.trim(),
    })
    message.success(t('peers.created'))
    showAdd.value = false
    await load()
  } catch (e) {
    message.error(e instanceof Error ? e.message : t('common.error'))
  } finally {
    saving.value = false
  }
}

async function removePeer(p: PeerInfo) {
  try {
    await deletePeer(p.name)
    message.success(t('peers.deleted'))
    await load()
  } catch (e) {
    message.error(e instanceof Error ? e.message : t('common.error'))
  }
}

function openDM(p: PeerInfo) {
  dmPeer.value = p
  dmForm.bot = ''
  dmForm.message = ''
  dmReply.value = ''
  showDM.value = true
}

async function sendDM() {
  if (!dmPeer.value) return
  if (!dmForm.bot.trim() || !dmForm.message.trim()) {
    message.warning(t('common.error'))
    return
  }
  sending.value = true
  dmReply.value = ''
  try {
    const res = await sendPeerDM(dmPeer.value.name, dmForm.bot.trim(), dmForm.message.trim())
    dmReply.value = res.reply || ''
  } catch (e) {
    // A relayed DM can legitimately take minutes; surface the remote error
    // verbatim rather than a generic toast.
    message.error(`${t('peers.dmFailed')}: ${e instanceof Error ? e.message : ''}`)
  } finally {
    sending.value = false
  }
}

async function copy(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    message.success(t('peers.copied'))
  } catch {
    message.error(t('common.error'))
  }
}

onMounted(load)
</script>

<style scoped>
.peer-reply {
  margin: 8px 0 0;
  white-space: pre-wrap;
  word-break: break-word;
  font-size: 12px;
}

/* Hint text sitting right after a control: give it breathing room so it
   doesn't visually touch the input / descriptions above it. */
.peer-hint {
  display: block;
  margin-top: 8px;
}
</style>
