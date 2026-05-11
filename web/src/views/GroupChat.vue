<template>
  <div class="group-chat">
    <n-grid :cols="4" :x-gap="16" :y-gap="16">
      <!-- Room List -->
      <n-gi :span="1">
        <n-card title="Rooms" class="room-list-card">
          <template #header-extra>
            <n-button size="small" @click="showCreateModal = true">
              <template #icon>
                <n-icon><AddOutline /></n-icon>
              </template>
            </n-button>
          </template>
          
          <n-space vertical>
            <n-input 
              v-model:value="searchQuery" 
              placeholder="Search rooms..." 
              clearable
            >
              <template #prefix>
                <n-icon><SearchOutline /></n-icon>
              </template>
            </n-input>
            
            <n-input 
              v-model:value="joinCode" 
              placeholder="Enter invite code..."
              @keyup.enter="handleJoinRoom"
            >
              <template #prefix>
                <n-icon><LogInOutline /></n-icon>
              </template>
              <template #suffix>
                <n-button size="tiny" @click="handleJoinRoom" :disabled="!joinCode">
                  Join
                </n-button>
              </template>
            </n-input>
          </n-space>
          
          <n-scrollbar style="max-height: 400px; margin-top: 12px">
            <n-list hoverable clickable @click="selectRoom(room.id)" v-if="filteredRooms.length > 0">
              <n-list-item v-for="room in filteredRooms" :key="room.id">
                <n-thing 
                  :title="room.name" 
                  :description="room.description"
                  :content-style="{ marginTop: '4px' }"
                >
                  <template #header-extra>
                    <n-badge 
                      :value="room.member_count" 
                      :max="99"
                      type="info"
                    />
                  </template>
                  <template #description>
                    <n-space align="center">
                      <span>{{ room.member_count }} members</span>
                      <n-tag v-if="room.is_active" type="success" size="small">Active</n-tag>
                    </n-space>
                  </template>
                </n-thing>
              </n-list-item>
            </n-list>
            <n-empty v-else description="No rooms found" />
          </n-scrollbar>
        </n-card>
      </n-gi>
      
      <!-- Chat Panel -->
      <n-gi :span="2">
        <n-card class="chat-panel-card" :bordered="false">
          <template #header>
            <n-space v-if="currentRoom">
              <span>{{ currentRoom.name }}</span>
              <n-tag type="info">{{ messages.length }} messages</n-tag>
            </n-space>
            <span v-else>Select a room to start chatting</span>
          </template>
          
          <template #header-extra>
            <n-space v-if="currentRoom">
              <n-button size="small" @click="showInviteModal = true">
                <template #icon><n-icon><QrCodeOutline /></n-icon></template>
                Invite
              </n-button>
              <n-button size="small" @click="handleLeaveRoom">
                <template #icon><n-icon><ExitOutline /></n-icon></template>
                Leave
              </n-button>
            </n-space>
          </template>
          
          <div v-if="currentRoom" class="chat-area">
            <n-scrollbar ref="scrollbarRef" style="max-height: 500px" id="message-container">
              <div class="message-list">
                <MessageBubble
                  v-for="msg in sortedMessages"
                  :key="msg.id"
                  :message="msg"
                  :is-own="msg.user_id === currentUserId"
                />
              </div>
            </n-scrollbar>
            
            <div class="typing-indicator" v-if="typingUsers.size > 0">
              {{ Array.from(typingUsers).join(', ') }} {{ typingUsers.size === 1 ? 'is' : 'are' }} typing...
            </div>
            
            <div class="message-input">
              <n-input
                v-model:value="messageInput"
                type="textarea"
                placeholder="Type your message..."
                :rows="2"
                @keydown.enter.exact.prevent="handleSendMessage"
                @input="handleTyping"
              />
              <n-button type="primary" @click="handleSendMessage" :disabled="!messageInput.trim()">
                <template #icon><n-icon><SendOutline /></n-icon></template>
              </n-button>
            </div>
          </div>
          
          <n-empty v-else description="No room selected" />
        </n-card>
      </n-gi>
      
      <!-- Members Panel -->
      <n-gi :span="1">
        <n-card title="Members" class="members-card">
          <template #header-extra>
            <n-badge :value="onlineMembers.length" type="success" />
          </template>
          
          <n-scrollbar style="max-height: 500px">
            <n-list v-if="members.length > 0">
              <n-list-item v-for="member in members" :key="member.id">
                <n-space align="center">
                  <n-avatar :size="32" round>
                    {{ member.username.charAt(0).toUpperCase() }}
                  </n-avatar>
                  <n-thing :title="member.username">
                    <template #description>
                      <n-space align="center">
                        <n-badge :dot="member.is_online" type="success" />
                        <span>{{ member.is_online ? 'Online' : 'Offline' }}</span>
                      </n-space>
                    </template>
                  </n-thing>
                </n-space>
              </n-list-item>
            </n-list>
            <n-empty v-else description="No members" />
          </n-scrollbar>
        </n-card>
      </n-gi>
    </n-grid>
    
    <!-- Create Room Modal -->
    <n-modal v-model:show="showCreateModal">
      <n-card style="width: 400px" title="Create Room" :bordered="false">
        <n-form>
          <n-form-item label="Room Name">
            <n-input v-model:value="newRoom.name" placeholder="Enter room name" />
          </n-form-item>
          <n-form-item label="Description">
            <n-input 
              v-model:value="newRoom.description" 
              type="textarea"
              placeholder="Enter room description"
            />
          </n-form-item>
        </n-form>
        <template #footer>
          <n-space justify="end">
            <n-button @click="showCreateModal = false">Cancel</n-button>
            <n-button type="primary" @click="handleCreateRoom" :loading="isLoading">
              Create
            </n-button>
          </n-space>
        </template>
      </n-card>
    </n-modal>
    
    <!-- Invite Modal -->
    <n-modal v-model:show="showInviteModal">
      <n-card style="width: 400px" title="Invite Code" :bordered="false">
        <n-space vertical align="center">
          <n-qr-code :value="currentRoom?.invite_code || ''" :size="200" />
          <n-input :value="currentRoom?.invite_code" readonly />
          <n-button @click="copyInviteCode">
            <template #icon><n-icon><CopyOutline /></n-icon></template>
            Copy Code
          </n-button>
          <n-button type="primary" @click="regenerateInviteCode">
            <template #icon><n-icon><RefreshOutline /></n-icon></template>
            Regenerate
          </n-button>
        </n-space>
      </n-card>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, nextTick } from 'vue'
import { 
  NGrid, NGi, NCard, NButton, NIcon, NInput, NSpace, NList, NListItem, 
  NThing, NTag, NScrollbar, NBadge, NEmpty, NModal, NForm, NFormItem, NAvatar, NQrCode,
  useMessage, useDialog
} from 'naive-ui'
import {
  AddOutline, SearchOutline, LogInOutline, SendOutline, 
  QrCodeOutline, ExitOutline, CopyOutline, RefreshOutline
} from '@vicons/ionicons5'
import { useGroupChatStore } from '../stores/groupchat'
import MessageBubble from './MessageBubble.vue'

const message = useMessage()
const dialog = useDialog()
const store = useGroupChatStore()

const scrollbarRef = ref()
const searchQuery = ref('')
const joinCode = ref('')
const messageInput = ref('')
const currentUserId = ref('user_' + Math.random().toString(36).substr(2, 9))

const showCreateModal = ref(false)
const showInviteModal = ref(false)
const newRoom = ref({ name: '', description: '' })

const filteredRooms = computed(() => {
  if (!searchQuery.value) return store.rooms
  return store.rooms.filter(r => 
    r.name.toLowerCase().includes(searchQuery.value.toLowerCase()) ||
    r.description.toLowerCase().includes(searchQuery.value.toLowerCase())
  )
})

const currentRoom = computed(() => store.currentRoom)
const messages = computed(() => store.sortedMessages)
const members = computed(() => store.members)
const onlineMembers = computed(() => store.onlineMembers)
const typingUsers = computed(() => store.typingUsers)
const isLoading = computed(() => store.isLoading)

async function selectRoom(roomId: string) {
  await store.selectRoom(roomId)
  await nextTick()
  scrollToBottom()
}

async function handleCreateRoom() {
  try {
    const room = await store.createRoom(newRoom.value.name, newRoom.value.description)
    showCreateModal.value = false
    newRoom.value = { name: '', description: '' }
    await selectRoom(room.id)
    message.success('Room created successfully')
  } catch (e) {
    message.error('Failed to create room')
  }
}

async function handleJoinRoom() {
  if (!joinCode.value) return
  try {
    const room = await store.joinRoom(joinCode.value)
    joinCode.value = ''
    await selectRoom(room.id)
    message.success('Joined room successfully')
  } catch (e) {
    message.error('Failed to join room')
  }
}

async function handleLeaveRoom() {
  if (!currentRoom.value) return
  dialog.warning({
    title: 'Leave Room',
    content: 'Are you sure you want to leave this room?',
    positiveText: 'Leave',
    negativeText: 'Cancel',
    onPositiveClick: async () => {
      try {
        await store.leaveRoom(currentRoom.value!.id)
        message.success('Left room successfully')
      } catch (e) {
        message.error('Failed to leave room')
      }
    }
  })
}

async function handleSendMessage() {
  if (!messageInput.value.trim()) return
  try {
    await store.sendMessage(messageInput.value)
    messageInput.value = ''
    await nextTick()
    scrollToBottom()
  } catch (e) {
    message.error('Failed to send message')
  }
}

function handleTyping() {
  store.sendTyping(true)
}

function scrollToBottom() {
  const container = document.getElementById('message-container')
  if (container) {
    container.scrollTop = container.scrollHeight
  }
}

async function copyInviteCode() {
  if (currentRoom.value?.invite_code) {
    await navigator.clipboard.writeText(currentRoom.value.invite_code)
    message.success('Invite code copied')
  }
}

async function regenerateInviteCode() {
  if (!currentRoom.value) return
  try {
    const response = await fetch(`/api/groupchat/rooms/${currentRoom.value.id}/invite`, {
      method: 'POST'
    })
    const data = await response.json()
    currentRoom.value.invite_code = data.code
    message.success('Invite code regenerated')
  } catch (e) {
    message.error('Failed to regenerate invite code')
  }
}

onMounted(async () => {
  await store.fetchRooms()
})

onUnmounted(() => {
  store.cleanup()
})
</script>

<style scoped>
.group-chat {
  padding: 16px;
  height: calc(100vh - 100px);
}

.room-list-card,
.chat-panel-card,
.members-card {
  height: 100%;
}

.chat-area {
  display: flex;
  flex-direction: column;
  height: calc(100% - 60px);
}

.message-list {
  padding: 12px;
}

.message-input {
  display: flex;
  gap: 8px;
  padding: 12px;
  border-top: 1px solid var(--border-color);
}

.typing-indicator {
  padding: 8px 12px;
  font-size: 12px;
  color: var(--text-color-3);
  font-style: italic;
}
</style>
