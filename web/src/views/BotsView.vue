<template>
  <div class="bots-shell">
    <!-- ========== Left rail: bot list / room list (Grok-style) ========== -->
    <aside class="bot-rail" :class="{ 'rail-collapsed-mobile': railHasActive }">
      <div class="rail-header">
        <div class="rail-tabs">
          <button
            class="rail-tab"
            :class="{ active: viewMode === 'bots' }"
            @click="switchView('bots')"
          >{{ t('bots.title') }}</button>
          <button
            class="rail-tab"
            :class="{ active: viewMode === 'rooms' }"
            @click="switchView('rooms')"
          >{{ t('rooms.title') }}</button>
        </div>
        <n-space :size="4" align="center">
          <n-button quaternary size="tiny" :loading="viewMode === 'bots' ? botsStore.loading : roomsStore.loading" @click="handleRailRefresh">
            <template #icon><n-icon><RefreshOutline /></n-icon></template>
          </n-button>
          <n-button size="tiny" type="primary" :disabled="viewMode === 'bots' && botModeDisabled" @click="handleRailCreate">
            <template #icon><n-icon><AddOutline /></n-icon></template>
          </n-button>
        </n-space>
      </div>

      <div class="rail-search">
        <n-input
          v-model:value="searchQuery"
          size="small"
          clearable
          :placeholder="searchPlaceholder"
        >
          <template #prefix><n-icon><SearchOutline /></n-icon></template>
        </n-input>
      </div>

      <n-alert v-if="viewMode === 'bots' && botModeDisabled" type="warning" class="rail-alert" :title="t('bots.modeDisabledTitle')">
        {{ t('bots.modeDisabledHint') }}
      </n-alert>

      <!-- ===== Bots list ===== -->
      <div v-if="viewMode === 'bots'" class="bot-list">
        <n-spin v-if="botsStore.loading && !botsStore.bots.length" size="small" class="list-spinner" />
        <n-empty
          v-else-if="!botsStore.bots.length"
          :description="t('bots.noBots')"
          class="list-empty"
        />
        <n-empty
          v-else-if="!filteredBots.length"
          :description="t('bots.noSearchResult')"
          class="list-empty"
        />
        <div
          v-for="b in filteredBots"
          :key="b.name"
          class="bot-row"
          :class="{ active: b.name === botsStore.activeBotName, hidden: b.hidden }"
          @click="botsStore.openChat(b.name)"
        >
          <n-avatar v-if="isImageAvatar(b.avatar)" round size="medium" :src="b.avatar" class="row-avatar" />
          <n-avatar v-else round size="medium" class="row-avatar" :style="{ backgroundColor: avatarColor(b.name), fontSize: isCustomAvatar(b.avatar) ? '20px' : '13px' }">
            {{ isCustomAvatar(b.avatar) ? b.avatar : (b.mention_tag || b.name).slice(0, 2).toUpperCase() }}
          </n-avatar>
          <div class="row-main">
            <div class="row-name">
              <span class="row-name-text">{{ b.title || b.name }}</span>
              <span class="row-mention">@{{ b.mention_tag || b.name }}</span>
            </div>
            <n-text depth="3" class="row-sub" :title="b.title || b.description || ''">
              {{ b.title || b.description || b.model || '' }}
            </n-text>
          </div>
          <span class="row-status" :class="{ online: b.runtime?.online, 'active-now': isActiveNow(b) }" />
          <n-dropdown
            trigger="click"
            :options="cardMenuOptions(b)"
            placement="bottom-end"
            @select="(key: string) => handleCardAction(key, b)"
            @click.stop
          >
            <n-button quaternary size="tiny" class="row-menu" @click.stop>
              <template #icon><n-icon><EllipsisHorizontalOutline /></n-icon></template>
            </n-button>
          </n-dropdown>
        </div>
      </div>

      <!-- ===== Rooms list ===== -->
      <div v-else class="bot-list">
        <n-spin v-if="roomsStore.loading && !roomsStore.rooms.length" size="small" class="list-spinner" />
        <n-empty
          v-else-if="!roomsStore.rooms.length"
          :description="t('rooms.empty')"
          class="list-empty"
        />
        <n-empty
          v-else-if="!filteredRooms.length"
          :description="t('bots.noSearchResult')"
          class="list-empty"
        />
        <div
          v-for="room in filteredRooms"
          :key="room.id"
          class="bot-row room-row"
          :class="{ active: roomsStore.activeRoomId === room.id }"
          @click="roomsStore.selectRoom(room.id)"
        >
          <span class="room-row-avatar">
            <n-icon size="18"><PeopleOutline /></n-icon>
          </span>
          <div class="row-main">
            <div class="row-name">
              <span class="row-name-text">{{ room.name || room.id }}</span>
            </div>
            <div class="room-row-meta">
              <span v-for="m in room.members.slice(0, 3)" :key="m" class="mini-chip">{{ m }}</span>
              <span v-if="room.members.length > 3" class="mini-chip">+{{ room.members.length - 3 }}</span>
            </div>
          </div>
          <n-popconfirm v-if="roomsStore.activeRoomId === room.id" @positive-click="handleDeleteRoom(room.id)">
            <template #trigger>
              <n-button quaternary size="tiny" class="row-menu room-delete-btn" @click.stop>
                <template #icon><n-icon><CloseOutline /></n-icon></template>
              </n-button>
            </template>
            {{ t('rooms.confirmDelete') }}
          </n-popconfirm>
        </div>
      </div>

      <div class="rail-footer">
        <n-button v-if="viewMode === 'bots'" quaternary size="tiny" :type="showHidden ? 'primary' : 'default'" @click="showHidden = !showHidden">
          {{ showHidden ? t('bots.hideHidden') : t('bots.showHidden') }}
        </n-button>
        <n-text v-else depth="3" style="font-size: 12px;">
          {{ roomsStore.rooms.length }} {{ t('rooms.title') }}
        </n-text>
      </div>
    </aside>

    <!-- ========== Right pane ========== -->
    <section class="chat-pane" :class="{ 'pane-collapsed-mobile': !railHasActive }">
      <!-- ---- Welcome: bots ---- -->
      <div v-if="viewMode === 'bots' && !botsStore.activeBotName" class="welcome">
        <div class="welcome-inner">
          <div class="welcome-greeting">{{ t('bots.welcomeTitle') }}</div>
          <p class="welcome-sub">{{ t('bots.welcomeSubtitle') }}</p>
          <n-space justify="center" :size="12" class="welcome-stats">
            <div class="stat-card">
              <div class="stat-value">{{ botsStore.bots.length }}</div>
              <div class="stat-label">{{ t('bots.statTotal') }}</div>
            </div>
            <div class="stat-card">
              <div class="stat-value stat-online">{{ onlineCount }}</div>
              <div class="stat-label">{{ t('bots.statOnline') }}</div>
            </div>
            <div class="stat-card">
              <div class="stat-value">{{ activeRoutineCount }}</div>
              <div class="stat-label">{{ t('bots.statActiveRoutines') }}</div>
            </div>
          </n-space>
          <n-text depth="3" style="display: block; margin-top: 28px;">{{ t('bots.welcomePick') }}</n-text>
          <div class="welcome-chips">
            <button
              v-for="b in visibleBots.slice(0, 8)"
              :key="b.name"
              class="welcome-chip"
              @click="botsStore.openChat(b.name)"
            >
              <n-avatar v-if="isImageAvatar(b.avatar)" round size="small" :src="b.avatar" />
              <n-avatar v-else round size="small" :style="{ backgroundColor: avatarColor(b.name), fontSize: isCustomAvatar(b.avatar) ? '14px' : '11px' }">
                {{ isCustomAvatar(b.avatar) ? b.avatar : (b.mention_tag || b.name).slice(0, 2).toUpperCase() }}
              </n-avatar>
              <span class="chip-name">@{{ b.mention_tag || b.name }}</span>
            </button>
          </div>
        </div>
      </div>

      <!-- ---- Chat: a bot is selected ---- -->
      <template v-else-if="viewMode === 'bots' && botsStore.activeBotName">
        <div class="chat-header">
          <n-button quaternary size="small" class="back-btn" @click="botsStore.closeChat()">
            <template #icon><n-icon><ArrowBackOutline /></n-icon></template>
          </n-button>
          <div class="chat-title">
            <n-avatar v-if="isImageAvatar(activeBot?.avatar)" round size="small" :src="activeBot?.avatar" />
            <n-avatar v-else round size="small" :style="{ backgroundColor: avatarColor(activeBot?.name || ''), fontSize: '15px' }">
              {{ isCustomAvatar(activeBot?.avatar) ? activeBot?.avatar : botAvatarText }}
            </n-avatar>
            <div class="chat-title-text">
              <n-text strong style="font-size: 16px;">@{{ activeBot?.mention_tag || botsStore.activeBotName }}</n-text>
              <n-text v-if="activeBot?.title" depth="3" style="font-size: 12px;">{{ activeBot.title }}</n-text>
            </div>
          </div>
          <n-space :size="6" align="center" class="chat-actions">
            <n-tag :type="(activeBot?.runtime?.online) ? 'success' : 'default'" size="small">
              {{ activeBot?.runtime?.online ? t('bots.online') : t('bots.offline') }}
            </n-tag>
            <n-tag v-if="isActiveNow(activeBotObj)" type="success" size="small" :bordered="false">● {{ t('bots.activeNow') }}</n-tag>
            <n-button quaternary size="small" @click="openRoutinesModal">
              <template #icon><n-icon><TimeOutline /></n-icon></template>
              {{ t('bots.routines') }} ({{ botsStore.routines.length }})
            </n-button>
            <n-popconfirm @positive-click="handleClearChat">
              <template #trigger>
                <n-button quaternary size="small" type="error" :loading="clearingChat">
                  {{ t('bots.clearChat') }}
                </n-button>
              </template>
              {{ t('bots.clearChatConfirm') }}
            </n-popconfirm>
            <n-button quaternary size="small" @click="openEditModal(activeBotObj)">
              <template #icon><n-icon><CreateOutline /></n-icon></template>
              {{ t('common.edit') }}
            </n-button>
          </n-space>
        </div>

        <div class="chat-body">
          <div ref="messagesEl" class="chat-messages" @click="handleCodeClick">
            <!-- Empty conversation: bot profile + quick starters (Grok-style) -->
            <div
              v-if="!botsStore.messages.length && !botsStore.chatLoading && !botsStore.sending"
              class="empty-chat"
            >
              <n-avatar v-if="isImageAvatar(activeBot?.avatar)" round :size="64" :src="activeBot?.avatar" class="empty-avatar" />
              <n-avatar v-else round :size="64" class="empty-avatar" :style="{ backgroundColor: avatarColor(activeBot?.name || ''), fontSize: isCustomAvatar(activeBot?.avatar) ? '32px' : '24px' }">
                {{ isCustomAvatar(activeBot?.avatar) ? activeBot?.avatar : botAvatarText }}
              </n-avatar>
              <div class="empty-name">@{{ activeBot?.mention_tag || botsStore.activeBotName }}</div>
              <p v-if="activeBot?.title || activeBot?.description" class="empty-desc">
                {{ activeBot.title || activeBot.description }}
              </p>
              <p v-else class="empty-desc">{{ t('bots.noMessages') }}</p>
              <n-space v-if="activeBot?.model || activeBot?.provider" :size="6" style="margin-top: 8px;">
                <n-tag v-if="activeBot?.model" size="tiny" type="info">{{ activeBot.model }}</n-tag>
                <n-tag v-if="activeBot?.provider" size="tiny">{{ activeBot.provider }}</n-tag>
                <n-tag size="tiny" :bordered="false">
                  {{ t('bots.routineCount', { count: activeBot?.runtime?.active_routines ?? 0 }) }}
                </n-tag>
              </n-space>
              <div class="starter-title">{{ t('bots.starterTitle') }}</div>
              <div class="starter-chips">
                <button
                  v-for="(s, i) in starters"
                  :key="i"
                  class="starter-chip"
                  :disabled="botsStore.sending"
                  @click="handleStarter(s)"
                >{{ s }}</button>
              </div>
            </div>
            <template v-else>
              <div v-for="(msgs, dateKey) in groupedMessages" :key="dateKey">
                <div v-if="dateKey" class="date-separator"><span>{{ dateKey }}</span></div>
                <div
                  v-for="msg in msgs"
                  :key="msg.id"
                  class="message"
                  :class="msg.role"
                >
                  <div class="avatar" :class="msg.role === 'assistant' ? 'bot-avatar' : 'user-avatar'">
                    <img v-if="msg.role === 'assistant' && isImageAvatar(activeBot?.avatar)" :src="activeBot?.avatar" class="avatar-img" alt="" />
                    <span v-else-if="msg.role === 'assistant'">{{ isCustomAvatar(activeBot?.avatar) ? activeBot?.avatar : botAvatarText }}</span>
                    <span v-else>U</span>
                  </div>
                  <div class="message-body" :class="{ 'bot-body': msg.role === 'assistant' }">
                    <div class="message-header">
                      <n-text strong class="sender-name">{{ msg.role === 'assistant' ? botDisplayName : t('bots.you') }}</n-text>
                      <n-tag v-if="msg.role === 'assistant'" size="tiny" type="success">AI</n-tag>
                      <span v-if="formatTime(msg.timestamp)" class="message-time">{{ formatTime(msg.timestamp) }}</span>
                    </div>
                    <div class="message-bubble" :class="[msg.role === 'assistant' ? 'agent-bubble' : 'user-bubble', { streaming: msg._streaming }]">
                      <n-spin v-if="msg._streaming && !msg.content" size="small" class="stream-spin" />
                      <template v-if="msg.role === 'assistant' && msg.content">
                        <ReasoningContent :content="msg.content" :streaming="msg._streaming" />
                      </template>
                      <div
                        v-else
                        class="bubble-content"
                        v-html="msg.content ? renderMarkdown(msg.content) : '<span class=\'placeholder\'>...</span>'"
                      ></div>
                    </div>
                  </div>
                </div>
              </div>
            </template>
            <div v-if="botsStore.sending && !botsStore.messages.some(m => m._streaming)" class="message assistant">
              <div class="avatar bot-avatar">
                <img v-if="isImageAvatar(activeBot?.avatar)" :src="activeBot?.avatar" class="avatar-img" alt="" />
                <span v-else>{{ isCustomAvatar(activeBot?.avatar) ? activeBot?.avatar : botAvatarText }}</span>
              </div>
              <div class="message-body bot-body">
                <div class="message-header">
                  <n-text strong class="sender-name">{{ botDisplayName }}</n-text>
                  <n-tag size="tiny" type="success">AI</n-tag>
                </div>
                <div class="message-bubble agent-bubble thinking">
                  <div class="typing-indicator">
                    <span class="dot"></span>
                    <span class="dot"></span>
                    <span class="dot"></span>
                  </div>
                </div>
              </div>
            </div>
            <div v-if="botsStore.chatLoading && !botsStore.messages.length" class="history-loading">
              <n-spin size="small" />
              <n-text depth="3" style="font-size: 13px; margin-left: 8px;">{{ t('bots.loadingHistory') }}</n-text>
            </div>
          </div>
        </div>

        <div class="input-area">
          <div class="input-wrapper">
            <n-input
              v-model:value="draft"
              type="textarea"
              :placeholder="t('bots.inputPlaceholder')"
              :autosize="{ minRows: 1, maxRows: 6 }"
              :disabled="botsStore.sending"
              class="chat-input"
              @keydown.enter.exact.prevent="handleSend"
            />
            <button
              class="send-btn-inline"
              :class="{ stopping: botsStore.sending }"
              :disabled="!botsStore.sending && !draft.trim()"
              @click="handleSend"
              @mousedown.prevent
              :title="botsStore.sending ? t('bots.sending') : t('bots.send')"
            >
              <svg v-if="!botsStore.sending" viewBox="0 0 24 24" width="18" height="18" fill="currentColor">
                <path d="M3.4 20.4l17.45-7.48a1 1 0 000-1.84L3.4 3.6a.993.993 0 00-1.39.91L2 9.12c0 .5.37.93.87.99L17 12 2.87 13.88c-.5.07-.87.5-.87 1l.01 4.61c0 .71.73 1.2 1.39.91z"/>
              </svg>
              <svg v-else viewBox="0 0 24 24" width="16" height="16" fill="currentColor">
                <rect x="6" y="6" width="12" height="12" rx="2"/>
              </svg>
            </button>
          </div>
        </div>
      </template>

      <!-- ---- Welcome: rooms ---- -->
      <div v-else-if="viewMode === 'rooms' && !roomsStore.activeRoomId" class="welcome">
        <div class="welcome-inner">
          <div class="welcome-greeting">{{ t('rooms.welcomeTitle') }}</div>
          <p class="welcome-sub">{{ t('rooms.welcomeSubtitle') }}</p>
          <n-space justify="center" :size="12" class="welcome-stats">
            <div class="stat-card">
              <div class="stat-value">{{ roomsStore.rooms.length }}</div>
              <div class="stat-label">{{ t('rooms.title') }}</div>
            </div>
            <div class="stat-card">
              <div class="stat-value">{{ totalRoomMembers }}</div>
              <div class="stat-label">{{ t('rooms.memberCountTotal') }}</div>
            </div>
          </n-space>
          <n-text depth="3" style="display: block; margin-top: 28px;">{{ t('rooms.selectRoom') }}</n-text>
          <div class="welcome-chips">
            <button
              v-for="room in roomsStore.rooms.slice(0, 8)"
              :key="room.id"
              class="welcome-chip"
              @click="roomsStore.selectRoom(room.id)"
            >
              <span class="chip-name">{{ room.name || room.id }}</span>
            </button>
          </div>
        </div>
      </div>

      <!-- ---- Chat: a room is selected ---- -->
      <template v-else-if="viewMode === 'rooms' && roomsStore.activeRoomId && activeRoom">
        <div class="chat-header">
          <n-button quaternary size="small" class="back-btn" @click="closeRoomChat()">
            <template #icon><n-icon><ArrowBackOutline /></n-icon></template>
          </n-button>
          <div class="chat-title">
            <div class="chat-title-text">
              <n-text strong style="font-size: 16px;">{{ activeRoom.name || activeRoom.id }}</n-text>
              <n-text depth="3" style="font-size: 12px;">
                {{ activeRoom.max_rounds }} {{ t('rooms.rounds') }} · {{ activeRoom.max_messages }} {{ t('rooms.msgsCap') }}
              </n-text>
            </div>
          </div>
          <div class="member-tags">
            <n-tag v-for="m in activeRoom.members" :key="m" size="small" :bordered="false" :color="{ color: tagColor(m), textColor: '#333' }">
              {{ m }}
            </n-tag>
          </div>
          <n-space :size="6" align="center" class="chat-actions">
            <n-button quaternary size="small" @click="openRoomEdit">
              <template #icon><n-icon><CreateOutline /></n-icon></template>
            </n-button>
            <n-popconfirm @positive-click="handleDeleteRoom(activeRoom.id)">
              <template #trigger>
                <n-button quaternary size="small" type="error">
                  <template #icon><n-icon><TrashOutline /></n-icon></template>
                </n-button>
              </template>
              {{ t('rooms.confirmDelete') }}
            </n-popconfirm>
          </n-space>
        </div>

        <div v-if="activeRoom.topic" class="room-topic">{{ activeRoom.topic }}</div>

        <div class="chat-body">
          <div ref="roomMessagesEl" class="chat-messages" @click="handleCodeClick">
            <n-empty v-if="!roomsStore.loading && roomsStore.messages.length === 0 && !roomsStore.sending" size="small" :description="t('rooms.noMessages')" style="margin-top: 60px;" />
            <div
              v-for="msg in roomsStore.messages"
              :key="msg.id"
              class="message"
              :class="[isRoomUserMsg(msg) ? 'user' : 'assistant', { system: msg.from === '@system' }]"
            >
              <template v-if="msg.from === '@system'">
                <div class="system-notice">{{ msg.content }}</div>
              </template>
              <template v-else>
                <div class="avatar" :class="isRoomUserMsg(msg) ? 'user-avatar' : 'room-bot-avatar'" :style="isRoomUserMsg(msg) ? {} : { background: roomAvatarColor(msg.from) }">
                  <span v-if="isRoomUserMsg(msg)">U</span>
                  <span v-else>{{ roomAvatarText(msg.from) }}</span>
                </div>
                <div class="message-body" :class="{ 'bot-body': !isRoomUserMsg(msg) }">
                  <div class="message-header">
                    <n-text strong class="sender-name">{{ roomDisplayName(msg.from) }}</n-text>
                    <n-tag v-if="!isRoomUserMsg(msg)" size="tiny" type="success" :bordered="false">{{ t('rooms.bot') }}</n-tag>
                    <span v-if="roomFormatTime(msg.timestamp)" class="message-time">{{ roomFormatTime(msg.timestamp) }}</span>
                    <span v-if="msg.content.startsWith('⚠️')" class="send-error">{{ t('rooms.sendFailed') }}</span>
                  </div>
                  <div class="message-bubble" :class="[isRoomUserMsg(msg) ? 'user-bubble' : 'agent-bubble', { 'bubble-error': msg.content.startsWith('⚠️') }]">
                    <template v-if="!isRoomUserMsg(msg)">
                      <ReasoningContent :content="msg.content" :streaming="false" />
                    </template>
                    <div v-else class="bubble-content" v-html="renderMarkdown(msg.content)"></div>
                  </div>
                </div>
              </template>
            </div>

            <!-- Room sending indicator -->
            <div v-if="roomsStore.sending" class="room-typing">
              <div class="room-typing-bubble">
                <div class="room-typing-dots"><span></span><span></span><span></span></div>
                <span class="room-typing-text">{{ t('rooms.botsReplying') }}</span>
                <n-button size="tiny" text type="error" @click="roomsStore.cancelSend()">
                  <template #icon><n-icon><CloseOutline /></n-icon></template>
                </n-button>
              </div>
            </div>
          </div>
        </div>

        <div class="input-area">
          <div class="input-hint">
            {{ t('rooms.inputHint') }}
            <span v-if="roomActiveTarget" class="target-chip">→ {{ roomActiveTarget }}</span>
          </div>
          <div class="input-wrapper">
            <n-input
              v-model:value="roomDraft"
              type="textarea"
              :autosize="{ minRows: 1, maxRows: 6 }"
              :placeholder="t('rooms.typeMessage')"
              :disabled="roomsStore.sending"
              class="chat-input"
              @keydown="onRoomKeydown"
              @input="onRoomInput"
            />
            <div v-if="roomShowMention && roomFilteredMentions.length" class="mention-popup">
              <div
                v-for="(opt, idx) in roomFilteredMentions"
                :key="opt"
                :class="['mention-item', { active: idx === roomMentionActiveIdx }]"
                @mousedown.prevent="selectRoomMention(opt)"
                @mouseenter="roomMentionActiveIdx = idx"
              >
                <span class="mention-avatar" :style="{ background: roomAvatarColor(opt) }">{{ roomAvatarText(opt) }}</span>
                <span class="mention-name">{{ opt }}</span>
              </div>
            </div>
            <button
              class="send-btn-inline"
              :disabled="roomsStore.sending || !roomDraft.trim()"
              @click="roomSend()"
              @mousedown.prevent
              :title="t('rooms.send')"
            >
              <svg viewBox="0 0 24 24" width="18" height="18" fill="currentColor">
                <path d="M3.4 20.4l17.45-7.48a1 1 0 000-1.84L3.4 3.6a.993.993 0 00-1.39.91L2 9.12c0 .5.37.93.87.99L17 12 2.87 13.88c-.5.07-.87.5-.87 1l.01 4.61c0 .71.73 1.2 1.39.91z"/>
              </svg>
            </button>
          </div>
        </div>
      </template>
    </section>

    <!-- ========== Create/Edit Bot Modal ========== -->
    <n-modal v-model:show="showEditModal" :title="editingBot ? t('bots.editBot') : t('bots.createBot')" preset="card" class="modal-responsive modal-scroll" style="width: 560px; max-width: 96vw;">
      <n-form label-placement="top">
        <n-form-item :label="t('bots.name')" required>
          <n-input v-model:value="form.name" :disabled="!!editingBot" :placeholder="t('bots.namePlaceholder')" />
          <template #feedback>
            <n-text depth="3" style="font-size: 12px;">{{ t('bots.nameHint') }}</n-text>
          </template>
        </n-form-item>
        <n-form-item :label="t('bots.titleLabel')">
          <n-input v-model:value="form.title" :placeholder="t('bots.titlePlaceholder')" />
        </n-form-item>
        <n-form-item :label="t('common.description')">
          <n-input v-model:value="form.description" type="textarea" :rows="2" :placeholder="t('bots.descPlaceholder')" />
        </n-form-item>
        <n-form-item :label="t('bots.systemPrompt')">
          <n-input v-model:value="form.system_prompt" type="textarea" :rows="5" :placeholder="t('bots.systemPromptPlaceholder')" />
        </n-form-item>
        <n-form-item :label="t('bots.modelPin')">
          <n-select
            v-model:value="selectedModelId"
            :options="botModelSelectOptions"
            :placeholder="t('bots.inheritGlobal')"
            clearable
            filterable
            :consistent-menu-width="false"
            :render-label="renderBotModelLabel"
          />
        </n-form-item>
        <div class="advanced-toggle" @click="advancedExpanded = !advancedExpanded">
          <n-icon size="15" :class="{ rotated: advancedExpanded }"><ChevronForwardOutline /></n-icon>
          <span>{{ t('bots.advancedSection') }}</span>
        </div>
        <template v-if="advancedExpanded">
        <n-form-item :label="t('bots.avatarLabel')">
          <n-input v-model:value="form.avatar" :placeholder="t('bots.avatarPlaceholder')" />
          <template #feedback>
            <n-text depth="3" style="font-size: 12px;">{{ t('bots.avatarHint') }}</n-text>
          </template>
        </n-form-item>
        <n-form-item :label="t('bots.memoryLabel')">
          <n-input v-model:value="form.memory" type="textarea" :rows="3" :placeholder="t('bots.memoryPlaceholder')" />
        </n-form-item>
        <n-form-item :label="t('bots.toolsLabel')">
          <n-select
            v-model:value="form.tools"
            multiple
            filterable
            tag
            :options="toolOptions"
            :placeholder="t('bots.toolsPlaceholder')"
            :consistent-menu-width="false"
          />
          <template #feedback>
            <n-text depth="3" style="font-size: 12px;">{{ t('bots.toolsHint') }}</n-text>
          </template>
        </n-form-item>
        <n-form-item :label="t('bots.skillsLabel')">
          <n-select
            v-model:value="form.skills"
            multiple
            filterable
            tag
            :options="skillOptions"
            :placeholder="t('bots.skillsPlaceholder')"
            :consistent-menu-width="false"
          />
          <template #feedback>
            <n-text depth="3" style="font-size: 12px;">{{ t('bots.skillsHint') }}</n-text>
          </template>
        </n-form-item>
        <n-form-item :label="t('bots.envLabel')">
          <n-input v-model:value="form.env_text" type="textarea" :rows="4" :placeholder="t('bots.envPlaceholder')" />
          <template #feedback>
            <n-text depth="3" style="font-size: 12px;">{{ t('bots.envHint') }}</n-text>
          </template>
        </n-form-item>
        </template>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showEditModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="saving" @click="handleSaveBot">{{ t('common.save') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Clone Modal -->
    <n-modal v-model:show="showCloneModal" :title="t('bots.cloneTitle')" preset="card" class="modal-responsive" style="width: 420px; max-width: 96vw;">
      <n-form label-placement="top">
        <n-form-item :label="t('bots.name')" required>
          <n-input v-model:value="cloneName" :placeholder="t('bots.cloneNamePlaceholder')" @keydown.enter.prevent="handleCloneConfirm" />
          <template #feedback>
            <n-text depth="3" style="font-size: 12px;">{{ t('bots.cloneHint') }}</n-text>
          </template>
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showCloneModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="cloning" :disabled="!cloneName.trim()" @click="handleCloneConfirm">{{ t('common.confirm') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Rename Modal -->
    <n-modal v-model:show="showRenameModal" :title="t('bots.renameTitle')" preset="card" class="modal-responsive" style="width: 420px; max-width: 96vw;">
      <n-form label-placement="top">
        <n-form-item :label="t('bots.titleLabel')" required>
          <n-input v-model:value="renameName" :placeholder="t('bots.renamePlaceholder')" @keydown.enter.prevent="handleRenameConfirm" />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showRenameModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="renaming" :disabled="!renameName.trim()" @click="handleRenameConfirm">{{ t('common.confirm') }}</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- Routines Modal -->
    <n-modal v-model:show="showRoutinesModal" :title="t('bots.routinesFor', { name: '@' + (activeBot?.mention_tag || '') })" preset="card" class="modal-responsive modal-scroll" style="width: 660px; max-width: 96vw;">
      <n-empty v-if="!botsStore.routines.length" :description="t('bots.noRoutines')" />

      <n-space vertical v-else>
        <n-card v-for="rt in botsStore.routines" :key="rt.id" size="small"
                :class="{ 'routine-editing': rt.id === editingRoutineId }">
          <n-space justify="space-between" align="center">
            <n-space align="center" :size="8">
              <n-switch size="small" :value="rt.enabled"
                        :loading="togglingIds.has(rt.id)"
                        @update:value="(v: boolean) => handleToggleRoutine(rt, v)" />
              <n-text strong style="font-size: 14px;">{{ rt.name || rt.id }}</n-text>
            </n-space>
            <n-space :size="4">
              <n-button v-if="rt.enabled" size="tiny" type="primary"
                        :loading="runningIds.has(rt.id)"
                        @click="handleRunRoutineNow(rt)">
                {{ t('bots.runNow') }}
              </n-button>
              <n-button size="tiny" @click="openEditRoutine(rt)">{{ t('common.edit') }}</n-button>
              <n-popconfirm @positive-click="handleRemoveRoutine(rt.id)">
                <template #trigger>
                  <n-button size="tiny" type="error">{{ t('common.delete') }}</n-button>
                </template>
                {{ t('bots.confirmDeleteRoutine') }}
              </n-popconfirm>
            </n-space>
          </n-space>

          <n-grid :cols="3" :x-gap="16" style="margin-top: 10px;">
            <n-gi>
              <n-text depth="3" style="font-size: 12px;">{{ t('cron.cronExpression') }}</n-text>
              <div><n-text code>{{ rt.schedule }}</n-text></div>
            </n-gi>
            <n-gi>
              <n-text depth="3" style="font-size: 12px;">{{ t('cron.previousRun') }}</n-text>
              <div>{{ rt.last_run ? formatTime(rt.last_run) : '-' }}</div>
              <div v-if="rt.last_status">
                <n-tag :type="rt.last_status === 'success' ? 'success' : rt.last_status === 'failed' ? 'error' : 'warning'" size="tiny">
                  {{ rt.last_status }}
                </n-tag>
              </div>
            </n-gi>
            <n-gi>
              <n-text depth="3" style="font-size: 12px;">{{ t('bots.routinePrompt') }}</n-text>
              <n-text depth="3" style="font-size: 12px; display: block; max-width: 260px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">
                {{ rt.prompt }}
              </n-text>
            </n-gi>
          </n-grid>
        </n-card>
      </n-space>

      <n-divider />
      <n-h6 style="margin: 0 0 10px 0;">
        {{ editingRoutineId ? t('bots.editRoutine') : t('bots.addRoutine') }}
        <n-button v-if="editingRoutineId" quaternary size="tiny" style="margin-left: 8px;" @click="cancelEditRoutine">
          {{ t('common.cancel') }}
        </n-button>
      </n-h6>
      <n-form label-placement="top">
        <n-form-item :label="t('cron.jobName')">
          <n-input v-model:value="routineForm.name" :placeholder="t('bots.routineName')" />
        </n-form-item>
        <n-form-item :label="t('cron.cronExpression')" required>
          <n-input v-model:value="routineForm.schedule" placeholder="0 9 * * *" />
          <template #feedback>
            <n-text v-if="routineForm.schedule.trim()" :type="scheduleHint.type" style="font-size: 12px;">
              {{ scheduleHint.text }}
            </n-text>
          </template>
        </n-form-item>
        <n-form-item :label="t('cron.commonExpressions')">
          <n-space>
            <n-button size="tiny" @click="routineForm.schedule = '0 8 * * *'">{{ t('cron.daily8am') }}</n-button>
            <n-button size="tiny" @click="routineForm.schedule = '0 8 * * 1-5'">{{ t('cron.weekday8am') }}</n-button>
            <n-button size="tiny" @click="routineForm.schedule = '0 */2 * * *'">{{ t('cron.every2hours') }}</n-button>
            <n-button size="tiny" @click="routineForm.schedule = '0 0 * * *'">{{ t('cron.dailyMidnight') }}</n-button>
          </n-space>
        </n-form-item>
        <n-form-item :label="t('bots.routinePrompt')" required>
          <n-input v-model:value="routineForm.prompt" type="textarea" :rows="3" :placeholder="t('bots.routinePrompt')" />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-space justify="end">
          <n-button @click="showRoutinesModal = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="addingRoutine" :disabled="!routineForm.schedule.trim() || !routineForm.prompt.trim()" @click="handleSaveRoutine">
            {{ editingRoutineId ? t('common.save') : t('common.add') }}
          </n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- ========== Create/Edit Room Modal ========== -->
    <n-modal v-model:show="showRoomEditor" preset="card" class="modal-responsive" style="width: 560px; max-width: 96vw;" closable @close="closeRoomEditor">
      <template #header>{{ editingRoomId ? t('rooms.editRoom') : t('rooms.newRoom') }}</template>
      <div class="editor-body">
        <n-form label-placement="top">
          <n-form-item :label="t('rooms.roomName')">
            <n-input v-model:value="roomForm.name" :placeholder="t('rooms.roomNamePlaceholder')" />
          </n-form-item>
          <n-form-item :label="t('rooms.topic')">
            <n-input v-model:value="roomForm.topic" :placeholder="t('rooms.topicPlaceholder')" />
          </n-form-item>
          <n-form-item :label="t('rooms.members')">
            <div class="member-picker">
              <div v-if="botsStore.loading" class="picker-loading">
                <n-spin size="small" /> {{ t('rooms.loadingBots') }}
              </div>
              <n-empty v-else-if="botsStore.bots.length === 0" size="small" :description="t('rooms.noBots')" />
              <template v-else>
                <div
                  v-for="b in botsStore.bots"
                  :key="b.name"
                  :class="['member-option', { picked: roomForm.members.includes(b.name) }]"
                  @click="toggleRoomMember(b.name)"
                >
                  <span class="member-avatar" :style="{ background: avatarColor(b.name) }">{{ avatarText(b.name) }}</span>
                  <span class="member-option-name">{{ b.name }}</span>
                  <span v-if="b.title" class="member-option-title">{{ b.title }}</span>
                  <n-icon v-if="roomForm.members.includes(b.name)" size="14" color="#18a058"><CheckmarkOutline /></n-icon>
                </div>
                <div class="member-count">{{ t('rooms.memberCount', { n: roomForm.members.length }) }}</div>
                <n-alert v-if="botsStore.bots.length < 2" type="warning" :title="t('rooms.minMembersTitle')" style="margin-top: 10px;">
                  {{ t('rooms.minMembersHint', { n: botsStore.bots.length }) }}
                </n-alert>
              </template>
            </div>
          </n-form-item>
          <n-grid :cols="2" :x-gap="12">
            <n-form-item-gi :label="t('rooms.maxRounds')">
              <div style="display: flex; align-items: center; gap: 10px; width: 100%;">
                <n-slider v-model:value="roomForm.max_rounds" :min="1" :max="6" :step="1" style="flex: 1;" />
                <n-text style="min-width: 16px;">{{ roomForm.max_rounds }}</n-text>
              </div>
            </n-form-item-gi>
            <n-form-item-gi :label="t('rooms.maxMessages')">
              <div style="display: flex; align-items: center; gap: 10px; width: 100%;">
                <n-slider v-model:value="roomForm.max_messages" :min="4" :max="40" :step="2" style="flex: 1;" />
                <n-text style="min-width: 24px;">{{ roomForm.max_messages }}</n-text>
              </div>
            </n-form-item-gi>
          </n-grid>
        </n-form>
      </div>
      <template #action>
        <div style="display: flex; justify-content: flex-end; gap: 8px;">
          <n-button @click="closeRoomEditor">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" :loading="roomSaving" @click="saveRoomEditor">
            {{ editingRoomId ? t('common.save') : t('common.create') }}
          </n-button>
        </div>
      </template>
    </n-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, h, nextTick, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import {
  NAlert, NAvatar, NButton, NCard, NDivider, NDropdown, NEmpty, NForm,
  NFormItem, NFormItemGi, NGi, NGrid, NH6, NIcon, NInput, NModal,
  NPopconfirm, NSpace, NSelect, NSlider, NSpin, NSwitch, NTag, NText, useMessage,
} from 'naive-ui'
import {
  AddOutline, ArrowBackOutline, CheckmarkOutline, ChevronForwardOutline, CloseOutline, CreateOutline,
  EllipsisHorizontalOutline, PeopleOutline, RefreshOutline, SearchOutline, TimeOutline, TrashOutline,
} from '@vicons/ionicons5'
import { useI18n } from 'vue-i18n'
import { useBotsStore } from '@/stores/bots'
import { useModelsStore } from '@/stores/models'
import { useRoomsStore } from '@/stores/rooms'
import type { Bot, BotRoutine } from '@/api/bots'
import type { RoomMessage, RoomSendResult } from '@/api/rooms'
import { request } from '@/api/client'
import { marked } from 'marked'
import hljs from 'highlight.js'
import 'highlight.js/styles/github-dark.css'
import ReasoningContent from '@/components/ReasoningContent.vue'

const { t } = useI18n()
const message = useMessage()
const botsStore = useBotsStore()
const modelsStore = useModelsStore()
const roomsStore = useRoomsStore()

const showEditModal = ref(false)
const advancedExpanded = ref(false)
const showRoutinesModal = ref(false)
const editingBot = ref<Bot | null>(null)
const saving = ref(false)
const addingRoutine = ref(false)
const draft = ref('')
const messagesEl = ref<HTMLElement | null>(null)

// ========== View switching: bots | rooms ==========
const viewMode = ref<'bots' | 'rooms'>('bots')
const searchQuery = ref('')

const railHasActive = computed(() =>
  (viewMode.value === 'bots' && !!botsStore.activeBotName) ||
  (viewMode.value === 'rooms' && !!roomsStore.activeRoomId)
)

const searchPlaceholder = computed(() =>
  viewMode.value === 'rooms' ? t('rooms.searchPlaceholder') : t('bots.searchPlaceholder')
)

function switchView(v: 'bots' | 'rooms') {
  viewMode.value = v
  searchQuery.value = ''
}

function handleRailCreate() {
  if (viewMode.value === 'rooms') openRoomCreate()
  else openCreateModal()
}

function handleRailRefresh() {
  if (viewMode.value === 'rooms') {
    void roomsStore.loadRooms()
    if (roomsStore.activeRoomId) void roomsStore.refreshMessages()
  } else {
    void botsStore.loadBots()
  }
}

const form = reactive({
  name: '',
  title: '',
  description: '',
  system_prompt: '',
  model: '',
  provider: '',
  tools: [] as string[],
  skills: [] as string[],
  memory: '',
  avatar: '',
  env_text: '',
})

// Clone modal state
const showCloneModal = ref(false)
const cloneName = ref('')
const cloning = ref(false)
let cloneTarget: Bot | null = null

// Hidden-bot visibility toggle + quick rename modal
const showHidden = ref(false)
const showRenameModal = ref(false)
const renameName = ref('')
const renaming = ref(false)
let renameTarget: Bot | null = null

// Candidate whitelists fetched from /api/tools and /api/skills.
const toolOptions = ref<{ label: string; value: string }[]>([])
const skillOptions = ref<{ label: string; value: string }[]>([])

const routineForm = reactive({ name: '', schedule: '', prompt: '' })
// Non-null while the form is editing an existing routine instead of adding.
const editingRoutineId = ref<string | null>(null)
const togglingIds = reactive(new Set<string>())
const clearingChat = ref(false)
const runningIds = reactive(new Set<string>)

const scheduleHint = computed(() => {
  const s = routineForm.schedule.trim()
  if (!s) return { text: '', type: 'default' as const }
  const parts = s.split(/\s+/)
  if (parts.length < 5) return { text: t('cron.scheduleHint5fields'), type: 'error' as const }
  if (parts.length > 6) return { text: t('cron.scheduleHint6fields'), type: 'error' as const }
  if (s === '0 8 * * *') return { text: t('cron.scheduleHintDaily8'), type: 'success' as const }
  if (s === '0 8 * * 1-5') return { text: t('cron.scheduleHintWeekday9'), type: 'success' as const }
  if (s === '0 */2 * * *') return { text: t('cron.scheduleHint2hours'), type: 'success' as const }
  if (s === '0 0 * * *') return { text: t('cron.scheduleHintMidnight'), type: 'success' as const }
  return { text: t('cron.scheduleValid'), type: 'success' as const }
})

const activeBot = computed(() =>
  botsStore.bots.find(b => b.name === botsStore.activeBotName) || null
)
// The list endpoint already embeds runtime info; keep a non-null object for template use
const activeBotObj = computed<Bot | null>(() => activeBot.value)

// Hidden bots stay running (routines/DMs/rooms) but are filtered from the rail
// unless the user explicitly opts in to see them.
const visibleBots = computed(() =>
  botsStore.bots.filter(b => !b.hidden || showHidden.value)
)

const filteredBots = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return visibleBots.value
  return visibleBots.value.filter(b =>
    b.name.toLowerCase().includes(q) ||
    (b.mention_tag || '').toLowerCase().includes(q) ||
    (b.title || '').toLowerCase().includes(q) ||
    (b.description || '').toLowerCase().includes(q)
  )
})

const onlineCount = computed(() => botsStore.bots.filter(b => b.runtime?.online).length)

const activeRoutineCount = computed(() =>
  botsStore.bots.reduce((acc, b) => acc + (b.runtime?.active_routines ?? 0), 0)
)

// Grok-style conversation starters shown for an empty conversation.
const starters = computed(() => [
  t('bots.starter1'),
  t('bots.starter2'),
  t('bots.starter3'),
  t('bots.starter4'),
])

// A bot is "Active now" when its most recent completed turn is under 5 min old.
function isActiveNow(b: Bot | null | undefined): boolean {
  if (!b?.runtime?.online || !b.runtime.last_active) return false
  return Date.now() - b.runtime.last_active * 1000 < 5 * 60 * 1000
}

// Driven by the store: set when GET /api/bots returns 503 (bot mode off).
const botModeDisabled = computed(() => botsStore.modeDisabled)

const botModelSelectOptions = computed(() => modelsStore.modelSelectOptions)

function renderBotModelLabel(option: { label: string; value: string }) {
  const parts = option.label.split(' / ')
  const provider = parts[0] || ''
  const model = parts[1] || ''
  return h('div', {
    style: 'display: flex; align-items: center; gap: 6px; padding: 4px 0;'
  }, [
    h('span', null, provider),
    h('span', null, `/ ${model}`),
  ])
}

// Unified "provider/model" select (matches Chat UI).
// value === "" means inherit global; otherwise "provider/model" id string.
const selectedModelId = computed<string>({
  get: () => {
    if (form.provider && form.model) return `${form.provider}/${form.model}`
    return ''
  },
  set: (v: string) => {
    if (!v) {
      form.provider = ''
      form.model = ''
      return
    }
    const idx = v.indexOf('/')
    if (idx < 0) {
      form.model = v
      form.provider = ''
    } else {
      form.provider = v.slice(0, idx)
      form.model = v.slice(idx + 1)
    }
  },
})

function cardMenuOptions(b: Bot) {
  return [
    { label: t('bots.openChat'), key: 'chat' },
    { label: t('common.edit'), key: 'edit' },
    { label: t('bots.renameBot'), key: 'rename' },
    { label: t('bots.manageRoutines'), key: 'routines' },
    { label: t('bots.cloneBot'), key: 'clone' },
    { label: b.hidden ? t('bots.unhideBot') : t('bots.hideBot'), key: 'hidden' },
    { label: t('common.delete'), key: 'delete' },
  ]
}

function handleCardAction(key: string, b: Bot) {
  switch (key) {
    case 'chat':
      botsStore.openChat(b.name)
      break
    case 'edit':
      openEditModal(b)
      break
    case 'rename':
      openRenameModal(b)
      break
    case 'routines':
      botsStore.openChat(b.name)
      openRoutinesModal()
      break
    case 'clone':
      openCloneModal(b)
      break
    case 'hidden':
      void handleToggleHidden(b)
      break
    case 'delete':
      void handleDeleteBot(b)
      break
  }
}

function openCreateModal() {
  editingBot.value = null
  form.name = ''
  form.title = ''
  form.description = ''
  form.system_prompt = ''
  form.model = ''
  form.provider = ''
  form.tools = []
  form.skills = []
  form.memory = ''
  form.avatar = ''
  form.env_text = ''
  advancedExpanded.value = false
  showEditModal.value = true
}

function openEditModal(b: Bot | null) {
  if (!b) return
  editingBot.value = b
  form.name = b.name
  form.title = b.title || ''
  form.description = b.description || ''
  form.system_prompt = b.system_prompt || ''
  form.model = b.model || ''
  form.provider = b.provider || ''
  form.tools = [...(b.tools || [])]
  form.skills = [...(b.skills || [])]
  form.memory = b.memory || ''
  form.avatar = b.avatar || ''
  form.env_text = envToText(b.env)
  advancedExpanded.value = false
  showEditModal.value = true
}

async function handleSaveBot() {
  if (!editingBot.value && !form.name.trim()) {
    message.warning(t('bots.enterName'))
    return
  }
  saving.value = true
  try {
    const payload: Record<string, unknown> = {
      title: form.title,
      description: form.description,
      system_prompt: form.system_prompt,
      model: form.model,
      provider: form.provider,
      memory: form.memory,
      avatar: form.avatar,
    }
    const env = parseEnvPairs(form.env_text)
    if (editingBot.value) {
      const orig = editingBot.value
      // Whitelists: send only on change; empty + clear flag = wipe to "inherit all".
      if (!arraysEqual(orig.tools || [], form.tools)) {
        if (form.tools.length === 0) payload.clear_tools = true
        else payload.tools = form.tools
      }
      if (!arraysEqual(orig.skills || [], form.skills)) {
        if (form.skills.length === 0) payload.clear_skills = true
        else payload.skills = form.skills
      }
      if (!envsEqual(orig.env || {}, env)) {
        if (Object.keys(env).length === 0) payload.clear_env = true
        else payload.env = env
      }
      await botsStore.updateBot(editingBot.value.name, payload)
      message.success(t('bots.botUpdated'))
    } else {
      payload.tools = form.tools
      payload.skills = form.skills
      if (Object.keys(env).length > 0) payload.env = env
      await botsStore.createBot({ name: form.name.trim(), ...payload })
      message.success(t('bots.botCreated'))
    }
    showEditModal.value = false
  } catch (e: any) {
    message.error(e.message || t('common.operationFailed'))
  } finally {
    saving.value = false
  }
}

function openCloneModal(b: Bot) {
  cloneTarget = b
  cloneName.value = ''
  showCloneModal.value = true
}

async function handleCloneConfirm() {
  if (!cloneTarget || !cloneName.value.trim()) return
  const newName = cloneName.value.trim()
  cloning.value = true
  try {
    await botsStore.cloneBot(cloneTarget.name, newName)
    message.success(t('bots.cloneSuccess', { name: '@' + newName }))
    showCloneModal.value = false
  } catch (e: any) {
    message.error(e.message || t('common.operationFailed'))
  } finally {
    cloning.value = false
  }
}

async function handleToggleHidden(b: Bot) {
  try {
    await botsStore.updateBot(b.name, { hidden: !b.hidden })
    message.success(b.hidden ? t('bots.botShown') : t('bots.botHidden'))
  } catch (e: any) {
    message.error(e.message || t('common.operationFailed'))
  }
}

function openRenameModal(b: Bot) {
  renameTarget = b
  renameName.value = b.title || ''
  showRenameModal.value = true
}

async function handleRenameConfirm() {
  if (!renameTarget || !renameName.value.trim()) return
  const title = renameName.value.trim()
  renaming.value = true
  try {
    await botsStore.updateBot(renameTarget.name, { title })
    message.success(t('bots.renamed'))
    showRenameModal.value = false
  } catch (e: any) {
    message.error(e.message || t('common.operationFailed'))
  } finally {
    renaming.value = false
  }
}

async function handleDeleteBot(b: Bot) {
  try {
    await botsStore.deleteBot(b.name)
    message.success(t('bots.botDeleted'))
  } catch (e: any) {
    message.error(e.message || t('common.operationFailed'))
  }
}

function openRoutinesModal() {
  routineForm.name = ''
  routineForm.schedule = ''
  routineForm.prompt = ''
  editingRoutineId.value = null
  showRoutinesModal.value = true
}

function resetRoutineForm() {
  routineForm.name = ''
  routineForm.schedule = ''
  routineForm.prompt = ''
  editingRoutineId.value = null
}

async function handleSaveRoutine() {
  addingRoutine.value = true
  try {
    if (editingRoutineId.value) {
      await botsStore.updateRoutine(editingRoutineId.value, {
        name: routineForm.name.trim(),
        schedule: routineForm.schedule.trim(),
        prompt: routineForm.prompt.trim(),
      })
      message.success(t('bots.routineUpdated'))
    } else {
      await botsStore.addRoutine({
        name: routineForm.name.trim(),
        schedule: routineForm.schedule.trim(),
        prompt: routineForm.prompt.trim(),
      })
      message.success(t('bots.routineAdded'))
    }
    resetRoutineForm()
  } catch (e: any) {
    message.error(e.message || t('common.operationFailed'))
  } finally {
    addingRoutine.value = false
  }
}

async function handleToggleRoutine(rt: BotRoutine, enabled: boolean) {
  if (togglingIds.has(rt.id)) return
  togglingIds.add(rt.id)
  try {
    await botsStore.toggleRoutine(rt, enabled)
    message.success(enabled ? t('bots.routineEnabled') : t('bots.routineDisabled'))
  } catch (e: any) {
    message.error(e.message || t('common.operationFailed'))
  } finally {
    togglingIds.delete(rt.id)
  }
}

async function handleRunRoutineNow(rt: BotRoutine) {
  if (runningIds.has(rt.id)) return
  runningIds.add(rt.id)
  try {
    await botsStore.runRoutineNow(rt.id)
    message.success(t('bots.routineTriggered'))
  } catch (e: any) {
    message.error(e.message || t('common.operationFailed'))
  } finally {
    runningIds.delete(rt.id)
  }
}

async function handleClearChat() {
  if (!botsStore.activeBotName) return
  clearingChat.value = true
  try {
    await botsStore.clearMessages()
    message.success(t('bots.chatCleared'))
  } catch (e: any) {
    message.error(e.message || t('common.operationFailed'))
  } finally {
    clearingChat.value = false
  }
}

function openEditRoutine(rt: BotRoutine) {
  editingRoutineId.value = rt.id
  routineForm.name = rt.name || ''
  routineForm.schedule = rt.schedule || ''
  routineForm.prompt = rt.prompt || ''
}

function cancelEditRoutine() {
  resetRoutineForm()
}

async function handleRemoveRoutine(routineId: string) {
  try {
    await botsStore.removeRoutine(routineId)
    if (editingRoutineId.value === routineId) {
      resetRoutineForm()
    }
    message.success(t('bots.routineRemoved'))
  } catch (e: any) {
    message.error(e.message || t('common.operationFailed'))
  }
}

async function handleSend() {
  const text = draft.value.trim()
  if (!text) return
  // Canonical chat protection: bot conversations are persistent by design,
  // so session-reset commands are not available here. Use "Clear chat" instead.
  if (/^\/(new|reset)\b/i.test(text)) {
    message.warning(t('bots.canonicalChatHint'))
    return
  }
  draft.value = ''
  try {
    await botsStore.sendMessage(text)
  } catch (e: any) {
    message.error(e.message || t('common.operationFailed'))
  }
}

// Quick starter chips send their prompt immediately (Grok-style starters).
async function handleStarter(text: string) {
  if (!text || botsStore.sending) return
  try {
    await botsStore.sendMessage(text)
  } catch (e: any) {
    message.error(e.message || t('common.operationFailed'))
  }
}

watch(
  () => [botsStore.messages.length, botsStore.sending],
  async () => {
    await nextTick()
    const el = messagesEl.value
    if (el) el.scrollTop = el.scrollHeight
  }
)

// ========== Rooms (Bot group chat) ==========
const activeRoom = computed(() => roomsStore.getActiveRoom())

const filteredRooms = computed(() => {
  const q = searchQuery.value.trim().toLowerCase()
  if (!q) return roomsStore.rooms
  return roomsStore.rooms.filter(r =>
    (r.name || '').toLowerCase().includes(q) ||
    (r.topic || '').toLowerCase().includes(q) ||
    r.members.some(m => m.toLowerCase().includes(q))
  )
})

const totalRoomMembers = computed(() =>
  roomsStore.rooms.reduce((acc, r) => acc + r.members.length, 0)
)

function closeRoomChat() {
  roomsStore.activeRoomId = null
  roomsStore.messages = []
}

// ---------- room editor ----------
const showRoomEditor = ref(false)
const editingRoomId = ref<string | null>(null)
const roomSaving = ref(false)

// Room limit defaults — must match backend DefaultRoomMaxRounds/MaxMessages.
const DEFAULT_MAX_ROUNDS = 3
const DEFAULT_MAX_MESSAGES = 10

/** Clamp a room limit into its valid range; fall back to the default when 0/NaN. */
function normRounds(v: number | undefined): number {
  const n = Number.isFinite(v) && (v as number) > 0 ? (v as number) : DEFAULT_MAX_ROUNDS
  return Math.min(6, Math.max(1, Math.round(n)))
}
function normMessages(v: number | undefined): number {
  const n = Number.isFinite(v) && (v as number) > 0 ? (v as number) : DEFAULT_MAX_MESSAGES
  return Math.min(40, Math.max(4, Math.round(n)))
}

const roomForm = reactive({
  name: '',
  topic: '',
  members: [] as string[],
  max_rounds: DEFAULT_MAX_ROUNDS,
  max_messages: DEFAULT_MAX_MESSAGES,
})

function openRoomCreate() {
  editingRoomId.value = null
  roomForm.name = ''
  roomForm.topic = ''
  roomForm.members = []
  roomForm.max_rounds = DEFAULT_MAX_ROUNDS
  roomForm.max_messages = DEFAULT_MAX_MESSAGES
  showRoomEditor.value = true
  if (botsStore.bots.length === 0) {
    void botsStore.loadBots()
  }
}

function openRoomEdit() {
  if (!activeRoom.value) return
  editingRoomId.value = activeRoom.value.id
  roomForm.name = activeRoom.value.name
  roomForm.topic = activeRoom.value.topic || ''
  roomForm.members = [...activeRoom.value.members]
  // Old rooms may carry 0 / missing limits — normalize before rendering so the
  // sliders never show an out-of-range thumb (0 on a min=1 slider).
  roomForm.max_rounds = normRounds(activeRoom.value.max_rounds)
  roomForm.max_messages = normMessages(activeRoom.value.max_messages)
  showRoomEditor.value = true
  if (botsStore.bots.length === 0) {
    void botsStore.loadBots()
  }
}

function closeRoomEditor() {
  showRoomEditor.value = false
}

const canSaveRoom = computed(() =>
  roomForm.members.length >= 2 && roomForm.members.length <= 6 && !!roomForm.name.trim()
)

function toggleRoomMember(name: string) {
  const idx = roomForm.members.indexOf(name)
  if (idx >= 0) {
    roomForm.members.splice(idx, 1)
  } else if (roomForm.members.length < 6) {
    roomForm.members.push(name)
  } else {
    message.warning(t('rooms.maxMembers'))
  }
}

async function saveRoomEditor() {
  if (roomSaving.value) return
  if (!roomForm.name.trim()) {
    message.warning(t('rooms.nameRequired'))
    return
  }
  if (roomForm.members.length < 2) {
    message.warning(t('rooms.minMembers'))
    return
  }
  if (!canSaveRoom.value) return
  roomSaving.value = true
  const data = {
    name: roomForm.name.trim(),
    topic: roomForm.topic.trim(),
    members: [...roomForm.members],
    max_rounds: normRounds(roomForm.max_rounds),
    max_messages: normMessages(roomForm.max_messages),
  }
  try {
    if (editingRoomId.value) {
      await roomsStore.updateRoom(editingRoomId.value, data)
      message.success(t('rooms.updated'))
    } else {
      const room = await roomsStore.createRoom(data)
      message.success(t('rooms.created'))
      await roomsStore.selectRoom(room.id)
    }
    showRoomEditor.value = false
  } catch (e: any) {
    message.error(e?.message || t('common.operationFailed'))
  } finally {
    roomSaving.value = false
  }
}

async function handleDeleteRoom(id: string) {
  try {
    await roomsStore.deleteRoom(id)
    message.success(t('rooms.deleted'))
  } catch (e: any) {
    message.error(e instanceof Error ? e.message : String(e))
  }
}

// ---------- room chat input (@mention) ----------
const roomDraft = ref('')
const roomMessagesEl = ref<HTMLElement | null>(null)

const roomShowMention = ref(false)
const roomMentionActiveIdx = ref(0)
const roomMentionQuery = ref('')

const roomFilteredMentions = computed(() => {
  const members = activeRoom.value?.members || []
  const q = roomMentionQuery.value.toLowerCase()
  const list = q ? members.filter(m => m.toLowerCase().includes(q)) : members
  return list.slice(0, 8)
})

function onRoomInput() {
  const val = roomDraft.value
  const caret = (document.activeElement as HTMLTextAreaElement)?.selectionStart ?? val.length
  const uptoCaret = val.slice(0, caret)
  const m = uptoCaret.match(/@(\S*)$/)
  if (m && !uptoCaret.slice(0, uptoCaret.length - m[0].length).endsWith('@@')) {
    roomShowMention.value = true
    roomMentionQuery.value = m[1] || ''
    if (roomMentionActiveIdx.value >= roomFilteredMentions.value.length) {
      roomMentionActiveIdx.value = 0
    }
  } else {
    roomShowMention.value = false
  }
}

function selectRoomMention(name: string) {
  const el = document.activeElement as HTMLTextAreaElement
  const caret = el?.selectionStart ?? roomDraft.value.length
  const uptoCaret = roomDraft.value.slice(0, caret)
  const m = uptoCaret.match(/@(\S*)$/)
  if (m) {
    const start = caret - m[0].length
    roomDraft.value = roomDraft.value.slice(0, start) + '@' + name + ' ' + roomDraft.value.slice(caret)
    nextTick(() => {
      const pos = start + name.length + 2
      el?.setSelectionRange(pos, pos)
    })
  }
  roomShowMention.value = false
}

const roomActiveTarget = computed(() => {
  if (!activeRoom.value) return ''
  const m = roomDraft.value.match(/@(\S+)/)
  if (!m) return ''
  const name = m[1].replace(/[^\w-]/g, '')
  return activeRoom.value.members.includes(name) ? name : ''
})

function onRoomKeydown(e: KeyboardEvent) {
  if (roomShowMention.value && roomFilteredMentions.value.length) {
    if (e.key === 'ArrowDown') {
      e.preventDefault()
      roomMentionActiveIdx.value = (roomMentionActiveIdx.value + 1) % roomFilteredMentions.value.length
      return
    }
    if (e.key === 'ArrowUp') {
      e.preventDefault()
      roomMentionActiveIdx.value = (roomMentionActiveIdx.value - 1 + roomFilteredMentions.value.length) % roomFilteredMentions.value.length
      return
    }
    if (e.key === 'Enter' || e.key === 'Tab') {
      e.preventDefault()
      selectRoomMention(roomFilteredMentions.value[roomMentionActiveIdx.value])
      return
    }
    if (e.key === 'Escape') {
      roomShowMention.value = false
      return
    }
  }
  if (e.key === 'Enter' && !e.shiftKey) {
    e.preventDefault()
    void roomSend()
  }
}

async function roomSend(): Promise<void> {
  const text = roomDraft.value.trim()
  if (!text || roomsStore.sending || !activeRoom.value) return
  const target = roomActiveTarget.value || undefined
  roomDraft.value = ''
  roomShowMention.value = false
  try {
    const res: RoomSendResult | null = await roomsStore.sendMessage(text, target)
    if (res?.needs_user) {
      roomsStore.messages.push({
        id: 'sys_' + Date.now(),
        from: '@system',
        content: t('rooms.needsUser'),
        timestamp: Date.now(),
      })
    }
  } catch {
    message.error(t('rooms.sendFailed'))
    // restore draft so the user doesn't lose their message
    if (!roomDraft.value) roomDraft.value = text
  }
}

// ---------- room helpers ----------
function isRoomUserMsg(msg: RoomMessage): boolean {
  return msg.from === '@user' || msg.from.startsWith('user:')
}

function roomDisplayName(from: string): string {
  if (from === '@user') return t('rooms.you')
  if (from === '@system') return 'System'
  return from
}

function roomAvatarText(from: string): string {
  if (from === '@user') return t('rooms.you').slice(0, 1)
  const n = from.replace(/^@/, '')
  return n ? n.slice(0, 1).toUpperCase() : 'B'
}

function roomAvatarColor(from: string): string {
  const hue = (hashCode(from) % 360 + 360) % 360
  return `hsl(${hue}, 55%, 78%)`
}

function tagColor(name: string): string {
  const hue = (hashCode(name) % 360 + 360) % 360
  return `hsl(${hue}, 60%, 90%)`
}

function roomToDate(ts: string | number): Date {
  if (typeof ts === 'number') {
    return ts > 1e12 ? new Date(ts) : new Date(ts * 1000)
  }
  const d = new Date(ts)
  return isNaN(d.getTime()) ? new Date(0) : d
}

function roomFormatTime(timestamp: string | number): string {
  if (!timestamp) return ''
  const date = roomToDate(timestamp)
  if (date.getTime() === 0) return ''
  const now = new Date()
  const isToday = date.toDateString() === now.toDateString()
  if (isToday) {
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }
  return (
    date.toLocaleDateString([], { month: 'short', day: 'numeric' }) +
    ' ' +
    date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  )
}

watch(
  () => roomsStore.messages.length,
  async () => {
    await nextTick()
    const el = roomMessagesEl.value
    if (el) el.scrollTop = el.scrollHeight
  }
)

function formatTime(ts: number) {
  if (!ts) return ''
  const d = new Date(ts)
  if (isNaN(d.getTime())) return ''
  const now = new Date()
  const isToday = d.toDateString() === now.toDateString()
  if (isToday) {
    return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }
  return d.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' +
    d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

function formatDate(ts: number): string {
  if (!ts) return ''
  const d = new Date(ts)
  if (isNaN(d.getTime())) return ''
  const now = new Date()
  const today = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  const yesterday = new Date(today.getTime() - 86400000)
  const msgDate = new Date(d.getFullYear(), d.getMonth(), d.getDate())
  if (msgDate.getTime() === today.getTime()) return ''
  if (msgDate.getTime() === yesterday.getTime()) return 'Yesterday'
  return d.toLocaleDateString([], { month: 'short', day: 'numeric', year: 'numeric' })
}

const groupedMessages = computed(() => {
  const groups: Record<string, any[]> = {}
  for (const msg of botsStore.messages) {
    const key = formatDate(msg.timestamp)
    if (!groups[key]) groups[key] = []
    groups[key].push(msg)
  }
  return groups
})

const botDisplayName = computed(() => {
  const bot = activeBot.value
  return bot ? `@${bot.mention_tag || bot.name}` : 'Bot'
})

const botAvatarText = computed(() => {
  const bot = activeBot.value
  const tag = bot?.mention_tag || bot?.name || 'B'
  return tag.slice(0, 2).toUpperCase()
})

const codeRenderer = (code: string, lang?: string): string => {
  const language = lang && hljs.getLanguage(lang) ? lang : null
  const highlighted = language
    ? hljs.highlight(code, { language }).value
    : hljs.highlightAuto(code).value
  const copyBtn = `<button class="code-copy-btn" type="button">Copy</button>`
  return `<div class="code-block">${copyBtn}<pre><code class="hljs${language ? ` language-${language}` : ''}">${highlighted}</code></pre></div>`
}

marked.use({
  renderer: { code: codeRenderer },
  breaks: true,
  gfm: true,
})

const mdCache = new Map<string, string>()
const MD_CACHE_LIMIT = 200

function renderMarkdown(content: string): string {
  const cached = mdCache.get(content)
  if (cached !== undefined) return cached
  const html = marked.parse(content) as string
  if (mdCache.size >= MD_CACHE_LIMIT) {
    const keys = mdCache.keys()
    for (let i = 0; i < MD_CACHE_LIMIT / 2; i++) {
      const r = keys.next()
      if (r.done) break
      mdCache.delete(r.value)
    }
  }
  mdCache.set(content, html)
  return html
}

function handleCodeClick(e: MouseEvent) {
  const target = e.target as HTMLElement
  const btn = target.closest('.code-copy-btn') as HTMLElement | null
  if (btn) {
    const codeEl = btn.parentElement?.querySelector('code')
    const code = codeEl?.textContent || ''
    navigator.clipboard.writeText(code).catch(() => {})
    const original = btn.textContent
    btn.textContent = '✓'
    setTimeout(() => { btn.textContent = original }, 2000)
    return
  }
  const pre = target.closest('pre')
  if (pre) {
    const code = pre.querySelector('code')
    if (code) {
      navigator.clipboard.writeText(code.textContent || '').then(() => {
        message.success(t('groupchat.copied') || 'Copied')
      }).catch(() => {})
    }
  }
}

function handleDocumentCodeClick(e: MouseEvent) {
  const target = e.target as HTMLElement
  const btn = target.closest('.code-copy-btn') as HTMLElement | null
  if (!btn) return
  const codeEl = btn.parentElement?.querySelector('code')
  const code = codeEl?.textContent || ''
  navigator.clipboard.writeText(code).catch(() => {})
  const original = btn.textContent
  btn.textContent = '✓'
  setTimeout(() => { btn.textContent = original }, 2000)
}

onMounted(() => {
  void botsStore.loadBots()
  void roomsStore.loadRooms()
  void modelsStore.loadModels()
  void loadCandidates()
  document.addEventListener('click', handleDocumentCodeClick)
})

onUnmounted(() => {
  document.removeEventListener('click', handleDocumentCodeClick)
})

const palette = ['#4f7cff', '#18a058', '#f0a020', '#d03050', '#722ed1', '#13c2c2']
function avatarColor(name: string) {
  let h = 0
  for (let i = 0; i < name.length; i++) h = (h * 31 + name.charCodeAt(i)) % 997
  return palette[h % palette.length]
}

function avatarText(name: string): string {
  const n = name.replace(/^@/, '')
  return n ? n.slice(0, 1).toUpperCase() : 'B'
}

// ========== Capability isolation helpers ==========
function isImageAvatar(avatar?: string): boolean {
  if (!avatar) return false
  return /^(https?:\/\/|data:image\/)/.test(avatar.trim())
}

// A short multi-byte token (emoji / symbol) is rendered verbatim; anything
// else falls back to the initials avatar.
function isCustomAvatar(avatar?: string): boolean {
  if (!avatar) return false
  const a = avatar.trim()
  if (!a || isImageAvatar(a)) return false
  return [...a].length <= 2
}

function arraysEqual(a: string[], b: string[]): boolean {
  if (a.length !== b.length) return false
  const sa = [...a].sort()
  const sb = [...b].sort()
  return sa.every((v, i) => v === sb[i])
}

function envsEqual(a: Record<string, string>, b: Record<string, string>): boolean {
  const ka = Object.keys(a)
  const kb = Object.keys(b)
  if (ka.length !== kb.length) return false
  return ka.every(k => a[k] === b[k])
}

function envToText(env?: Record<string, string>): string {
  if (!env) return ''
  return Object.entries(env).map(([k, v]) => `${k}=${v}`).join('\n')
}

function parseEnvPairs(text: string): Record<string, string> {
  const out: Record<string, string> = {}
  for (const raw of text.split('\n')) {
    const line = raw.trim()
    if (!line || line.startsWith('#')) continue
    const idx = line.indexOf('=')
    if (idx <= 0) continue
    const k = line.slice(0, idx).trim()
    const v = line.slice(idx + 1).trim()
    if (k) out[k] = v
  }
  return out
}

function hashCode(s: string): number {
  let h = 0
  for (let i = 0; i < s.length; i++) {
    h = (h * 31 + s.charCodeAt(i)) | 0
  }
  return h
}

async function loadCandidates() {
  try {
    const tools = (await request('/tools')) as { name?: string }[]
    toolOptions.value = (tools || [])
      .map(x => ({ label: x.name || '', value: x.name || '' }))
      .filter(x => x.value)
    const skills = (await request('/skills')) as { name?: string }[]
    skillOptions.value = (skills || [])
      .map(x => ({ label: x.name || '', value: x.name || '' }))
      .filter(x => x.value)
  } catch {
    /* candidates are optional; tag mode still allows free-text entry */
  }
}
</script>

<style scoped>
/* ========== Shell: left rail + right pane (Grok-style) ========== */
.bots-shell {
  height: 100vh;
  height: 100dvh;
  display: flex;
  overflow: hidden;
}

/* ========== Advanced section collapse toggle ========== */
.advanced-toggle {
  display: flex;
  align-items: center;
  gap: 6px;
  cursor: pointer;
  user-select: none;
  color: var(--n-text-color, #333);
  font-weight: 600;
  font-size: 14px;
  padding: 4px 0 10px;
  border-bottom: 1px solid #ececec;
  margin-bottom: 14px;
}
.advanced-toggle .n-icon {
  transition: transform 0.2s ease;
  color: #999;
}
.advanced-toggle .n-icon.rotated {
  transform: rotate(90deg);
  color: inherit;
}
.advanced-toggle:hover {
  color: var(--primary-color, #18a058);
}

/* ========== Left rail ========== */
.bot-rail {
  width: 292px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  border-right: 1px solid #e8e8e8;
}

.rail-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 12px 14px 8px;
  flex-shrink: 0;
}

/* Segmented view switcher: Bot | 群聊 */
.rail-tabs {
  display: flex;
  background: #f0f0f2;
  border-radius: 8px;
  padding: 3px;
  flex: 1;
  min-width: 0;
}

.rail-tab {
  flex: 1;
  border: none;
  background: transparent;
  padding: 5px 10px;
  border-radius: 6px;
  font-size: 13px;
  font-weight: 600;
  color: #6b7280;
  cursor: pointer;
  transition: background 0.15s, color 0.15s, box-shadow 0.15s;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.rail-tab:hover {
  color: #374151;
}

.rail-tab.active {
  background: #fff;
  color: #1f2937;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.12);
}

.rail-search {
  padding: 0 14px 8px;
  flex-shrink: 0;
}

.rail-alert {
  margin: 0 14px 8px;
  flex-shrink: 0;
}

.bot-list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 2px 8px 8px;
}

.list-spinner,
.list-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px 0;
}

.rail-footer {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  padding: 6px 12px;
  border-top: 1px solid #efefef;
  flex-shrink: 0;
}

/* ========== Bot row ========== */
.bot-row {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border-radius: 10px;
  cursor: pointer;
  transition: background 0.15s;
  position: relative;
}

.bot-row:hover {
  background: rgba(0, 0, 0, 0.05);
}

.bot-row.active {
  background: rgba(79, 124, 255, 0.12);
}

.bot-row.active::before {
  content: '';
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 3px;
  height: 22px;
  border-radius: 2px;
  background: #4f7cff;
}

.bot-row.hidden {
  opacity: 0.55;
}

.bot-row.hidden:hover {
  opacity: 0.85;
}

.row-avatar {
  flex-shrink: 0;
}

.row-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.row-name {
  display: flex;
  align-items: baseline;
  gap: 6px;
  min-width: 0;
}

.row-name-text {
  font-weight: 600;
  font-size: 13px;
  color: #1f2937;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.row-mention {
  font-size: 11px;
  color: #9ca3af;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex-shrink: 1;
}

.row-sub {
  font-size: 12px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.row-status {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: #d1d5db;
  flex-shrink: 0;
}

.row-status.online {
  background: #18a058;
}

.row-status.active-now {
  box-shadow: 0 0 0 3px rgba(24, 160, 88, 0.25);
  animation: status-pulse 1.6s ease-in-out infinite;
}

@keyframes status-pulse {
  0%, 100% { box-shadow: 0 0 0 3px rgba(24, 160, 88, 0.15); }
  50% { box-shadow: 0 0 0 4px rgba(24, 160, 88, 0.35); }
}

.row-menu {
  flex-shrink: 0;
  opacity: 0;
  transition: opacity 0.15s;
}

.bot-row:hover .row-menu,
.row-menu:focus-within {
  opacity: 1;
}

/* ========== Room row ========== */
.room-row-avatar {
  width: 34px;
  height: 34px;
  border-radius: 50%;
  flex-shrink: 0;
  display: flex;
  align-items: center;
  justify-content: center;
  background: linear-gradient(135deg, #8b5cf6 0%, #6d28d9 100%);
  color: #fff;
}

.room-row-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 3px;
}

.mini-chip {
  font-size: 10px;
  background: #f0f0f0;
  color: #666;
  padding: 0 6px;
  border-radius: 8px;
}

.room-delete-btn {
  opacity: 0;
}

.room-row:hover .room-delete-btn {
  opacity: 1;
}

/* ========== Right pane ========== */
.chat-pane {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  background: #fff;
}

/* ========== Welcome ========== */
.welcome {
  flex: 1;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
  overflow-y: auto;
}

.welcome-inner {
  max-width: 560px;
  text-align: center;
}

.welcome-greeting {
  font-size: 26px;
  font-weight: 700;
  margin-bottom: 8px;
}

.welcome-sub {
  color: #6b7280;
  font-size: 14px;
  margin: 0 0 28px;
}

.welcome-stats {
  margin-bottom: 8px;
}

.stat-card {
  min-width: 108px;
  padding: 14px 18px;
  border-radius: 12px;
  background: #fafafa;
  border: 1px solid #eee;
}

.stat-value {
  font-size: 24px;
  font-weight: 700;
  color: #1f2937;
}

.stat-value.stat-online {
  color: #18a058;
}

.stat-label {
  font-size: 12px;
  color: #9ca3af;
  margin-top: 2px;
}

.welcome-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  justify-content: center;
  margin-top: 16px;
}

.welcome-chip {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 14px 6px 8px;
  border-radius: 999px;
  border: 1px solid #e5e7eb;
  background: #fff;
  cursor: pointer;
  transition: border-color 0.15s, box-shadow 0.15s, transform 0.15s;
}

.welcome-chip:hover {
  border-color: #4f7cff;
  box-shadow: 0 2px 8px rgba(79, 124, 255, 0.15);
  transform: translateY(-1px);
}

.chip-name {
  font-size: 13px;
  font-weight: 600;
  color: #374151;
}

/* ========== Empty chat (bot profile + starters) ========== */
.empty-chat {
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  padding: 60px 16px 24px;
  max-width: 520px;
  margin: 0 auto;
  width: 100%;
}

.empty-avatar {
  margin-bottom: 14px;
}

.empty-name {
  font-size: 20px;
  font-weight: 700;
}

.empty-desc {
  color: #6b7280;
  font-size: 14px;
  margin: 8px 0 0;
  line-height: 1.6;
}

.starter-title {
  margin-top: 28px;
  font-size: 12px;
  font-weight: 600;
  color: #9ca3af;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

.starter-chips {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  justify-content: center;
  margin-top: 12px;
}

.starter-chip {
  padding: 8px 16px;
  border-radius: 999px;
  border: 1px solid #e5e7eb;
  background: #fff;
  font-size: 13px;
  color: #374151;
  cursor: pointer;
  transition: border-color 0.15s, box-shadow 0.15s, transform 0.15s;
}

.starter-chip:hover:not(:disabled) {
  border-color: #4f7cff;
  color: #4f7cff;
  box-shadow: 0 2px 8px rgba(79, 124, 255, 0.15);
  transform: translateY(-1px);
}

.starter-chip:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

/* ========== Chat view ========== */
.chat-header {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 16px;
  border-bottom: 1px solid #e8e8e8;
  flex-shrink: 0;
}

.chat-title {
  flex: 1;
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
}

.chat-title-text {
  display: flex;
  flex-direction: column;
  min-width: 0;
  line-height: 1.35;
}

.chat-body {
  flex: 1;
  min-height: 0;
  overflow: hidden;
  display: flex;
  flex-direction: column;
}

.chat-messages {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 20px 24px;
  padding-bottom: 80px;
  display: flex;
  flex-direction: column;
  gap: 0;
}

.chat-messages > * {
  max-width: 960px;
  margin-left: auto;
  margin-right: auto;
  width: 100%;
}

/* ========== Message Layout ========== */
.message {
  display: flex;
  gap: 12px;
  margin-bottom: 20px;
  animation: fadeIn 0.3s ease;
}

@keyframes fadeIn {
  from { opacity: 0; transform: translateY(8px); }
  to { opacity: 1; transform: translateY(0); }
}

.message.user {
  flex-direction: row-reverse;
}

.message.system {
  justify-content: center;
}

.message-body {
  max-width: 72%;
  min-width: 0;
  display: flex;
  flex-direction: column;
}

.message-body.bot-body {
  max-width: 80%;
}

/* ========== Avatars ========== */
.avatar {
  width: 34px;
  height: 34px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  font-size: 14px;
  color: #fff;
  font-weight: 600;
  user-select: none;
}

.user-avatar {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}

.bot-avatar {
  background: linear-gradient(135deg, #18a058 0%, #36ad6a 100%);
}

.avatar-img {
  width: 100%;
  height: 100%;
  border-radius: 50%;
  object-fit: cover;
}

/* ========== Message Header ========== */
.message-header {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 4px;
  font-size: 13px;
}

.message.user .message-header {
  justify-content: flex-end;
  flex-direction: row-reverse;
}

.message.user .message-body {
  align-items: flex-end;
}

.sender-name {
  color: #333;
}

.message-time {
  font-size: 11px;
  color: #bbb;
}

.send-error {
  font-size: 11px;
  color: #d03050;
}

.stream-spin {
  margin-right: 4px;
}

/* ========== Message Bubbles ========== */
.message-bubble {
  padding: 14px 18px;
  border-radius: 16px;
  line-height: 1.75;
  word-break: break-word;
  overflow-wrap: break-word;
  display: flex;
  align-items: flex-start;
  gap: 8px;
}

.user-bubble {
  background: linear-gradient(135deg, #4f7cff 0%, #3a5fd9 100%);
  color: #fff;
  border-bottom-right-radius: 4px;
}

.agent-bubble {
  background: #fff;
  color: #1f2937;
  border: 1px solid #e8e8e8;
  border-bottom-left-radius: 4px;
  box-shadow: 0 1px 2px rgba(0, 0, 0, 0.06);
}

.bubble-error {
  border-color: #f0a0b0 !important;
}

.message-bubble.thinking {
  padding: 12px 16px;
}

.typing-indicator {
  display: flex;
  align-items: center;
  gap: 5px;
  padding: 2px 0;
}

.typing-indicator .dot {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #18a058;
  animation: typing-bounce 1.4s ease-in-out infinite;
}

.typing-indicator .dot:nth-child(2) {
  animation-delay: 0.2s;
}

.typing-indicator .dot:nth-child(3) {
  animation-delay: 0.4s;
}

@keyframes typing-bounce {
  0%, 60%, 100% {
    transform: translateY(0);
    opacity: 0.4;
  }
  30% {
    transform: translateY(-6px);
    opacity: 1;
  }
}

.history-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 40px 0;
}

.message-bubble.streaming {
  border-style: dashed;
  animation: pulse-border 1.5s ease-in-out infinite;
}

@keyframes pulse-border {
  0%, 100% { border-color: #c8e6c9; }
  50% { border-color: #66bb6a; }
}

.bubble-content {
  word-break: break-word;
  overflow-wrap: break-word;
  flex: 1;
  min-width: 0;
}

.bubble-content :deep(.placeholder) {
  color: #999;
}

/* ========== Markdown Content ========== */
.message-bubble :deep(p) { margin: 0 0 10px 0; }
.message-bubble :deep(p:last-child) { margin-bottom: 0; }
.message-bubble :deep(ul), .message-bubble :deep(ol) { margin: 10px 0; padding-left: 28px; }
.message-bubble :deep(li) { margin: 5px 0; }

.message-bubble :deep(blockquote) {
  margin: 10px 0;
  padding: 8px 16px;
  border-left: 4px solid #d0d0d0;
  background: rgba(0, 0, 0, 0.03);
  color: inherit;
}

.message-bubble :deep(table) {
  border-collapse: collapse;
  margin: 10px 0;
  width: 100%;
}

.message-bubble :deep(th), .message-bubble :deep(td) {
  border: 1px solid #d0d0d0;
  padding: 6px 12px;
}

.message-bubble :deep(th) {
  background: rgba(0, 0, 0, 0.04);
}

.message-bubble :deep(h1),
.message-bubble :deep(h2),
.message-bubble :deep(h3),
.message-bubble :deep(h4) {
  margin: 14px 0 8px;
  font-weight: 600;
}

.message-bubble :deep(h1) { font-size: 20px; }
.message-bubble :deep(h2) { font-size: 18px; }
.message-bubble :deep(h3) { font-size: 16px; }
.message-bubble :deep(h4) { font-size: 15px; }

.message-bubble :deep(hr) {
  border: none;
  border-top: 1px solid #d0d0d0;
  margin: 14px 0;
}

.message-bubble :deep(pre) {
  background: #1e1e1e;
  color: #d4d4d4;
  padding: 12px 16px;
  border-radius: 8px;
  overflow-x: auto;
  max-width: 100%;
  margin: 10px 0;
}

.message-bubble :deep(.code-block) {
  position: relative;
  margin: 10px 0;
  border-radius: 8px;
  overflow: hidden;
  background: #1e1e1e;
}

.message-bubble :deep(.code-block pre) {
  margin: 0;
  padding: 12px 15px;
  overflow-x: auto;
}

.message-bubble :deep(.code-block code) {
  font-family: 'SF Mono', 'Consolas', monospace;
  font-size: 13px;
  color: #d4d4d4;
  line-height: 1.6;
}

.message-bubble :deep(.code-copy-btn) {
  position: absolute;
  top: 6px;
  right: 6px;
  background: rgba(255, 255, 255, 0.1);
  border: 1px solid rgba(255, 255, 255, 0.2);
  color: #ccc;
  padding: 2px 10px;
  border-radius: 4px;
  font-size: 11px;
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.2s;
}

.message-bubble :deep(.code-block:hover .code-copy-btn) {
  opacity: 1;
}

.message-bubble :deep(code) {
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 13px;
}

.message-bubble :deep(a) {
  color: inherit;
  text-decoration: underline;
  font-weight: 600;
}

.user-bubble :deep(a) {
  color: #e0f0ff;
  text-shadow: 0 1px 2px rgba(0, 0, 0, 0.3);
}

.user-bubble :deep(a:hover) {
  color: #fff;
}

/* ========== Date Separator ========== */
.date-separator {
  text-align: center;
  padding: 16px 0 8px;
  font-size: 12px;
  color: #999;
}

.date-separator span {
  background: #f5f5f5;
  padding: 2px 12px;
  border-radius: 10px;
}

/* ========== Code Block Click Hint ========== */
.chat-messages :deep(pre:hover) {
  outline: 2px solid #4f7cff;
  outline-offset: 2px;
  cursor: pointer;
  transition: outline 0.2s;
}

.chat-messages :deep(pre) {
  position: relative;
  border-radius: 6px;
}

.chat-messages :deep(pre:hover::after) {
  content: 'Click to copy';
  position: absolute;
  top: 4px;
  right: 8px;
  font-size: 11px;
  color: #4f7cff;
  background: rgba(255, 255, 255, 0.9);
  padding: 0 6px;
  border-radius: 3px;
  pointer-events: none;
}

/* ========== Room chat extras ========== */
.member-tags {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
  overflow: hidden;
  max-width: 40%;
  justify-content: flex-end;
}

.room-topic {
  flex-shrink: 0;
  padding: 6px 16px;
  font-size: 12px;
  color: #888;
  border-bottom: 1px dashed #eee;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.system-notice {
  font-size: 12px;
  color: #b45309;
  background: #fef3c7;
  border: 1px solid #fde68a;
  border-radius: 6px;
  padding: 6px 14px;
  max-width: 80%;
  text-align: center;
}

.room-typing {
  display: flex;
  justify-content: flex-start;
  margin-top: 2px;
}

.room-typing-bubble {
  display: flex;
  align-items: center;
  gap: 10px;
  background: #f5f5f5;
  border: 1px solid #e8e8e8;
  border-radius: 16px;
  border-bottom-left-radius: 4px;
  padding: 10px 14px;
}

.room-typing-dots {
  display: flex;
  gap: 4px;
}

.room-typing-dots span {
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: #999;
  animation: room-bounce 1.2s infinite ease-in-out;
}

.room-typing-dots span:nth-child(2) { animation-delay: 0.15s; }
.room-typing-dots span:nth-child(3) { animation-delay: 0.3s; }

@keyframes room-bounce {
  0%, 60%, 100% { transform: translateY(0); opacity: 0.5; }
  30% { transform: translateY(-5px); opacity: 1; }
}

.room-typing-text {
  font-size: 12px;
  color: #888;
}

.input-hint {
  font-size: 11px;
  color: #aaa;
  margin-bottom: 6px;
  display: flex;
  align-items: center;
  gap: 8px;
}

.target-chip {
  font-size: 11px;
  background: #e8f4ff;
  color: #2080f0;
  padding: 1px 8px;
  border-radius: 8px;
}

/* mention popup */
.mention-popup {
  position: absolute;
  bottom: 100%;
  left: 12px;
  width: 220px;
  max-height: 240px;
  overflow-y: auto;
  background: #fff;
  border: 1px solid #e0e0e0;
  border-radius: 8px;
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.12);
  z-index: 20;
  padding: 4px;
}

.mention-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 8px;
  border-radius: 6px;
  cursor: pointer;
}

.mention-item.active {
  background: #f0f0f0;
}

.mention-avatar {
  width: 22px;
  height: 22px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 11px;
  font-weight: 600;
  color: #333;
  flex-shrink: 0;
}

.mention-name {
  font-size: 13px;
  color: #333;
}

/* ========== Input Area ========== */
.input-area {
  display: flex;
  flex-direction: column;
  gap: 4px;
  padding: 12px 24px 16px;
  border-top: 1px solid #e0e0e0;
  background: #fff;
  align-items: stretch;
  flex-shrink: 0;
}

.input-area > .input-wrapper {
  max-width: 960px;
  margin-left: auto;
  margin-right: auto;
  width: 100%;
}

.input-hint {
  max-width: 960px;
  width: 100%;
  margin-left: auto;
  margin-right: auto;
}

.input-wrapper {
  position: relative;
  min-width: 0;
  width: 100%;
  border: 1px solid #d9d9d9;
  border-radius: 12px;
  padding: 12px 16px;
  padding-right: 48px;
  background: #fff;
  transition: border-color 0.2s, box-shadow 0.2s;
}

.input-wrapper:focus-within {
  border-color: #18a058;
  box-shadow: 0 0 0 2px rgba(24, 160, 88, 0.15);
}

.send-btn-inline {
  position: absolute;
  right: 8px;
  bottom: 8px;
  width: 32px;
  height: 32px;
  border: none;
  border-radius: 8px;
  background: #18a058;
  color: #fff;
  cursor: pointer;
  display: flex;
  align-items: center;
  justify-content: center;
  transition: background 0.2s, opacity 0.2s;
  flex-shrink: 0;
}

.send-btn-inline:hover:not(:disabled) {
  background: #20803a;
}

.send-btn-inline:disabled {
  background: #c5c5c5;
  cursor: not-allowed;
}

.send-btn-inline.stopping {
  background: #f0a020;
}

.send-btn-inline.stopping:hover {
  background: #d98610;
}

.chat-input {
  --n-border: none !important;
  --n-border-hover: none !important;
  --n-border-focus: none !important;
  --n-box-shadow-focus: none !important;
  --n-padding-left: 0 !important;
  --n-padding-right: 0 !important;
  background: transparent !important;
}

.chat-input :deep(.n-input) {
  background: transparent !important;
  box-shadow: none !important;
}

.chat-input :deep(.n-input__border),
.chat-input :deep(.n-input__border-focus),
.chat-input :deep(.n-input__state-border) {
  border: none !important;
  box-shadow: none !important;
  display: none !important;
}

.chat-input.n-input :deep(.n-input__state-border),
.chat-input:deep(.n-input--focus .n-input__state-border) {
  border: none !important;
  box-shadow: none !important;
  display: none !important;
}

.chat-input :deep(.n-input__textarea-el) {
  resize: none;
}

/* ========== Room editor modal ========== */
.editor-body {
  max-height: 65vh;
  overflow-y: auto;
  padding-right: 4px;
}

.member-picker {
  display: flex;
  flex-direction: column;
  gap: 6px;
}

.picker-loading {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  color: #888;
  padding: 12px 0;
}

.member-option {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 10px;
  border: 1px solid #e5e5e5;
  border-radius: 8px;
  cursor: pointer;
  transition: all 0.15s;
}

.member-option:hover {
  border-color: #2080f0;
  background: #f6fafe;
}

.member-option.picked {
  border-color: #18a058;
  background: #f0faf2;
}

.member-avatar {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  font-weight: 600;
  color: #fff;
  flex-shrink: 0;
}

.member-option-name {
  font-size: 13px;
  font-weight: 600;
  color: #333;
}

.member-option-title {
  font-size: 11px;
  color: #999;
  flex: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.member-count {
  font-size: 11px;
  color: #999;
  text-align: right;
}

.routine-editing {
  border: 1px solid var(--n-color-target, #18a058);
}

/* ========== Mobile: single pane at a time ========== */
@media (max-width: 768px) {
  .bots-shell {
    height: 100vh;
    height: 100dvh;
    overflow: hidden;
  }

  .bot-rail {
    width: 100%;
    border-right: none;
    animation: mobile-rail-in 0.22s ease;
  }

  .bot-rail.rail-collapsed-mobile {
    display: none;
  }

  .chat-pane.pane-collapsed-mobile {
    display: none;
  }

  .chat-pane {
    width: 100%;
    animation: mobile-pane-in 0.22s ease;
  }

  .back-btn {
    display: flex;
  }

  /* 头部两行布局：第一行 返回+标题+状态；第二行 操作按钮右对齐 */
  .chat-header {
    flex-wrap: wrap;
    gap: 6px;
    padding: 8px 10px;
  }
  .chat-title {
    gap: 6px;
  }
  .chat-header :deep(.n-button) {
    font-size: 12px;
    height: 30px;
    padding: 0 8px;
  }
  .chat-actions {
    order: 3;
    width: 100%;
    justify-content: flex-end;
    margin-left: auto;
    flex-wrap: wrap; /* 操作按钮多时(在线/活跃/routines/清空/编辑)允许内部换行 */
  }

  .chat-messages {
    padding: 10px 12px;
    padding-bottom: 16px;
  }
  .message {
    margin-bottom: 14px;
    gap: 8px;
  }
  .avatar {
    width: 30px;
    height: 30px;
    font-size: 14px;
  }
  .message-body {
    max-width: 88%;
  }
  .message-body.bot-body {
    max-width: 90%;
  }

  .empty-chat {
    padding: 32px 12px 16px;
  }
  .starter-chip {
    font-size: 12px;
    padding: 7px 14px;
  }

  .input-area {
    padding: 8px 10px;
    padding-bottom: calc(8px + env(safe-area-inset-bottom));
  }
  .input-wrapper {
    padding: 8px 12px;
    padding-right: 42px; /* 内置发送按钮仍留空间 */
  }

  /* 欢迎页统计卡片均分宽度，避免 3 卡片溢出小屏 */
  .welcome {
    padding: 16px;
  }
  .welcome-stats {
    width: 100%;
  }
  .stat-card {
    min-width: 0;
    flex: 1;
    padding: 12px 8px;
  }

  /* rail 行触控面积增大 */
  .bot-row {
    padding: 10px;
  }
  .row-menu {
    opacity: 1;
  }
  .room-delete-btn {
    opacity: 1; /* 手机无 hover，删除按钮必须常显 */
  }

  .member-tags {
    display: none;
  }

  /* 群聊 @ 提及浮层贴边不溢出 */
  .mention-popup {
    left: 10px;
    right: 10px;
    width: auto;
  }

  /* 弹窗内操作行(如 routines 的运行/编辑/删除)允许换行，避免溢出 */
  .modal-responsive :deep(.n-space) {
    flex-wrap: wrap;
  }
}

/* rail/pane 切换进入动画（仅移动端 display 切换时播放） */
@keyframes mobile-pane-in {
  from { opacity: 0; transform: translateX(10px); }
  to { opacity: 1; transform: translateX(0); }
}

@keyframes mobile-rail-in {
  from { opacity: 0; transform: translateX(-10px); }
  to { opacity: 1; transform: translateX(0); }
}

/* ========== Small phones (<480px) ========== */
@media (max-width: 480px) {
  .rail-header {
    padding: 10px 10px 6px;
  }
  .welcome-greeting {
    font-size: 22px;
  }
  .stat-value {
    font-size: 20px;
  }
  .chat-header {
    padding: 6px 8px;
  }
  .message {
    gap: 6px;
  }
  .avatar {
    width: 28px;
    height: 28px;
    font-size: 13px;
  }
}

/* Desktop: back button not needed */
@media (min-width: 769px) {
  .back-btn {
    display: none;
  }
}

/* ========== Dark Mode ========== */
@media (prefers-color-scheme: dark) {
  .bot-rail {
    background: #16161a;
    border-right-color: #374151;
  }
  .rail-tabs {
    background: #222228;
  }
  .rail-tab {
    color: #9ca3af;
  }
  .rail-tab:hover {
    color: #d1d5db;
  }
  .rail-tab.active {
    background: #2c2c33;
    color: #f3f4f6;
  }
  .bot-row:hover {
    background: rgba(255, 255, 255, 0.06);
  }
  .bot-row.active {
    background: rgba(79, 124, 255, 0.18);
  }
  .row-name-text {
    color: #e5e7eb;
  }
  .mini-chip {
    background: #2c2c33;
    color: #9ca3af;
  }
  .rail-footer {
    border-top-color: #2c2c33;
  }
  .chat-pane {
    background: #1a1a1f;
  }
  .welcome-greeting {
    color: #f3f4f6;
  }
  .welcome-sub {
    color: #9ca3af;
  }
  .stat-card {
    background: #1f1f23;
    border-color: #374151;
  }
  .stat-value {
    color: #e5e7eb;
  }
  .welcome-chip,
  .starter-chip {
    background: #1f1f23;
    border-color: #374151;
  }
  .welcome-chip:hover {
    border-color: #4f7cff;
  }
  .chip-name {
    color: #d1d5db;
  }
  .starter-chip {
    color: #d1d5db;
  }
  .starter-chip:hover:not(:disabled) {
    color: #93c5fd;
    border-color: #4f7cff;
  }
  .empty-name {
    color: #f3f4f6;
  }
  .empty-desc {
    color: #9ca3af;
  }
  .chat-header {
    border-bottom-color: #374151;
  }
  .sender-name {
    color: #d1d5db;
  }
  .message-time {
    color: #6b7280;
  }
  .agent-bubble {
    background: #1f1f23;
    color: #e5e7eb;
    border-color: #374151;
  }
  .user-bubble {
    background: linear-gradient(135deg, #4f7cff 0%, #3a5fd9 100%);
  }
  .message-bubble :deep(blockquote) {
    border-left-color: #4b5563;
    background: rgba(255, 255, 255, 0.05);
  }
  .message-bubble :deep(th) {
    background: rgba(255, 255, 255, 0.05);
  }
  .message-bubble :deep(th),
  .message-bubble :deep(td) {
    border-color: #374151;
  }
  .date-separator span {
    background: #2c2c33;
  }
  .room-topic {
    color: #9ca3af;
    border-bottom-color: #2c2c33;
  }
  .room-typing-bubble {
    background: #1f1f23;
    border-color: #374151;
  }
  .room-typing-text {
    color: #9ca3af;
  }
  .input-area {
    border-top-color: #374151;
    background: #1a1a1f;
  }
  .input-wrapper {
    background: #1a1a1f;
    border-color: #374151;
  }
  .input-wrapper:focus-within {
    border-color: #18a058;
    box-shadow: 0 0 0 2px rgba(24, 160, 88, 0.2);
  }
  .chat-messages :deep(pre:hover::after) {
    background: rgba(0, 0, 0, 0.8);
    color: #60a5fa;
  }
  .typing-indicator .dot {
    background: #36ad6a;
  }
  .mention-popup {
    background: #1f1f23;
    border-color: #374151;
  }
  .mention-item.active {
    background: #2c2c33;
  }
  .mention-name {
    color: #d1d5db;
  }
  .target-chip {
    background: rgba(32, 128, 240, 0.2);
    color: #7cb6ff;
  }
}
</style>
