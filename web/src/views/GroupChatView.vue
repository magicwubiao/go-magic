<template>
  <div style="height: calc(100vh - 48px); display: flex;">
    <!-- Room List -->
    <div style="width: 240px; border-right: 1px solid #e8e8e8; padding: 12px; overflow-y: auto;">
      <n-space justify="space-between" style="margin-bottom: 12px;">
        <n-text strong>{{ t('groupchat.rooms') }}</n-text>
        <n-button size="small" type="primary" @click="showCreateRoom = true">+</n-button>
      </n-space>
      <div
        v-for="room in groupchatStore.rooms"
        :key="room.id"
        :class="['room-item', { active: groupchatStore.activeRoomId === room.id }]"
        @click="groupchatStore.selectRoom(room.id)"
      >
        <n-text strong>{{ room.name }}</n-text>
        <br />
        <n-text depth="3" style="font-size: 12px;">{{ room.members?.length || 0 }} {{ t('groupchat.members') }}</n-text>
      </div>
    </div>

    <!-- Chat Area -->
    <div style="flex: 1; display: flex; flex-direction: column;">
      <template v-if="groupchatStore.activeRoomId">
        <div class="messages" ref="messagesRef">
          <div v-for="msg in groupchatStore.messages" :key="msg.id" class="message" :class="msg.role">
            <div class="msg-header">
              <strong>{{ msg.sender }}</strong>
              <span class="msg-time">{{ new Date(msg.timestamp).toLocaleTimeString() }}</span>
            </div>
            <div class="msg-content">{{ msg.content }}</div>
          </div>
        </div>
        <div class="input-area">
          <n-input
            v-model:value="inputValue"
            :placeholder="t('groupchat.typeMessage')"
            @keydown.enter="send"
          />
          <n-button type="primary" @click="send">{{ t('groupchat.send') }}</n-button>
        </div>
      </template>
      <div v-else style="flex: 1; display: flex; align-items: center; justify-content: center;">
        <n-text depth="3">{{ t('groupchat.selectRoom') }}</n-text>
      </div>
    </div>

    <!-- Create Room Modal -->
    <n-modal v-model:show="showCreateRoom" :title="t('groupchat.newRoom')">
      <n-card style="width: 400px;">
        <n-form>
          <n-form-item :label="t('groupchat.roomName')">
            <n-input v-model:value="newRoom.name" />
          </n-form-item>
          <n-form-item :label="t('groupchat.description')">
            <n-input v-model:value="newRoom.description" />
          </n-form-item>
        </n-form>
        <template #footer>
          <n-space justify="end">
            <n-button @click="showCreateRoom = false">{{ t('common.cancel') }}</n-button>
            <n-button type="primary" @click="createRoom">{{ t('common.create') }}</n-button>
          </n-space>
        </template>
      </n-card>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, reactive, onMounted } from 'vue'
import { useMessage } from 'naive-ui'
import { useI18n } from 'vue-i18n'
import { useGroupChatStore } from '@/stores/groupchat'

const { t } = useI18n()
const message = useMessage()
const groupchatStore = useGroupChatStore()
const inputValue = ref('')
const showCreateRoom = ref(false)
const newRoom = reactive({ name: '', description: '' })

async function send() {
  if (!inputValue.value.trim()) return
  const content = inputValue.value
  inputValue.value = ''
  await groupchatStore.sendMessage(content)
}

async function createRoom() {
  if (!newRoom.name) return
  await groupchatStore.createRoom({ ...newRoom })
  newRoom.name = ''
  newRoom.description = ''
  showCreateRoom.value = false
  message.success(t('groupchat.created'))
}

onMounted(() => groupchatStore.loadRooms())
</script>

<style scoped>
.room-item {
  padding: 8px 12px;
  border-radius: 6px;
  cursor: pointer;
  margin-bottom: 4px;
}

.room-item:hover {
  background: #f0f0f0;
}

.room-item.active {
  background: #e8f4ff;
}

.messages {
  flex: 1;
  overflow-y: auto;
  padding: 16px;
}

.message {
  margin-bottom: 12px;
}

.message .msg-header {
  display: flex;
  gap: 8px;
  margin-bottom: 4px;
}

.message .msg-time {
  font-size: 12px;
  color: #999;
}

.message .msg-content {
  padding: 8px 12px;
  border-radius: 8px;
  background: #f5f5f5;
}

.message.agent .msg-content {
  background: #e8f5e9;
}

.input-area {
  display: flex;
  gap: 8px;
  padding: 12px;
  border-top: 1px solid #e8e8e8;
}
</style>
