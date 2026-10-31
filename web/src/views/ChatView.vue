<template>
  <div class="chat-container">
    <!-- Session Sidebar -->
    <div class="session-sidebar" :class="{ 'mobile-expanded': mobileSessionExpanded }">
      <!-- 移动端顶部工具条：收起时它就是整个顶栏（桌面端隐藏）。
           中间 = 当前会话标题（绝对居中，点按任意空白处展开/收起会话抽屉），
           右侧 = 搜索 / 新建两个高频操作成组外露，不必先展开抽屉再找。
           旧版这里只有一根 30px 的拖动条：既看不到当前会话是哪个，
           任何操作也得先展开——这是移动端头部体验的最大短板。 -->
      <div class="mobile-topbar" @click="mobileSessionExpanded = !mobileSessionExpanded">
        <n-icon size="16" class="mobile-topbar-caret" :class="{ open: mobileSessionExpanded }"><ChevronDownOutline /></n-icon>
        <span class="mobile-topbar-title">{{ mobileTopbarTitle }}</span>
        <span class="mobile-topbar-actions">
          <button
            type="button"
            class="mobile-topbar-btn"
            :aria-label="t('chat.searchPlaceholder')"
            @click.stop="expandAndFocusSearch"
          >
            <n-icon :size="18"><SearchOutline /></n-icon>
          </button>
          <button
            type="button"
            class="mobile-topbar-btn"
            :aria-label="t('chat.newSession')"
            @click.stop="createSession"
          >
            <n-icon :size="18"><AddOutline /></n-icon>
          </button>
        </span>
      </div>
      <div class="sidebar-header" v-show="!isMobile || mobileSessionExpanded">
        <!-- 会话搜索：本地过滤已加载会话；命中不足时自动补齐后端全量 web 会话 -->
        <n-input
          ref="sidebarSearchInput"
          v-model:value="sessionSearch"
          size="small"
          round
          clearable
          class="sidebar-search-input"
          :placeholder="t('chat.searchPlaceholder')"
          @keydown.esc="sessionSearch = ''"
        >
          <template #prefix>
            <n-icon :component="SearchOutline" :size="14" />
          </template>
        </n-input>
        <!-- 新建聊天（移动端隐藏：顶栏工具条上已有 + 按钮，避免重复入口） -->
        <n-button v-show="!isMobile" type="primary" class="new-chat-btn" @click="createSession" size="small">
          <template #icon>
            <n-icon><AddOutline /></n-icon>
          </template>
        </n-button>
      </div>
      <div class="session-list" ref="sessionListRef" v-show="!isMobile || mobileSessionExpanded">
        <!-- 会话骨架：数据（尤其是全量补齐）到位前占位，避免"空 → 20 条 → 分组"的多次跳变 -->
        <div v-if="sessionListSkeletonVisible" class="session-skeleton">
          <div v-for="i in 8" :key="`sk${i}`" class="session-skeleton-row">
            <div class="session-skeleton-line sk-title"></div>
            <div class="session-skeleton-line sk-meta"></div>
          </div>
        </div>
        <div v-if="isSearching" class="profile-group-header">{{ t('chat.searchResults', { count: visibleSessions.length }) }}</div>
        <template v-for="row in sidebarRows" :key="row.key">
          <!-- 分组头（点标题折叠/展开）。仅当存在"设置过工作目录"的分组时出现，
               全是未设置目录的会话时侧栏保持扁平列表。 -->
          <div
            v-if="row.kind === 'group'"
            class="session-group-head"
            :class="{ collapsed: row.collapsed, 'group-dir': row.userSet }"
            :title="row.label"
            @click="toggleGroup(row.groupKey)"
          >
            <n-icon size="12" class="session-group-caret"><ChevronDownOutline /></n-icon>
            <n-icon size="13" class="session-group-icon" :component="row.userSet ? FolderOutline : ChatbubbleOutline" />
            <span class="session-group-label">{{ row.userSet ? dirLabel(row.label) : row.label }}</span>
            <!-- 悬停时用操作按钮顶替数量徽标：分组头本身很窄，留出位置给「新建/删除」 -->
            <span class="session-group-count">{{ row.count }}</span>
            <span class="session-group-actions">
              <n-button
                size="tiny"
                quaternary
                circle
                class="session-group-action"
                :title="row.userSet ? t('chat.newSessionInDirInherit', { dir: row.label }) : t('chat.newSession')"
                @click.stop="createSessionInGroup(row)"
              >
                <template #icon><n-icon size="13"><AddOutline /></n-icon></template>
              </n-button>
              <n-button
                size="tiny"
                quaternary
                circle
                type="error"
                class="session-group-action"
                :title="row.userSet ? t('chat.deleteDirSessions') : t('chat.deleteGroupSessions')"
                @click.stop="deleteGroupSessions(row)"
              >
                <template #icon><n-icon size="13"><TrashOutline /></n-icon></template>
              </n-button>
            </span>
          </div>
          <!-- 组内"加载更多"：该目录下还有没渲染出来的会话 -->
          <div
            v-else-if="row.kind === 'more'"
            class="session-group-more"
            :class="{ 'in-group': showGroupHeaders }"
            @click="showMoreInGroup(row.groupKey)"
          >
            {{ t('chat.loadMoreSessions', { count: row.hidden }) }}
          </div>
          <div
            v-else
            class="session-item"
            :class="{ active: chatStore.activeSessionId === row.session.id, 'in-group': showGroupHeaders }"
            @click="handleSessionClick(row.session.id)"
            @mouseenter="loadSessionGoals(row.session.id)"
          >
            <div class="session-content">
              <div class="session-title">
                <div style="display: flex; align-items: center; gap: 4px;">
                  <!-- 正在执行标识：该会话当前有回合在流式执行（小尺寸，避免喧宾夺主） -->
                  <n-spin v-if="chatStore.isSessionRunning(row.session.id)" :size="12" style="flex-shrink: 0;" />
                  <!-- Goal indicator - icon only, hover shows details -->
                  <n-popover 
                    v-if="getSessionGoals(row.session.id).length" 
                    trigger="hover"
                    placement="right"
                    :show-arrow="true"
                    :duration="100"
                    :show="popoverShow[row.session.id]"
                    @mouseenter="popoverShow[row.session.id] = true"
                    @mouseleave="popoverShow[row.session.id] = false"
                  >
                    <template #trigger>
                      <n-icon :component="FlagOutline" :size="14" color="#2080f0" style="cursor: pointer; flex-shrink: 0;" />
                    </template>
                    <template #default>
                      <div style="min-width: 240px; padding: 8px;">
                        <n-text strong style="font-size: 13px; display: block; margin-bottom: 8px;">
                          {{ t('goals.linkedGoals') }}
                        </n-text>
                        <div class="session-goal-list">
                          <div 
                            v-for="goal in getSessionGoals(row.session.id)" 
                            :key="goal.id" 
                            class="session-goal-item"
                          >
                            <div style="display: flex; align-items: center; justify-content: space-between; width: 100%;">
                              <span style="font-size: 13px; flex: 1; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;">{{ goal.title }}</span>
                              <n-progress :percentage="goal.progress" :show-indicator="false" :height="6" style="width: 60px; margin-left: 8px;" />
                            </div>
                            <n-text depth="3" style="font-size: 11px;">{{ t('goals.statusOptions.' + goal.status) }}</n-text>
                            <n-space :size="4" style="margin-top: 4px;">
                              <n-button size="tiny" text @click="(e: Event) => { e.stopPropagation(); goToGoal(goal.id); }">{{ t('goals.details') }}</n-button>
                              <n-button size="tiny" text type="error" @click="(e: Event) => { e.stopPropagation(); unlinkSessionGoal(goal.id, row.session.id, row.session.id); }">{{ t('goals.unlinkGoal') }}</n-button>
                            </n-space>
                          </div>
                        </div>
                      </div>
                    </template>
                  </n-popover>
                  <span class="session-title-text">{{ row.session.title || t('chat.untitled') }}</span>
                </div>
              </div>
              <div class="session-meta">
                <n-tag v-if="row.session.source && row.session.source !== 'web'" size="tiny" :type="sourceType(row.session.source)" style="margin-right: 4px;">
                  {{ row.session.source }}
                </n-tag>
                {{ row.session.message_count || 0 }} {{ t('chat.messages') }}
              </div>
            </div>
            <div class="session-actions">
              <n-dropdown
                trigger="click"
                placement="bottom-end"
                :options="sessionMenuOptions(row.session)"
                @select="(key: string) => onSessionMenuSelect(key, row.session)"
              >
                <n-button
                  class="session-menu-btn"
                  size="tiny"
                  quaternary
                  circle
                  @click.stop
                >
                  <template #icon>
                    <n-icon size="14"><EllipsisHorizontalOutline /></n-icon>
                  </template>
                </n-button>
              </n-dropdown>
            </div>
          </div>
        </template>
        <n-text v-if="isSearching && !chatStore.sessionsLoading && !searchLoading && visibleSessions.length === 0" depth="3" style="padding: 16px; display: block; text-align: center;">
          {{ t('chat.searchNoResults') }}
        </n-text>
        <n-text v-if="!isSearching && !sidebarRows.length && !chatStore.sessionsLoading && !searchLoading && !sessionListSkeletonVisible" depth="3" style="padding: 16px; display: block; text-align: center;">
          {{ t('chat.noSessions') }}
        </n-text>
      </div>
      <!-- 加载更多时的指示器：绝对定位覆盖在列表底部，**不进入滚动流**。
           它曾作为 .session-list 的最后一个子元素渲染，出现/消失会让 scrollHeight
           变化 66px；浏览器随之钳制/锚定 scrollTop，列表在底部时表现为
           "删掉一个会话后整列先跳一下、再弹回、再跳"——即用户看到的抖动。
           同时 pointer-events:none，不挡住底下那一行的点击。
           门控必须用 sessionsLoadingMore（分页专用），不能用 sessionsLoading：
           后者由 loadSessions 驱动，而它在每轮对话落库收尾时都会跑一次。 -->
      <div
        v-if="(!isMobile || mobileSessionExpanded) && !sessionListSkeletonVisible && (chatStore.sessionsLoadingMore || (isSearching && searchLoading))"
        class="session-list-loading"
      >
        <n-spin size="small" />
      </div>
    </div>

    <!-- Chat Area -->
    <div class="chat-main">
      <n-alert v-if="chatStore.error" type="error" closable style="margin: 12px;" @close="chatStore.error = null">
        {{ chatStore.error.message }}
      </n-alert>

      <n-alert v-if="isGatewaySession" type="info" style="margin: 12px;">
        {{ t('chat.gatewaySession', { source: activeSessionSource }) }}
      </n-alert>

      

      <div class="messages-wrap">
      <div
        class="messages"
        ref="messagesRef"
        @scroll.passive="handleMessagesScroll"
        @wheel.passive="handleMessagesWheel"
        @touchstart.passive="handleMessagesTouchStart"
        @touchmove.passive="handleMessagesTouchMove"
      >
        <div
          v-for="msg in chatStore.messages"
          :key="msg.id"
          class="message"
          :class="msg.role"
        >
          <!-- User message -->
          <template v-if="msg.role === 'user'">
            <div class="avatar user-avatar">👤</div>
            <div class="message-body">
              <div class="message-bubble user-bubble">
                <!-- File attachments in message：统一渲染成「图标 + 文件名」小 chip。
                     图片不再画大图缩略——气泡宽度有限，img 被 max-width/max-height
                     两头钳制后纵横比失真（用户截图报过），缩略图换弹窗原比例看。
                     点击任意附件打开预览弹窗（FilePreviewDialog）。
                     正文里带 [[att:n]] 占位标记的附件已在下方文字流原位置渲染，
                     这里只兜底显示没被标记引用到的附件（老消息 / 标记残缺）。 -->
                <div v-if="msgFilesWithoutMarker(msg).length" class="message-files">
                  <div
                    v-for="(file, idx) in msgFilesWithoutMarker(msg)"
                    :key="idx"
                    class="message-file-item"
                    :class="{ 'message-file-item--clickable': !!file.url }"
                    :title="attachmentLabel(file)"
                    @click="file.url && openAttachmentPreview(file)"
                  >
                    <n-icon size="16">
                      <ImageOutline v-if="isImageAttachment(file)" />
                      <DocumentOutline v-else />
                    </n-icon>
                    <span class="message-file-name">{{ attachmentLabel(file) }}</span>
                  </div>
                </div>
                <!-- 指令消息：按消息里 token 的原始顺序渲染——胶囊、附件和正文交替出现，
                     和输入框里看到的一致（不是把所有胶囊挤到最前面）。
                     多技能时逐条渲染，服务端按位置把正文绑到前面的胶囊上；
                     [[att:n]] 标记位置渲染出行内附件胶囊（可点开预览）。 -->
                <div v-if="msg.content && userDirectiveRefs(msg.content)" class="user-directive-block">
                  <template v-for="(tk, ti) in userDirectiveRefs(msg.content)!" :key="ti">
                    <span v-if="tk.kind === 'chip'" class="user-skill-chip">
                      <n-icon size="13" aria-hidden="true"><ExtensionPuzzleOutline /></n-icon>
                      <span class="user-skill-chip-name">{{ tk.name }}</span>
                    </span>
                    <span
                      v-else-if="tk.kind === 'att' && msgAttFile(msg, tk.attIndex!)"
                      class="user-att-chip"
                      :class="{ 'user-att-chip--clickable': !!msgAttFile(msg, tk.attIndex!)?.url }"
                      :title="attachmentLabel(msgAttFile(msg, tk.attIndex!)!)"
                      @click="msgAttFile(msg, tk.attIndex!)?.url && openAttachmentPreview(msgAttFile(msg, tk.attIndex!)!)"
                    >
                      <n-icon size="13" aria-hidden="true">
                        <ImageOutline v-if="isImageAttachment(msgAttFile(msg, tk.attIndex!)!)" />
                        <DocumentOutline v-else />
                      </n-icon>
                      <span class="user-skill-chip-name">{{ attachmentLabel(msgAttFile(msg, tk.attIndex!)!) }}</span>
                    </span>
                    <span v-else class="user-skill-rest">{{ tk.text }}</span>
                  </template>
                </div>
                <div v-else-if="msg.content" v-html="renderMarkdown(msg.content)"></div>
                <div v-else-if="!msgFilesWithoutMarker(msg).length" class="empty-content">{{ t('chat.fileBtn') }}</div>
              </div>
              <div v-if="formatTime(msg.timestamp)" class="message-time">{{ formatTime(msg.timestamp) }}</div>
            </div>
          </template>

          <!-- Assistant message -->
          <template v-else-if="msg.role === 'assistant'">
            <div class="avatar bot-avatar">🤖</div>
            <div class="message-body assistant-body">
              <div class="assistant-content">
                <!-- 本回合执行耗时：服务端随 assistant 消息落库（turn_elapsed_ms），
                     刷新/切回会话后仍能回看。放在消息最上面，与流式期间的实时计时
                     位置一致；老数据没有该字段时不渲染。 -->
                <div v-if="messageElapsedSeconds(msg) > 0" class="turn-elapsed-line">
                  {{ t('chat.turnElapsedDone', { duration: formatTurnElapsed(messageElapsedSeconds(msg)) }) }}
                </div>
                <!-- 按 streaming_timeline_snapshot 顺序穿插渲染：推理文本 ↔ 工具调用卡片。 -->
                <TimelineMessage
                  :content="msg.content"
                  :segments="msg.streaming_timeline_snapshot"
                  :tools="messageToolCalls(msg)"
                />
                <!-- 本轮变更的文件（仅写/删动作，按路径去重），仅展示头部提示 -->
                <FileChangesBlock
                  v-if="changedFiles(msg).length > 0"
                  :files="changedFiles(msg)"
                  :authoritative="!!(msg.file_ops && msg.file_ops.length)"
                />
              </div>
            </div>
          </template>

          <!-- Tool message: hidden from chat display -->
          <template v-else-if="msg.role === 'tool'">
          </template>

          <!-- System message: command results, notifications -->
          <template v-else-if="msg.role === 'system'">
            <div class="avatar system-avatar">💡</div>
            <div class="message-body">
              <div class="message-bubble system-bubble">
                <div v-html="renderMarkdown(msg.content)"></div>
              </div>
              <div v-if="formatTime(msg.timestamp)" class="message-time">{{ formatTime(msg.timestamp) }}</div>
            </div>
          </template>
        </div>

        <!-- Streaming area -->
        <template v-if="chatStore.streaming">
          <!-- Streaming message with tool calls inline -->
          <div class="message assistant">
            <div class="avatar bot-avatar">🤖</div>
            <div class="message-body assistant-body">
              <!-- 回合执行状态行：贴着机器人头像、在回合内容之上，只有
                   文字、不带图标。回合一开跑就出现，秒数在走 = 回合确实
                   还在被服务端执行，回应"界面停了但不确定是否真停"的疑虑。
                   时长优先取服务端认领时刻（active_started_at，刷新页面
                   不归零），服务端数据未同步时回退本地流式计时。
                   回合结束后流式区消失；最终时长由服务端随 assistant 消息
                   落库（turn_elapsed_ms），历史气泡上继续显示"本回合耗时"。 -->
              <div class="turn-elapsed-line">
                {{ t('chat.turnElapsed', { duration: formatTurnElapsed(turnElapsedForDisplay) }) }}
              </div>

              <!-- Status panel when no content yet & no running tools -->
              <div v-if="!chatStore.streamContent && chatStore.activeToolCalls.length === 0 && chatStore.pendingApprovals.length === 0" class="agent-status-panel">
                <div class="status-header">
                  <div class="status-spinner"></div>
                  <span class="status-phase">{{ agentPhase }}</span>
                </div>
                <div class="status-hint">{{ t(thinkingHints[hintIndex]) }}</div>
              </div>

              <!-- 思考过程 + 最终回答 + 工具调用：按 streamingSegments 顺序穿插渲染。 -->
              <TimelineMessage
                v-if="chatStore.streamContent || chatStore.streamingSegments.length > 0"
                :content="chatStore.streamContent"
                :segments="chatStore.streamingSegments"
                :tools="chatStore.toolCalls"
                :streaming="chatStore.streaming"
              />
              <!-- 流式期间实时展示本轮已变更文件数量；done 后由后端 file_ops 接管 -->
              <FileChangesBlock :files="changedFiles()" />

              <!-- 执行状态坞：贴在本轮回答内容的最下方（属于消息内容的一部分，
                   不悬浮、不吸底）。任务一开跑就出现在这里，随着工具逐条执行
                   往下长，回合结束后随消息一起留在历史里。 -->
              <div v-if="taskDockVisible" class="task-dock-inline">
                <TaskTimeline
                  :steps="taskTimelineSteps"
                  :progress="chatStore.taskProgress"
                />
              </div>
            </div>
          </div>
        </template>

        <n-text v-if="!chatStore.messages.length && !chatStore.streaming" depth="3" class="empty-hint">
          {{ t('chat.selectSession') }}
        </n-text>
      </div>

      <!-- 回到底部：用户上滚看历史时出现（此时自动跟随已让位），点击一键回到最新。
           有未读新消息时角标给出条数，让"下面还有东西"这件事不必靠猜。
           放在 .messages 外层是为了不随内容一起滚动（absolute 在滚动容器里会跟着走）。 -->
      <Transition name="jump-fade">
        <button
          v-if="showJumpBottom"
          type="button"
          class="jump-bottom-btn"
          :title="jumpTitle"
          :aria-label="jumpTitle"
          @click="jumpToBottom"
        >
          <n-icon size="18" :component="ArrowDownOutline" />
          <span v-if="unreadBelow > 0" class="jump-badge">{{ unreadBelow > 99 ? '99+' : unreadBelow }}</span>
        </button>
      </Transition>
      </div>

      <!-- 排队中的消息（沉底 dock）：固定在输入框上方，不随对话滚动。
           放在 .messages 之外是刻意的——排队气泡若留在滚动区里，会随着
           流式内容增长被不断顶动（"晃动"），且位置落在正在生成的回答
           下方，读起来是倒的。做成独立 dock 后它始终待在输入框正上方，
           符合"待发件箱"的直觉，也是主流智能体的做法。
           正在执行的那条不在 queued 里（已转入上面的流式渲染）。

           每一行的结构对齐主流智能体的"待发件箱"：拖动柄 + 单行内容 +
           重发/编辑/删除三个常驻图标，行与行之间用发丝线分隔，整块保持
           中性底色。刻意不做卡片、不加标题栏、不用序号——排队区是"已提交
           内容的清单"，不是需要被强调的告警，越安静越不打扰正在读的回答。

           唯一的例外是底部这行"清空待发"：它是一个作用于整块的操作，
           塞进单行卡片会与那一行的三个图标混淆语义。做成一条安静的
           文字按钮贴在末尾，既不抢视线，也让"一次撤掉后面全部"有出口。 -->
      <div v-if="chatStore.queuedMessages.length" class="queue-dock">
        <div class="queue-dock-inner">
          <div
            v-for="(q, qi) in chatStore.queuedMessages"
            :key="q.turnId"
            class="queue-row"
            :class="{ 'queue-row-dragging': draggingTurnId === q.turnId }"
            @dragover.prevent="onQueueDragOver($event, qi)"
            @drop.prevent="onQueueDrop($event, qi)"
            @dragend="onQueueDragEnd"
          >
            <!-- 拖动柄：原生 draggable 让"拖动排序"这件事对键盘/触屏之外
                 的交互也有语义（拖到别处=移动），不必自造指针逻辑。 -->
            <button
              type="button"
              class="queue-handle"
              draggable="true"
              :title="t('chat.queuedDrag')"
              @dragstart="onQueueDragStart($event, q.turnId, qi)"
            >
              <span class="queue-handle-dots" aria-hidden="true"></span>
            </button>
            <div class="queue-row-main">
              <div class="queue-row-text">{{ queueContentPreview(q.content) }}</div>
              <div v-if="queuedFilesFor(q.turnId).length" class="queue-row-files">
                <span
                  v-for="(f, fi) in queuedFilesFor(q.turnId)"
                  :key="fi"
                  class="queue-row-file"
                >
                  <n-icon size="11"><DocumentOutline /></n-icon>
                  {{ attachmentLabel(f) }}
                </span>
              </div>
            </div>
            <!-- 操作常驻显示（触屏无 hover，悬停才出现则永远点不到） -->
            <div class="queue-row-actions">
              <!-- 引导按钮（置于原"重试"位置）：把本条排队内容注入正在运行的回合
                   直接发送——模型在下一次 LLM 调用前看到它并调整方向，不排队、
                   不打断生成。引导成功后本条从队列移除，不再重复执行。 -->
              <button
                type="button"
                class="queue-row-btn queue-row-btn-guide"
                :title="t('chat.guideSendHint')"
                :disabled="!chatStore.busy"
                @click="handleGuide(q.turnId, queueContentPreview(q.content))"
              >
                <n-icon size="15"><FlashOutline /></n-icon>
              </button>
              <button
                type="button"
                class="queue-row-btn"
                :title="t('chat.queuedEdit')"
                @click="editQueued(q.turnId)"
              >
                <n-icon size="15"><PencilOutline /></n-icon>
              </button>
              <button
                type="button"
                class="queue-row-btn queue-row-btn-danger"
                :title="t('chat.queuedDelete')"
                @click="removeQueued(q.turnId)"
              >
                <n-icon size="15"><TrashOutline /></n-icon>
              </button>
            </div>
          </div>
        </div>
      </div>

      <!-- 执行状态坞已移入流式消息内容内（见上方 .task-dock-inline），
           此处不再保留吸底的独立 dock 区，避免同一状态出现两处。 -->

      <!-- 澄清提问卡片（AI 需求不明确时挂起回合弹出，等待用户选择/补充说明） -->
      <div v-if="chatStore.pendingClarifications.length" class="clarify-bar">
        <div class="clarify-bar-list">
          <ChatClarificationCard
            v-for="clarify in chatStore.pendingClarifications"
            :key="clarify.id"
            :clarification="clarify"
            :session-id="chatStore.activeSessionId || ''"
          />
        </div>
      </div>

      <!-- 底部固定审批栏：待审批命令始终可见可操作，不随对话滚动。 -->
      <div v-if="chatStore.pendingApprovals.length > 0" class="approval-bar">
        <div class="approval-bar-list">
          <ChatApprovalCard
            v-for="approval in chatStore.pendingApprovals"
            :key="approval.id"
            :approval="approval"
            :session-id="chatStore.activeSessionId || ''"
          />
        </div>
      </div>

      <!-- 上传的文件不再单独占一条 preview-bar：它们作为行内胶囊嵌在输入框
           的文字流里（与技能胶囊同一套机制，见 composerChips），可和正文穿插、
           随光标位置插入，点 × 或退格摘掉。 -->

      <!-- ChatGPT-style input box（支持折叠/展开） -->
      <div class="input-area">
        <!-- Command suggestions -->
        <!-- 斜杠候选面板：对齐 WorkBuddy 的分组形态（「技能 (N)」/「指令 (N)」两段，
             每行图标 + 名称 + 描述）。分组标题只在对应段非空时出现；
             键盘上下键/回车的索引语义不变（对应 commandSuggestions 的扁平顺序）。 -->
        <div v-if="commandSuggestions.length > 0" class="command-suggestions">
          <template v-for="group in suggestionGroups" :key="group.key">
            <div class="suggestion-group-title">
              <span>{{ group.title }}</span>
              <span class="suggestion-group-count">({{ group.items.length }})</span>
            </div>
            <div
              v-for="suggestion in group.items"
              :key="group.key + '-' + suggestion.value"
              class="suggestion-item suggestion-item-rich"
              :class="{ active: suggestion === activeSuggestionItem }"
              @click="selectSuggestion(suggestion)"
            >
              <span class="suggestion-avatar" :class="'av-' + (suggestion.group || 'commands')">
                <template v-if="suggestion.group === 'skills'">{{ skillInitial(suggestion.name || '') }}</template>
                <template v-else>/</template>
              </span>
              <span class="suggestion-main">
                <span class="suggestion-name">{{ suggestion.name || suggestion.value.trim() }}</span>
                <span v-if="suggestion.label" class="suggestion-label">{{ suggestion.label }}</span>
              </span>
            </div>
          </template>
        </div>
        <!-- 技能面板：对齐 WorkBuddy 的「+ → 技能 → 二级面板」，每项带描述与启用开关。
             数据源优先用 skillsStore.skills（启用态的唯一权威来源，且含 disabled 项，
             capabilities 只含已启用项）；打开时刷新能力清单，避免"面板里有、真发出去
             用不到"。点击某项 = 把 /skill <name> 填进输入框，走既有的服务端展开链路。 -->
        <div v-if="activePanel === 'skills'" class="cap-panel cap-panel-skills">
          <div class="cap-panel-head">
            <span class="cap-panel-title">{{ t('chat.skills') }}</span>
            <span class="cap-panel-hint">{{ t('chat.skillsHint') }}</span>
          </div>
          <n-input
            v-model:value="skillPanelSearch"
            size="small"
            clearable
            class="cap-panel-search"
            :placeholder="t('chat.skillSearchPlaceholder')"
          >
            <template #prefix><n-icon :component="SearchOutline" :size="14" /></template>
          </n-input>
          <div class="cap-panel-body">
            <div v-if="filteredSkillPanelItems.length === 0" class="cap-panel-empty">
              <div class="cap-panel-empty-title">{{ t('chat.noSkills') }}</div>
              <div class="cap-panel-empty-hint">{{ t('chat.noSkillsHint') }}</div>
            </div>
            <button
              v-for="skill in filteredSkillPanelItems"
              :key="skill.name"
              type="button"
              class="cap-item"
              :class="{ 'is-off': !skill.enabled }"
              :title="skill.description"
              @click="useSkill(skill.name)"
            >
              <span class="cap-item-avatar" :class="{ 'is-off': !skill.enabled }">{{ skillInitial(skill.name) }}</span>
              <span class="cap-item-main">
                <span class="cap-item-name">{{ skill.name }}</span>
                <span class="cap-item-desc">{{ skill.description || '—' }}</span>
              </span>
              <n-switch
                class="cap-item-switch"
                size="small"
                :value="skill.enabled"
                :loading="togglingSkill === skill.name"
                @click.stop
                @update:value="(v: boolean) => onToggleSkill(skill, v)"
              />
            </button>
          </div>
          <div class="cap-panel-foot">
            <button type="button" class="cap-foot-btn" @click="openSkillPicker">
              <n-icon size="14"><AddOutline /></n-icon>
              <span>{{ t('chat.addSkillFromLocal') }}</span>
            </button>
            <span class="cap-foot-sep"></span>
            <button type="button" class="cap-foot-btn" @click="gotoSkillsManage">
              <n-icon size="14"><SettingsOutline /></n-icon>
              <span>{{ t('chat.manageSkills') }}</span>
            </button>
          </div>
          <n-upload
            ref="panelSkillUploadRef"
            :show-file-list="false"
            :multiple="true"
            accept=".zip,.md,.tar,.gz"
            class="cap-upload-hidden"
            @change="onPanelSkillFileChange"
          />
        </div>

        <!-- 插件面板：一个插件是"连接器 + CLI 工具 + 技能"的打包单位，所以面板以插件
             为行、以"它带来了什么"为展开内容——比逐个 MCP 服务器更接近用户的心智
             （用户装的是插件，不是 server）。安装与凭据仍在插件页：这里只放高频动作
             （启用/停用、看状态、点示例问法），不把整套授权表单搬进输入框上方。 -->
        <div v-else-if="activePanel === 'plugins'" class="cap-panel cap-panel-plugins">
          <div class="cap-panel-head">
            <span class="cap-panel-title">{{ t('chat.plugins') }}</span>
            <span class="cap-panel-hint">{{ t('chat.pluginsHint') }}</span>
          </div>
          <n-input
            v-model:value="pluginPanelSearch"
            size="small"
            clearable
            class="cap-panel-search"
            :placeholder="t('chat.pluginSearchPlaceholder')"
          >
            <template #prefix><n-icon :component="SearchOutline" :size="14" /></template>
          </n-input>
          <div class="cap-panel-body">
            <div v-if="agentPluginsStore.loading && !pluginPanelItems.length" class="cap-panel-empty">
              <n-spin size="small" />
            </div>
            <div v-else-if="pluginPanelItems.length === 0" class="cap-panel-empty">
              <div class="cap-panel-empty-title">{{ t('chat.pluginNoPlugins') }}</div>
              <div class="cap-panel-empty-hint">{{ t('chat.pluginNoPluginsHint') }}</div>
            </div>
            <div v-for="p in pluginPanelItems" :key="p.name" class="cap-plugin-group">
              <div class="cap-item cap-item-static" :class="{ 'is-off': !p.enabled }">
                <span class="cap-item-avatar av-plugin" :class="{ 'is-off': !p.enabled }">
                  {{ skillInitial(p.name) }}
                </span>
                <!-- 每行固定两行文字：第一行 名字+版本（右侧摘要），第二行 描述/未就绪
                     原因。之前名字/描述/摘要/原因各占一行，一条插件四行，列表一屏看不
                     几个；细节本来展开就有，没必要在折叠态全铺开。 -->
                <span class="cap-item-main">
                  <span class="cap-plugin-line">
                    <span class="cap-plugin-name" :title="p.name">{{ p.name }}</span>
                    <span v-if="p.version" class="cap-plugin-version">v{{ p.version }}</span>
                    <span v-if="pluginMetaText(p)" class="cap-plugin-meta">{{ pluginMetaText(p) }}</span>
                  </span>
                  <!-- 未就绪时第二行让位给原因（缺凭据 / 命令没装 / 连不上 / 清单被拒）：
                       不写原因用户只会觉得"拨开关没反应"。描述退到 title 里悬停看。 -->
                  <span
                    class="cap-plugin-sub"
                    :class="{ 'is-blocked': !!pluginFirstBlockerText(p) }"
                    :title="p.description || t('chat.pluginNoDescription')"
                  >
                    {{ pluginFirstBlockerText(p) || p.description || t('chat.pluginNoDescription') }}
                  </span>
                </span>
                <button
                  v-if="pluginDetailCount(p) > 0"
                  type="button"
                  class="cap-expand-btn"
                  :title="t('chat.pluginDetails')"
                  @click="togglePluginDetail(p.name)"
                >
                  <n-icon size="14">
                    <n-icon :component="expandedPlugin === p.name ? ChevronDownOutline : ChevronForwardOutline" />
                  </n-icon>
                </button>
                <n-switch
                  class="cap-item-switch"
                  size="small"
                  :value="p.enabled"
                  :loading="pluginBusyName === p.name"
                  :disabled="!!p.rejected"
                  @update:value="(v: boolean) => onTogglePlugin(p, v)"
                />
              </div>
              <!-- 展开 = 这个插件到底给了什么。MCP 条目的工具名要连上才知道（后端只
                   给数量），所以这里只报"连了几个 / 共几个"，点工具挂胶囊那套留给
                   /mcp 指令——插件工具注册的是 ap_* 名字，不走 /mcp 的 server/tool 路由。 -->
              <div v-if="expandedPlugin === p.name" class="cap-tool-list">
                <div v-for="srv in p.mcp_servers" :key="'mcp-' + srv.name" class="cap-plugin-entry">
                  <n-icon
                    size="12"
                    :component="srv.connected ? LinkOutline : UnlinkOutline"
                    :class="srv.connected ? 'cap-status-on' : 'cap-status-off'"
                  />
                  <span class="cap-plugin-entry-name" :title="srv.name">{{ srv.name }}</span>
                  <span class="cap-plugin-entry-meta">
                    {{ srv.connected ? t('chat.mcpConnected') : t('chat.mcpDisconnected') }}
                    <span class="cap-dot">·</span>{{ t('chat.mcpToolsCount', { n: srv.tools || 0 }) }}
                  </span>
                  <span v-if="!srv.connected && srv.error" class="cap-item-error" :title="srv.error">
                    {{ srv.error }}
                  </span>
                </div>
                <div v-for="cli in p.cli_connectors || []" :key="'cli-' + cli.name" class="cap-plugin-entry">
                  <n-icon size="12" :component="TerminalOutline" class="cap-status-off" />
                  <span class="cap-plugin-entry-name" :title="cli.command">{{ cli.name }}</span>
                  <span class="cap-plugin-entry-meta">
                    {{ cli.error ? cli.error : t('chat.mcpToolsCount', { n: cli.tools.length }) }}
                    <template v-if="cli.binary_found === false">
                      <span class="cap-dot">·</span>{{ t('chat.pluginBinaryMissing', { command: cli.command }) }}
                    </template>
                  </span>
                </div>
                <div v-if="(p.skills || []).length" class="cap-plugin-entry">
                  <n-icon size="12" :component="ExtensionPuzzleOutline" class="cap-status-off" />
                  <span class="cap-plugin-entry-name">{{ t('chat.skills') }}</span>
                  <span class="cap-plugin-entry-meta">{{ p.skills.join(', ') }}</span>
                </div>
                <!-- 示例问法：插件给模型的是工具/技能，没有可挂的斜杠指令，把一句真
                     实问法填进输入框就是最直接的试用入口。 -->
                <div v-if="(p.examples || []).length" class="cap-plugin-examples">
                  <span class="cap-plugin-examples-title">{{ t('chat.pluginExamples') }}</span>
                  <button
                    v-for="ex in p.examples"
                    :key="ex"
                    type="button"
                    class="cap-plugin-example"
                    :title="ex"
                    @click="usePluginExample(ex)"
                  >
                    {{ ex }}
                  </button>
                </div>
              </div>
            </div>
          </div>
          <div class="cap-panel-foot">
            <button type="button" class="cap-foot-btn" @click="gotoPluginsManage">
              <n-icon size="14"><SettingsOutline /></n-icon>
              <span>{{ t('chat.pluginManage') }}</span>
            </button>
          </div>
        </div>

        <div class="chat-input-box">
          <!-- 输入区：技能/连接器/指令胶囊作为**行内 token** 嵌在文字里（WorkBuddy
               的 mention 交互）。胶囊的增删由 ComposerEditor 负责呈现，数据源仍是
               这里的 pendingDirectives；发送时编辑器把胶囊还原成 `/skill x` 指令行。 -->
          <ComposerEditor
            ref="composerRef"
            v-model="inputValue"
            :chips="composerChips"
            :placeholder="composerPlaceholder"
            :remove-label="t('common.delete')"
            @change="handleComposerInput"
            @send="handleEnter"
            @nav="handleArrow"
            @escape="handleComposerEsc"
            @chip-remove="onComposerChipRemove"
            @paste="handlePaste"
          />
          <!-- Toolbar inside input box -->
          <div class="input-toolbar">
            <!-- 工具栏单行滚动条：小屏下 + / 技能 / 插件 不再挤成两行 -->
              <div class="toolbar-scroll">
              <!-- 工具按钮：上传文件 / 技能 / 插件 合并到加号下拉菜单中 -->
              <n-dropdown
                trigger="click"
                placement="bottom-start"
                :options="toolbarMenuOptions"
                :render-label="renderToolbarMenuLabel"
                @select="handleToolbarMenuSelect"
                @update:show="(v: boolean) => (toolbarMenuOpen = v)"
              >
                <n-button
                  size="tiny"
                  quaternary
                  class="toolbar-btn"
                  :title="t('chat.toolbarActions')"
                >
                  <template #icon>
                    <n-icon><AddOutline /></n-icon>
                  </template>
                </n-button>
              </n-dropdown>
              <!-- 技能 / 插件的入口统一收在加号菜单里（工具栏不再放常驻胶囊按钮，
                   避免与模型下拉抢宽度、小屏挤压）。 -->
              <!-- 隐藏的上传入口：由下拉菜单"上传文件"触发 -->
              <n-upload
                ref="toolbarUploadRef"
                :show-file-list="false"
                :multiple="true"
                :custom-request="handleFileSelect"
                class="toolbar-upload-hidden"
              />
            </div>
            <div class="toolbar-right">
              <n-select
                v-if="modelOptions.length > 0"
                ref="modelSelectRef"
                v-model:value="currentModelId"
                :options="modelOptions"
                size="small"
                style="min-width: 160px; max-width: 260px; margin-right: 8px"
                :placeholder="t('chat.selectModel')"
                :consistent-menu-width="false"
                :render-label="renderModelLabel"
                :menu-props="{ class: 'model-switch-menu', style: { maxWidth: modelMenuMaxWidth } }"
                @update:show="onModelMenuShow"
                @update:value="handleModelChange"
              />
              <!-- 同一槽位按需切换，优先照顾"用户想发消息"这件事：
                   1. 输入框有内容（文字或附件）→ 永远显示发送键。用户正在打字
                      就是明确要发，此时把按钮换成停止键是反直觉的（这也是本次
                      修的 bug：busy 期间按钮被停止键占走，打好的字发不出去）。
                   2. 否则若在忙（流式或队列非空）→ 显示停止键。
                   3. 否则显示发送键（禁用态）。
                   两种情况下发送键都可点：服务端队列保证串行执行，busy 期间
                   发送就是排队，不会丢也不会并发。 -->
              <n-button
                v-if="chatStore.busy && !hasComposerContent"
                type="warning"
                size="small"
                circle
                @click="stopGeneration"
                :title="t('chat.queuedStopAll')"
                class="send-circle-btn"
              >
                <template #icon>
                  <n-icon><StopCircleOutline /></n-icon>
                </template>
              </n-button>
              <n-button
                v-else
                type="primary"
                size="small"
                circle
                @click="send"
                :disabled="!hasComposerContent"
                :title="chatStore.busy ? t('chat.queuedSendHint') : t('chat.send')"
                class="send-circle-btn"
              >
                <template #icon>
                  <n-icon><SendOutline /></n-icon>
                </template>
              </n-button>
            </div>
          </div>
        </div>
      </div>

      <!-- 底部状态栏：紧凑一行 = 当前会话所属分身（只读）+ 工作目录 + 审批策略。
           工作目录在会话创建时（发出第一条消息前）确定一次，之后固定；底栏只在
           "还没发过消息"的会话上提供「设置目录」按钮，其余情况一律只读展示。
           路径可点不是为了切换目录，而是展开"同目录会话 / 按目录分组"面板。 -->
      <div class="workdir-bar">
        <div class="workdir-bar-inner">
          <span class="workdir-bar-profile" :title="t('chat.workDirProfile')">
            <n-icon size="12"><PersonOutline /></n-icon>
            <span class="workdir-bar-profile-name">{{ activeProfileName }}</span>
          </span>
          <!-- 目录来源标记：路径统一可点，用来展开同目录会话面板；用户选了目录
               （chosen）时不加任何标记，系统按会话分配的默认目录才补一个"默认目录"
               角标。两种情况都是**只读**——目录在发出第一条消息前就已确定，底栏
               不提供随时换目录的入口，避免用户以为能换而把旧目录丢成孤岛。 -->
          <template v-if="chatStore.currentWorkDir">
            <div ref="dirSessAnchorRef" class="dir-sess-anchor">
              <!-- 路径本身有两种形态，视觉一致（同样的字号/图标/位置），只差交互：
                   可展开的目录 → 加下拉箭头，点击弹同目录会话面板；
                   会话级默认目录（<全局>/chat/Chat_xxx，天然"一组一会话"）→ 不加箭头、
                   不弹面板，因为它里面永远只有当前这一个会话，展开没有信息量。 -->
              <span
                v-if="workDirExpandable"
                class="workdir-bar-path workdir-bar-path-link"
                :title="t('chat.dirSessionsOpen')"
                @click="toggleDirSessions"
              >
                <n-icon size="12"><FolderOutline /></n-icon>
                <span class="workdir-bar-path-text">{{ chatStore.currentWorkDir }}</span>
                <span
                  v-if="!chatStore.currentWorkDirChosen && !canChooseWorkDir"
                  class="workdir-bar-badge"
                >{{ t('chat.workDirDefaultBadge') }}</span>
                <!-- 内容还没拉到时，用占位而不是转圈：底栏只要"箭头还没来"这件事 -->
                <n-spin v-if="dirGroupsLoading" :size="10" class="workdir-bar-pending" />
                <n-icon v-else size="12" class="workdir-bar-caret"><ChevronDownOutline /></n-icon>
              </span>
              <span v-else class="workdir-bar-path" :title="chatStore.currentWorkDir">
                <n-icon size="12"><FolderOutline /></n-icon>
                <span class="workdir-bar-path-text">{{ chatStore.currentWorkDir }}</span>
                <!-- 已聊过的会话：目录固定，标一句"默认目录"说明它不是用户选的 -->
                <span
                  v-if="!chatStore.currentWorkDirChosen && !canChooseWorkDir"
                  class="workdir-bar-badge"
                >{{ t('chat.workDirDefaultBadge') }}</span>
              </span>
              <div
                v-if="workDirExpandable && showDirSessions"
                class="dir-sess-panel"
                @click.stop
              >
              <!-- 头部：当前工作目录 + 新建会话 + 批量删除(同目录全部) -->
              <div class="dir-sess-panel-header">
                <n-icon size="14" class="dir-sess-panel-folder"><FolderOutline /></n-icon>
                <span class="dir-sess-panel-title" :title="chatStore.currentWorkDir">{{ chatStore.currentWorkDir }}</span>
                <n-button
                  size="tiny"
                  class="dir-sess-new-btn"
                  :title="t('chat.newSessionInDirInherit', { dir: chatStore.currentWorkDir })"
                  @click.stop="createDirSession()"
                >
                  <template #icon><n-icon size="13"><AddOutline /></n-icon></template>
                </n-button>
                <n-button
                  size="tiny"
                  type="error"
                  quaternary
                  class="dir-sess-del-btn"
                  :disabled="!currentDirSessions.length"
                  :title="t('chat.deleteDirSessions')"
                  @click.stop="deleteDirSessions()"
                >
                  <template #icon><n-icon size="13"><TrashOutline /></n-icon></template>
                </n-button>
              </div>

              <div v-if="dirSessionsLoading" class="dir-sess-loading">
                <n-spin size="small" />
              </div>

              <template v-else>
                <!-- 当前目录模式：只列同目录会话 -->
                <template v-if="dirPanelMode === 'current'">
                  <div v-if="currentDirSessions.length" class="dir-sess-list">
                    <div
                      v-for="s in currentDirSessions"
                      :key="s.id"
                      class="dir-sess-item"
                      :class="{ active: s.id === chatStore.activeSessionId }"
                      @click="selectDirSession(s)"
                    >
                      <div class="dir-sess-title">
                        <span class="dir-sess-title-text">{{ s.title || t('chat.untitled') }}</span>
                        <span v-if="s.id === chatStore.activeSessionId" class="dir-sess-current">{{ t('chat.dirSessionsCurrent') }}</span>
                      </div>
                      <div class="dir-sess-meta">
                        {{ s.message_count || 0 }} {{ t('chat.messages') }} · {{ formatTime(s.last_active) }}
                      </div>
                    </div>
                  </div>
                  <div v-else class="dir-sess-empty">{{ t('chat.dirSessionsEmpty') }}</div>
                  <div v-if="dirGroups.length > 1" class="dir-sess-panel-footer">
                    <n-button size="tiny" quaternary type="primary" @click="dirPanelMode = 'all'">
                      {{ t('chat.dirSessionsAll') }}（{{ dirGroups.length }}）
                    </n-button>
                  </div>
                </template>

                <!-- 全部分组模式：目录 → 其下会话 -->
                <template v-else>
                  <div v-if="dirGroups.length" class="dir-sess-groups">
                    <div v-for="g in dirGroups" :key="g.dir" class="dir-sess-group">
                      <div class="dir-sess-group-head" :title="g.dir">
                        <n-icon size="13" class="dir-sess-group-folder"><FolderOutline /></n-icon>
                        <span class="dir-sess-group-dir">{{ g.dir }}</span>
                        <!-- 会话级默认目录：一个会话独占一个目录，标一句"会话目录"，
                             免得用户以为是漏了同级会话 -->
                        <span v-if="g.is_default_dir" class="dir-sess-group-tag">{{ t('chat.workDirSessionScoped') }}</span>
                        <span class="dir-sess-group-count">{{ g.sessions.length }}</span>
                      </div>
                      <div class="dir-sess-list">
                        <div
                          v-for="s in g.sessions"
                          :key="s.id"
                          class="dir-sess-item"
                          :class="{ active: s.id === chatStore.activeSessionId }"
                          @click="selectDirSession(s)"
                        >
                          <div class="dir-sess-title">
                            <span class="dir-sess-title-text">{{ s.title || t('chat.untitled') }}</span>
                            <span v-if="s.id === chatStore.activeSessionId" class="dir-sess-current">{{ t('chat.dirSessionsCurrent') }}</span>
                          </div>
                          <div class="dir-sess-meta">
                            {{ s.message_count || 0 }} {{ t('chat.messages') }} · {{ formatTime(s.last_active) }}
                          </div>
                        </div>
                      </div>
                    </div>
                  </div>
                  <div v-else class="dir-sess-empty">{{ t('chat.dirSessionsAllEmpty') }}</div>
                  <div class="dir-sess-panel-footer">
                    <n-button size="tiny" quaternary @click="dirPanelMode = 'current'">
                      <template #icon><n-icon><ArrowBackOutline /></n-icon></template>
                      {{ t('chat.dirSessionsBack') }}
                    </n-button>
                  </div>
                </template>
              </template>
              </div>
            </div>
          </template>
          <span v-else class="workdir-bar-path workdir-bar-empty" :title="t('chat.workDirNone')">
            <n-icon size="12"><FolderOutline /></n-icon>
            <span class="workdir-bar-empty-text">{{ t('chat.workDirNone') }}</span>
          </span>
          <!-- 设置目录：本页唯一的目录写入口，且只出现在"还没发过消息"的会话上。
               目录的选择时机只有这一个窗口——发出第一条消息后即固定，底部栏
               不再渲染任何可点控件（后端 PUT 同样按消息数拦截）。 -->
          <n-button
            v-if="canChooseWorkDir"
            size="tiny"
            quaternary
            class="workdir-bar-set-btn"
            :title="t('chat.workDirSet')"
            @click.stop="handleWorkDirMenu('browse')"
          >
            <template #icon><n-icon size="13"><FolderOpenOutline /></n-icon></template>
            {{ t('chat.workDirSet') }}
          </n-button>
          <!-- 快速设置审批策略：聊天页直接切换，无需进入审批管理页 -->
          <n-tooltip
            v-if="approvalStrategyOptions.length > 0"
            :disabled="!approvalStrategyOptions.length"
            placement="top-end"
          >
            <template #trigger>
              <!-- consistent-menu-width=false：菜单按内容自适应宽度，否则菜单=触发框宽(96px)，
                   选项"手动审批"等长文案被截成"手…"（截图问题根因之一）。
                   menu-props 加 approval-select-menu class 供全局样式精确定位（菜单 teleport 到 body） -->
              <n-select
                v-model:value="approvalStrategy"
                :options="approvalStrategyOptions"
                size="tiny"
                class="workdir-bar-approval"
                :consistent-menu-width="false"
                :menu-props="{ class: 'approval-select-menu' }"
                :placeholder="t('chat.quickApproval')"
                :loading="approvalLoading"
                :render-label="renderApprovalLabelShort"
                @update:value="handleApprovalStrategyChange"
              />
            </template>
            {{ t('chat.quickApprovalHint') }}
          </n-tooltip>
        </div>
      </div>
    </div>

    <!-- Goal Sidebar -->
    <RightSidebar v-model:mobile-visible="rightSidebarMobileVisible" />

    <!-- 消息附件预览：公共组件（url 票据来源，禁用编辑） -->
    <FilePreviewDialog
      v-model:show="showAttachmentPreview"
      :name="previewAttachment ? attachmentLabel(previewAttachment) : ''"
      :type-name="previewAttachment ? (previewAttachment.name || previewAttachment.filename || '') : ''"
      :size="previewAttachment?.size"
      :url="previewAttachment?.url || ''"
      :editable="false"
    />

    <!-- Mobile right sidebar toggle: edge handle embedded into the screen's right edge
         (mirrors the left session-sidebar handle), half inside/half outside, so it
         never overlaps the chat content. -->
    <div
      class="right-sidebar-fab"
      :class="{ open: rightSidebarMobileVisible }"
      @click="rightSidebarMobileVisible = !rightSidebarMobileVisible"
      :title="t('sidebar.expand')"
    >
      <n-icon :component="GridOutline" :size="16" />
    </div>

    <!-- Work Directory Picker Modal -->
    <n-modal v-model:show="showDirPicker" :title="t('chat.workDir')" preset="card" class="modal-responsive" style="width: 520px; max-width: 96vw;">
      <!-- Recommended: previously used directories -->
      <div v-if="recommendedDirs.length" class="dir-picker-recommended">
        <div class="dir-picker-recommended-title">{{ t('chat.workDirRecommended') }}</div>
        <div
          v-for="d in recommendedDirs"
          :key="d"
          class="dir-picker-recommended-item"
          :title="d"
          @click="applyRecommendedDir(d)"
        >
          <n-icon size="16"><FolderOpenOutline /></n-icon>
          <span class="dir-picker-recommended-path">{{ d }}</span>
          <n-icon size="14" class="dir-picker-recommended-open" :title="t('chat.workDirOpen')" @click.stop="openWorkDirInExplorer(d)">
            <OpenOutline />
          </n-icon>
        </div>
      </div>

      <!-- 当前路径 + 新建文件夹（返回上级由目录列表中的 .. 条目承担） -->
      <div class="dir-picker-breadcrumb">
        <n-text class="dir-picker-current" :title="dirCurrentPath">{{ dirCurrentPath }}</n-text>
        <n-button size="tiny" quaternary @click="startNewFolder" :title="t('chat.newFolder')">
          <template #icon><n-icon><AddOutline /></n-icon></template>
        </n-button>
      </div>

      <!-- 说清这次选择的分量：它只在会话发出第一条消息前有效，选完即固定。
           不写这句的话，用户会以为以后还能改，于是随手选一个。 -->
      <div class="dir-picker-hint">
        <n-icon size="13"><InformationCircleOutline /></n-icon>
        <span>{{ t('chat.newDirFixedHint') }}</span>
      </div>

      <!-- New folder input -->
      <div v-if="showNewFolderInput" class="dir-picker-new-folder">
        <n-input
          v-model:value="newFolderName"
          size="small"
          :placeholder="t('chat.newFolder')"
          @keyup.enter="createNewFolder"
          @blur="cancelNewFolder"
          ref="newFolderInputRef"
        />
      </div>

      <!-- Directory list -->
      <div class="dir-picker-list">
        <div v-if="dirLoading" class="dir-picker-loading">
          <n-spin size="small" />
        </div>
        <div v-else-if="dirEntries.length === 0" class="dir-picker-empty">
          <n-text depth="3">{{ t('chat.workDirEmpty') }}</n-text>
        </div>
        <div
          v-for="entry in dirEntries"
          v-else
          :key="entry.path"
          class="dir-picker-item"
          @click="navigateDir(entry.path)"
        >
          <n-icon size="16"><FolderOutline /></n-icon>
          <span>{{ entry.name }}</span>
        </div>
      </div>

      <template #footer>
        <n-space justify="end">
          <n-button @click="showDirPicker = false">{{ t('common.cancel') }}</n-button>
          <n-button type="primary" @click="handleWorkDirMenu('set')" :disabled="!chatStore.activeSessionId || !dirCurrentPath">
            {{ t('chat.workDirSet') }}
          </n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- 重命名会话：弹窗形式，替代侧栏里的就地编辑 -->
    <n-modal
      v-model:show="showRenameModal"
      preset="card"
      class="session-rename-modal"
      :title="t('chat.renameSession')"
      :bordered="false"
      :mask-closable="!renaming"
      :close-on-esc="!renaming"
      @after-leave="renamingSessionId = null"
    >
      <n-input
        ref="renameInputRef"
        v-model:value="renameInput"
        :placeholder="t('chat.renamePlaceholder')"
        :maxlength="200"
        show-count
        clearable
        @keydown.enter.prevent="saveRename"
      />
      <template #footer>
        <n-space justify="end">
          <n-button :disabled="renaming" @click="closeRenameModal">{{ t('common.cancel') }}</n-button>
          <n-button
            type="primary"
            :loading="renaming"
            :disabled="!renameTrimmed || renameUnchanged"
            @click="saveRename"
          >
            {{ t('common.save') }}
          </n-button>
        </n-space>
      </template>
    </n-modal>

  </div>
</template>

<script setup lang="ts">
import { ref, computed, h, onMounted, onUnmounted, onActivated, onDeactivated, nextTick, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useMessage, useDialog, NIcon } from 'naive-ui'
import { NAlert, NButton, NDropdown, NInput, NModal, NPopover, NProgress, NSelect, NSpace, NSpin, NSwitch, NTag, NText, NTooltip, NUpload } from 'naive-ui'
import { marked } from 'marked'
import { stripZeroWidth } from '@/utils/text'
import {
  allWebSessions,
  fullSessionsLoading,
  fullSessionsFailed,
  fullSessionsFetchedAt,
} from '@/utils/sessionCache'
import hljs from 'highlight.js/lib/core'
import javascript from 'highlight.js/lib/languages/javascript'
import typescript from 'highlight.js/lib/languages/typescript'
import python from 'highlight.js/lib/languages/python'
import go from 'highlight.js/lib/languages/go'
import bash from 'highlight.js/lib/languages/bash'
import json from 'highlight.js/lib/languages/json'
import xml from 'highlight.js/lib/languages/xml'
import css from 'highlight.js/lib/languages/css'
import markdown from 'highlight.js/lib/languages/markdown'
import 'highlight.js/styles/github-dark.css'

hljs.registerLanguage('javascript', javascript)
hljs.registerLanguage('typescript', typescript)
hljs.registerLanguage('python', python)
hljs.registerLanguage('go', go)
hljs.registerLanguage('bash', bash)
hljs.registerLanguage('json', json)
hljs.registerLanguage('xml', xml)
hljs.registerLanguage('css', css)
hljs.registerLanguage('markdown', markdown)
import { useChatStore, type ToolCallEvent } from '@/stores/chat'
import { useGoalsStore } from '@/stores/goals'
import { useModelsStore } from '@/stores/models'
import { useConfigStore } from '@/stores/config'
import RightSidebar from '@/components/RightSidebar.vue'
import TaskTimeline from '@/components/TaskTimeline.vue'
import ChatApprovalCard from '@/components/ChatApprovalCard.vue'
import ChatClarificationCard from '@/components/ChatClarificationCard.vue'
import FileChangesBlock from '@/components/FileChangesBlock.vue'
import TimelineMessage from '@/components/TimelineMessage.vue'
import ComposerEditor from '@/components/ComposerEditor.vue'
import FilePreviewDialog from '@/components/FilePreviewDialog.vue'
import type { TimelineStep } from '@/components/TaskTimeline.vue'
import { toolCallSummary, toolShortName } from '@/utils/toolCallView'
import { SendOutline, StopCircleOutline, FlashOutline, DocumentOutline, ImageOutline, FlagOutline, GridOutline, FolderOpenOutline, FolderOutline, AddOutline, SearchOutline, OpenOutline, PersonOutline, ChevronDownOutline, ChevronForwardOutline, ArrowBackOutline, ArrowDownOutline, EllipsisHorizontalOutline, PencilOutline, TrashOutline, ChatbubbleOutline, ShieldCheckmarkOutline, ExtensionPuzzleOutline, LinkOutline, UnlinkOutline, SettingsOutline, InformationCircleOutline, TerminalOutline } from '@vicons/ionicons5'
import type { DropdownOption, UploadCustomRequestOptions } from 'naive-ui'
import * as sessionsApi from '@/api/sessions'
import * as approvalApi from '@/api/approval'
import * as skillsApi from '@/api/skills'
import { useSkillsStore } from '@/stores/skills'
import { useAgentPluginsStore } from '@/stores/agentplugins'
import type { AgentPlugin, AgentPluginBlocker } from '@/api/agentplugins'
import { useRouter, onBeforeRouteLeave } from 'vue-router'

const { t } = useI18n()
const chatStore = useChatStore()
const goalsStore = useGoalsStore()
const modelsStore = useModelsStore()
const configStore = useConfigStore()
const skillsStore = useSkillsStore()
const agentPluginsStore = useAgentPluginsStore()
const router = useRouter()
const message = useMessage()
const dialogStore = useDialog()

// naive-ui 的 useDialog().warning() 返回的是 DialogReactive 对象（不是 Promise，
// 也没有 then），`await` 它只会立刻拿到一个真值对象 —— 于是
// `if (await dialog.warning(...))` 这种写法根本不等用户点按钮：
// 弹窗刚弹出来，删除就已经执行完了（曾致「还没确认，会话已经被删掉」）。
// 破坏性操作必须由用户点击驱动，这里把 onPositiveClick/onNegativeClick/onClose
// 桥接成 Promise<boolean>，只有点「确认」才返回 true。
function confirmDialog(options: {
  title: string
  content: string
  positiveText: string
  negativeText: string
}): Promise<boolean> {
  return new Promise<boolean>((resolve) => {
    let settled = false
    const finish = (v: boolean) => {
      if (!settled) {
        settled = true
        resolve(v)
      }
    }
    dialogStore.warning({
      ...options,
      onPositiveClick: () => finish(true),
      onNegativeClick: () => finish(false),
      onClose: () => finish(false),
      onMaskClick: () => finish(false),
      onEsc: () => finish(false),
      // 兜底：任何关闭路径都要落定，避免 await 永远挂住
      onAfterLeave: () => finish(false),
    })
  })
}
const inputValue = ref('')
// ComposerEditor 暴露的能力：聚焦、按 DOM 顺序取"指令行 + 正文"、清空。
const composerRef = ref<{ focus: () => void; getPayload: () => string; clear: () => void } | null>(null)
const composerPlaceholder = computed(() =>
  chatStore.isCommand(inputValue.value) ? t('chat.commandPlaceholder') : t('chat.placeholder'),
)
const rightSidebarMobileVisible = ref(false)
const mobileSessionExpanded = ref(false)
const sidebarSearchInput = ref<{ focus: () => void } | null>(null)
const isMobile = ref(window.innerWidth <= 768)

// 移动端顶部工具条的标题：当前会话名；尚未选中会话时给出引导文案
const mobileTopbarTitle = computed(() => chatStore.activeSession?.title || t('chat.selectSession'))

// 顶栏搜索按钮：展开会话抽屉并聚焦搜索框。
// v-show 从 display:none 复显后必须等 nextTick 才能聚焦，同步调用会被 display:none 吞掉。
function expandAndFocusSearch() {
  mobileSessionExpanded.value = true
  nextTick(() => sidebarSearchInput.value?.focus())
}

function handleResize() {
  isMobile.value = window.innerWidth <= 768
}

onMounted(() => {
  window.addEventListener('resize', handleResize)
})

onUnmounted(() => {
  window.removeEventListener('resize', handleResize)
})
const messagesRef = ref<HTMLDivElement>()
const sessionListRef = ref<HTMLDivElement>()

// ========== 「回到对话底部」悬浮按钮 ==========
// 之前只要有新消息/新工具调用就无条件 scrollToBottom，用户上翻看历史时会被
// 反复拽回底部。这里改成"贴底跟随"：只有用户本身就在底部时才自动跟随；一旦
// 上滚，就露出悬浮按钮 + 未读计数，由用户自己决定何时回去。
const stickBottom = ref(true)       // 用户是否贴着底部（决定要不要自动跟随）
const showJumpBottom = ref(false)   // 悬浮按钮显隐
const unreadBelow = ref(0)          // 上滚期间新到达的消息条数
const STICK_THRESHOLD = 80          // 距底 ≤ 此值即视为贴底（吸收亚像素/平滑滚动残差）
const JUMP_BTN_THRESHOLD = 200      // 距底 > 此值才显示按钮，避免贴底时按钮闪烁
const TOOL_FOLLOW_THRESHOLD = 480   // 新工具调用到达时距底 ≤ 此值即自动贴底跟随（正在看底部工具区）
let jumpScrolling = false           // 程序触发的平滑滚动进行中（scroll 事件不算用户上滚）
let jumpScrollTimer: ReturnType<typeof setTimeout> | null = null
let msgScrollRaf = 0
let followRaf = 0                   // 贴底跟随的合并帧
// 最近一次"程序自身"滚动的时间戳。程序滚动同样会派发 scroll 事件，而流式内容
// 在这个过程中还在长高，于是事件里量到的离底距离可能已经超过阈值——若不豁免，
// 就会被误判成"用户上滚"从而关掉跟随，表现为跟随到一半突然卡住不再贴底。
let programScrollAt = 0
const PROGRAM_SCROLL_GRACE = 200    // 程序滚动后这段时间内的 scroll 事件不判用户意图

function messagesBottomDistance(): number {
  const el = messagesRef.value
  if (!el) return 0
  return el.scrollHeight - el.scrollTop - el.clientHeight
}

// 贴底跟随：把视口对齐到内容底部。必须用即时滚动（直接写 scrollTop）而不是
// smooth——smooth 每次调用都会重启一段动画，在持续增高的内容上永远追不上，
// 观感就是"内容都写了几秒才滑到底"。
function followBottom() {
  if (!stickBottom.value) return
  if (followRaf) return
  followRaf = requestAnimationFrame(() => {
    followRaf = 0
    if (!stickBottom.value) return
    const el = messagesRef.value
    if (!el) return
    if (messagesBottomDistance() <= 0) return  // 已经贴底，别再写 scrollTop
    programScrollAt = performance.now()
    el.scrollTop = el.scrollHeight
  })
}

// 用户主动离开底部：停止跟随、露出悬浮按钮
function detachFollow() {
  if (!stickBottom.value) return
  stickBottom.value = false
  showJumpBottom.value = messagesBottomDistance() >= JUMP_BTN_THRESHOLD
}

function handleMessagesScroll() {
  // 滚动事件很密集，用 rAF 合并到一帧一次（读的是帧内最新的 scrollTop）
  if (msgScrollRaf) return
  msgScrollRaf = requestAnimationFrame(() => {
    msgScrollRaf = 0
    const dist = messagesBottomDistance()
    if (dist <= STICK_THRESHOLD) {
      stickBottom.value = true
      showJumpBottom.value = false
      unreadBelow.value = 0
      endJumpScrolling()
      return
    }
    if (jumpScrolling) return
    // 程序滚动刚把视口推到底、内容又长了高：这是流式输出，不是用户想离开底部
    if (performance.now() - programScrollAt < PROGRAM_SCROLL_GRACE) return
    stickBottom.value = false
    showJumpBottom.value = dist >= JUMP_BTN_THRESHOLD
  })
}

// 用户主动滚动/触摸时立刻交还控制权：平滑滚动途中用户反悔要能中断
function cancelJumpScrolling() {
  if (!jumpScrolling) return
  endJumpScrolling()
}

// 滚轮：向上滚 = 明确要看历史，立即放开跟随（不能等 scroll 事件，那会被上面
// 那个程序滚动时间窗吃掉一拍）
function handleMessagesWheel(e: WheelEvent) {
  cancelJumpScrolling()
  if (e.deltaY < 0) detachFollow()
}

// 触摸：手指下移 = 内容上移 = 看历史，同样立即放开跟随
let touchStartY = 0
function handleMessagesTouchStart(e: TouchEvent) {
  cancelJumpScrolling()
  touchStartY = e.touches[0]?.clientY ?? 0
}

function handleMessagesTouchMove(e: TouchEvent) {
  const y = e.touches[0]?.clientY ?? 0
  if (y - touchStartY > 8) detachFollow()
}

onUnmounted(() => {
  if (followRaf) { cancelAnimationFrame(followRaf); followRaf = 0 }
  if (msgScrollRaf) { cancelAnimationFrame(msgScrollRaf); msgScrollRaf = 0 }
  if (jumpScrollTimer) { clearTimeout(jumpScrollTimer); jumpScrollTimer = null }
})

// ===== keep-alive 下的滚动位置保存与还原 =====
// 实例被停用时，Vue 会把整棵 DOM 移入一个游离的 storage container。游离元素没有
// 滚动盒——scrollTop 读回来是 0，重新插回主文档时也是 0（实测：侧栏滚到 150，回来变 0）。
// 也就是说浏览器不会替我们保住这两个容器的阅读位置，必须显式存取。
// 消息区还要一并记住"是否贴底"：离开时贴着底部（流式跟随的常态）就重新落到最新处；
// 否则精确回到原来那一屏，只把期间新增的消息计成未读，不打扰正在看历史的用户。
let savedSessionListScrollTop = 0
let savedMessagesScrollTop = 0
let savedStickBottom = true
let deactivatedMsgCount = 0
let hasSavedScrollState = false

// 关键时序：**不能在 onDeactivated 里读 scrollTop**。
// Vue 的 KeepAlive.deactivate 是先把 DOM 移进游离的 storage container，再在
// post-render 里调 onDeactivated（见 runtime-core 的 sharedContext.deactivate）。
// 那时元素已经没有滚动盒，scrollTop 读回来恒为 0——于是"保存"下来的是 0，
// 回来自然还原成 0。必须在离开路由的守卫里取，那一刻 DOM 还在文档中、位置是真的。
onBeforeRouteLeave(() => {
  const list = sessionListRef.value
  if (list) savedSessionListScrollTop = list.scrollTop
  const el = messagesRef.value
  if (el) savedMessagesScrollTop = el.scrollTop
  savedStickBottom = stickBottom.value
  deactivatedMsgCount = chatStore.messages.length
  hasSavedScrollState = true
})

onDeactivated(() => {
  // 兜底（覆盖"非路由来源的停用"）：此时 DOM 已被移走、位置已读不到，只能保住
  // 贴底意图与消息条数；位置由上面的守卫负责，且这里**不能**覆写它。
  savedStickBottom = stickBottom.value
  deactivatedMsgCount = chatStore.messages.length
  hasSavedScrollState = true
})

onActivated(() => {
  // 首次挂载也会触发 activated，此时没有可还原的状态，交给原有的首屏逻辑
  if (!hasSavedScrollState) return
  // 必须等 DOM 重新插回主文档、布局可用后再写：游离状态下写 scrollTop 是空操作
  nextTick(() => {
    // 侧栏：直接回到原来那一屏。列表内容来自缓存，激活时已在 DOM 里，可以立即写。
    const list = sessionListRef.value
    if (list) list.scrollTop = savedSessionListScrollTop

    const el = messagesRef.value
    if (!el) return
    if (savedStickBottom) {
      stickBottom.value = true
      showJumpBottom.value = false
      unreadBelow.value = 0
      el.scrollTop = el.scrollHeight
      return
    }
    el.scrollTop = savedMessagesScrollTop
    // 离开期间新消息仍会落库（SSE 不随路由中断），但用户在看历史——保持不打扰：
    // 位置不动，只把新增条数补进未读角标，由用户决定何时回到底部
    const arrived = chatStore.messages.length - deactivatedMsgCount
    stickBottom.value = false
    unreadBelow.value = arrived > 0 ? arrived : 0
    showJumpBottom.value = unreadBelow.value > 0
  })
})

function endJumpScrolling() {
  jumpScrolling = false
  if (jumpScrollTimer) {
    clearTimeout(jumpScrollTimer)
    jumpScrollTimer = null
  }
}

const jumpTitle = computed(() => (
  unreadBelow.value > 0
    ? t('chat.jumpToBottomUnread', { count: unreadBelow.value })
    : t('chat.jumpToBottom')
))

// 回合状态行显示的已执行秒数。唯一来源是 store 里的**会话级起点**
// （activeTurnStartedAt）：它由 stream_started 随事件下发的 active_started_at
// 写入，也会被 /running、queue_changed 的对账校正，属于会话状态而非组件状态，
// 所以切换会话、组件被 keep-alive 停用都不会把时长清零。
//
// 这里曾经另起一个"组件本地、从 streaming 起算"的计数器做兜底。它的问题
// 恰恰在兜底被高频触发的场景：本地计数器以 streaming false→true 为重置条件，
// 而切换会话必然经历这两跳，于是"切走再切回来"执行时间从头开始计。
const turnElapsedForDisplay = computed(() => chatStore.activeTurnElapsedSeconds)

// Model selection
const modelOptions = computed(() => modelsStore.modelSelectOptions)
const currentModelId = ref(modelsStore.currentModelInfo?.id || '')
const modelSelectRef = ref<{ $el?: HTMLElement } | null>(null)
// 菜单宽度上限按触发器位置动态计算（inline style 直通 naive 菜单，优先级最高）：
// naive follower 放不下时会 flip 到 bottom-end（右缘对齐触发器），两种对齐方式能容纳的
// 菜单宽分别是 (vw - 触发器左缘) 和 (触发器右缘)，取较大者留 12px 边距即绝对安全——
// 无论触发器在屏幕什么位置、视口多窄，naive 总能选到放得下的一种，不再出现左侧被裁。
// 静态 CSS 钳制做不到这一点：可用空间取决于触发器位置，是动态值。
const modelMenuMaxWidth = ref('660px')
// 隐藏的上传入口（由工具下拉菜单"上传文件"触发），通过 openOpenFileDialog 打开系统文件选择器。
const toolbarUploadRef = ref<{ openOpenFileDialog: () => void } | null>(null)

function onModelMenuShow(show: boolean) {
  if (!show) return
  const host = modelSelectRef.value?.$el as HTMLElement | undefined
  const vw = window.innerWidth
  if (!host || !vw) {
    modelMenuMaxWidth.value = '660px'
    return
  }
  const r = host.getBoundingClientRect()
  const margin = 12
  const byEnd = r.right - margin      // bottom-end 对齐：菜单右缘=触发器右缘
  const byStart = vw - r.left - margin // bottom-start 对齐：菜单左缘=触发器左缘
  const usable = Math.max(byEnd, byStart, 160)
  modelMenuMaxWidth.value = `${Math.floor(Math.min(usable, 660))}px`
}

// Sync currentModelId when store changes
watch(() => modelsStore.currentModelInfo, (info) => {
  if (info?.id) {
    currentModelId.value = info.id
  }
}, { immediate: true })

function handleModelChange(value: string) {
  const [provider, ...rest] = value.split('/')
  const model = rest.join('/')
  if (provider && model) {
    modelsStore.setModel(model, provider.trim())
  }
}

function renderModelLabel(option: { label: string; value: string }) {
  const parts = option.label.split(' / ')
  const provider = parts[0] || ''
  const model = parts[1] || ''
  return h('div', {
    class: 'model-switch-option',
    // 内联 max-width 是最后防线：部分移动端 webview 连 class 级 CSS 都可能没加载时，
    // 也能把最宽选项的本征宽度钳在屏幕内（配合 flex + overflow-wrap 让长名折行）。
    style: 'display: flex; align-items: center; gap: 6px; padding: 2px 0; min-width: 0; max-width: calc(100vw - 56px);'
  }, [
    h('span', { class: 'msw-provider', style: 'flex-shrink: 0; color: var(--n-option-text-color-disabled, #999); font-size: .92em;' }, provider),
    h('span', { class: 'msw-model', style: 'min-width: 0; overflow-wrap: anywhere;' }, `/ ${model}`),
  ])
}

// 快速设置审批策略：聊天页直接切换，无需进入审批管理页。
const approvalStrategy = ref<string>('smart')
// 服务端已生效的那一份策略。为什么不能直接用 approvalStrategy 做"值没变就
// 跳过"的判断：`v-model:value` 与 `@update:value` 同时存在时 Vue 会把两者合并
// 成数组且**赋值在前**（编译产物：`onUpdate:value: [$event => x = $event, handler]`），
// 于是处理函数里 `value === approvalStrategy.value` 恒为真——曾经这行让保存
// 整段被 return 掉，表现为"切了策略看着变了、实际没保存、刷新又回去"。
// 已保存值必须独立于绑定值记一份。
const appliedStrategy = ref<string>('smart')
const approvalLoading = ref(false)
const approvalStrategyOptions = computed(() => [
  { label: t('approval.settings.strategies.manual'), value: 'manual' },
  { label: t('approval.settings.strategies.auto'), value: 'auto' },
  { label: t('approval.settings.strategies.smart'), value: 'smart' },
  { label: t('approval.settings.strategies.whitelist'), value: 'whitelist' },
])

// 底部工具栏使用的紧凑版审批策略标签：仅显示短标题（如“智能审批”），避免文案过长。
// 注意：naive-ui 的 render-label 同时作用于触发框和下拉菜单选项，同一份内联截断样式
// 会把菜单选项也压成"手…"半个字。触发框的截断必须保留（内联写法，h() 渲染的节点
// 可能带不上 scoped 的 data-v 属性，scoped CSS 不可靠）；菜单侧由全局样式块
// .approval-select-menu 用 !important 覆盖放开（!important 优先级高于内联样式）。
function renderApprovalLabelShort(option: { label: string; value: string }) {
  const title = (option.label.split(' - ')[0] || option.label).trim()
  return h('div', {
    class: 'approval-short-label',
    style: 'display: flex; align-items: center; gap: 4px; min-width: 0;',
  }, [
    h(NIcon, { component: ShieldCheckmarkOutline, size: 12, style: 'flex-shrink: 0;' }),
    h('span', { class: 'approval-short-label__text', style: 'min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;' }, title),
  ])
}

// 加载当前审批策略
async function loadApprovalStrategy() {
  approvalLoading.value = true
  try {
    const settings = await approvalApi.getSettings()
    const s = settings.strategy || 'smart'
    approvalStrategy.value = s
    appliedStrategy.value = s
  } catch (e) {
    console.error('load approval strategy failed', e)
  } finally {
    approvalLoading.value = false
  }
}

// 切换审批策略：仅更新 strategy，其余设置保持不变
async function handleApprovalStrategyChange(value: string) {
  // 与"服务端已生效值"比较（不能与 approvalStrategy 比，原因见其声明处注释）
  if (!value || value === appliedStrategy.value) return
  const prev = appliedStrategy.value
  try {
    const settings = await approvalApi.getSettings()
    settings.strategy = value
    await approvalApi.saveSettings(settings)
    appliedStrategy.value = value
    approvalStrategy.value = value
    const label = approvalStrategyOptions.value.find(o => o.value === value)?.label.split(' - ')[0] || value
    message.success(t('chat.quickApprovalSaved', { strategy: label }))
  } catch (e: any) {
    message.error(t('chat.quickApprovalSaveFailed'))
    // 保存失败：把选择框拉回服务端实际的值
    approvalStrategy.value = prev
  }
}

onMounted(() => {
  loadApprovalStrategy()
})

// Current agent phase
const agentPhase = computed(() => {
  if (!chatStore.streaming) return ''
  const runningTools = chatStore.activeToolCalls
  if (runningTools.length > 0) {
    return t('chat.executingTool', { name: runningTools[0].name })
  }
  if (chatStore.streamContent) {
    return t('chat.generating')
  }
  if (chatStore.toolCalls.length > 0) {
    return t('chat.generating')
  }
  return t('chat.thinkingPhase')
})

// 单条消息对应的工具调用列表：兼容两种数据源——
// - 历史消息：msg.tool_calls_snapshot（前端在流式结束时写入内存快照）
// - 流式进行中：chatStore.toolCalls（实时更新）
function messageToolCalls(msg?: sessionsApi.Message): ToolCallEvent[] {
  if (msg) {
    const snap = (msg.tool_calls_snapshot ?? []) as ToolCallEvent[]
    return snap
  }
  return chatStore.toolCalls
}

// ===== 变更的文件（数据汇聚；渲染见 FileChangesBlock.vue） =====
// 仅把"写/删/批"视为变更动作；read/list/search/access 不进入列表。
const FILE_CHANGED_ACTIONS = new Set(['write', 'delete', 'batch'])

// 本轮全部 file_ops：
// - 历史消息：优先后端落库返回的 msg.file_ops（含 diff，刷新后仍可用），
//   内存态消息退化为从 tool_calls_snapshot 中聚合（流式结束写入的内存快照）；
// - 流式进行中：直接聚合 chatStore.toolCalls。
function collectTurnFileOps(msg?: sessionsApi.Message): sessionsApi.FileOp[] {
  if (msg) {
    if (msg.file_ops && msg.file_ops.length > 0) return msg.file_ops
    const snap = (msg.tool_calls_snapshot ?? []) as ToolCallEvent[]
    return snap.flatMap(tc => tc.file_ops || [])
  }
  return chatStore.toolCalls.flatMap(tc => tc.file_ops || [])
}

// 变更的文件：按路径去重。同路径出现多次时以后一次为准
// （写→删=删；删→写=重建=写）；携带 diff 的条目原样透传给渲染组件。
// 排队消息的附件清单。store 里用 turnId 索引（local_* 期间也能查到），
// 用于在排队区展示"这条消息带了哪些文件"。
// 类型是 Partial：刷新后从服务端队列快照恢复的附件只有 name/url/mime，
// 没有 id/size（uploads 元数据不在队列里）。
function queuedFilesFor(turnId: string): Partial<sessionsApi.UploadedFile>[] {
  return chatStore.queuedAttachments.get(turnId) || []
}

// "当前回合已执行时长"的展示格式：秒级实时（"X 秒"/"X 分 Y 秒"），
// 1 小时以上折算成"X 小时 Y 分钟"。服务端回合超时上限最长 24h，
// 长任务跨小时并不罕见。
function formatTurnElapsed(totalSeconds: number): string {
  const minutes = Math.floor(totalSeconds / 60)
  const seconds = totalSeconds % 60
  if (minutes >= 60) {
    const hours = Math.floor(minutes / 60)
    return t('chat.turnElapsedHoursMinutes', { hours, minutes: minutes % 60 })
  }
  if (minutes > 0) return t('chat.turnElapsedMinutesSeconds', { minutes, seconds })
  return t('chat.turnElapsedSeconds', { seconds })
}

// 历史回放：assistant 消息携带的本回合执行时长（秒）。毫秒按秒取整，
// 不足 1 秒的回合按 1 秒显示（"0 秒"反而像出了错）。0 = 老数据无该字段。
function messageElapsedSeconds(msg: sessionsApi.Message): number {
  const ms = Number(msg.turn_elapsed_ms || 0)
  return ms > 0 ? Math.max(1, Math.round(ms / 1000)) : 0
}

// 删除一条排队消息。只动这一条，正在执行的回合继续跑（那是停止键的职责）。
async function removeQueued(turnId: string) {
  const sessionId = chatStore.activeSessionId
  if (!sessionId) return
  await chatStore.removeQueuedMessage(sessionId, turnId)
}

// 编辑一条排队消息：把它从队列撤下，内容与附件回填到输入框，用户改完再发。
// 若该条已经开始执行则撤不下来（服务端返回 null），此时提示改用停止。
async function editQueued(turnId: string) {
  const sessionId = chatStore.activeSessionId
  if (!sessionId) return
  const picked = await chatStore.editQueuedMessage(sessionId, turnId)
  if (!picked) {
    message.warning(t('chat.queuedEditTooLate'))
    return
  }
  // 回填时把 `/skill x`、`/mcp srv tool` 前缀还原成行内胶囊，正文回到输入框，
  // 而不是让用户对着一段指令文本改。
  // 附件占位标记（[[att:n]]）一并摘掉：编辑器暂不支持把胶囊插回原位置，
  // 附件统一回到胶囊列表，重发时按新位置重新生成标记。
  const parsed = parseDirectivesFromContent(queueContentPreview(picked.content))
  inputValue.value = parsed.text
  pendingDirectives.value = parsed.chips
  if (picked.attachments.length) {
    selectedFiles.value = [...picked.attachments] as sessionsApi.UploadedFile[]
    // 回填的附件没有 _uid：排队消息的 attachments 是上传响应的克隆。
    // 没有它胶囊 id 会退化成"按文件名"，同名文件会互相顶掉，先补齐。
    for (const f of selectedFiles.value) {
      if (!(f as { _uid?: string })._uid) (f as { _uid?: string })._uid = nextFileUid()
    }
  }
  nextTick(() => {
    composerRef.value?.focus()
  })
}

// ---- 排队区拖动排序 ----
// 用 HTML5 原生 drag 事件而不是自造指针逻辑：桌面端浏览器已经处理好了
// 拖动阈值、拖动时的手指/光标形态、以及跨元素拖放的语义，自造一套只会
// 在触屏与移动端边界上反复踩坑。触屏排序后续如需支持再补指针事件即可。
const draggingTurnId = ref('')
const draggingFromIndex = ref(-1)

function onQueueDragStart(e: DragEvent, turnId: string, index: number) {
  draggingTurnId.value = turnId
  draggingFromIndex.value = index
  if (e.dataTransfer) {
    // 必须 setData 才会被浏览器认定为"有效拖动"（否则部分浏览器直接取消）。
    e.dataTransfer.effectAllowed = 'move'
    e.dataTransfer.setData('text/plain', turnId)
  }
}

// dragover 的目标行：把被拖行**插入**到这一行的位置。
// 这里不做实时预览换位（那需要为每一帧重排列表，长队列下会明显掉帧），
// 只在落下时执行一次移动——拖动过程中被拖行半透明，落点一目了然。
function onQueueDragOver(_e: DragEvent, _index: number) {
  // 占位：dragover 必须被处理（模板里已 preventDefault）才能收到 drop，
  // 但落点的计算推迟到 drop 时做，避免拖动过程中的高频重排。
}

async function onQueueDrop(_e: DragEvent, index: number) {
  const turnId = draggingTurnId.value
  const from = draggingFromIndex.value
  draggingTurnId.value = ''
  draggingFromIndex.value = -1

  const sessionId = chatStore.activeSessionId
  if (!sessionId || !turnId || from < 0 || from === index) return

  // 从上往下拖时，被拖行原本占着 from，摘除后目标位置会后移一位。
  const to = from < index ? index - 1 : index
  await chatStore.moveQueuedMessage(sessionId, turnId, to)
}

function onQueueDragEnd() {
  draggingTurnId.value = ''
  draggingFromIndex.value = -1
}

function changedFiles(msg?: sessionsApi.Message): { action: string; path: string; diff?: string }[] {
  const entries: { action: string; path: string; diff?: string }[] = []
  const index = new Map<string, number>()
  for (const op of collectTurnFileOps(msg)) {
    if (!op || !op.path) continue
    if (!FILE_CHANGED_ACTIONS.has(op.action)) continue
    const existing = index.get(op.path)
    const entry = { action: op.action, path: op.path, diff: op.diff }
    if (existing === undefined) {
      index.set(op.path, entries.length)
      entries.push(entry)
    } else {
      entries[existing] = entry
    }
  }
  return entries
}

// Rotating hints during thinking
const thinkingHints = [
  'chat.hintAnalyzing',
  'chat.hintPlanning',
  'chat.hintReasoning',
]
const hintIndex = ref(0)
let hintTimer: ReturnType<typeof setInterval> | null = null

watch(() => chatStore.streaming, (streaming) => {
  if (streaming && !chatStore.streamContent && chatStore.activeToolCalls.length === 0) {
    hintIndex.value = 0
    hintTimer = setInterval(() => {
      hintIndex.value = (hintIndex.value + 1) % thinkingHints.length
    }, 4000)
  } else {
    if (hintTimer) {
      clearInterval(hintTimer)
      hintTimer = null
    }
  }
})

watch(() => chatStore.streamContent, (val) => {
  if (val && hintTimer) {
    clearInterval(hintTimer)
    hintTimer = null
  }
})

onUnmounted(() => {
  if (hintTimer) clearInterval(hintTimer)
})

// Rename session：用弹窗改名（不再在侧栏条目里就地编辑）。
// 就地编辑在小尺寸侧栏里可用空间太窄，长标题会被裁掉；弹窗还能明确区分
// 「保存 / 取消」，避免 @blur 自动保存导致的误改。
const showRenameModal = ref(false)
const renamingSessionId = ref<string | null>(null)
const renameInput = ref('')
const renaming = ref(false)
const renameInputRef = ref<{ focus: () => void } | null>(null)
const renameTrimmed = computed(() => renameInput.value.trim())
const renameUnchanged = computed(() => {
  const id = renamingSessionId.value
  if (!id) return true
  const session = chatStore.sessions.find(s => s.id === id)
    ?? allWebSessions.value?.find(s => s.id === id)
  return renameTrimmed.value === (session?.title || '')
})

// File upload
const selectedFiles = ref<sessionsApi.UploadedFile[]>([])
const uploadingFiles = ref<Set<string>>(new Set())

// hasComposerContent 表示输入区"有东西可发"（文字或附件）。
// 底部按钮据此在发送键与停止键之间取舍：只要用户已经开始输入，就必须让发送键
// 可见——busy（流式/排队）期间把它换成停止键会让打好的字发不出去。
const hasComposerContent = computed(
  () => !!inputValue.value.trim() || selectedFiles.value.length > 0 || pendingDirectives.value.length > 0,
)

function formatFileSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return ''
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`
}

// Work directory
const showDirPicker = ref(false)
const dirCurrentPath = ref('')
const dirEntries = ref<sessionsApi.DirEntry[]>([])
const dirLoading = ref(false)
const showNewFolderInput = ref(false)
const newFolderName = ref('')
const workDirHistory = ref<string[]>([])

// 配置页「通用」中设置的全局工作目录（后端 cfg.WorkingDir）。会话没有显式设置工作
// 目录时，后端会把会话目录自动建在 <全局工作目录>/chat/Chat_<时间>-<短ID> 下。
const globalWorkDir = computed(() => configStore.config?.working_dir || '')

// 配置按需拉取（首次打开选择器时），避免聊天页每次进入都多一个请求。
async function ensureConfigLoaded(): Promise<void> {
  if (configStore.config) return
  try {
    await configStore.loadConfig()
  } catch (e) {
    console.error('Failed to load config for work dir picker:', e)
  }
}

// 统一路径分隔符并忽略 Windows 盘符大小写，用于排除当前会话已设置的工作目录
function normalizeDirPath(p: string): string {
  let s = (p || '').trim().replace(/[\\/]+/g, '\\').replace(/[\\]+$/, '')
  if (/^[A-Za-z]:/.test(s)) s = s.toLowerCase()
  return s
}

// 已使用过的目录作为推荐项；排除当前会话已设置的工作目录
const recommendedDirs = computed(() => {
  const current = normalizeDirPath(chatStore.currentWorkDir || '')
  return workDirHistory.value.filter(d => normalizeDirPath(d) !== current)
})
const newFolderInputRef = ref<{ focus: () => void } | null>(null)

// Session goals cache
const sessionGoals = ref<Record<string, sessionsApi.SessionGoal[]>>({})

// Popover show state
const popoverShow = ref<Record<string, boolean>>({})

// Custom code renderer for highlight.js
const codeRenderer = (code: string, lang?: string): string => {
  const language = lang && hljs.getLanguage(lang) ? lang : null
  const highlighted = language
    ? hljs.highlight(code, { language }).value
    : hljs.highlightAuto(code).value
  // 移除 inline onclick，改用 class + 事件委托（见 handleCodeBlockClick）
  const copyBtn = `<button class="code-copy-btn" type="button">Copy</button>`
  return `<div class="code-block">${copyBtn}<pre><code class="hljs${language ? ` language-${language}` : ''}">${highlighted}</code></pre></div>`
}

// ReasoningContent 已改用独立 Marked 实例，不再往全局单例上叠加配置；
// 这里显式声明 breaks/gfm，保持正文渲染行为与之前（配置被叠加后）一致。
marked.use({ renderer: { code: codeRenderer }, breaks: true, gfm: true })

// 处理代码块按钮点击（事件委托替代 inline onclick，避免 v-html + inline handler XSS 风险）
function handleCodeBlockClick(e: MouseEvent) {
  const target = e.target as HTMLElement
  const btn = target.closest('.code-copy-btn') as HTMLElement | null
  if (!btn) return
  const codeEl = btn.parentElement?.querySelector('code')
  const code = codeEl?.textContent || ''
  navigator.clipboard.writeText(code).catch(() => { /* ignore */ })
  const original = btn.textContent
  btn.textContent = '✓ Copied'
  setTimeout(() => { btn.textContent = original }, 2000)
}

// 代码块复制按钮的点击委托。走 activated/deactivated 而不是 mounted/unmounted：
// /chat 被 keep-alive 缓存后，切到别的页面并不会卸载组件，onUnmounted 不会触发，
// 挂在 document 上的监听会跟着缓存实例一直留着，在别的页面上做无谓空转。
// onUnmounted 仍需兜底——缓存实例被真正销毁时（登出、keep-alive 父级销毁）只走 unmount，
// 不经过 deactivated。同一个函数引用重复 addEventListener 是幂等的，不会重复触发。
onActivated(() => {
  document.addEventListener('click', handleCodeBlockClick)
})

onDeactivated(() => {
  document.removeEventListener('click', handleCodeBlockClick)
})

onUnmounted(() => {
  document.removeEventListener('click', handleCodeBlockClick)
})

// Markdown 渲染缓存：避免 v-for 重渲染时重复解析同一内容
const mdCache = new Map<string, string>()
const MD_CACHE_LIMIT = 200

// 用户消息里的指令前缀 → 有序 token 列表（参考 WorkBuddy 的技能引用样式）：
// 胶囊和正文按消息里的原始顺序交替，渲染出来与输入框里看到的排版一致。
// 「胶囊A 正文A 胶囊B 正文B」不会再被压成「胶囊A 胶囊B 正文A 正文B」。
const USER_SKILL_LINE_RE = /^\/skills?\s+(\S+)(?:\s+([\s\S]*))?$/
const USER_MCP_LINE_RE = /^\/mcp\s+(\S+)(?:\s+(\S+))?(?:\s+([\s\S]*))?$/

interface UserDirectiveToken {
  kind: 'chip' | 'att' | 'text'
  /** kind === 'chip' 时的胶囊名字 */
  name?: string
  /** kind === 'chip' 时的分组（只用于样式） */
  group?: 'skills' | 'mcp'
  /** kind === 'text' 时的正文片段 */
  text?: string
  /** kind === 'att' 时的附件下标（msg.files 的索引，发送端按最终顺序写入） */
  attIndex?: number
}

// 附件占位标记：发送时由编辑器的文件胶囊原位置生成（ComposerEditor.getPayload
// → ChatView.send 改写下标）。渲染时把标记拆成独立 token，附件以行内胶囊的
// 形态回到文字流原位置，而不是整块堆在气泡顶部。
const USER_ATT_RE = /\[\[att:(\d+)\]\]/g

// 同一内容在 v-for 重渲染时会被模板调用多次，用和 mdCache 一样的思路
// 按内容缓存，避免每条消息每帧都重跑一遍解析。
const directiveRefCache = new Map<string, UserDirectiveToken[] | null>()

function userDirectiveRefs(content: string): UserDirectiveToken[] | null {
  const key = stripZeroWidth(content)
  if (directiveRefCache.has(key)) return directiveRefCache.get(key) ?? null

  const tokens: UserDirectiveToken[] = []
  let buf: string[] = []
  const flush = () => {
    if (!buf.length) return
    let text = buf.join('\n')
    buf = []
    // 指令行两侧的换行是"为了让服务端能按行解析"补的，不属于用户排版：
    // 去掉它们，气泡里才能像输入框那样把胶囊和文字排在同一行。
    if (tokens.length && tokens[tokens.length - 1].kind === 'chip') text = text.replace(/^\n+/, '')
    text = text.replace(/\n+$/, '')
    if (!text) return
    // 正文里的附件占位标记拆成独立 token（标记外的文字保持原样）。
    USER_ATT_RE.lastIndex = 0
    let last = 0
    let m: RegExpExecArray | null
    while ((m = USER_ATT_RE.exec(text)) !== null) {
      if (m.index > last) tokens.push({ kind: 'text', text: text.slice(last, m.index) })
      tokens.push({ kind: 'att', attIndex: Number(m[1]) })
      last = m.index + m[0].length
    }
    if (last < text.length) tokens.push({ kind: 'text', text: text.slice(last) })
  }

  for (const rawLine of key.split('\n')) {
    const line = rawLine.trim()
    if (!line) {
      buf.push('')
      continue
    }
    const skill = USER_SKILL_LINE_RE.exec(line)
    if (skill) {
      flush()
      tokens.push({ kind: 'chip', name: skill[1], group: 'skills' })
      if (skill[2]?.trim()) buf.push(skill[2].trim())
      continue
    }
    const mcp = USER_MCP_LINE_RE.exec(line)
    if (mcp) {
      flush()
      tokens.push({ kind: 'chip', name: mcp[2] ? `${mcp[1]} · ${mcp[2]}` : mcp[1], group: 'mcp' })
      if (mcp[3]?.trim()) buf.push(mcp[3].trim())
      continue
    }
    buf.push(line)
  }
  flush()

  // 有胶囊（指令或附件）才走 token 渲染；纯文本消息仍走整段 markdown。
  const refs = tokens.some((t) => t.kind !== 'text') ? tokens : null
  if (directiveRefCache.size >= MD_CACHE_LIMIT) directiveRefCache.clear()
  directiveRefCache.set(key, refs)
  return refs
}

// 没有被正文里的 [[att:n]] 占位标记引用到的附件，继续走独立的附件块渲染。
// 老消息没有标记 → 全部留在块里（行为不变）；新消息标记齐全时块为空，
// 附件只在文字流原位置出现，不再重复显示一份。
function msgFilesWithoutMarker(msg: sessionsApi.Message): Partial<sessionsApi.UploadedFile>[] {
  const files = msg.files || []
  if (!msg.content || !files.length) return files
  const used = new Set<number>()
  const re = /\[\[att:(\d+)\]\]/g
  let m: RegExpExecArray | null
  while ((m = re.exec(stripZeroWidth(msg.content))) !== null) {
    const i = Number(m[1])
    if (Number.isInteger(i) && i >= 0 && i < files.length) used.add(i)
  }
  return files.filter((_, i) => !used.has(i))
}

// 按 token 的 attIndex 取对应附件（越界/缺文件时返回 undefined，模板据此兜底）。
function msgAttFile(msg: sessionsApi.Message, idx: number): Partial<sessionsApi.UploadedFile> | undefined {
  return msg.files?.[idx]
}

// 排队气泡预览 / 引导注入用的正文：摘掉附件占位标记。附件已由 queue-row-files
// 单独展示，`[[att:n]]` 对用户没有意义；引导注入是纯文本通道，也不能带着标记。
function queueContentPreview(content: string): string {
  return stripZeroWidth(content).replace(/\s*\[\[att:(?:file:[^\]\s]+|\d+)\]\]/g, '').trim()
}

// 把一段"指令行 + 正文"的消息还原成胶囊 + 正文（编辑排队消息时回填输入框用）。
// 与 userDirectiveRefs 共用同一套行正则，保证"显示成什么样、回填就成什么样"。
function parseDirectivesFromContent(content: string): { chips: PendingDirective[]; text: string } {
  const chips: PendingDirective[] = []
  const restLines: string[] = []
  for (const rawLine of stripZeroWidth(content).split('\n')) {
    const line = rawLine.trim()
    if (!line) continue
    const skill = USER_SKILL_LINE_RE.exec(line)
    if (skill) {
      chips.push({
        id: nextDirectiveId(),
        text: `/skill ${skill[1]}`,
        name: skill[1],
        group: 'skills',
        avatar: skillInitial(skill[1]),
      })
      if (skill[2]?.trim()) restLines.push(skill[2].trim())
      continue
    }
    const mcp = USER_MCP_LINE_RE.exec(line)
    if (mcp) {
      chips.push({
        id: nextDirectiveId(),
        text: mcp[2] ? `/mcp ${mcp[1]} ${mcp[2]}` : `/mcp ${mcp[1]}`,
        name: mcp[2] ? `${mcp[1]} · ${mcp[2]}` : mcp[1],
        group: 'mcp',
        avatar: '/',
      })
      if (mcp[3]?.trim()) restLines.push(mcp[3].trim())
      continue
    }
    restLines.push(line)
  }
  return { chips, text: restLines.join('\n').trim() }
}

function renderMarkdown(content: string): string {
  content = stripZeroWidth(content)
  const cached = mdCache.get(content)
  if (cached !== undefined) return cached
  const html = marked.parse(content) as string
  // 简单的 LRU：超过上限时清空最早的一半
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

// ===== 侧栏会话数据源：全量 web 会话缓存 =====
// 说明：侧栏现在按"工作目录"分组（见下方 sessionGroups），分组只有在拿到**全部**
// web 会话时才正确——只看 store 里分页加载的 20 条，会出现的不是"某目录只这么
// 几条会话"，而是"该目录的会话还没加载出来"。因此侧栏挂载时（onMounted）就一次性
// 补齐后端全部 web 会话到本地缓存（allWebSessions），搜索过滤与分组都基于它做纯
// 前端计算；store.sessions 仍保留（分页），作为缓存就绪前的兜底数据源。
// 缓存就绪前显示的条数会偏少，属于预期的中间态。
//
// allWebSessions / fullSessionsLoading / fullSessionsFailed 三者来自模块级模块
// （见 chatSessionCache.ts）而不是组件内 ref：ChatView 随路由切换卸载重建，
// 缓存若挂在组件里，每次回到会话页都要重新全量拉取 + 再铺一次骨架屏。
const sessionSearch = ref('')
const searchLoading = fullSessionsLoading // 本视图沿用的旧名：同一次"全量补齐"
const SESSION_FETCH_STEP = 100
// 分页请求的并发上限。串行 for 在会话上千时会退化成几十次串行往返，每次至少
// 一个网络 RTT；有限并发把这段等待压到 RTT × ceil(页数 / 并发度)。
// 不宜更大：后端会话库是单连接（见 internal/session/store.go 的 NewStore），
// 并发过高只会在数据库层排队，白白占住首屏的其它请求。
const SESSION_FETCH_CONCURRENCY = 4

const isSearching = computed(() => sessionSearch.value.trim().length > 0)

function isWebSession(s: sessionsApi.Session): boolean {
  return !s.source || s.source === 'web'
}

// 侧栏数据源：缓存就绪后用缓存（含分组），否则退回 store 分页列表。
const sidebarSessionPool = computed(() => allWebSessions.value ?? chatStore.sessions)

// 搜索命中的会话（扁平结果，不分目录——搜索时按相关性给结果更有用）
const visibleSessions = computed(() => {
  const q = sessionSearch.value.trim().toLowerCase()
  if (!q) return sidebarSessionPool.value
  return sidebarSessionPool.value.filter(s => {
    if (!isWebSession(s)) return false
    const title = (s.title || '').toLowerCase()
    const preview = (s.preview || '').toLowerCase()
    return title.includes(q) || preview.includes(q)
  })
})

// 一次性拉全后端 web 会话（分页循环，避免截断）。侧栏挂载时就调用，供分组与搜索使用。
// 缓存命中后多久算"过期"（毫秒）。窗口内回访完全不发请求；过期后的刷新也是
// 静默的——侧栏已经有缓存数据在渲染，不会出现骨架或空白。取值只需覆盖
// "用户在几个页面之间来回切"这种高频回访，不必更短。
const FULL_SESSIONS_STALE_MS = 30_000

async function loadFullSessions(force = false): Promise<void> {
  if (searchLoading.value) return
  // 已有缓存：直接用，不重拉。只有缓存明显过期才在后台静默刷新一次
  // （保留自愈能力：别的端新建的会话仍会被补进来）。
  if (!force && allWebSessions.value
      && Date.now() - fullSessionsFetchedAt.value < FULL_SESSIONS_STALE_MS) {
    return
  }
  searchLoading.value = true
  try {
    const web: sessionsApi.Session[] = []
    const first = await sessionsApi.getSessions(SESSION_FETCH_STEP, 0)
    web.push(...first.sessions.filter(isWebSession))
    const total = first.total ?? 0
    // 先按服务端给出的总数把剩余页下标算好，再按有限并发成批拉。
    // 并发下不再有"某页不满即提前 break"的机会：多算出来的页最多是空页，
    // 拼接结果不受影响（顺序仍由后端的 updated_at 倒序保证）。
    const offsets: number[] = []
    for (let offset = SESSION_FETCH_STEP; offset < total; offset += SESSION_FETCH_STEP) {
      offsets.push(offset)
    }
    for (let i = 0; i < offsets.length; i += SESSION_FETCH_CONCURRENCY) {
      const pages = await Promise.all(
        offsets.slice(i, i + SESSION_FETCH_CONCURRENCY)
          .map(offset => sessionsApi.getSessions(SESSION_FETCH_STEP, offset)),
      )
      for (const page of pages) {
        web.push(...page.sessions.filter(isWebSession))
      }
    }
    allWebSessions.value = web
    fullSessionsFetchedAt.value = Date.now()
    fullSessionsFailed.value = false
  } catch (e) {
    // 拉全量失败时退化为 store 分页列表（侧栏仍可用，只是分组不完整）
    fullSessionsFailed.value = true
    console.error('Failed to load full sessions:', e)
  } finally {
    searchLoading.value = false
  }
}

// 兜底：挂载时的全量补齐失败（缓存仍为 null）时，用户开始搜索再补一次，250ms 防抖
let searchDebounceTimer: ReturnType<typeof setTimeout> | null = null
watch(sessionSearch, (val) => {
  if (searchDebounceTimer) {
    clearTimeout(searchDebounceTimer)
    searchDebounceTimer = null
  }
  if (!val.trim() || allWebSessions.value) return
  searchDebounceTimer = setTimeout(loadFullSessions, 250)
})

// 缓存就绪后，store 中新增/刷新出的会话保持同步（如新建会话后立即出现在侧栏）。
// 除新增外，还要把刷新后变化的字段（标题、预览、消息数、token、最近活动等）
// 回写进缓存里的既有会话，否则会话标题要等下次全量刷新/整页刷新才更新。
//
// immediate: true 是"缓存跨路由存活"的必要补偿：组件重新挂载时这个 watch 是
// 新注册的（旧实例的已随卸载销毁），若不在挂载瞬间跑一次，那段时间里 store
// 已刷新的数据（例如别的端新建的会话、刚改的名字）就进不了缓存，而全量补齐
// 又在 30s 新鲜窗口内被抑制 —— 表现为"回来看不到新会话"。
// 挂载时 onMounted 会先 await loadSessions()，这里拿到的是刚拉到的第一页。
watch(() => chatStore.sessions, (list) => {
  if (!allWebSessions.value) return
  const known = new Set(allWebSessions.value.map(s => s.id))
  const fresh = list.filter(s => isWebSession(s) && !known.has(s.id))
  let changed = false
  for (const ns of list) {
    if (!isWebSession(ns)) continue
    const cached = allWebSessions.value.find(s => s.id === ns.id)
    if (!cached) continue
    // 仅当字段有变化时才写，避免无谓触发 computed 重算
    if (
      cached.title !== ns.title ||
      cached.preview !== ns.preview ||
      cached.last_active !== ns.last_active ||
      cached.message_count !== ns.message_count ||
      cached.input_tokens !== ns.input_tokens ||
      cached.output_tokens !== ns.output_tokens ||
      cached.work_dir !== ns.work_dir ||
      cached.work_dir_user_set !== ns.work_dir_user_set
    ) {
      Object.assign(cached, ns)
      changed = true
    }
  }
  if (fresh.length > 0) allWebSessions.value = [...fresh, ...allWebSessions.value]
  else if (changed) allWebSessions.value = [...allWebSessions.value]
}, { immediate: true })

// 就地修改（改名、改工作目录）不会改变 store.sessions 的数组引用，watcher 不会触发，
// 这里手动把变化同步进缓存，否则分组/标题要等下一次刷新才更新。
function patchCachedSession(id: string, patch: Partial<sessionsApi.Session>): void {
  if (!allWebSessions.value) return
  const hit = allWebSessions.value.find(s => s.id === id)
  if (hit) Object.assign(hit, patch)
}

function removeCachedSession(id: string): void {
  if (allWebSessions.value) {
    allWebSessions.value = allWebSessions.value.filter(s => s.id !== id)
  }
}

// ===== 侧栏会话分组：按"是否设置过工作目录" =====
// - 用户显式设置过工作目录的会话 → 按目录各自成组（可折叠，组内分页"加载更多"）
// - 没设置过的（创建时自动分配的临时目录）→ 全部并进一个兜底分组
// 组间按组内最近活动倒序，兜底组排在最后；组数据来自全量缓存 allWebSessions。
const NO_DIR_GROUP_KEY = '__no_work_dir__'
// 目录 key 归一化：分隔符统一、去尾部斜杠、忽略大小写（Windows 路径大小写不敏感）
function dirKey(p: string): string {
  return p.replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase()
}

// 分组标题的展示名：多级路径只取最后一段（完整路径放在 title 提示里）。
// 侧栏宽度有限，长路径若从头截断，D:\a\web 与 D:\b\web 会显示成一模一样。
function dirLabel(p: string): string {
  const norm = p.replace(/\\/g, '/').replace(/\/+$/, '')
  const idx = norm.lastIndexOf('/')
  const tail = idx >= 0 ? norm.slice(idx + 1) : norm
  return tail || p
}

interface SidebarGroup {
  key: string
  label: string
  userSet: boolean
  sessions: sessionsApi.Session[]
}

// 每组默认渲染的会话条数：同一目录会话多时，靠"加载更多"按页展开，
// 避免一组几十条把侧栏整个顶下去。
const GROUP_PAGE_SIZE = 5
const collapsedGroups = ref<Record<string, boolean>>({})
const groupPageCount = ref<Record<string, number>>({})

function isGroupCollapsed(key: string): boolean {
  return collapsedGroups.value[key] === true
}

// 收起某组的分页进度：删掉 key 而不是写回 GROUP_PAGE_SIZE —— groupLimit 的语义
// 是"缺省即默认页"，写死一个 5 会让默认值出现第二份事实来源。
function resetGroupPage(key: string): void {
  if (groupPageCount.value[key] === undefined) return
  const next = { ...groupPageCount.value }
  delete next[key]
  groupPageCount.value = next
}

function toggleGroup(key: string): void {
  collapsedGroups.value = { ...collapsedGroups.value, [key]: !isGroupCollapsed(key) }
  // 折叠/展开都是"收起这一组"的意图：顺手把这组的分页进度收回默认页。
  // 重新展开后只显示 GROUP_PAGE_SIZE 条，要再点「加载更多」才继续展开——
  // 否则折叠前翻过十几页的组，一展开又整片铺回来，与"折叠"的意图正好相反。
  resetGroupPage(key)
}

function groupLimit(key: string): number {
  return groupPageCount.value[key] ?? GROUP_PAGE_SIZE
}

function showMoreInGroup(key: string): void {
  groupPageCount.value = { ...groupPageCount.value, [key]: groupLimit(key) + GROUP_PAGE_SIZE }
}

const sessionGroups = computed<SidebarGroup[]>(() => {
  const byDir = new Map<string, sessionsApi.Session[]>()
  const noDir: sessionsApi.Session[] = []
  for (const s of sidebarSessionPool.value) {
    if (!isWebSession(s)) continue
    if (s.work_dir_user_set && s.work_dir) {
      const k = dirKey(s.work_dir)
      const list = byDir.get(k)
      if (list) list.push(s)
      else byDir.set(k, [s])
    } else {
      noDir.push(s)
    }
  }
  const byRecency = (a: sessionsApi.Session, b: sessionsApi.Session) => (b.last_active || 0) - (a.last_active || 0)
  const newestAt = (list: sessionsApi.Session[]) => list.reduce((m, s) => Math.max(m, s.last_active || 0), 0)
  const groups: SidebarGroup[] = [...byDir.entries()]
    .map(([k, list]) => ({
      key: `dir:${k}`,
      label: list[0]?.work_dir || k,
      userSet: true,
      sessions: [...list].sort(byRecency),
    }))
    .sort((a, b) => newestAt(b.sessions) - newestAt(a.sessions))
  // 未设置工作目录的会话固定成一组「默认」，始终排在最前面，不参与插入排序，
  // 避免它随着最近活动时间上下浮动、位置不可预期。
  if (noDir.length) {
    groups.unshift({
      key: NO_DIR_GROUP_KEY,
      label: t('chat.groupNoWorkDir'),
      userSet: false,
      sessions: [...noDir].sort(byRecency),
    })
  }
  return groups
})

// 只有存在"设置过工作目录"的分组时才画分组标题：全都是未设置目录的会话时，
// 侧栏保持与以前一致的扁平列表，不凭空多出一行无信息量的表头（也就没有可折叠的东西）。
const showGroupHeaders = computed(() => sessionGroups.value.some(g => g.userSet))

// 某组当前实际渲染的会话：截到分页上限；若活动会话落在上限之外，一并展开到它，
// 否则切到某个老会话后，侧栏里看不见自己在哪里。
function groupVisibleSessions(group: SidebarGroup): sessionsApi.Session[] {
  const limit = groupLimit(group.key)
  const activeId = chatStore.activeSessionId
  let end = Math.min(limit, group.sessions.length)
  if (activeId) {
    const idx = group.sessions.findIndex(s => s.id === activeId)
    if (idx >= end) end = idx + 1
  }
  return group.sessions.slice(0, end)
}

type SidebarRow =
  | { kind: 'group'; key: string; groupKey: string; label: string; workDir: string; count: number; collapsed: boolean; userSet: boolean }
  | { kind: 'session'; key: string; session: sessionsApi.Session }
  | { kind: 'more'; key: string; groupKey: string; hidden: number }

// 模板只遍历这一份扁平的"行"列表（分组头 / 会话 / 加载更多 按顺序混排），
// 会话条目模板因此只保留一份；搜索时就是不掺分组头的纯会话行。
const sidebarRows = computed<SidebarRow[]>(() => {
  if (isSearching.value) {
    return visibleSessions.value.map(s => ({ kind: 'session' as const, key: `s:${s.id}`, session: s }))
  }
  const rows: SidebarRow[] = []
  for (const g of sessionGroups.value) {
    const visible = groupVisibleSessions(g)
    if (showGroupHeaders.value) {
      rows.push({
        kind: 'group',
        key: `g:${g.key}`,
        groupKey: g.key,
        label: g.label,
        // 分组头快捷操作要用真实目录（「默认」组为空 → 新建时走系统默认目录）
        workDir: g.userSet ? g.label : '',
        count: g.sessions.length,
        collapsed: isGroupCollapsed(g.key),
        userSet: g.userSet,
      })
    }
    if (isGroupCollapsed(g.key)) continue
    for (const s of visible) rows.push({ kind: 'session', key: `s:${s.id}`, session: s })
    const hidden = g.sessions.length - visible.length
    if (hidden > 0) rows.push({ kind: 'more', key: `m:${g.key}`, groupKey: g.key, hidden })
  }
  return rows
})

// ===== 侧栏骨架屏 =====
// 首屏有两个"列表形态还会变"的窗口：
//   ① 分页列表还没回来 —— 侧栏是纯空白；
//   ② 分页回来了、但全量缓存还在补 —— 此时若把 20 条画出来，全量到达后会整体
//      变成"按目录分组"的结构，整列位置大挪移。这正是"会话多时首次打开观感不好"
//      里最扎眼的一环。
// 两段都用固定行高的骨架占位，把"空 → 20 条 → 分组"三次切换压成"骨架 → 分组"一次。
const awaitingFullSessions = computed(() =>
  allWebSessions.value === null && !fullSessionsFailed.value && chatStore.sessionsHasMore
)

const sessionListSkeletonVisible = computed(() => {
  // 搜索态有自己的"检索中 / 无结果"提示，不叠加会话骨架
  if (isSearching.value) return false
  // 已经有行可渲染时，绝不拿骨架去盖：从别的页面切回会话页时，store 里本就留着
  // 上一轮的分页数据（全局 pinia），此时再铺一次骨架就是"每次进入都重新加载"
  // 这一观感的直接来源。
  if (sidebarRows.value.length > 0) return false
  if (awaitingFullSessions.value) return true
  // 兜底：连分页列表都还没到（且确实在加载）
  return chatStore.sessionsLoading
})

// 切到某个会话时，若它所在的分组处于折叠状态就自动展开——否则侧栏里看不见当前会话
// （从"按目录查看会话"面板或搜索结果跳进一个折叠分组里的会话时尤其明显）。
watch(() => chatStore.activeSessionId, (id) => {
  if (!id || !showGroupHeaders.value) return
  const group = sessionGroups.value.find(g => g.sessions.some(s => s.id === id))
  if (group && isGroupCollapsed(group.key)) {
    collapsedGroups.value = { ...collapsedGroups.value, [group.key]: false }
  }
})

// 切会话时刷新能力清单（/skill、/mcp 的候选来源）。技能与 MCP 连接状态是
// 会话级的，不刷新会把上一个会话的候选带过来。
watch(() => chatStore.activeSessionId, (id) => {
  if (id) chatStore.loadCapabilities(id)
}, { immediate: true })

// 点击搜索命中的会话：若不在 store 已加载列表中，先并入再进入，保证派生状态完整
async function handleSessionClick(id: string) {
  // 侧栏数据源是全量缓存 allWebSessions（含「加载更多」展开出的会话），未必在
  // store 的分页列表里。不补进 store 的话 activeSession 找不到这条会话，
  // currentWorkDir 变成空串，底部目录栏会把已设置过目录的会话误显示成
  // 「未设置（使用默认目录）」。以前只在搜索态补，非搜索态点「加载更多」
  // 展开的会话必踩这个坑，所以这里对任何来源都兜底合并。
  if (!chatStore.sessions.some(s => s.id === id) && allWebSessions.value) {
    const hit = allWebSessions.value.find(s => s.id === id)
    if (hit) chatStore.mergeSessions([hit])
  }
  await selectSession(id)
  // 移动端：选完会话自动收起顶部抽屉，把屏幕还给聊天区（旧版得再点一次拖动条）
  if (isMobile.value) mobileSessionExpanded.value = false
}

// 侧栏会话列表：不再按分身分组，展示一份展平的统一列表（按更新时间/后端返回顺序）。
// 分身在单条会话的可视化上以会话自身的 profile 标记呈现（见底部栏“当前分身”）。
const isGatewaySession = computed(() => {
  const session = chatStore.activeSession
  return session && session.source && session.source !== 'web'
})

const activeSessionSource = computed(() => {
  return chatStore.activeSession?.source || ''
})

// 底部栏展示的“当前分身”：取当前活动会话归属的 profile，default/空归一为聊天默认名
const activeProfileName = computed(() => {
  const p = chatStore.activeSession?.profile?.trim()
  if (!p || p.toLowerCase() === 'default') return t('chat.default')
  return p
})

// Task timeline computed from tool calls and progress
const synthesisStarted = ref(false)

// Reset synthesis flag when a new message starts streaming
watch(() => chatStore.streaming, (val) => {
  if (!val) synthesisStarted.value = false
})

const taskTimelineSteps = computed((): TimelineStep[] => {
  const steps: TimelineStep[] = []
  const tcs = chatStore.toolCalls

  // Phase 1: Planning (if we have a task progress)
  if (chatStore.taskProgress) {
    steps.push({
      key: 'planning',
      title: t('chat.taskPlanning'),
      description: t('chat.taskPlanningDesc'),
      status: 'completed',
      isTool: false,
    })
  }

  // Phase 2: Tool execution steps —— 逐条展示，不再按工具名去重。
  // 去重会把「连续读 5 个文件」压成一条，坞里就只剩一个名字、毫无过程感；
  // 而坞的价值恰恰在于"看到它一条条在推进"。重复调用由展开体的滚动区消化。
  for (const tc of tcs) {
    steps.push({
      key: tc.id,
      title: toolShortName(tc.name) || tc.name,
      summary: toolCallSummary(tc, 72),
      description: tc.args?.substring(0, 80) || '',
      status:
        tc.status === 'running'
          ? 'running'
          : tc.status === 'completed'
            ? 'completed'
            : tc.status === 'error'
              ? 'failed'
              : 'pending',
      detail: tc.status === 'running' ? t('chat.executing') : undefined,
      duration: tc.duration,
      isTool: true,
    })
  }

  // Phase 3: Synthesis (if streaming and no running tools; 仅真正的工具/长任务回合才展示该阶段，
  // 纯文本回答不显示任务坞)
  const toolTurnActive = chatStore.toolCalls.length > 0 || chatStore.taskProgress !== null
  const showSynthesis = toolTurnActive && chatStore.streaming && chatStore.activeToolCalls.length === 0 && chatStore.streamContent
  if (showSynthesis) synthesisStarted.value = true

  if (synthesisStarted.value && chatStore.streaming) {
    steps.push({
      key: 'synthesis',
      title: t('chat.resultSynthesis'),
      description: t('chat.resultSynthesisDesc'),
      status: 'running',
      detail: t('chat.generating'),
      isTool: false,
    })
  }

  return steps
})

// 任务坞仅在真正的工具/长任务回合显示（纯文本回答不出现，避免噪音）
const taskDockVisible = computed(() => {
  if (!chatStore.streaming) return false
  return chatStore.taskProgress !== null || chatStore.toolCalls.length > 0 || chatStore.activeToolCalls.length > 0
})

function sourceType(source: string) {
  const map: Record<string, string> = {
    telegram: 'info',
    discord: 'info',
    wechat: 'success',
    wecom: 'success',
    slack: 'warning',
  }
  return (map[source] || 'default') as any
}

function formatTime(timestamp: string | number): string {
  if (!timestamp) return ''
  const date = toDate(timestamp)
  if (isNaN(date.getTime())) return ''
  const now = new Date()
  const isToday = date.toDateString() === now.toDateString()
  if (isToday) {
    return date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  }
  return date.toLocaleDateString([], { month: 'short', day: 'numeric' }) + ' ' +
    date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

// 兼容三种 timestamp 格式：
//   1. ISO8601 字符串  (推荐，如 "2026-07-30T12:34:56.789Z")
//   2. 毫秒级数字      (如 1785474896789)
//   3. 秒级数字        (如 1785474896，< 1e12 时自动当作秒处理)
function toDate(ts: string | number): Date {
  if (typeof ts === 'number') {
    const n = ts as number
    // 秒级 Unix timestamp: 10 位数 → 2001-09 ~ 2286-11 之间
    // 毫秒级 Unix timestamp: 13 位数 → 2001-09 ~ 2286-11 之间 *1000
    // 也兼容纳秒字符串被转成大数字
    if (n < 1e12) return new Date(n * 1000) // 秒 → 毫秒
    return new Date(n) // 已是毫秒
  }
  // 字符串
  const s = String(ts).trim()
  if (!s) return new Date(NaN)
  // 纯数字字符串
  if (/^-?\d+$/.test(s)) {
    const n = Number(s)
    if (isFinite(n)) {
      if (n < 1e12) return new Date(n * 1000)
      return new Date(n)
    }
  }
  // 普通日期字符串（含 ISO RFC3339、中文格式等）
  const d = new Date(s)
  if (!isNaN(d.getTime())) return d
  // 补 Z 兜底（UTC 无时区标识）
  return new Date(s + 'Z')
}

// File handling - upload to server
async function sha1OfFile(file: File): Promise<string> {
  // SubtleCrypto gives us a real content hash for dedup without shipping the
  // entire file off the device first. The old name+size key was trivially
  // bypassed by changing one byte; sha1 catches real duplicates.
  try {
    if (typeof crypto !== 'undefined' && crypto.subtle && typeof crypto.subtle.digest === 'function') {
      const buf = await crypto.subtle.digest('SHA-1', await file.arrayBuffer())
      const bytes = new Uint8Array(buf)
      let hex = ''
      for (let i = 0; i < bytes.length; i++) {
        hex += bytes[i].toString(16).padStart(2, '0')
      }
      return hex
    }
  } catch (_e) {
    // fall through to name-based key
  }
  return ''
}

// Shared upload path for the file picker and clipboard paste: dedup key,
// upload, push to selectedFiles (keeping the raw File for the multimodal
// channel) and localized error toast. Returns true on success.
async function uploadNativeFile(nativeFile: File): Promise<boolean> {
  // Two-tier dedup key: sha1 (when supported) plus name+size as a fallback so
  // very old browsers still get some protection.
  const hash = await sha1OfFile(nativeFile)
  const fileKey = (hash || `${nativeFile.name}-${nativeFile.size}-${nativeFile.lastModified}`)
  if (uploadingFiles.value.has(fileKey)) {
    return false
  }
  uploadingFiles.value.add(fileKey)

  try {
    const uploaded = await sessionsApi.uploadFile(nativeFile, chatStore.activeSessionId ?? undefined)
    // Keep the raw File in memory: the multimodal channel reads it directly
    // with FileReader (the /api/uploads URL sits behind requireAuth, so a
    // bare fetch of it would 401 and silently kill the image channel).
    uploaded._native = nativeFile
    ;(uploaded as { _uid?: string })._uid = nextFileUid()
    selectedFiles.value.push(uploaded)
    return true
  } catch (e) {
    console.error('Upload failed:', e)
    message.error(describeUploadError(e as Error & { code?: string }, nativeFile))
    return false
  } finally {
    uploadingFiles.value.delete(fileKey)
  }
}
// 工具下拉菜单：上传文件（规划模式已直接显示在工具栏上，不再放这里）。
const toolbarMenuOpen = ref(false)

// ===== 技能 / 插件面板（输入框内的二级面板）=====
//
// 旧交互：想在对话里用技能或 MCP，必须先手打 "/skill" / "/mcp" 才能看到候选，
// 而且候选列表只能"选"，不能"启用/连接"，也没有描述和搜索——用户看到的是一个
// 无从下手的裸列表（这就是本次要修的用户不友好）。
// 新交互：+ 菜单或工具栏胶囊按钮展开面板 → 每项带名称/描述/开关 → 点击即填入
// 输入框或直接连接，随发随用，不需要背指令。
//
// 2026-10-31：第二个面板从"连接器（MCP 服务器列表）"换成"插件"。理由是用户
// 装的是插件、用的是插件带来的工具，逐个 server 的视角既不知道插件装没装、也
// 管不了它带来的 CLI 与技能；而插件行天然能承载"启用/停用"这个高频动作。

type CapPanel = '' | 'skills' | 'plugins'
const activePanel = ref<CapPanel>('')
const panelSkillUploadRef = ref<{ openOpenFileDialog: () => void } | null>(null)
const skillPanelSearch = ref('')
const pluginPanelSearch = ref('')
const togglingSkill = ref('')
// 正在切换启用态的插件名（开关 loading 用；插件启停要重连它的连接器，几十秒都可能）。
const pluginBusyName = ref('')
// 插件面板里展开明细的插件名（一次只展开一个，避免面板被撑爆）。
const expandedPlugin = ref('')

// 待执行指令胶囊（pending directive chips）。
//
// 不只是技能：**任何斜杠指令都可以挂成胶囊**——技能 / 连接器工具 / 普通命令，
// 胶囊直接嵌在输入框的文字流里（见 ComposerEditor），点 × 或退格撤掉；
// 发送时由编辑器按顺序还原成 `/xxx args` 指令行。
//
// 存**完整指令文本**而不是只存名字：`/mcp <server> <tool>` 这类光有命令名不成立，
// 必须连参数一起记住；id 用于和编辑器里的 DOM 胶囊一一对账，name/group/avatar
// 只用于胶囊外观。
interface PendingDirective {
  id: string
  text: string
  name: string
  group: 'skills' | 'commands' | 'mcp'
  avatar: string
}
const pendingDirectives = ref<PendingDirective[]>([])

// 胶囊 id：只要求会话内唯一，用于编辑器增量对账（增删而不重建）。
let directiveSeq = 0
function nextDirectiveId(): string {
  directiveSeq += 1
  return `d${Date.now().toString(36)}${directiveSeq.toString(36)}`
}

// 输入框里的胶囊被用户摘掉（点 × / 退格）时，父组件的列表跟着删。
function removePendingDirective(id: string) {
  pendingDirectives.value = pendingDirectives.value.filter((d) => d.id !== id)
}

// ---- 文件附件胶囊 ----
//
// 上传的文件与技能胶囊一样嵌进输入框的文字流（WorkBuddy 的 mention 交互）：
// 可与正文穿插、随光标位置插入、随换行折行，点 × 或退格整颗摘掉。
// 文件本体仍走发送请求的 files/images 通道（见 send）；getPayload 会在胶囊
// 原位置留下 [[att:file:<uid>]] 占位标记，发送时改写成最终下标，渲染端据此
// 把附件画回文字流原位置（绝不会把文件名序列化成指令行）。
//
// _uid 是"DOM 胶囊 ↔ selectedFiles 条目"的对账键：上传响应里没有稳定 id，
// 自己发一个会话内唯一的即可。
let fileUidSeq = 0
function nextFileUid(): string {
  fileUidSeq += 1
  return `f${Date.now().toString(36)}${fileUidSeq.toString(36)}`
}

const fileChips = computed(() =>
  selectedFiles.value.map((f) => {
    const name = f.name || f.filename || t('chat.imageBtn')
    return {
      id: `file:${(f as { _uid?: string })._uid || name}`,
      text: name,
      name,
      group: 'files',
      avatar: '📄',
      kind: 'file' as const,
      sizeLabel: f.size ? formatFileSize(f.size) : '',
    }
  }),
)

// 输入框胶囊总表 = 指令胶囊 + 文件胶囊。顺序无关紧要（胶囊按各自被插入的
// DOM 位置渲染），编辑器只按 id 做增量对账。
const composerChips = computed(() => [...pendingDirectives.value, ...fileChips.value])

// 胶囊被摘掉：文件胶囊删掉 selectedFiles 里的对应条目，指令胶囊走原逻辑。
function onComposerChipRemove(id: string) {
  if (id.startsWith('file:')) {
    const uid = id.slice('file:'.length)
    const idx = selectedFiles.value.findIndex((f) => (f as { _uid?: string })._uid === uid)
    if (idx >= 0) selectedFiles.value.splice(idx, 1)
    return
  }
  removePendingDirective(id)
}

function togglePluginDetail(name: string) {
  expandedPlugin.value = expandedPlugin.value === name ? '' : name
}

// skillsStore.skills 是"启用态"的权威来源（含 disabled 项），capabilities 只含
// 已启用项。优先用前者，未加载成功时回落到后者，保证面板永远有内容可看。
const skillPanelItems = computed(() => {
  const fromStore = skillsStore.skills
  if (fromStore.length) {
    // 启用态只认 store 里的 s.enabled（toggleSkill 会就地更新它）。
    // 之前用 capabilities 兜底 OR（capSet.has(...)），capabilities 刷新有延迟且
    // 服务端可能仍返回该技能，导致刚关掉的开关立刻弹回"启用"，状态变化看不到。
    return fromStore
      .filter(s => s.status !== 'archived' && s.status !== 'rejected')
      .map(s => ({
        name: s.name || s.id,
        description: s.description || '',
        enabled: !!s.enabled,
      }))
  }
  return chatStore.capabilities.skills.map(s => ({
    name: s.name,
    description: s.description || '',
    enabled: true,
  }))
})

const filteredSkillPanelItems = computed(() => {
  const p = skillPanelSearch.value.trim().toLowerCase()
  if (!p) return skillPanelItems.value
  return skillPanelItems.value.filter(
    s => s.name.toLowerCase().includes(p) || s.description.toLowerCase().includes(p),
  )
})

// 插件面板以 agentPluginsStore.plugins 为准（含未启用项——启用态就是这一栏的
// 主操作）。排序：已启用的在前（用户来这里多半是想用某个已开的东西），被拒的
// 沉到末尾但仍然可见（带原因，不能悄悄消失成"插件不见了"）。
const pluginPanelItems = computed(() => {
  const p = pluginPanelSearch.value.trim().toLowerCase()
  return agentPluginsStore.plugins
    .filter(x => !p || x.name.toLowerCase().includes(p) || (x.description || '').toLowerCase().includes(p))
    .slice()
    .sort((a, b) => {
      if (a.rejected !== b.rejected) return a.rejected ? 1 : -1
      if (a.enabled !== b.enabled) return a.enabled ? -1 : 1
      return a.name.localeCompare(b.name)
    })
})

// 一行能说清的两件事：这插件给了什么（连接器/CLI/技能各多少），以及能不能用。
function pluginMetaText(p: AgentPlugin): string {
  const parts: string[] = []
  const servers = p.mcp_servers || []
  if (servers.length) {
    const on = servers.filter(s => s.connected).length
    parts.push(t('chat.pluginMCPCount', { on, total: servers.length }))
  }
  const clis = p.cli_connectors || []
  if (clis.length) parts.push(t('chat.pluginCLICount', { n: clis.length }))
  if ((p.skills || []).length) parts.push(t('chat.pluginSkillCount', { n: p.skills.length }))
  return parts.join(' · ')
}

// 有明细才给展开箭头：没有内容的箭头点了只会让用户怀疑自己点错了。
function pluginDetailCount(p: AgentPlugin): number {
  return (p.mcp_servers?.length || 0) + (p.cli_connectors?.length || 0)
    + (p.skills?.length || 0) + (p.examples?.length || 0)
}

/** 把一条 blocker 翻译成人话。文案与插件页共用（agentplugins.blocker*）。 */
function pluginBlockerText(b: AgentPluginBlocker): string {
  if (b.kind === 'oauth') return t('agentplugins.blockerOAuthText', { name: b.name || '' })
  if (b.kind === 'auth') {
    const names = b.fields?.length ? b.fields.map(f => f.label || f.key) : (b.keys || [])
    return t('agentplugins.blockerAuth', { name: b.name || '', keys: names.join(', ') })
  }
  if (b.kind === 'connect') {
    return t('agentplugins.blockerConnect', { name: b.name || '', detail: b.detail || '' })
  }
  if (b.kind === 'missing_binary') {
    return t('agentplugins.blockerCli', { name: b.name || '', command: b.command || '' })
  }
  return t('agentplugins.blockerRejected')
}

// 行内只显示第一条原因：多条会把行撑成一段话，剩下的留给插件页。
function pluginFirstBlockerText(p: AgentPlugin): string {
  if (p.rejected) return t('agentplugins.blockerRejected')
  const first = (p.blockers || [])[0]
  return first ? pluginBlockerText(first) : ''
}

function skillInitial(name: string): string {
  return (name || '?').trim().charAt(0).toUpperCase()
}

function openPanel(panel: Exclude<CapPanel, ''>) {
  activePanel.value = panel
  // 面板打开时清掉斜杠候选，避免两层浮层叠在一起。
  commandSuggestions.value = []
  if (panel === 'skills') {
    if (!skillsStore.skills.length) skillsStore.loadSkills().catch(() => {})
  } else {
    // 插件列表带连接状态与 blocker，可能刚在别的页面改过，每次打开都刷一遍。
    agentPluginsStore.loadPlugins().catch(() => {})
    chatStore.refreshCapabilities().catch(() => {})
  }
}

// 选完技能/收起面板后把光标还给输入框，用户可以直接接着打字发送。
function focusComposer() {
  nextTick(() => composerRef.value?.focus())
}

// 面板打开期间任何一次输入框输入都应把它收掉：用户已经在打字，说明他不需要
// 面板了；同时避免面板挡住新出现的斜杠候选。
function handleComposerInput() {
  if (activePanel.value) activePanel.value = ''
  // 只要输入内容变了，"已接受"这条指令就不再成立（防止改写后按回车被误判直发）。
  acceptedSuggestion.value = ''
  handleInput()
}

// Esc 的优先级：先关候选面板 → 再关二级面板 → 最后撤胶囊。一次只撤一层，不连坐。
function handleComposerEsc() {
  if (commandSuggestions.value.length) {
    commandSuggestions.value = []
    return
  }
  if (activePanel.value) {
    activePanel.value = ''
    return
  }
  // 胶囊整体撤空（逐个退太啰嗦，Esc 的语义是"取消这次附加"）。
  if (pendingDirectives.value.length) pendingDirectives.value = []
}

// 点击技能项 = 挂成 `/skill <name>` 胶囊（直接嵌在输入框文字里）。
// 把一个斜杠候选挂成胶囊（**技能 / 连接器 / 指令都走这条路**）。
// text 是完整指令（不含正文），group/name/avatar 只影响胶囊外观。
function addDirectiveChip(s: { value: string; group?: ChatSuggestion['group']; name?: string }) {
  const text = s.value.trim()
  if (!text) return
  const group = s.group || 'commands'
  const name = s.name || text.replace(/^\//, '').split(/\s+/)[0]
  if (!hasPendingDirective(text)) {
    pendingDirectives.value.push({
      id: nextDirectiveId(),
      text,
      name,
      group,
      avatar: group === 'skills' ? skillInitial(name) : '/',
    })
  }
  // 挂成胶囊后清空候选状态：指令已经以 token 形式在输入框里了。
  acceptedSuggestion.value = ''
  commandSuggestions.value = []
  activePanel.value = ''
  focusComposer()
}

// 点击技能项 = 挂胶囊（/skill <name>），正文照常打。
function useSkill(name: string) {
  addDirectiveChip({ value: `/skill ${name}`, group: 'skills', name })
}

// 点击插件示例问法 = 直接填进输入框（插件工具是模型自己调的，没有可挂的
// 斜杠指令；一句真实问法就是最好的"试一下"）。已有草稿时追加，不覆盖。
function usePluginExample(text: string) {
  const draft = inputValue.value.trimEnd()
  inputValue.value = draft ? `${draft} ${text}` : text
  // 走一遍 handleInput：把斜杠候选按新正文重算（否则残留的候选会让下一次
  // 回车被当成"选中候选"而不是发送）。
  handleInput()
  activePanel.value = ''
  focusComposer()
}

async function onToggleSkill(skill: { name: string; enabled: boolean }, value: boolean) {
  togglingSkill.value = skill.name
  try {
    await skillsStore.toggleSkill(skill.name, value)
    await chatStore.refreshCapabilities()
    message.success(t(value ? 'chat.skillEnabledToast' : 'chat.skillDisabledToast', { name: skill.name }))
  } catch (e) {
    message.error(t('chat.skillToggleFailed'))
  } finally {
    togglingSkill.value = ''
  }
}

/**
 * 插件启用/停用。
 *
 * 未就绪（缺凭据 / 命令没装 / 连不上 / 清单被拒）时不硬拨开关：后端会 409 拒绝，
 * 用户看到的只是"拨了又弹回来"。这里提前把第一条原因说出来——面板里不做授权与
 * 安装（那需要凭据表单和一键装包），交给插件页，所以提示里指个方向。
 */
async function onTogglePlugin(p: AgentPlugin, value: boolean) {
  const reason = pluginFirstBlockerText(p)
  if (value && reason) {
    message.warning(t('chat.pluginNotReady', { name: p.name, reason }), { duration: 6000 })
    return
  }
  pluginBusyName.value = p.name
  try {
    await agentPluginsStore.setEnabled(p.name, value)
    // 插件带来的技能/工具会进出能力清单，刷新后斜杠候选才是真的可用集合。
    await chatStore.refreshCapabilities()
    message.success(t(value ? 'chat.pluginEnabledToast' : 'chat.pluginDisabledToast', { name: p.name }))
  } catch (e: any) {
    message.error(e?.message || t('chat.pluginToggleFailed'), { duration: 6000 })
  } finally {
    pluginBusyName.value = ''
  }
}

function openSkillPicker() {
  panelSkillUploadRef.value?.openOpenFileDialog()
}

async function onPanelSkillFileChange(options: { file: { file: File | null } }) {
  const native = options.file?.file
  if (!native) return
  try {
    const res = await skillsApi.uploadSkill(native, native.name)
    message.success(t('chat.uploadSuccess', { count: 1 }))
    await skillsStore.loadSkills()
    await chatStore.refreshCapabilities()
    if (res?.name) skillPanelSearch.value = res.name
  } catch (e) {
    message.error(t('chat.uploadFail'))
  }
}

function gotoSkillsManage() {
  activePanel.value = ''
  router.push('/skills')
}

function gotoPluginsManage() {
  activePanel.value = ''
  router.push('/plugins')
}

// 面板打开期间点/按到别处就收起（Esc 由输入框与全局监听各兜一半：面板展开时
// 焦点常在开关上，输入框的 keydown 收不到）。
// 注意 `.toolbar-menu-row` 也要排除：加号下拉的菜单项点击时若先触发 mousedown
// 关面板，再触发 dropdown 的 select，就会出现"点技能没反应"的竞态。
function closePanelsOnOutsideClick(e: MouseEvent) {
  if (!activePanel.value) return
  const el = e.target as HTMLElement | null
  if (el && el.closest('.cap-panel, .n-dropdown-menu, .n-base-select-menu')) return
  activePanel.value = ''
}

function closePanelsOnEsc(e: KeyboardEvent) {
  if (e.key === 'Escape') activePanel.value = ''
}

const toolbarMenuOptions = computed<DropdownOption[]>(() => [
  {
    label: t('chat.uploadFile'),
    key: 'upload',
    icon: () => h(NIcon, null, { default: () => h(DocumentOutline) }),
    // 无活动会话时禁用上传
    disabled: !chatStore.activeSessionId,
  },
  {
    label: t('chat.skills'),
    key: 'skills',
    icon: () => h(NIcon, null, { default: () => h(ExtensionPuzzleOutline) }),
    disabled: !chatStore.activeSessionId,
  },
  {
    label: t('chat.plugins'),
    key: 'plugins',
    icon: () => h(NIcon, null, { default: () => h(LinkOutline) }),
    disabled: !chatStore.activeSessionId,
  },
  // 计划模式开关已于 2026-09-29 冻结，随后按"确认永不恢复"全量删除（agent 侧的
  // PlanExecutor 与服务端 /plan-mode 路由都没了，见
  // .workbuddy/reviews/2026-09-29-plan-mode-review.md）。原功能的用途是"先规划再执行"，
  // 现已下沉为"复杂任务先建待办"的引导（服务端 server.go、CLI cmd/magic/chat.go、
  // gateway.go 的 TASK PLANNING 段与 todo 工具描述）。
  // 会话表的 plan_mode 列仍在（免迁移），但已无任何读写代码。
])

// 下拉行右侧的计数徽标（已启用技能数 / 已启用插件数）——让用户不展开也能知道
// 有多少东西真的可用（不是"装了多少"，那是插件页关心的事）。
function renderToolbarMenuLabel(option: DropdownOption) {
  const label = String((option as { label?: string }).label ?? '')
  let badge = ''
  if ((option as { key?: string }).key === 'skills') {
    badge = t('chat.hasSkillsShort', { n: skillPanelItems.value.filter(s => s.enabled).length })
  } else if ((option as { key?: string }).key === 'plugins') {
    const n = agentPluginsStore.plugins.filter(p => p.enabled && !p.rejected).length
    badge = t('chat.hasPluginsShort', { n })
  }
  if (!badge) return label
  return h('div', { class: 'toolbar-menu-row' }, [
    h('span', { class: 'toolbar-menu-label' }, label),
    h('span', { class: 'toolbar-menu-badge' }, badge),
  ])
}

async function handleToolbarMenuSelect(key: string) {
  if (key === 'upload') {
    // 无活动会话时禁用上传
    if (!chatStore.activeSessionId) return
    toolbarUploadRef.value?.openOpenFileDialog()
    return
  }
  if (key === 'skills' || key === 'plugins') {
    if (!chatStore.activeSessionId) return
    openPanel(key)
  }
}

async function handleFileSelect({ file, onFinish, onError }: UploadCustomRequestOptions) {
  // 无活动会话时禁止上传
  if (!chatStore.activeSessionId) {
    onError()
    return
  }
  const nativeFile = file.file
  if (!nativeFile) {
    onError()
    return
  }
  const ok = await uploadNativeFile(nativeFile)
  if (ok) {
    onFinish()
  } else {
    onError()
  }
}

// Clipboard paste into the chat input: screenshots / copied files (Ctrl+V,
// Win+Shift+S, right-click-copy) become attachments like the file picker.
// Text-only pastes keep the native textarea behavior untouched.
async function handlePaste(e: ClipboardEvent) {
  const items = e.clipboardData?.items
  if (!items) return
  const files: File[] = []
  for (let i = 0; i < items.length; i++) {
    const item = items[i]
    if (item.kind === 'file') {
      const f = item.getAsFile()
      if (f) files.push(f)
    }
  }
  if (!files.length) return
  // Screenshots pasted from clipboard have no meaningful name ("image.png");
  // give the upload a stable timestamped name so the bubble label reads well.
  const stamp = new Date()
  const pad = (n: number) => String(n).padStart(2, '0')
  const stampStr = `${stamp.getFullYear()}${pad(stamp.getMonth() + 1)}${pad(stamp.getDate())}-${pad(stamp.getHours())}${pad(stamp.getMinutes())}${pad(stamp.getSeconds())}`
  let imgIdx = 0
  e.preventDefault()
  for (const f of files) {
    let target = f
    if (/^image\//.test(f.type) && (!f.name || /^image\.png$/i.test(f.name))) {
      const ext = (f.type.split('/')[1] || 'png').replace('jpeg', 'jpg')
      imgIdx += 1
      target = new File([f], `${t('chat.imageBtn')}-${stampStr}${imgIdx > 1 ? '-' + imgIdx : ''}.${ext}`, { type: f.type })
    }
    await uploadNativeFile(target)
  }
}

// Translate backend upload errors into localized, human-readable messages.
// The backend tags rejects with an X-Error-Code header; fall back to the raw
// detail only for unknown failures.
function describeUploadError(e: Error & { code?: string }, f: File): string {
  switch (e.code) {
    case 'upload_type_not_allowed': {
      const ext = /\.[a-z0-9]+$/i.exec(f.name)?.[0] ?? f.name
      return t('chat.uploadUnsupportedType', { ext })
    }
    case 'upload_too_large': {
      // Backend detail is "<...> size of <N> bytes" — reuse it so the limit
      // always matches the server config.
      const m = /size of (\d+) bytes/.exec(e.message)
      const size = m ? formatFileSize(parseInt(m[1], 10)) : '100 MB'
      return t('chat.uploadTooLarge', { size })
    }
    case 'upload_content_mismatch':
      return t('chat.uploadContentMismatch')
    default:
      return `${t('chat.fileUploadFailed')} ${e.message}`
  }
}

// removeFile 已随 preview-bar 移除：附件现在以行内胶囊呈现在输入框里，
// 删除统一走 onComposerChipRemove（编辑器胶囊 × / 退格）。

// 消息里的附件是图片吗？后端会带回 mime；老数据（或上传响应）可能没写或只写了
// application/octet-stream，这时按扩展名兜底。SVG 明确排除：它走文本/文件通道，
// 且按文本 MIME 提供时放进 <img> 根本不渲染。
const IMAGE_THUMB_EXT_RE = /\.(png|jpe?g|gif|webp|bmp|avif)$/i
function isImageAttachment(file: Partial<sessionsApi.UploadedFile>): boolean {
  const mime = ((file as { mime?: string }).mime || '').toLowerCase()
  if (mime.startsWith('image/') && mime !== 'image/svg+xml') return true
  if (mime && mime !== 'application/octet-stream') return false
  return IMAGE_THUMB_EXT_RE.test(file.name || file.filename || '')
}

// 附件显示名。图片落库时若拿不到原始名（客户端没传、元数据也丢了），退回本地化
// 的「图片」——绝不把 uuid 晾在气泡上。
function attachmentLabel(file: Partial<sessionsApi.UploadedFile>): string {
  return file.name || file.filename || t('chat.imageBtn')
}

// ===== 消息附件预览 =====
// 点击气泡里的附件条目打开公共预览弹窗（FilePreviewDialog 走 props.url 的
// uploads 票据来源，编辑已禁用；预览/下载/新标签打开都在弹窗内完成）。
const showAttachmentPreview = ref(false)
const previewAttachment = ref<Partial<sessionsApi.UploadedFile> | null>(null)

function openAttachmentPreview(file: Partial<sessionsApi.UploadedFile>) {
  if (!file.url) return
  previewAttachment.value = file
  showAttachmentPreview.value = true
}

// Work directory functions
async function loadWorkDirHistory() {
  try {
    workDirHistory.value = await sessionsApi.listWorkDirHistory()
  } catch (e) {
    console.error('Failed to load work dir history:', e)
    workDirHistory.value = []
  }
}

// 设置/清空某个会话的工作目录，并同步进全量缓存：分组"归属哪个目录"由工作目录
// 决定，不同步的话侧栏要等下次刷新才会把会话挪进新分组。
async function setSessionWorkDir(id: string, dir: string): Promise<void> {
  await chatStore.updateSessionWorkDir(id, dir)
  patchCachedSession(id, { work_dir: dir, work_dir_user_set: dir !== '', work_dir_chosen: dir !== '' })
}

// 点击推荐目录直接应用为当前会话的工作目录。默认目录会话（未显式设置过）走这里：
// 直接改本会话目录，见 applyRecommendedDir 的说明。
async function applyRecommendedDir(path: string) {
  if (!chatStore.activeSessionId) return
  try {
    await setSessionWorkDir(chatStore.activeSessionId, path)
    showDirPicker.value = false
    message.success(t('chat.workDir') + ': ' + path)
  } catch (e: any) {
    message.error(e?.message || t('chat.workDirLocked'))
  }
}

// 用系统文件管理器（Windows 资源管理器）打开目录；不传 path 时打开当前会话工作目录
// 注意：该功能只能打开 go-magic 服务所在机器上的目录。通过域名/远程地址访问时，
// 服务端没有浏览器所在机器的桌面环境，无法打开，因此直接提示并复制路径。
function openWorkDirInExplorer(path?: string) {
  const target = path || chatStore.currentWorkDir
  if (!target) return
  const hostname = window.location.hostname
  const isLocal = hostname === 'localhost' || hostname === '127.0.0.1' || hostname === '[::1]' || hostname === '::1'
  if (!isLocal) {
    // 远程访问：服务端目录打不开，路径复制到剪贴板就行 —— 不必再回显一遍完整路径，
    // 那条 Chat_<时间>-<短ID> 的路径很长，气泡会被撑成两行。
    navigator.clipboard?.writeText(target).then(
      () => message.info(t('chat.workDirOpenRemote')),
      () => message.info(t('chat.workDirOpenRemote'))
    )
    return
  }
  sessionsApi.openFolderInExplorer(target).catch((e: any) => {
    // 后端失败时回的是 `HTTP 501: no file manager available on server (xdg-open not found); /path/…`
    // 这类英文长串 —— 状态码和路径都别往气泡上晾。按 HTTP 状态压成一句短提示，
    // 无状态码（网络层报错）才退回原始 message。
    const status = /^HTTP (\d{3})/.exec(String(e?.message || ''))?.[1]
    if (status === '501') {
      message.error(t('chat.workDirNoDesktop'))
      return
    }
    if (status === '404') {
      message.error(t('chat.workDirNotFound'))
      return
    }
    message.error(status ? t('chat.workDirOpenFailed') : e?.message || t('chat.workDirOpenFailed'))
  })
}

async function loadDirs(path?: string): Promise<boolean> {
  dirLoading.value = true
  try {
    const res = await sessionsApi.listDirs(path)
    dirCurrentPath.value = res.current
    dirEntries.value = res.dirs || []
    return true
  } catch (e) {
    console.error('Failed to list directories:', e)
    dirEntries.value = []
    return false
  } finally {
    dirLoading.value = false
  }
}

function navigateDir(path: string) {
  if (!path) return
  showNewFolderInput.value = false
  newFolderName.value = ''
  loadDirs(path)
}

function startNewFolder() {
  showNewFolderInput.value = true
  newFolderName.value = ''
  nextTick(() => {
    newFolderInputRef.value?.focus()
  })
}

function cancelNewFolder() {
  setTimeout(() => {
    showNewFolderInput.value = false
    newFolderName.value = ''
  }, 150)
}

async function createNewFolder() {
  const name = newFolderName.value.trim()
  if (!name) {
    showNewFolderInput.value = false
    return
  }
  try {
    await sessionsApi.createDir(dirCurrentPath.value, name)
    newFolderName.value = ''
    showNewFolderInput.value = false
    loadDirs(dirCurrentPath.value)
  } catch (e: any) {
    message.error(e?.message || t('common.operationFailed'))
  }
}

// ===== 工作目录的时机约束 =====
// 规则：目录只能在"发出第一条消息之前"设置一次；一旦有了消息，目录就固定，
// 此后只读展示。前后端同一口径——后端 PUT 以 messages 是否为空来判定
// （见 internal/server/sessions.go），前端这里只是把按钮收起来，避免用户
// 点出一个注定 409 的控件。
const canChooseWorkDir = computed(() =>
  !!chatStore.activeSessionId && !chatStore.activeSession?.message_count
)

// 消息正在飞行（流式/排队）时，"第一条"其实已经发出了，立刻禁掉入口，
// 否则这一轮的工作目录会和消息语义对不上。
const dirChoiceLocked = computed(() => chatStore.busy)

async function handleWorkDirMenu(key: string) {
  if (key === 'browse') {
    // 按钮本身只在未开始对话时渲染，这里再兜一层，防止定时器/快捷键绕过 UI
    if (!canChooseWorkDir.value) {
      message.info(t('chat.newDirLocked'))
      return
    }
    if (dirChoiceLocked.value) {
      message.info(t('chat.newDirLocked'))
      return
    }
    showDirPicker.value = true
    // 起点优先取配置页设置的全局工作目录（如 D:\workspace），而不是会话被自动创建
    // 的子目录 <全局>/chat/Chat_<时间>-<短ID>——后者会被误当成会话根目录。全局目录
    // 不存在或不可读时，再退回当前会话的工作目录。
    await Promise.all([loadWorkDirHistory(), ensureConfigLoaded()])
    const prefer = globalWorkDir.value
    if (prefer && (await loadDirs(prefer))) return
    if (prefer) {
      console.warn('Global working dir unreadable, falling back to session work dir:', prefer)
    }
    await loadDirs(chatStore.currentWorkDir || undefined)
  } else if (key === 'set') {
    if (!chatStore.activeSessionId || !dirCurrentPath.value) return
    if (dirChoiceLocked.value) {
      message.info(t('chat.newDirLocked'))
      return
    }
    try {
      await setSessionWorkDir(chatStore.activeSessionId, dirCurrentPath.value)
      showDirPicker.value = false
      message.success(t('chat.workDir') + ': ' + dirCurrentPath.value)
    } catch (e: any) {
      message.error(e?.message || t('chat.workDirLocked'))
    }
  }
}

// ===== 底部状态栏："按工作目录查看会话"面板 =====
// 数据源：/sessions/dir-groups（仅统计用户显式设置过工作目录的 web 会话，
// 按目录聚合、组内/组间按最近活动倒序）。面板每次打开都重新拉取保证最新。
//
// 但"有目录"不等于"值得展开"：系统给每个会话自动分配的目录是
// `<全局工作目录>/chat/<会话名>-<短ID>`，一个会话占一个目录，展开后永远只有它自己
//（面板里显示成"暂无其他会话使用该目录"）。所以这里的规则是：
//   - 用户自己选的目录（chosen）→ 路径可点，展开同目录会话面板；
//   - 系统分配的会话目录 → 路径不可点、不给下拉箭头，只保留"打开所在文件夹"
//     这一个真正有用的动作。分组视图里这类目录照常参与计数，但不参与"点开"。
const showDirSessions = ref(false)
const dirSessionsLoading = ref(false)
const dirGroups = ref<sessionsApi.SessionDirGroup[]>([])
const dirPanelMode = ref<'current' | 'all'>('current')
const dirSessAnchorRef = ref<HTMLElement>()
// 目录 key 归一化复用上方分组用的 dirKey（分隔符统一、去尾部斜杠、忽略大小写）

async function loadDirGroups(): Promise<void> {
  if (dirSessionsLoading.value) return
  dirSessionsLoading.value = true
  try {
    const res = await sessionsApi.getSessionDirGroups()
    dirGroups.value = res.groups || []
    dirGroupsLoaded.value = true
  } catch (e) {
    console.error('Failed to load session dir groups:', e)
    dirGroups.value = []
    // 失败不置 loaded：下次切换/新建时会再试一次，避免一次抖动把"会话目录"
    // 误判焊死到本次页面生命周期里。
  } finally {
    dirSessionsLoading.value = false
  }
}

// 目录分组只用于"这个目录值不值得展开"的判断，属于背景信息。
// 切会话 / 新建会话时预取一次，避免用户点目录时才现拉数据、底部栏先渲染成
// "默认目录"再跳成"可展开"（用户反馈的延迟感）。失败静默，不打扰用户。
const dirGroupsLoaded = ref(false)
const dirGroupsLoading = computed(() => !dirGroupsLoaded.value && !!dirSessionsLoading.value)
let dirGroupsPrefetchedAt = 0

function prefetchDirGroups(): void {
  if (dirSessionsLoading.value) return
  // 30s 内不重复拉：快速连点侧栏时不至于每个会话都打一次接口
  const now = Date.now()
  if (dirGroupsLoaded.value && now - dirGroupsPrefetchedAt < 30_000) return
  dirGroupsPrefetchedAt = now
  void loadDirGroups()
}

// 切换/新建会话后立刻预取一次（activeSessionId 每次变化都会触发）
watch(
  () => chatStore.activeSessionId,
  (id) => {
    if (id) prefetchDirGroups()
  },
  { immediate: true }
)

// 当前会话所在目录的"分组信息"（可能还没拉过 → undefined）
const currentDirGroup = computed(() => {
  const key = dirKey(chatStore.currentWorkDir || '')
  if (!key) return undefined
  return dirGroups.value.find(g => dirKey(g.dir) === key)
})

// 工作目录是否值得展开"同目录会话"面板。
// 先看当前会话自己的来源标记（chosen），再兜底看分组结论——
// 老会话可能 chosen=false 但目录其实是用户选的（历史数据），此时以分组为准。
const workDirExpandable = computed(() => {
  if (!chatStore.currentWorkDir) return false
  if (chatStore.currentWorkDirChosen) return true
  const g = currentDirGroup.value
  return !!g && !g.is_default_dir
})

// 打开/关闭面板：打开时刷新数据并默认落在"当前目录"视图
// （每次打开都重新拉取，避免新建/删除会话后目录分组过期）
async function toggleDirSessions() {
  if (showDirSessions.value) {
    closeDirSessions()
    return
  }
  showDirSessions.value = true
  dirPanelMode.value = 'current'
  await loadDirGroups()
  // 拉完数据才知道这个目录是不是"一组一会话"的会话目录：不可展开就直接收起来，
  // 免得弹出一个只有"暂无其他会话"的空面板（用户反馈的问题）。
  // 用户自己选的目录即使暂时只有自己，也保留面板（还能顺手在同目录新建）。
  if (!chatStore.currentWorkDirChosen && currentDirGroup.value?.is_default_dir) {
    showDirSessions.value = false
  }
}

// 点击面板外部或切换会话时关闭
function closeDirSessions() {
  if (!showDirSessions.value) return
  showDirSessions.value = false
  dirPanelMode.value = 'current'
}

function onDirSessDocMouseDown(e: MouseEvent) {
  const anchor = dirSessAnchorRef.value
  if (anchor && !anchor.contains(e.target as Node)) {
    closeDirSessions()
  }
}

watch(showDirSessions, (open) => {
  if (open) {
    document.addEventListener('mousedown', onDirSessDocMouseDown)
  } else {
    document.removeEventListener('mousedown', onDirSessDocMouseDown)
  }
})

// 组件卸载时兜底移除监听（防止面板打开状态切走路由导致泄漏）
onUnmounted(() => {
  document.removeEventListener('mousedown', onDirSessDocMouseDown)
})

// keep-alive 下"切走路由"不再触发 onUnmounted，上面那条兜底就管不到了：面板开着
// 切走时，document 上的外部点击监听会留在缓存实例上，在别的页面点一下就会去关一个
// 看不见的面板。离开页面时直接收起面板——watch 随之摘掉监听，回来时是干净的闭合态。
onDeactivated(() => {
  closeDirSessions()
})

// 与当前会话工作目录匹配的那一组会话
const currentDirSessions = computed(() => {
  const key = dirKey(chatStore.currentWorkDir || '')
  if (!key) return []
  const group = dirGroups.value.find(g => dirKey(g.dir) === key)
  return group?.sessions || []
})

// 点击面板中的会话：老会话先并入 store（同搜索命中逻辑），再切换过去
async function selectDirSession(s: sessionsApi.Session) {
  closeDirSessions()
  if (!chatStore.sessions.some(x => x.id === s.id)) {
    chatStore.mergeSessions([s])
  }
  await chatStore.selectSession(s.id)
}

// 在当前工作目录下新建会话：复用 createSession(workDir) 让新会话默认沿用该目录，
// 随后刷新目录分组并保留面板打开（方便继续新建/切换）。
// 「按工作目录查看会话」面板头上的「＋」：同样在该目录下新建并直接继承目录。
// 这里保留面板打开，方便用户在同目录里连续开几个会话。
async function createDirSession() {
  const dir = chatStore.currentWorkDir
  if (!dir) return
  const session = await chatStore.createSession(dir)
  if (session) {
    // 重新拉取目录分组，让新建的会话立即出现在"当前目录"列表里
    await loadDirGroups()
    sessionListRef.value?.scrollTo({ top: 0 })
  }
}

// 一键批量删除当前工作目录下的全部会话（仅删除"设置过该工作目录"的会话记录，
// 目录本身与用户文件保留；确认后逐个调用删除接口并统一刷新）。
async function deleteDirSessions() {
  const dir = chatStore.currentWorkDir
  const sessions = currentDirSessions.value
  if (!dir || !sessions.length) return
  // 破坏性批量操作：必须等用户点「确认」才开始删（confirmDialog 说明见上）
  const ok = await confirmDialog({
    title: t('chat.deleteDirSessions'),
    content: t('chat.deleteDirSessionsConfirm', { count: sessions.length, dir }),
    positiveText: t('chat.delete'),
    negativeText: t('common.cancel'),
  })
  if (!ok) return
  let failed = 0
  for (const s of sessions) {
    // 这些会话均由用户显式设置过工作目录 → 不删除目录/文件本身
    const deleteFiles = !s.work_dir_user_set
    if (!(await chatStore.deleteSession(s.id, deleteFiles))) failed++
  }
  if (failed > 0) {
    message.error(t('chat.deleteFailed'))
  } else {
    message.success(t('chat.deleteSessionDone'))
  }
  // 同步搜索候选缓存：被删除的会话立即从搜索结果中移除
  const ids = new Set(sessions.map(s => s.id))
  if (allWebSessions.value) {
    allWebSessions.value = allWebSessions.value.filter(s => !ids.has(s.id))
  }
  await chatStore.loadSessions()
  await loadDirGroups()
}

// 侧栏分组头上的「新建」：在该目录下开一个新会话（「默认」组不传目录，走系统默认）。
// 后端返回的 last_active 就是当前时间、且 work_dir_user_set=true，所以新会话必然
// 落在同一分组的第一条；同时分组按「最新活动」重排，该分组会跳到侧栏最顶部。
// 因此这里除了把折叠的组展开，还必须把滚动条拉回顶部——否则用户若是在侧栏偏下
// 的分组里新建，新会话跑到顶部后会被滚轴遮住看不到。
// 侧栏分组头上的「＋」：在**该分组对应的目录**下新建会话。目录随创建请求一起
// 提交，新会话一出生就带着 workdir_user_set/chosen，因此不会再在底部栏出现
// "设置目录"按钮——这正是"在某个目录里新建，就别让用户再选一次"。
// 「默认」组 row.workDir 为空，走系统默认目录分配。
async function createSessionInGroup(row: Extract<SidebarRow, { kind: 'group' }>) {
  const session = await chatStore.createSession(row.workDir || undefined)
  if (!session) {
    message.error(t('chat.createSessionFailed'))
    return
  }
  if (isGroupCollapsed(row.groupKey)) toggleGroup(row.groupKey)
  // 新会话所在分组已因最新活动跳至列表顶部，滚动定位到顶部让用户立刻看到它
  sessionListRef.value?.scrollTo({ top: 0 })
}

// 侧栏分组头上的「删除」：删除该分组下的全部会话。
// 目录分组只删会话记录（目录与用户文件保留）；「默认」分组的会话没有用户指定目录，
// 其临时工作目录随会话一起清掉（与单条删除的 deleteFiles 规则保持一致）。
async function deleteGroupSessions(row: Extract<SidebarRow, { kind: 'group' }>) {
  const group = sessionGroups.value.find(g => g.key === row.groupKey)
  if (!group || !group.sessions.length) return
  // 破坏性批量操作：必须等用户点「确认」才开始删（confirmDialog 说明见上）
  const ok = await confirmDialog({
    title: row.userSet ? t('chat.deleteDirSessions') : t('chat.deleteGroupSessions'),
    content: row.userSet
      ? t('chat.deleteDirSessionsConfirm', { count: group.sessions.length, dir: group.label })
      : t('chat.deleteGroupSessionsConfirm', { count: group.sessions.length, group: group.label }),
    positiveText: t('chat.delete'),
    negativeText: t('common.cancel'),
  })
  if (!ok) return
  let failed = 0
  for (const s of group.sessions) {
    if (!(await chatStore.deleteSession(s.id, !s.work_dir_user_set))) failed++
  }
  if (failed > 0) {
    message.error(t('chat.deleteFailed'))
  } else {
    message.success(t('chat.deleteSessionDone'))
  }
  // 同步侧栏数据源（分组由它派生，不清掉的话被删会话还会留在分组里）
  const ids = new Set(group.sessions.map(s => s.id))
  if (allWebSessions.value) {
    allWebSessions.value = allWebSessions.value.filter(s => !ids.has(s.id))
  }
  await chatStore.loadSessions()
  await loadDirGroups()
}

// 候选面板数据源。value 是点击后写入输入框的完整文本（如 "/skill foo "），
// label 是右侧/下方的补充说明（技能描述 / MCP 工具描述 / 命令用途）。
// group 用于分组渲染（对齐 WorkBuddy 的「技能 (N) / 指令 (N)」两段式面板），
// name 是展示用的短名（技能名 / 命令名），value 仍是点击后写入输入框的完整文本。
export interface ChatSuggestion {
  value: string
  label?: string
  group?: 'skills' | 'commands' | 'mcp'
  name?: string
}
const commandSuggestions = ref<ChatSuggestion[]>([])
const suggestionIndex = ref(0)

// 是否已有某条指令的胶囊（避免重复添加同一条）。
function hasPendingDirective(text: string): boolean {
  return pendingDirectives.value.some(d => d.text === text)
}
// 刚从面板/斜杠候选里"接受"下来的完整指令文本。用户一改输入就作废
// （见 handleComposerInput），用于让回车在"选完就能发"和"边打边补全"之间切换。
const acceptedSuggestion = ref('')

// buildSuggestions 解析当前输入决定候选。注意 `/skill`、`/mcp` 是"服务端展开型"
// 指令（后端把它们翻成注入模型的内容），所以一旦命令名完整就转入参数候选：
//   "/ski"            → 命令名补全（/skill）
//   "/skill "         → 技能候选
//   "/mcp "           → 服务器候选
//   "/mcp srv "       → 该服务器的工具候选（再往后是 JSON 参数，不再提示）
// 裸 "/" 阶段**技能 + 指令一起列**（技能在前）：选中技能会挂成胶囊（不污染输入框），
// 因此不会再出现"技能混进列表导致回车发不出去"的老问题——留着它用户才不用先记住
// `/skill` 才能发现技能。
function buildSuggestions(raw: string): ChatSuggestion[] {
  const text = raw.trimStart()
  if (!text.startsWith('/')) return []
  const hasArg = /\s/.test(text)
  const parts = text.split(/\s+/)
  const cmd = parts[0].toLowerCase()

  if (cmd === '/skill' || cmd === '/skills') {
    return chatStore
      .skillSuggestions(hasArg ? parts[1] || '' : '')
      .map(s => ({ ...s, group: 'skills' as const, name: s.value.trim().split(/\s+/)[1] || '' }))
  }
  if (cmd === '/mcp') {
    // 服务器/工具都是单段参数；两段之后是 JSON，交给用户自己写。
    const args = parts.slice(1)
    if (args.length > 2) return []
    return chatStore
      .mcpSuggestions(hasArg ? args : [])
      .map(s => ({ ...s, group: 'mcp' as const, name: s.value.trim().split(/\s+/).slice(1).join(' ') }))
  }

  // 命令名仍在输入中（"/"、"/ski"）：三段同列——技能 / 连接器 / 指令。
  // 每段列的都是"点一下就能用"的东西；`/skill`、`/mcp` 这两个**容器命令**不再作为
  // 候选出现（技能、连接器各自成段，它们只会重复一遍且点了还得再补参数）。
  const partial = text.slice(1).trim().toLowerCase()
  const out: ChatSuggestion[] = []
  for (const skill of skillPanelItems.value) {
    const n = skill.name.toLowerCase()
    if (!partial || n.startsWith(partial) || n.includes(partial)) {
      out.push({
        value: `/skill ${skill.name}`,
        label: skill.description,
        group: 'skills',
        name: skill.name,
      })
    }
    if (out.length >= 8) break
  }
  // 已连接的连接器：点一下进入该服务的工具候选（`/mcp <server> ` 交给输入框继续补参）。
  for (const server of chatStore.capabilities.mcp_servers) {
    const n = server.name.toLowerCase()
    if (!partial || n.startsWith(partial) || n.includes(partial)) {
      out.push({
        value: `/mcp ${server.name} `,
        label: t('chat.mcpToolsCount', { n: server.tools.length }),
        group: 'mcp',
        name: server.name,
      })
    }
  }
  for (const value of chatStore.autocompleteCommand(text)) {
    const name = value.slice(1).split(/\s+/)[0]
    // 容器命令不进「指令」段：技能/连接器已经各自成段（见上）。
    if (name === 'skill' || name === 'skills' || name === 'mcp') continue
    out.push({
      value: value + ' ',
      label: t(`chat.commands.${name}`),
      group: 'commands',
      name,
    })
  }
  return out
}

// 分组渲染：保持 commandSuggestions 的数组顺序（技能在前、指令在后），
// 这里只负责按 group 切段并给出段标题。空段不渲染。
const suggestionGroups = computed(() => {
  const defs: Array<{ key: NonNullable<ChatSuggestion['group']>; title: string }> = [
    { key: 'skills', title: t('chat.skills') },
    { key: 'mcp', title: t('chat.mcp') },
    { key: 'commands', title: t('chat.commandsGroup') },
  ]
  const groups: Array<{ key: NonNullable<ChatSuggestion['group']>; title: string; items: ChatSuggestion[] }> = []
  for (const def of defs) {
    const items = commandSuggestions.value.filter(s => s.group === def.key || (!s.group && def.key === 'commands'))
    if (items.length) groups.push({ key: def.key, title: def.title, items })
  }
  return groups
})

// 键盘高亮的对象形式：分组渲染后 DOM 按 suggestionGroups 输出，但索引仍对应
// commandSuggestions 的扁平顺序（两者顺序一致），用对象比对而不是下标。
const activeSuggestionItem = computed(() => commandSuggestions.value[suggestionIndex.value] || null)

function handleInput() {
  commandSuggestions.value = buildSuggestions(inputValue.value)
  suggestionIndex.value = 0
}

// selectSuggestion：点击/回车选中一条候选。分两种情况——
//   1. 候选是"完整指令"（`/skill code-review`、`/mcp github search`、`/undo`）
//      → 挂成胶囊，输入框清空；
//   2. 候选还只是"命令名 + 待补参数"（`/mcp `、`/skill `，或 `/ski` 这类补全）
//      → 写回输入框，等用户继续输入参数（此时仍走 handleEnter 的续写判断）。
function selectSuggestion(s: ChatSuggestion) {
  const value = s.value
  const isCommandNameOnly = /^\/\S+\s*$/.test(value)
  // `/mcp <server> ` 这类"服务器已定、工具未选"也还属于待补状态
  const needsMoreArgs = s.group === 'mcp' && value.trim().split(/\s+/).length < 3
  if (isCommandNameOnly || needsMoreArgs) {
    selectCommand(value)
    return
  }
  addDirectiveChip(s)
}

function selectCommand(value: string) {
  inputValue.value = value
  commandSuggestions.value = []
  suggestionIndex.value = 0
  // 记下"这条是选出来的"：紧接着按回车应当直接发送（见 handleEnter）。
  // 用户只要再动一次键盘/输入，handleComposerInput 就会把它清掉。
  acceptedSuggestion.value = value.trim()
}

// 候选面板方向键导航。上下移动高亮，Enter 选中；面板关闭时不拦截。
function moveSuggestion(delta: number) {
  const n = commandSuggestions.value.length
  if (!n) return
  if (delta === -1) suggestionIndex.value = suggestionIndex.value === 0 ? n - 1 : suggestionIndex.value - 1
  else suggestionIndex.value = (suggestionIndex.value + 1) % n
}

function acceptSuggestion() {
  const s = commandSuggestions.value[suggestionIndex.value]
  if (s) selectSuggestion(s)
}

// handleArrow：候选面板打开时上/下键在候选间移动（预览式：仅是高亮，
// 输入框内容不跟着变，避免用户按方向键改光标位置时被候选抢占）。
function handleArrow(delta: number) {
  if (!commandSuggestions.value.length) return
  moveSuggestion(delta)
}

// handleEnter：候选面板打开时回车= 选中当前候选；否则= 发送。
function handleEnter() {
  // 回车在候选面板打开时的语义要区分两种情况：
  // 1. 用户还在打命令名（候选 = 当前输入的"续写"）→ 选中候选；
  // 2. 用户已经在选中的指令后补了参数（如 `/skill code-review 查看下代码`）
  //    → 直接发送。此时若仍去"选中"，acceptSuggestion 会把候选 value 写回
  //    输入框，用户刚打的参数被整个吞掉，且永远发不出去（实测 2026-10-13）。
  const current = inputValue.value.trimEnd()
  const s = commandSuggestions.value[suggestionIndex.value]
  const selectable =
    !!s &&
    s.value.trimStart().startsWith(current) &&
    s.value.trimStart().length > current.length &&
    !isAcceptedSuggestion()
  if (commandSuggestions.value.length && selectable) {
    acceptSuggestion()
    return
  }
  commandSuggestions.value = []
  send()
}

// isAcceptedSuggestion：当前输入是否就是面板里刚选中的那一条完整指令
// （如 `/skill code-review`）。接受过就直接发，避免"选了技能却发不出去"。
function isAcceptedSuggestion(): boolean {
  const accepted = acceptedSuggestion.value
  return !!accepted && inputValue.value.trim() === accepted
}

// 引导（steer）：把排队中的这条内容注入正在运行的回合——模型在下一次 LLM
// 调用前看到它并调整方向，不排队、不打断当前生成。与 send 不同：引导只携带
// 纯文本（带附件的引导服务端会回落成普通排队消息），且不走命令解析。
// 引导成功后该条已不再需要排队，随即从队列移除，避免稍后又被重复执行。
async function handleGuide(turnId: string, content: string) {
  const text = content.trim()
  if (!text) return
  await chatStore.guideMessage(text)
  const sessionId = chatStore.activeSessionId
  if (!sessionId) return
  await chatStore.removeQueuedMessage(sessionId, turnId)
}

async function send() {
  const content = inputValue.value.trim()
  // 排队功能：回合进行中不再拦截发送——消息会进入服务端队列依次执行。
  // 只有真正空内容才忽略（提交失败会由 store 回滚本地占位并报错）。
  // 指令胶囊单独算"有东西可发"：只挂指令不打字也允许发（等价于裸指令）。
  if (!content && !selectedFiles.value.length && !pendingDirectives.value.length) return

  // 指令胶囊按输入框里的实际顺序还原成 `/skill x`、`/mcp srv tool` 指令行，
  // 正文接在后面（由编辑器按 DOM 顺序拼）。服务端逐行展开这些指令，
  // 所以多技能不会再出现"只有第一个生效"。
  const finalContent = composerRef.value?.getPayload() || content
  pendingDirectives.value = []

  // Handle commands - all logic (session create, execution, message push) handled by chatStore
  // 但"服务端展开型"指令（/skill、/mcp）例外：它们必须原样发给后端，由
  // expandChatDirectives 注入技能正文 / 真正的 MCP 调用结果。本地执行只会
  // 回一句占位文本，模型永远看不到（这正是要修的 bug）。
  if (chatStore.isCommand(finalContent) && !chatStore.isDirectiveCommand(finalContent)) {
    composerRef.value?.clear()
    inputValue.value = ''
    commandSuggestions.value = []
    await chatStore.executeCommand(finalContent)
    return
  }

  // Build content with file URL references for AI processing.
  // Raster image attachments are routed to the multimodal `images` channel so
  // the model actually sees the picture; everything else (including SVG,
  // which is XML text, not a raster image) falls through to the generic file
  // channel (the backend packages it as a text attachment).
  const allFiles = [...selectedFiles.value]
  const imageDataUrls: string[] = []
  const imageUrlRefs: string[] = []
  const imageNames: string[] = []
  // 图片条目本身（保持 selectedFiles 顺序）：最终附件列表 = 图片在前 + 非图片
  // 在后，与后端落库的 content parts 顺序一致（parseChatPayload 先处理 images
  // 再处理 files），这样本端乐观消息与刷新后的历史回放渲染顺序才能对上。
  const imageFileEntries: sessionsApi.UploadedFile[] = []
  const nonImageFiles: sessionsApi.UploadedFile[] = []

  function readAsDataURL(blob: Blob): Promise<string> {
    return new Promise((resolve, reject) => {
      const reader = new FileReader()
      reader.onload = () => resolve(reader.result as string)
      reader.onerror = reject
      reader.readAsDataURL(blob)
    })
  }

  // Downscale large raster images before sending. Multi-megabyte screenshots
  // waste the 16 MiB stream body and get re-sent on every turn; 2048px JPEG
  // keeps details visible while shrinking payloads ~10x. GIF is skipped
  // (canvas would drop animation) and small images are passed through.
  async function downscaleImage(file?: File): Promise<string | null> {
    if (!file) return null
    try {
      const bitmap = await createImageBitmap(file)
      const maxDim = 2048
      const scale = Math.min(1, maxDim / Math.max(bitmap.width, bitmap.height))
      if (scale >= 1 && file.size <= 3 * 1024 * 1024) return null // not worth re-encoding
      const canvas = document.createElement('canvas')
      canvas.width = Math.max(1, Math.round(bitmap.width * scale))
      canvas.height = Math.max(1, Math.round(bitmap.height * scale))
      const ctx = canvas.getContext('2d')
      if (!ctx) return null
      ctx.drawImage(bitmap, 0, 0, canvas.width, canvas.height)
      bitmap.close()
      return canvas.toDataURL('image/jpeg', 0.85)
    } catch (_e) {
      return null
    }
  }

  async function toImageDataUrl(f: sessionsApi.UploadedFile): Promise<string> {
    const native = f._native
    if (native && /image\/gif/i.test(native.type)) {
      return await readAsDataURL(native)
    }
    if (native && native.size <= 1.5 * 1024 * 1024) {
      return await readAsDataURL(native)
    }
    const downscaled = await downscaleImage(native)
    if (downscaled) return downscaled
    if (native) return await readAsDataURL(native)
    // No raw File (e.g. restored draft): fall back to fetching the upload
    // URL with credentials attached — a bare fetch would 401 behind auth.
    const { getAuthToken } = await import('@/api/client')
    const headers: Record<string, string> = {}
    const token = getAuthToken()
    if (token) headers['Authorization'] = `Bearer ${token}`
    const resp = await fetch(f.url, { headers })
    if (!resp.ok) throw new Error(`failed to fetch upload: ${resp.status}`)
    return await readAsDataURL(await resp.blob())
  }

  for (const file of allFiles) {
    const mime = (file as any).mime as string | undefined
    const looksLikeImage =
      (mime && mime.startsWith('image/') && mime !== 'image/svg+xml') ||
      /\.(png|jpe?g|gif|webp)(\?|$)/i.test(file.name)
    if (looksLikeImage) {
      try {
        const dataUrl = await toImageDataUrl(file)
        imageDataUrls.push(dataUrl)
        imageFileEntries.push(file)
        // Remember the uploaded path + original name so the backend can
        // persist a small reference (instead of megabytes of base64) that the
        // session replay can turn back into a thumbnail with a readable label.
        imageUrlRefs.push(file.url)
        imageNames.push(file.name)
        continue
      } catch (_e) {
        // fall through to non-image handling
      }
    }
    // No [attachment](url) markdown appended: the HTTP URL is meaningless to
    // the model (its tools are sandboxed to the workdir) and the file itself
    // travels via the `files` field of the stream request body.
    nonImageFiles.push(file)
  }

  // 编辑器在文件胶囊原位置留下的 `[[att:file:<uid>]]` 占位标记，这里改写成
  // `[[att:<n>]]`：n 是该附件在最终附件列表（图片在前、非图片在后）里的下标。
  // 标记随正文一起落库，渲染端（含刷新后的历史回放）据此把附件胶囊画回
  // 文字流原位置——否则"文字A 图片 文字B"会退化成"附件块 + 文字A 文字B"。
  const orderedFiles = [...imageFileEntries, ...nonImageFiles]
  let payloadContent = finalContent
  if (payloadContent.includes('[[att:file:')) {
    const orderedIndexByChipId = new Map<string, number>()
    orderedFiles.forEach((f, i) => {
      const chipId = `file:${(f as { _uid?: string })._uid || f.name || f.filename || ''}`
      orderedIndexByChipId.set(chipId, i)
    })
    payloadContent = payloadContent.replace(/\[\[att:(file:[^\]\s]+)\]\]/g, (_raw, id: string) => {
      const idx = orderedIndexByChipId.get(id)
      // 找不到对应附件（胶囊已被摘掉但标记残留）时把标记删干净，别让占位符漏到正文里
      return idx === undefined ? '' : `[[att:${idx}]]`
    })
  }

  // Drop references to uploaded files so the GC can reclaim the base64
  // data and uploaded URL closures as soon as the store has its own copy.
  composerRef.value?.clear()
  inputValue.value = ''
  selectedFiles.value = []
  commandSuggestions.value = []
  await chatStore.sendMessage(
    payloadContent,
    imageDataUrls.length ? imageDataUrls : undefined,
    nonImageFiles.length ? nonImageFiles : undefined,
    imageUrlRefs.length ? imageUrlRefs : undefined,
    imageNames.length ? imageNames : undefined,
    orderedFiles.length ? orderedFiles : undefined,
  )
}

function stopGeneration() {
  chatStore.stopGeneration()
}

async function createSession() {
  await chatStore.createSession()
  // store 已把新会话插入列表顶部，这里绝不能再调 loadSessions()：
  // 它会用后端第 1 页（20 条）整表替换列表并重置分页 offset，连续新建
  // 后侧栏永远只有 20 条，滚动加载也因此无从触发（表现为"超出隐藏、
  // 动一下窗口/刷新滚轴才出现"）。整表刷新交给手动刷新按钮。
  sessionListRef.value?.scrollTo({ top: 0 })
  // 移动端顶栏 + 新建后同样收起抽屉，直接进入新会话的聊天区
  if (isMobile.value) mobileSessionExpanded.value = false
}

async function deleteSession(id: string) {
  // 可能直接在搜索结果中删除尚未并入 store 的会话，回退到搜索缓存查找
  const session = chatStore.sessions.find(s => s.id === id)
    ?? allWebSessions.value?.find(s => s.id === id)
  const deleteFiles = !session?.work_dir_user_set
  // store 不再吞掉请求错误（只吞会让"到底删没删"无从判断），这里给出明确反馈
  const ok = await chatStore.deleteSession(id, deleteFiles)
  if (!ok) {
    message.error(t('chat.deleteFailed'))
  } else {
    message.success(t('chat.deleteSessionDone'))
  }
  await chatStore.loadSessions()
  // 同步全量缓存：被删除的会话立即从侧栏分组/搜索结果中消失
  removeCachedSession(id)
}

function openRenameModal(id: string) {
  // 搜索结果里可能选中尚未并入 store 的会话，回退到搜索缓存查找
  const session = chatStore.sessions.find(s => s.id === id)
    ?? allWebSessions.value?.find(s => s.id === id)
  if (!session) return
  renamingSessionId.value = id
  renameInput.value = session.title || ''
  showRenameModal.value = true
  // n-modal 内容懒挂载：必须等一帧，输入框实例才存在
  nextTick(() => {
    renameInputRef.value?.focus()
  })
}

function closeRenameModal() {
  showRenameModal.value = false
  renamingSessionId.value = null
  renameInput.value = ''
}

async function saveRename() {
  const id = renamingSessionId.value
  const name = renameTrimmed.value
  // 空标题或没改动：直接关闭，不发请求（后端不接受空标题）
  if (!id || !name || renameUnchanged.value) {
    closeRenameModal()
    return
  }
  renaming.value = true
  try {
    await chatStore.renameSession(id, name)
    // store 的 rename 是原地改对象、不会触发缓存同步 watcher（只监听数组引用），
    // 这里直接改缓存条目，保持侧栏标题与搜索结果即时更新
    patchCachedSession(id, { title: name })
    closeRenameModal()
  } finally {
    renaming.value = false
  }
}

// 会话项下拉菜单选项（重命名 / 删除）
function sessionMenuOptions(session: sessionsApi.Session) {
  const opts: any[] = []
  if (session.work_dir) {
    opts.push({
      label: t('chat.dirSessionsOpenFolder'),
      key: 'openFolder',
      icon: () => h(NIcon, null, { default: () => h(FolderOpenOutline) }),
    })
  }
  opts.push(
    {
      label: t('chat.rename'),
      key: 'rename',
      icon: () => h(NIcon, null, { default: () => h(PencilOutline) }),
    },
    {
      label: t('chat.deleteSession'),
      key: 'delete',
      icon: () => h(NIcon, null, { default: () => h(TrashOutline) }),
    }
  )
  return opts
}

async function onSessionMenuSelect(key: string, session: sessionsApi.Session) {
  if (key === 'openFolder') {
    openWorkDirInExplorer(session.work_dir)
  } else if (key === 'rename') {
    openRenameModal(session.id)
  } else if (key === 'delete') {
    // 删除前二次确认（confirmDialog 只有点「确认」才返回 true）
    const ok = await confirmDialog({
      title: t('chat.deleteSession'),
      content: t('chat.deleteSessionConfirm'),
      positiveText: t('chat.delete'),
      negativeText: t('common.cancel'),
    })
    if (ok) {
      await deleteSession(session.id)
    }
  }
}

async function selectSession(id: string) {
  await chatStore.selectSession(id)
}

// Load goals for a session
async function loadSessionGoals(sessionId: string) {
  // 等待 Vue 更新完成后再检查缓存
  await nextTick()
  if (sessionGoals.value[sessionId]) return
  try {
    sessionGoals.value[sessionId] = await sessionsApi.getSessionGoals(sessionId)
  } catch (e) {
    sessionGoals.value[sessionId] = []
  }
}

// Get goals for a session
function getSessionGoals(sessionId: string): sessionsApi.SessionGoal[] {
  return sessionGoals.value[sessionId] || []
}

// Navigate to goals page
function goToGoal(goalId: string) {
  router.push(`/goals/${goalId}`)
}

// Unlink session from goal
async function unlinkSessionGoal(goalId: string, sessionId: string, popoverKey: string) {
  try {
    console.log('Unlinking session:', goalId, sessionId)
    await goalsStore.unlinkSession(goalId, sessionId)
    console.log('Unlink successful')
    // 替换对象触发响应式更新
    popoverShow.value = { ...popoverShow.value, [popoverKey]: false }
    const newGoals = { ...sessionGoals.value }
    delete newGoals[sessionId]
    sessionGoals.value = newGoals
    await loadSessionGoals(sessionId)
    message.success(t('goals.unlinked'))
  } catch (e: any) {
    console.error('Unlink failed:', e)
    message.error(e?.message || t('common.operationFailed'))
  }
}

// 滚到底部。behavior 默认 smooth；会话切换等"换了一批消息"的场景用 auto，
// 免得几十屏的历史被平滑滚一遍。
function scrollToBottom(behavior: ScrollBehavior = 'smooth') {
  // 意图先落定：平滑滚动途中到达的新内容不该被算成"未读"
  stickBottom.value = true
  showJumpBottom.value = false
  unreadBelow.value = 0
  jumpScrolling = true
  if (jumpScrollTimer) clearTimeout(jumpScrollTimer)
  // 兜底解锁：平滑滚动被打断（内容高度突变等）时也可能收不到最终 scroll 事件
  jumpScrollTimer = setTimeout(endJumpScrolling, 1000)
  nextTick(() => {
    const el = messagesRef.value
    if (!el) return
    // 必须等 DOM patch 之后再量距离：若贴底跟随已经把视口对齐到底（流式进行中
    // 的常态），再滚一次只会让内容抖一下
    if (el.scrollHeight - el.scrollTop - el.clientHeight <= 1) {
      endJumpScrolling()
      return
    }
    programScrollAt = performance.now()
    el.scrollTo({ top: el.scrollHeight, behavior })
  })
}

function jumpToBottom() {
  scrollToBottom('smooth')
}

// Handle session list scroll - load more when scrolled to bottom
let loadMoreThrottleTimer: ReturnType<typeof setTimeout> | null = null
function handleSessionScroll(e: Event) {
  if (isSearching.value) return // 搜索模式候选集已全量，无需滚动加载更多
  if (loadMoreThrottleTimer) return
  // loadSessions 在飞时也不能分页：它会把 offset 归零，两边的 offset 会打架
  if (chatStore.sessionsLoading || chatStore.sessionsLoadingMore) return
  if (!chatStore.sessionsHasMore) return

  const target = e.target as HTMLDivElement
  const threshold = 100
  const isNearBottom = target.scrollHeight - target.scrollTop - target.clientHeight < threshold

  if (isNearBottom) {
    loadMoreThrottleTimer = setTimeout(() => {
      loadMoreThrottleTimer = null
      chatStore.loadMoreSessions()
    }, 200)
  }
}

onUnmounted(() => {
  if (searchDebounceTimer) {
    clearTimeout(searchDebounceTimer)
    searchDebounceTimer = null
  }
})

// 贴底才自动跟随；否则只累计未读数，由悬浮按钮提示（用户正在看历史，别拽他）
watch(() => chatStore.messages.length, (n, o) => {
  if (n <= o) return
  if (stickBottom.value) scrollToBottom()
  else unreadBelow.value += n - o
})
watch(() => chatStore.toolCalls.length, () => {
  // 新工具调用到达时：只要用户离底部不远（正在看底部工具区/已展开的工具），
  // 就自动贴底跟随，避免"展开工具拉到底、新工具又得手动滚一下"的反复摩擦。
  // 只有明确上翻到很靠上的历史（距底 > TOOL_FOLLOW_THRESHOLD）才不打扰，交给悬浮按钮。
  if (stickBottom.value) {
    followBottom()
  } else if (messagesBottomDistance() <= TOOL_FOLLOW_THRESHOLD) {
    stickBottom.value = true
    followBottom()
  }
})

// ===== 流式输出期间的贴底跟随 =====
// 流式文本是每 ~80ms 往 streamContent 里追加的，这期间 messages.length 不变，
// 只挂 messages.length 的话整段回答要等回合结束、消息落库那一下才被滚出来——
// 用户感知就是"内容都写完几秒了才拉到底"。这里让内容的每一次增长都触发贴底
// 跟随（rAF 合并，多路同时触发也只滚一帧）。
watch(() => chatStore.streamContent, () => followBottom())
watch(() => chatStore.streamingSegments, () => followBottom(), { deep: true })
watch(() => chatStore.taskProgress, () => followBottom())
// 回合收尾：流式气泡消失、历史消息接管，高度会变一次，补一次贴底
watch(() => chatStore.streaming, (v) => { if (!v) followBottom() })
// 新审批到达时强制滚动到底部（审批需要立刻看见、立刻处理，优先级高于"正在看历史"）
watch(() => chatStore.pendingApprovals.length, () => scrollToBottom())
// 切换会话=换了一批消息：重新贴底、清未读，用 auto 直接落到最新处
watch(() => chatStore.activeSessionId, () => {
  stickBottom.value = true
  unreadBelow.value = 0
  showJumpBottom.value = false
  scrollToBottom('auto')
})

// 审批快捷键：A=批准首个待审批，D=拒绝首个待审批。输入框聚焦时不响应。
function handleApprovalKeydown(e: KeyboardEvent) {
  const target = e.target as HTMLElement
  if (target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable)) {
    return
  }
  const pending = chatStore.activePendingApprovals
  if (pending.length === 0) return
  const key = e.key.toLowerCase()
  if (key === 'a') {
    e.preventDefault()
    const first = pending[0]
    if (first) chatStore.resolveChatApproval(chatStore.activeSessionId || '', first.id, true)
  } else if (key === 'd') {
    e.preventDefault()
    const first = pending[0]
    if (first) chatStore.resolveChatApproval(chatStore.activeSessionId || '', first.id, false)
  }
}
// 挂 window 而不是组件根元素：快捷键要在整页任意焦点位置都生效。
// 同样必须走 activated/deactivated——否则缓存实例的监听会在看板/设置页继续监听，
// 在那边按下 A/D 会直接批准/拒绝一条用户根本没看见的审批（且带 preventDefault，
// 吞掉目标页面的按键）。这是 keep-alive 下最需要防的一类串页行为。
onActivated(() => {
  window.addEventListener('keydown', handleApprovalKeydown)
})
onDeactivated(() => {
  window.removeEventListener('keydown', handleApprovalKeydown)
})
onUnmounted(() => {
  window.removeEventListener('keydown', handleApprovalKeydown)
})

// 监听关联变化，清空会话目标缓存
watch(() => goalsStore.linkVersion, () => {
  // 清空所有会话的目标缓存，下次鼠标悬停时会重新加载
  sessionGoals.value = {}
})

// Do NOT watch streamContent - the throttled buffer flush in chatStore handles updates

onMounted(async () => {
  await chatStore.loadSessions()
  modelsStore.loadModels()
  // 侧栏按工作目录分组需要全量会话（分页的 20 条会让分组残缺），挂载后补齐。
  // 放到首帧之后：会话库是单连接，补齐请求会和首屏首绘抢资源；先让上面那次
  // 分页把骨架换成真实内容，再在后台加量，观感上"有内容"的时刻会更早。
  setTimeout(() => { loadFullSessions() }, 0)
  // Bind scroll event for session list infinite scroll
  if (sessionListRef.value) {
    sessionListRef.value.addEventListener('scroll', handleSessionScroll)
  }
  // 技能 / 连接器 面板的收起点：点空白处、按 Esc。面板里的开关会把焦点抢走，
  // 输入框上的 keydown 收不到 Esc，所以必须再挂一层 document/window 监听。
  document.addEventListener('mousedown', closePanelsOnOutsideClick)
  window.addEventListener('keydown', closePanelsOnEsc)
})

onUnmounted(() => {
  document.removeEventListener('mousedown', closePanelsOnOutsideClick)
  window.removeEventListener('keydown', closePanelsOnEsc)
})

// 回访时的自愈刷新。加了 keep-alive 之后 onMounted 只在首次进入时跑一次，回访问不到
// 上面那段"挂载后补齐"，缓存就再没有刷新机会了（30s 新鲜窗口一旦过期就会永久用旧数据）。
// loadFullSessions 自带新鲜窗口判断，窗口内直接返回、不发请求，所以这里每次激活都调一次：
// 窗口内零开销，窗口外静默刷新——侧栏已有缓存内容在渲染，不会铺骨架、不会空白。
// 首次激活跳过，那一次仍走上面的延后补齐（避免与首屏分页抢数据库的单条连接）。
let keepAliveActivatedOnce = false
onActivated(() => {
  if (!keepAliveActivatedOnce) {
    keepAliveActivatedOnce = true
    return
  }
  loadFullSessions()
  // 模型/供应商配置可能在别处变化（设置页修改、外部直接编辑 config.json）：
  // keep-alive 下 onMounted 只跑一次，回访必须重新拉，否则下拉一直显示旧值。
  modelsStore.loadModels()
})
</script>

<style scoped>
.chat-container {
  display: flex;
  height: 100vh;
  min-height: 0;
}

/* ========== Sidebar ========== */
.session-sidebar {
  width: 240px;
  border-right: 1px solid #e0e0e0;
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
  background: #fff;
  height: 100vh;
  overflow: hidden;
  /* 底部"加载更多"指示条要绝对定位到列表底部（它不能进滚动流，否则改 scrollHeight）*/
  position: relative;
}

.sidebar-header {
  flex-shrink: 0;
  padding: 8px 10px;
  border-bottom: 1px solid #e0e0e0;
  height: auto;
  box-sizing: border-box;
  display: flex;
  align-items: center;
  gap: 6px;
}

.new-chat-btn {
  flex-shrink: 0;
  width: 34px;
}

/* 会话搜索框（与新建/刷新同一行） */
.sidebar-search-input {
  flex: 1;
  min-width: 0;
}

.sidebar-search-input .n-input {
  --n-height: 30px;
}

.session-list {
  flex: 1;
  overflow-y: auto;
  min-height: 0;
  /* 上下留一点呼吸，首/末行的圆角高亮不贴着表头和底部 */
  padding: 4px 0 6px;
  /* 侧栏只有 240px，通栏分隔线 + 贴边高亮在这么窄的列里显得很挤，所以行统一改成
     左右各内缩 8px 的圆角条目（见下方 .session-item / .session-group-head）。
     正因如此这里必须不保留滚动槽：经典（非 overlay）滚动条实占约 10px 宽度，
     会把右侧内缩顶成 18px、左右不对称，条目和分隔线也到不了该到的位置。
     滚轮/触摸照常可滚，每组还有「加载更多」分页兜底。两套写法覆盖新旧内核。 */
  scrollbar-width: none;
}

.session-list::-webkit-scrollbar {
  width: 0;
  height: 0;
}

/* 底部"加载更多"指示条：绝对定位覆盖在列表底部，不占布局高度。
   它的出现/消失会随每次分页请求发生；若参与布局（曾作为列表的最后一个子元素，
   高 66px）就会改 scrollHeight，浏览器随即钳制 scrollTop，
   在"滚到底部删除会话"时表现为整列跳一下、弹回、再跳一下的抖动。
   做成居中的白色小药丸：只遮住最后一行中间一小块，pointer-events:none 也不挡点击。 */
.session-list-loading {
  position: absolute;
  left: 0;
  right: 0;
  bottom: 8px;
  display: flex;
  justify-content: center;
  pointer-events: none;
}

.session-list-loading > * {
  background: #fff;
  border-radius: 999px;
  padding: 3px;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.14);
}

/* 会话骨架：外边距/内边距与 .session-item 对齐（2px+8px 外边距、8px+10px 内边距），
   骨架换成真实条目时列表总高度基本不变，不会产生位移。 */
.session-skeleton {
  padding: 4px 0 6px;
}

.session-skeleton-row {
  margin: 2px 8px;
  padding: 8px 10px;
  border-radius: 8px;
}

.session-skeleton-line {
  height: 10px;
  border-radius: 5px;
  background: #ececec;
  animation: session-skeleton-pulse 1.4s ease-in-out infinite;
}

.session-skeleton-line.sk-title {
  width: 62%;
}

.session-skeleton-line.sk-meta {
  width: 34%;
  height: 8px;
  margin-top: 7px;
}

@keyframes session-skeleton-pulse {
  0%, 100% { opacity: 1; }
  50% { opacity: 0.45; }
}

.profile-group-header {
  padding: 10px 16px 4px;
  font-size: 11px;
  font-weight: 600;
  color: #999;
  text-transform: uppercase;
  letter-spacing: 0.5px;
}

/* ===== 侧栏会话分组（按"是否设置过工作目录"） ===== */
.session-group-head {
  position: relative;
  display: flex;
  align-items: center;
  gap: 6px;
  /* 固定行高：悬停出现的操作按钮（22px）比文字徽标高，若参与布局会把行撑高，
     鼠标一进入整行就变高、下方内容跟着位移，看起来像在抖动。 */
  height: 30px;
  margin: 2px 8px;
  padding: 0 8px;
  box-sizing: border-box;
  cursor: pointer;
  background: #f2f2f2;
  border-radius: 8px;
  user-select: none;
  font-size: 12px;
  color: #666;
}

.session-group-head:hover {
  background: #e9e9e9;
}

.session-group-caret {
  flex-shrink: 0;
  color: #999;
  transition: transform 0.15s;
}

.session-group-head.collapsed .session-group-caret {
  transform: rotate(-90deg);
}

.session-group-icon {
  flex-shrink: 0;
  color: #999;
}

.session-group-head.group-dir .session-group-icon {
  color: #2080f0;
}

.session-group-label {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 数量徽标固定槽宽：始终占位，悬停时按钮落在同一位置，标题可用宽度全程不变 */
.session-group-count {
  flex-shrink: 0;
  width: 48px;
  text-align: right;
  font-size: 11px;
  color: #999;
}

/* 分组头快捷操作：绝对定位到右侧固定槽位，出现/消失不参与布局（行高与标题宽度都不变） */
.session-group-actions {
  position: absolute;
  right: 8px;
  top: 50%;
  transform: translateY(-50%);
  display: none;
  width: 48px; /* = 两个 22px 圆形按钮 + 2px 间距 */
  align-items: center;
  gap: 2px;
  justify-content: flex-end;
}

.session-group-head:hover .session-group-actions,
.session-group-head:focus-within .session-group-actions {
  display: flex;
}

/* 用 visibility 而非 display：保留占位，标题的省略号截断位置不会跳动 */
.session-group-head:hover .session-group-count,
.session-group-head:focus-within .session-group-count {
  visibility: hidden;
}

/* 触屏没有 hover：常驻显示操作按钮，否则分组头的新建/删除根本点不到 */
@media (hover: none) {
  .session-group-actions {
    display: flex;
  }
  .session-group-count {
    display: none;
  }
}

/* 组内"加载更多"：左对齐（不再居中），颜色取侧栏中性灰，
   避免蓝色主色在这种次要入口上过于抢眼 */
.session-group-more {
  margin: 2px 8px;
  padding: 6px 8px;
  border-radius: 8px;
  font-size: 12px;
  color: #888;
  text-align: left;
  cursor: pointer;
}

/* 有分组头时（组内会话缩进 22px），"加载更多"跟着缩进对齐会话标题；
   扁平列表（无分组头）则与普通会话条目一样贴 12px。 */
.session-group-more.in-group {
  padding-left: 22px;
}

.session-group-more:hover {
  background: #f0f0f0;
  color: #555;
}

/* 分组内的会话条目缩进一级，视觉上归属分组 */
.session-item.in-group {
  padding-left: 22px;
}

.session-item {
  margin: 2px 8px;
  padding: 8px 10px;
  border-radius: 8px;
  cursor: pointer;
  position: relative;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  transition: background 0.15s;
}

.session-item:hover { background: #f0f0f0; }
.session-item.active { background: #e8f5e9; }

.session-content {
  flex: 1;
  min-width: 0;
}

.session-title {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 14px;
  display: flex;
  align-items: center;
  gap: 4px;
}

.session-title-text {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.session-title-edit {
  flex: 1;
}

/* 重命名弹窗的宽度规则在文件末尾的全局 <style> 区（n-modal teleport 到 body，
   scoped 不生效），这里不再重复定义。 */

.session-meta {
  display: none;
}

.session-actions {
  display: flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
  opacity: 0;
  transition: opacity 0.2s;
}

.session-item:hover .session-actions {
  opacity: 1;
}

.session-menu-btn {
  opacity: 0.7;
}

.session-menu-btn:hover {
  opacity: 1;
}

/* ========== Chat Main ========== */
.chat-main {
  flex: 1;
  display: flex;
  flex-direction: column;
  min-width: 360px;
  background: #fff;
}

/* 滚动区的外壳：只用来给"回到底部"按钮提供定位上下文（滚动容器内部的
   absolute 会随内容滚动，所以必须包一层不滚动的父级） */
.messages-wrap {
  position: relative;
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.messages {
  flex: 1;
  overflow-y: auto;
  /* 避免消息内容(长代码/表格/长URL)横向溢出撑宽页面产生左右滚动条；
     长内容应在各自容器内局部滚动，而非撑破布局 */
  overflow-x: hidden;
  padding: 20px 24px;
  padding-bottom: 20px;
}

/* ========== 回到底部悬浮按钮 ========== */
.jump-bottom-btn {
  position: absolute;
  right: 28px;
  bottom: 16px;
  width: 36px;
  height: 36px;
  padding: 0;
  border-radius: 18px;
  display: flex;
  align-items: center;
  justify-content: center;
  border: 1px solid #e0e0e0;
  background: #fff;
  color: #4b5563;
  box-shadow: 0 2px 8px rgba(0, 0, 0, 0.12);
  cursor: pointer;
  z-index: 5;
  transition: background 0.15s ease, transform 0.15s ease;
}

.jump-bottom-btn:hover {
  background: #f5f5f5;
  color: #111827;
}

.jump-bottom-btn:active {
  transform: scale(0.94);
}

/* 未读角标：告诉用户"下面还有几条新消息"，而不是只给一个箭头 */
.jump-badge {
  position: absolute;
  top: -5px;
  right: -5px;
  min-width: 16px;
  height: 16px;
  padding: 0 4px;
  box-sizing: border-box;
  border-radius: 8px;
  background: #d03050;
  color: #fff;
  font-size: 10px;
  line-height: 16px;
  font-weight: 600;
  text-align: center;
}

.jump-fade-enter-active,
.jump-fade-leave-active {
  transition: opacity 0.18s ease, transform 0.18s ease;
}

.jump-fade-enter-from,
.jump-fade-leave-to {
  opacity: 0;
  transform: translateY(6px);
}

.empty-hint {
  padding: 40px;
  display: block;
  text-align: center;
}

/* ========== Message Layout ========== */
.message {
  display: flex;
  gap: 12px;
  margin-bottom: 20px;
  max-width: 900px;
  margin-left: auto;
  margin-right: auto;
  animation: fadeIn 0.3s ease;
}

@keyframes fadeIn {
  from { opacity: 0; transform: translateY(8px); }
  to { opacity: 1; transform: translateY(0); }
}

.message.user {
  flex-direction: row-reverse;
}

.message-body {
  max-width: 72%;
  min-width: 0;
}

/* Assistant 回答不使用气泡，撑满宽度 */
.message-body.assistant-body {
  max-width: calc(100% - 50px);
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
  font-size: 16px;
}

.user-avatar {
  background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
}

.bot-avatar {
  background: linear-gradient(135deg, #18a058 0%, #36ad6a 100%);
}

.tool-avatar {
  background: #f0f0f0;
}

/* ========== Message Bubbles ========== */
.message-bubble {
  padding: 14px 18px;
  border-radius: 16px;
  line-height: 1.75;
  word-break: break-word;
  overflow-wrap: break-word;
}

.user-bubble {
  background: linear-gradient(135deg, #18a058 0%, #20803a 100%);
  color: white;
  border-bottom-right-radius: 4px;
}

/* ---- 用户消息里的 /skill 技能引用胶囊 ----
   绿色气泡内做半透明白胶囊：图标+技能名成组，剩余文本跟在胶囊右侧。
   气泡本身是固定绿色渐变（不分主题），胶囊用 rgba 白即可两套主题通用。 */
/* 指令胶囊与正文同处一条文字流：胶囊用 inline-flex + vertical-align:middle，
   正文是 inline 的 pre-wrap 片段，所以「胶囊 胶囊 正文」会自然排在同一行，
   超出气泡宽度才换行（与 WorkBuddy 的技能引用一致）。 */
.user-directive-block {
  line-height: 1.85;
  word-break: break-word;
}
.user-skill-chip {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  vertical-align: middle;
  margin: 0 6px 2px 0;
  background: rgba(255, 255, 255, 0.22);
  border-radius: 999px;
  padding: 2px 10px 2px 8px;
  max-width: 100%;
}
.user-skill-chip-name {
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
/* 正文后面紧跟胶囊时补一点间距（胶囊自己只有右外边距） */
.user-skill-rest + .user-skill-chip {
  margin-left: 6px;
}
.user-skill-rest {
  min-width: 0;
  white-space: pre-wrap;
}
/* 行内附件胶囊（[[att:n]] 标记位置）：与指令胶囊同一条文字流，
   外观沿用 message-file-item 的「图标 + 文件名」语言但收窄成行内尺寸。 */
.user-att-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  vertical-align: middle;
  margin: 0 6px 2px 0;
  background: rgba(255, 255, 255, 0.22);
  border-radius: 999px;
  padding: 2px 10px 2px 8px;
  max-width: 100%;
  cursor: default;
}
.user-att-chip--clickable {
  cursor: pointer;
}
.user-skill-rest + .user-att-chip {
  margin-left: 6px;
}

/* ========== Queue dock（排队区）==========
   固定在输入框上方、消息滚动区之外。设计要点：
   - 与 task-dock-zone / clarify-bar / approval-bar 同一套"沉底 dock"范式，
     宽度与输入框对齐（max-width 900px 居中），视觉语言统一。
   - 每一行 = 拖动柄 + 单行截断内容 + 三个常驻图标（重发/编辑/删除），
     行间用发丝线分隔。刻意不做卡片、不加标题栏、不用序号圆标：排队区是
     "已提交内容的清单"，不是需要被强调的告警，越安静越不打扰正在读的回答。
   - 中性底色（不是绿色渐变）：这里的等待是常态，不是异常状态；
     绿色只留给"正在执行"的语义，两者同时出现时才不会互相稀释。 */
/* 回合执行状态行：贴着机器人头像、在回合内容之上，属于流式消息的
   一部分（不悬浮、不吸顶）。只有一行小字、不带图标——秒数在走 =
   回合仍被服务端执行，回应"是不是已经停了"的疑虑。 */
.turn-elapsed-line {
  margin: 2px 0 12px;
  font-size: 12px;
  color: var(--text-color-3, #999);
}

.queue-dock {
  padding: 4px 16px 0;
  background: var(--body-color, #fff);
  border-top: 1px solid var(--border-color, #efefef);
}

.queue-dock-inner {
  max-width: 900px;
  margin: 0 auto;
}

/* 多条排队时列表内部滚动，避免顶掉输入区。整块 dock 一起滚，
   行与行的分隔线因此不会在滚动时"悬空"在容器边缘。 */
.queue-dock-inner {
  max-height: 168px;
  overflow-y: auto;
}

/* 引导按钮（排队操作区，原"重试"位置）：把排队内容注入正在运行的回合直接
   发送。用 info 蓝区分于编辑/删除，hover 时高亮。 */
.queue-row-btn.queue-row-btn-guide {
  color: var(--primary-color, #18a058);
}
.queue-row-btn.queue-row-btn-guide:hover {
  background: rgba(24, 160, 88, 0.1);
  color: var(--primary-color, #18a058);
}

.queue-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 7px 0;
  /* 发丝分隔线：最后一行不画，避免与 dock 的 border-top 形成双线。 */
  border-bottom: 1px solid var(--border-color, #f0f0f0);
}

/* 发丝分隔线：最后一行不画，避免与 dock 的 border-top 形成双线。 */
.queue-row:last-child {
  border-bottom: none;
}

.queue-row-dragging {
  opacity: 0.45;
}

/* 拖动柄：2 列 × 3 行的小圆点阵列，用重复径向渐变画出来——
   ionicons 没有六点握把字形，画背景比塞一个额外的 SVG 元素更轻。 */
.queue-handle {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 14px;
  height: 20px;
  padding: 0;
  border: none;
  background: transparent;
  cursor: grab;
  color: var(--text-color-3, #c0c4cc);
  transition: color 0.15s;
}

.queue-handle:hover {
  color: var(--text-color-2, #888);
}

.queue-handle:active {
  cursor: grabbing;
}

.queue-handle-dots {
  width: 8px;
  height: 12px;
  background-image: radial-gradient(circle, currentColor 1.1px, transparent 1.2px);
  background-size: 4px 4px;
  background-position: 1px 1px;
  background-repeat: repeat;
}

.queue-row-main {
  flex: 1 1 auto;
  min-width: 0;
}

/* 单行截断：排队区只做"让你认出是哪条"，完整内容在编辑里可见 */
.queue-row-text {
  font-size: 13px;
  font-weight: 600;
  line-height: 1.5;
  color: var(--text-color, #1f2937);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.queue-row-files {
  display: flex;
  flex-wrap: wrap;
  gap: 5px;
  margin-top: 2px;
}

.queue-row-file {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  font-size: 11px;
  padding: 1px 6px;
  border-radius: 7px;
  background: rgba(0, 0, 0, 0.05);
  color: var(--text-color-2, #666);
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 操作按钮常驻（触屏无 hover，悬停才出现则永远点不到） */
.queue-row-actions {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  gap: 2px;
}

.queue-row-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 28px;
  height: 28px;
  padding: 0;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: var(--text-color-3, #8b8b8b);
  cursor: pointer;
  transition: background 0.15s, color 0.15s;
}

.queue-row-btn:hover {
  background: rgba(0, 0, 0, 0.06);
  color: var(--text-color, #333);
}

.queue-row-btn.queue-row-btn-danger:hover {
  background: rgba(208, 48, 80, 0.1);
  color: #d03050;
}

/* Assistant 回答不使用气泡，直接展示内容 */
.assistant-content {
  color: #1f2937;
  line-height: 1.75;
  word-break: break-word;
  overflow-wrap: break-word;
  /* 允许子元素在自身宽度内收缩，避免长内容(表格/代码)撑宽父容器 */
  min-width: 0;
}

/* 助手消息内的表格：允许在局部横向滚动，避免撑宽页面 */
.assistant-content :deep(table) {
  border-collapse: collapse;
  width: 100%;
  margin: 10px 0;
  font-size: 14px;
}
.assistant-content :deep(th),
.assistant-content :deep(td) {
  border: 1px solid #e0e0e0;
  padding: 8px 12px;
  text-align: left;
}

.tool-calls-wrap {
  display: flex;
  flex-direction: column;
  gap: 8px;
  margin-top: 10px;
  margin-bottom: 6px;
}

.system-bubble {
  background: linear-gradient(135deg, #fff7ed 0%, #fef3c7 100%);
  color: #854d0e;
  border: 1px solid #fcd34d;
  border-bottom-left-radius: 4px;
  font-size: 13px;
}

.system-avatar {
  background: #fef3c7;
  border: 1px solid #fcd34d;
}

.tool-bubble {
  background: transparent;
  border: none;
  border-bottom-left-radius: 4px;
  padding: 4px 0;
}

.tool-name-inline {
  font-weight: 500;
  font-size: 11px;
  color: #aaa;
  margin-bottom: 2px;
  font-family: 'SF Mono', 'Fira Code', monospace;
}

.tool-content-inline {
  font-size: 12px;
  color: #bbb;
  max-height: 120px;
  overflow-y: auto;
  white-space: pre-wrap;
  line-height: 1.5;
}

/* ========== Message Images ========== */
.message-images {
  margin-bottom: 8px;
}

/* ========== Image Preview Bar ========== */
.image-preview-bar {
  padding: 8px 24px;
  border-top: 1px solid #e0e0e0;
  background: #fafafa;
}

.preview-item {
  position: relative;
  display: inline-block;
}

.preview-remove {
  position: absolute;
  top: -6px;
  right: -6px;
  opacity: 0.8;
}

/* ========== Message Time ========== */
.message-time {
  font-size: 11px;
  color: #bbb;
  margin-top: 4px;
  padding: 0 4px;
}

.message.user .message-time {
  text-align: right;
}

/* ========== Tool Call Area ========== */
.tool-calls-inline {
  margin-bottom: 6px;
  display: flex;
  flex-wrap: wrap;
  gap: 2px;
  max-height: 60px;
  overflow: hidden;
}

/* ========== 长任务进度坞（内联在流式消息内容里） ========== */
.task-dock-inline {
  margin-top: 8px;
}

/* Agent status panel */
.agent-status-panel {
  padding: 14px 18px;
  background: #f5f5f5;
  border-radius: 16px;
  border-bottom-left-radius: 4px;
  min-width: 200px;
}

.status-header {
  display: flex;
  align-items: center;
  gap: 10px;
}

.status-spinner {
  width: 16px;
  height: 16px;
  border: 2px solid #d0d0d0;
  border-top-color: #666;
  border-radius: 50%;
  animation: spin 0.8s linear infinite;
  flex-shrink: 0;
}

@keyframes spin {
  to { transform: rotate(360deg); }
}

.status-phase {
  font-size: 13px;
  color: #333;
  font-weight: 500;
  flex: 1;
}

.status-hint {
  font-size: 12px;
  color: #888;
  margin-top: 8px;
  padding-left: 26px;
  animation: fadeInOut 4s ease-in-out infinite;
}

@keyframes fadeInOut {
  0%, 100% { opacity: 0.5; }
  50% { opacity: 1; }
}

/* ========== Markdown Content Styles ========== */
.message-bubble :deep(p) { margin: 0 0 10px 0; }
.message-bubble :deep(p:last-child) { margin-bottom: 0; }
.message-bubble :deep(ul), .message-bubble :deep(ol) { margin: 10px 0; padding-left: 28px; }
.message-bubble :deep(li) { margin: 5px 0; }
.message-bubble :deep(blockquote) {
  border-left: 3px solid #d0d0d0;
  padding-left: 14px;
  margin: 10px 0;
  color: #666;
}
.message-bubble :deep(table) {
  border-collapse: collapse;
  width: 100%;
  margin: 10px 0;
  font-size: 14px;
}
.message-bubble :deep(th), .message-bubble :deep(td) {
  border: 1px solid #e0e0e0;
  padding: 8px 12px;
  text-align: left;
}
.message-bubble :deep(th) {
  background: #f0f0f0;
  font-weight: 600;
}
.message-bubble :deep(h1),
.message-bubble :deep(h2),
.message-bubble :deep(h3),
.message-bubble :deep(h4) {
  margin: 18px 0 10px 0;
  font-weight: 600;
}
.message-bubble :deep(h1) { font-size: 20px; }
.message-bubble :deep(h2) { font-size: 18px; }
.message-bubble :deep(h3) { font-size: 16px; }
.message-bubble :deep(h4) { font-size: 15px; }
.message-bubble :deep(hr) {
  border: none;
  border-top: 1px solid #e0e0e0;
  margin: 14px 0;
}
.message-bubble :deep(pre) {
  margin: 10px 0;
}
.message-bubble :deep(a) {
  color: #e0f7e0;
  text-decoration: underline;
  font-weight: 600;
  text-shadow: 0 1px 2px rgba(0,0,0,0.3);
}
.message-bubble :deep(a:hover) {
  color: #ffffff;
}

/* ========== Code Block ========== */
.message-bubble :deep(.code-block) {
  position: relative;
  margin: 8px 0;
  border-radius: 8px;
  overflow: hidden;
  background: #1e1e1e;
}

.message-bubble :deep(.code-block pre) {
  margin: 0;
  padding: 12px 16px;
  overflow-x: auto;
}

.message-bubble :deep(.code-block code) {
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 13px;
  color: #d4d4d4;
}

/* 助手消息(无气泡)内的代码块同样需要横向滚动约束，否则长代码行会撑宽
   .assistant-content → .message-body → .messages → 页面，产生左右滚动条 */
.assistant-content :deep(.code-block) {
  position: relative;
  margin: 8px 0;
  border-radius: 8px;
  overflow: hidden;
  background: #1e1e1e;
  min-width: 0;
  max-width: 100%;
}
.assistant-content :deep(.code-block pre) {
  margin: 0;
  padding: 12px 16px;
  overflow-x: auto;
}
.assistant-content :deep(.code-block code) {
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 13px;
  color: #d4d4d4;
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
  font-size: 12px;
  cursor: pointer;
  opacity: 0;
  transition: opacity 0.2s, background 0.2s;
}

.message-bubble :deep(.code-block:hover .code-copy-btn) {
  opacity: 1;
}

.message-bubble :deep(.code-copy-btn:hover) {
  background: rgba(255, 255, 255, 0.2);
}

/* ========== Preview Bar ========== */
.preview-bar {
  padding: 8px 24px 0;
  background: #fff;
  max-width: 900px;
  margin-left: auto;
  margin-right: auto;
}

/* 底部固定审批栏 */
.approval-bar {
  padding: 8px 16px;
  background: linear-gradient(180deg, #fffdf5 0%, #fff 100%);
  border-top: 1px solid #f0e0c0;
  box-shadow: 0 -2px 8px rgba(240, 160, 32, 0.08);
}

.clarify-bar {
  padding: 8px 16px;
  background: linear-gradient(180deg, #fafaff 0%, #fff 100%);
  border-top: 1px solid #e2d9f7;
  box-shadow: 0 -2px 8px rgba(124, 77, 255, 0.06);
}

.clarify-bar-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: 320px;
  overflow-y: auto;
  max-width: 900px;
  margin-left: auto;
  margin-right: auto;
}

.approval-bar-list {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: 260px;
  overflow-y: auto;
  max-width: 900px;
  margin-left: auto;
  margin-right: auto;
}

.preview-item {
  position: relative;
  display: inline-block;
}

.preview-remove {
  position: absolute;
  top: -6px;
  right: -6px;
  min-width: 18px;
  min-height: 18px;
  padding: 0;
  line-height: 1;
  display: inline-flex;
  align-items: center;
  justify-content: center;
}

.file-preview {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  padding: 8px 12px;
  background: #f5f5f5;
  border-radius: 6px;
  border: 1px solid #e0e0e0;
}

.file-name {
  font-size: 12px;
  color: #333;
  max-width: 120px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* ========== ChatGPT-style Input Box ========== */
.input-area {
  padding: 12px 24px 16px;
  background: #fff;
  border-top: 1px solid #e0e0e0;
  flex-shrink: 0;
  /* 候选面板（.command-suggestions / .cap-panel）是 position:absolute + bottom:100%，
     必须有一个不滚动的定位祖先，否则会一路向上找/跑出视口 = 面板根本不出现。
     放在 .input-area 上是刻意的：它贴着输入框底部、且不是滚动容器（滚动的是
     .messages-wrap），面板才能稳定地在输入框正上方弹出。 */
  position: relative;
}

.chat-input-box {
  max-width: 900px;
  margin-left: auto;
  margin-right: auto;
}

/* Command suggestions dropdown */
/* 宽度对齐输入框（.chat-input-box 的 max-width:900px 居中）：
   面板铺满整个 input-area 会显得比对话内容还宽，观感失衡（用户反馈 2026-10-13）。 */
.command-suggestions {
  position: absolute;
  bottom: 100%;
  left: 50%;
  transform: translateX(-50%);
  width: min(900px, calc(100% - 48px));
  background: white;
  border: 1px solid #e0e0e0;
  border-radius: 8px 8px 0 0;
  box-shadow: 0 -4px 12px rgba(0, 0, 0, 0.1);
  max-height: 340px;
  overflow-y: auto;
  margin-bottom: -1px;
}

.suggestion-item {
  padding: 8px 16px;
  cursor: pointer;
  font-size: 14px;
  color: #333;
  border-bottom: 1px solid #f0f0f0;
  transition: background 0.15s;
  display: flex;
  align-items: baseline;
  gap: 10px;
}

/* 候选行右列：描述（技能说明 / MCP 工具说明 / 命令用途），过长时省略。
   富信息行（.suggestion-item-rich）会覆盖为两行截断。 */
.suggestion-label {
  font-size: 12px;
  color: #888;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.suggestion-item.active,
.suggestion-item:hover {
  background: #f5f5f5;
}

.suggestion-item:last-child {
  border-bottom: none;
}

/* ===== 分组式候选（对齐 WorkBuddy：技能 / 指令 两段）===== */
.suggestion-group-title {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 8px 14px 4px;
  font-size: 11px;
  color: #9ca3af;
  position: sticky;
  top: 0;
  background: white;
}

.suggestion-group-count {
  color: #c0c4cc;
}

/* 富信息行：图标头像 + 名称/描述 竖排 */
.suggestion-item-rich {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 14px;
}

.suggestion-avatar {
  flex: 0 0 auto;
  width: 22px;
  height: 22px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 6px;
  font-size: 11px;
  font-weight: 600;
  line-height: 1;
}

.suggestion-avatar.av-skills {
  background: #eef2ff;
  color: #4338ca;
}

.suggestion-avatar.av-commands,
.suggestion-avatar.av-mcp {
  background: #f1f2f4;
  color: #6b7280;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}

.suggestion-main {
  display: flex;
  flex-direction: column;
  min-width: 0;
  gap: 2px;
}

.suggestion-name {
  font-size: 13px;
  color: #1a1a1a;
}

.suggestion-item-rich .suggestion-label {
  white-space: normal;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  line-height: 1.4;
}

.chat-input-box {
  border: 1px solid #d9d9d9;
  border-radius: 12px;
  padding: 12px 16px;  background: #fff;
  transition: border-color 0.2s, box-shadow 0.2s;
}

.chat-input-box:focus-within {
  border-color: #18a058;
  box-shadow: 0 0 0 2px rgba(24, 160, 88, 0.15);
}

/* 输入框里的胶囊与文本样式由 ComposerEditor 自己维护（DOM 是 JS 创建的，
   ChatView 的 scoped 样式选不中它们，放在那边才不会出现"样式不生效"）。 */

.input-toolbar {
  display: flex;
  justify-content: space-between;
  align-items: center;
  margin-top: 8px;
  padding-top: 8px;
  border-top: 1px solid #f0f0f0;
  gap: 8px;
  min-width: 0;
}

/* 左侧操作区：单行横向滚动，窄屏下不会把工具栏撑成两行 */
.toolbar-scroll {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  flex: 1 1 auto;
  overflow-x: auto;
  scrollbar-width: none;
}

.toolbar-scroll::-webkit-scrollbar {
  display: none;
}

.toolbar-model-select {
  width: 220px;
}

.toolbar-btn {
  padding: 4px 8px;
}

/* 工具下拉里隐藏的上传入口：隐藏 n-upload 默认触发按钮，
   仅保留内部 file input（供 openFileDialog 触发文件选择）。 */
.toolbar-upload-hidden .n-upload-trigger,
.toolbar-upload-hidden .n-upload-trigger-button {
  display: none;
}

/* ========== 技能 / 连接器 二级面板 ========== */
/* 宽度与 .command-suggestions 一致（对齐 900px 输入框） */
.cap-panel {
  position: absolute;
  bottom: 100%;
  left: 50%;
  transform: translateX(-50%);
  width: min(900px, calc(100% - 48px));
  margin-bottom: 10px;
  display: flex;
  flex-direction: column;
  max-height: 380px;
  background: #fff;
  border: 1px solid #e5e7eb;
  border-radius: 14px;
  box-shadow: 0 12px 32px rgba(15, 23, 42, 0.14);
  overflow: hidden;
  z-index: 20;
}

.cap-panel-head {
  display: flex;
  align-items: baseline;
  gap: 8px;
  padding: 12px 14px 8px;
}

.cap-panel-title {
  font-size: 13px;
  font-weight: 600;
  color: #111827;
}

.cap-panel-hint {
  font-size: 11px;
  color: #9ca3af;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cap-panel-search {
  padding: 0 14px 8px;
}

.cap-panel-body {
  flex: 1 1 auto;
  overflow-y: auto;
  padding: 2px 6px 6px;
  min-height: 0;
}

.cap-panel-empty {
  padding: 22px 12px;
  text-align: center;
}

.cap-panel-empty-title {
  font-size: 13px;
  color: #6b7280;
}

.cap-panel-empty-hint {
  margin-top: 4px;
  font-size: 11px;
  color: #9ca3af;
}

/* 单行：头像 + 名称/描述 + 开关（或连接按钮） */
.cap-item {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 8px 10px;
  border: none;
  border-radius: 10px;
  background: transparent;
  text-align: left;
  cursor: pointer;
  transition: background 0.12s;
}

.cap-item:hover {
  background: #f5f7fa;
}

.cap-item-static {
  cursor: default;
}

.cap-item.is-off {
  opacity: 0.62;
}

/* 技能首字母头像（与 WorkBuddy 的列表观感一致：有"身份"的一列） */
.cap-item-avatar {
  flex: 0 0 auto;
  width: 24px;
  height: 24px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  border-radius: 7px;
  background: #eef2ff;
  color: #4338ca;
  font-size: 12px;
  font-weight: 600;
  line-height: 1;
}

.cap-item-avatar.is-off {
  background: #f1f2f4;
  color: #9ca3af;
}

.cap-item-main {
  display: flex;
  flex-direction: column;
  min-width: 0;
  flex: 1 1 auto;
  gap: 2px;
}

.cap-item-name {
  font-size: 13px;
  color: #111827;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cap-item-desc {
  font-size: 11px;
  color: #9ca3af;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cap-item-switch {
  flex: 0 0 auto;
}

/* 连接器面板：展开工具明细 */
.cap-expand-btn {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 22px;
  height: 22px;
  padding: 0;
  border: none;
  border-radius: 6px;
  background: transparent;
  color: #9ca3af;
  cursor: pointer;
  flex: 0 0 auto;
}

.cap-expand-btn:hover {
  background: #eef0f3;
  color: #4b5563;
}

.cap-tool-list {
  margin: 0 0 4px 26px;
  padding-left: 6px;
  border-left: 1px solid #eef0f2;
}

/* 插件首字母头像：与技能（靛蓝）、连接器（青）区分开，一眼能认出这是插件 */
.cap-item-avatar.av-plugin {
  background: #f5f3ff;
  color: #6d28d9;
}

/* 插件面板的行比技能行更紧凑：同样两行文字，靠 padding 与行距再省一点高度，
   一屏能多看到两三个插件（这一栏的用途就是"扫一眼有哪些"）。 */
.cap-panel-plugins .cap-item {
  padding: 5px 8px;
  gap: 8px;
}

.cap-panel-plugins .cap-item-main {
  gap: 1px;
}

.cap-panel-plugins .cap-item-avatar {
  width: 22px;
  height: 22px;
  border-radius: 6px;
  font-size: 11px;
}

/* 相邻插件之间一条极浅的分隔线：行高本来就矮，只靠间距分不清"这是另一个插件" */
.cap-plugin-group + .cap-plugin-group {
  margin-top: 2px;
  padding-top: 2px;
  border-top: 1px solid #f4f5f7;
}

.cap-plugin-version {
  margin-left: 5px;
  font-size: 10px;
  color: #b0b6bf;
  font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace;
}

/* "连接器 1/2 · CLI 2 项 · 技能 3 个"：跟在名字后面靠右，单行省略（细节在展开区） */
.cap-plugin-meta {
  margin-left: auto;
  flex: 0 1 auto;
  min-width: 0;
  padding-left: 6px;
  font-size: 11px;
  color: #b0b6bf;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 插件行的第一行：名字 + 版本 + 摘要同行，名字优先（摘要先被截） */
.cap-plugin-line {
  display: flex;
  align-items: baseline;
  min-width: 0;
}

.cap-plugin-name {
  flex: 0 1 auto;
  min-width: 0;
  font-size: 13px;
  color: #111827;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 插件行的第二行：描述或未就绪原因，恒为单行 */
.cap-plugin-sub {
  font-size: 11px;
  color: #9ca3af;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cap-plugin-sub.is-blocked {
  color: #d03050;
}

/* 展开区一行一条：图标 + 名字 + 状态，失败原因另起可悬停看全文 */
.cap-plugin-entry {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 5px 10px;
  min-width: 0;
}

.cap-plugin-entry-name {
  flex: 0 1 auto;
  max-width: 45%;
  font-size: 12px;
  color: #374151;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cap-plugin-entry-meta {
  flex: 1 1 auto;
  min-width: 0;
  font-size: 11px;
  color: #9ca3af;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.cap-plugin-examples {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 6px;
  padding: 6px 10px 2px;
}

.cap-plugin-examples-title {
  font-size: 11px;
  color: #9ca3af;
}

.cap-plugin-example {
  max-width: 100%;
  padding: 4px 8px;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
  background: #fff;
  font-size: 11px;
  color: #4b5563;
  text-align: left;
  cursor: pointer;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  transition: border-color 0.12s, background 0.12s, color 0.12s;
}

.cap-plugin-example:hover {
  border-color: #c7d2fe;
  background: #f8faff;
  color: #4338ca;
}

.cap-status-on {
  color: #16a34a;
}

.cap-status-off {
  color: #9ca3af;
}

/* 未连接服务器的失败原因：单行截断，悬停 title 看全文 */
.cap-item-error {
  display: block;
  margin-top: 2px;
  font-size: 11px;
  line-height: 1.4;
  color: #d03050;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  max-width: 100%;
}

.cap-dot {
  margin: 0 4px;
}

.cap-panel-foot {
  display: flex;
  align-items: center;
  gap: 4px;
  padding: 8px 10px;
  border-top: 1px solid #f1f2f4;
  background: #fafbfc;
}

.cap-foot-btn {
  display: inline-flex;
  align-items: center;
  gap: 5px;
  padding: 5px 8px;
  font-size: 12px;
  color: #4b5563;
  background: transparent;
  border: none;
  border-radius: 8px;
  cursor: pointer;
}

.cap-foot-btn:hover {
  background: #eef0f3;
  color: #111827;
}

.cap-foot-sep {
  width: 1px;
  height: 12px;
  background: #e5e7eb;
}

/* 面板里的隐藏上传入口（由「从本地添加技能」触发） */
.cap-upload-hidden .n-upload-trigger,
.cap-upload-hidden .n-upload-trigger-button {
  display: none;
}

/* 输入框折叠态：只显示一条细的展开栏 */

/* 历史备注：加号下拉菜单曾有一个「计划模式」行（type:'render' 自定义行），
   其 DOM 由 render 函数创建、挂在 naive-ui 组件（无 __scopeId）的渲染里，
   拿不到 data-v-xxx，scoped 规则对它一条都不生效，那样的行样式必须写在
   文件末尾的全局 <style> 区。该菜单行已于 2026-09-29 冻结移除。 */

.workdir-btn {
  display: flex;
  align-items: center;
  gap: 4px;
  max-width: 200px;
}

.workdir-label {
  font-size: 11px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.toolbar-right {
  display: flex;
  align-items: center;
}

.send-circle-btn {
  width: 32px;
  height: 32px;
}

/* ========== Dark Mode ========== */
@media (prefers-color-scheme: dark) {
  .session-sidebar {
    background: #fff;
    border-right-color: #333;
  }

  .session-item {
    border-bottom-color: #2a2a2a;
  }

  .session-item:hover { background: #252525; }
  .session-item.active { background: #1a3a1a; }

  .chat-main {
    background: #141414;
  }

  .jump-bottom-btn {
    background: #2a2a2a;
    border-color: #3a3a3a;
    color: #e5e7eb;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.4);
  }

  .jump-bottom-btn:hover {
    background: #333;
    color: #fff;
  }

  .assistant-content {
    color: #e5e7eb;
  }

  .system-bubble {
    background: #2a2410;
    color: #fbbf24;
    border: 1px solid #78350f;
  }

  .system-avatar {
    background: #2a2410;
    border: 1px solid #78350f;
  }

  .tool-bubble {
    background: transparent;
  }

  .agent-status-panel {
    background: #1e1e1e;
  }

  .status-spinner {
    border-color: #333;
    border-top-color: #888;
  }

  .status-phase {
    color: #ddd;
  }

  .status-hint {
    color: #666;
  }

  .message-time {
    color: #555;
  }

  .input-area {
    background: #1a1a1a;
    border-top-color: #333;
  }

  /* 技能胶囊（暗色） */
  .composer-chip {
    background: #2a2a2a;
    border-color: #3a3a3a;
  }
  .composer-chip-avatar {
    background: #312e81;
    color: #c7d2fe;
  }
  .composer-chip-name {
    color: #d1d5db;
  }
  .composer-chip-remove:hover {
    color: #d1d5db;
    background: #3a3a3a;
  }

  .approval-bar {
    background: linear-gradient(180deg, #2a2014 0%, #1e1e1e 100%);
    border-top-color: #4a3818;
  }

  .clarify-bar {
    background: linear-gradient(180deg, #221d2e 0%, #1e1e1e 100%);
    border-top-color: #40335c;
  }

  .chat-input-box {
    background: #2a2a2a;
    border-color: #444;
  }

  .chat-input-box:focus-within {
    border-color: #18a058;
    box-shadow: 0 0 0 2px rgba(24, 160, 88, 0.2);
  }

  .input-toolbar {
    border-top-color: #3a3a3a;
  }

  .workdir-bar {
    background: #161616;
    border-top-color: #333;
  }

  .workdir-bar-inner {
    color: #aaa;
  }

  .workdir-bar-path {
    color: #888;
  }

  /* 深色模式下的"默认目录"角标：浅色模式的 #f0f0f2 底在白字上会糊成一片 */
  .workdir-bar-badge {
    background: #2a2a2c;
    color: #888;
  }

  .workdir-bar-empty {
    color: #555;
  }

  .file-preview {
    background: #333;
    border-color: #444;
  }

  .file-name {
    color: #ddd;
  }

  .preview-bar {
    background: #1a1a1a;
  }

  .message-bubble :deep(th) {
    background: #252525;
  }

  .message-bubble :deep(th), .message-bubble :deep(td) {
    border-color: #333;
  }

  .message-bubble :deep(blockquote) {
    border-left-color: #444;
    color: #999;
  }

  .right-sidebar-fab {
    background: #2a2a2a;
    border-color: #444;
    color: #bbb;
  }
  .right-sidebar-fab:hover,
  .right-sidebar-fab:active,
  .right-sidebar-fab:focus {
    background: #333;
    color: #fff;
  }
}

/* ========== Message File Attachments ========== */
/* 全部附件统一「图标 + 文件名」chip（图片不再画缩略，点击弹窗原比例预览）。
   移动端要点：气泡很窄（message-body 72% + padding），所有附件条目必须能**收缩**
   —— flex 子项默认 min-width:auto，nowrap 的文件名会把自己的最小宽度撑到整串
   文字宽，chip 的 max-width 拦不住，文件名就溢出圆角框盖住气泡里的其他文字。
   因此每条 item 都要 min-width:0 + overflow:hidden，宽度上限用 min(固定值, 100%)。 */
.message-files {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  min-width: 0;
  margin-bottom: 8px;
}
/* 非图片附件：「图标 + 文件名」chip */
.message-file-item {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  max-width: min(260px, 100%);
  padding: 6px 10px;
  background: rgba(255, 255, 255, 0.15);
  border: 1px solid rgba(255, 255, 255, 0.25);
  border-radius: 8px;
  font-size: 13px;
  color: #fff;
  overflow: hidden;
}
.message-file-name {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
/* 有 url 的附件可点击预览：给手型与悬浮反馈，无 url（元数据丢失）保持普通态 */
.message-file-item--clickable {
  cursor: pointer;
  transition: background 0.15s ease;
}
.message-file-item--clickable:hover {
  background: rgba(255, 255, 255, 0.28);
}

/* Work directory picker */
.work-dir-label {
  font-size: 11px;
  max-width: 80px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  margin-left: 4px;
}

.dir-picker-recommended {
  padding: 4px 0 8px;
  border-bottom: 1px solid #f0f0f0;
  margin-bottom: 4px;
}

.dir-picker-recommended-title {
  font-size: 12px;
  color: #999;
  padding: 4px 12px 6px;
}

.dir-picker-recommended-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 12px;
  transition: background 0.15s;
}

.dir-picker-recommended-item:hover {
  background: #f0f0f0;
}

.dir-picker-recommended-path {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dir-picker-recommended-open {
  opacity: 0.55;
  flex-shrink: 0;
}

.dir-picker-recommended-open:hover {
  opacity: 1;
}

.dir-picker-breadcrumb {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 0;
  border-bottom: 1px solid #f0f0f0;
}

/* 目录选择器里的一次性提示条：解释"设置"= 切换本会话目录，而不是把它锁死。 */
.dir-picker-hint {
  display: flex;
  align-items: flex-start;
  gap: 6px;
  margin-top: 8px;
  padding: 6px 8px;
  border-radius: 6px;
  background: #f7f9fc;
  color: #7a8496;
  font-size: 12px;
  line-height: 1.5;
}

.dir-picker-hint .n-icon {
  margin-top: 2px;
  flex-shrink: 0;
}

.dir-picker-current {
  font-size: 12px;
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: monospace;
  color: #666;
}

.dir-picker-new-folder {
  padding: 8px 0;
  border-bottom: 1px solid #f0f0f0;
}

.dir-picker-list {
  max-height: 300px;
  overflow-y: auto;
  padding: 8px 0;
}

.dir-picker-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 8px 12px;
  border-radius: 4px;
  cursor: pointer;
  font-size: 13px;
  transition: background 0.15s;
}

.dir-picker-item:hover {
  background: #f0f0f0;
}

.dir-picker-empty,
.dir-picker-loading {
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 24px;
}

/* ========== Work Directory Bar（chat 最下层） ========== */
/* 底部紧凑状态栏：分身(只读) + 工作目录(只读) + 设置目录/打开文件夹 */
.workdir-bar {
  flex-shrink: 0;
  display: flex;
  align-items: center;
  min-width: 0;
  height: 30px;
  padding: 0 16px;
  background: #fafafa;
  border-top: 1px solid #e8e8e8;
  font-size: 12px;
  box-sizing: border-box;
}

.workdir-bar-inner {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  width: 100%;
  max-width: 900px;
  margin-left: auto;
  margin-right: auto;
  color: #555;
}

.workdir-bar-profile {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  flex-shrink: 0;
  color: #2080f0;
}

.workdir-bar-profile-name {
  font-weight: 500;
  max-width: 140px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.workdir-bar-path {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  flex: 1;
  min-width: 0;
  overflow: hidden;
  color: #888;
}

.workdir-bar-path-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 12px;
}

.workdir-bar-empty {
  color: #bbb;
}

/* 默认目录会话的角标：路径本身照常展示，需要一个弱标识告诉用户"这是系统按会话
   自动分配的目录，不是你选的"。 */
.workdir-bar-badge {
  flex-shrink: 0;
  padding: 0 5px;
  border-radius: 4px;
  background: #f0f0f2;
  color: #999;
  font-size: 10px;
  line-height: 15px;
  white-space: nowrap;
}

/* 未设置目录的占位文案：与路径文本同样禁止换行。此前漏了 nowrap —— 移动端
   窄屏下 flex 把这一项挤窄，"未设置（使用默认目录）"没有省略号兜底就折成
   两行，把 30px 高的底栏撑乱（默认/设置按钮/审批选择器全被顶得错位）。 */
.workdir-bar-empty-text {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.workdir-bar-set-btn {
  font-size: 11px;
  flex-shrink: 0;
  color: #2080f0;
}

/* 目录分组预取中的占位：只占位不解释，避免底栏出现"转圈"这种强提示 */
.workdir-bar-pending {
  flex-shrink: 0;
  opacity: 0.5;
}

/* 目录分组头上的"会话目录"标签：弱化的说明文字，不抢目录名 */
.dir-sess-group-tag {
  flex-shrink: 0;
  padding: 0 4px;
  border-radius: 3px;
  background: #f0f0f2;
  color: #999;
  font-size: 10px;
  line-height: 14px;
  white-space: nowrap;
}

/* 底部工具栏中的审批策略选择器：紧凑显示，避免文案过长，样式与工具栏一致且无外边框 */
.workdir-bar-approval {
  flex-shrink: 0;
  width: 96px;
  margin-left: 8px;
}
.workdir-bar-approval .n-base-selection {
  font-size: 12px;
  background: transparent;
  border: none;
  box-shadow: none;
}
.workdir-bar-approval .n-base-selection:hover {
  border: none;
  box-shadow: none;
}
.workdir-bar-approval .n-base-selection--active {
  border: none;
  box-shadow: none;
}
.workdir-bar-approval .n-base-selection-label {
  padding: 0 22px 0 8px;
  height: 24px;
  color: #555;
}
.workdir-bar-approval .n-base-selection-label span {
  /* 仅截断"触发框"内的标签（菜单 teleport 到 body，不受 scoped 样式影响），
     菜单内选项由下方全局样式放开，保证"手动审批"等完整展示 */
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.workdir-bar-approval .approval-short-label__text {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.workdir-bar-approval .n-base-selection-label .n-base-selection__arrow {
  color: #999;
}

/* 可点击的目录路径 + 按目录查看会话面板的展开锚点。
   注意：这里只是"看同目录还有哪些会话"，不提供切换目录的能力。 */
.dir-sess-anchor {
  position: relative;
  display: flex;
  flex: 1;
  min-width: 0;
  align-items: center;
}

.workdir-bar-path-link {
  cursor: pointer;
  color: #555;
}

.workdir-bar-path-link:hover {
  color: #2080f0;
}

.workdir-bar-caret {
  flex-shrink: 0;
  opacity: 0.6;
  margin-left: 2px;
  transition: transform 0.15s;
}

.dir-sess-panel {
  position: absolute;
  left: 0;
  bottom: calc(100% + 10px);
  width: min(430px, calc(100vw - 24px));
  max-height: min(58vh, 460px);
  display: flex;
  flex-direction: column;
  background: #fff;
  border: 1px solid #e8e8e8;
  border-radius: 10px;
  box-shadow: 0 8px 24px rgba(0, 0, 0, 0.12);
  z-index: 1200;
  overflow: hidden;
  font-size: 13px;
}

.dir-sess-panel-header {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 10px 12px;
  border-bottom: 1px solid #f0f0f0;
}

.dir-sess-new-btn {
  flex-shrink: 0;
  margin-left: auto;
}

.dir-sess-del-btn {
  flex-shrink: 0;
  margin-left: 2px;
}

.dir-sess-panel-folder {
  flex-shrink: 0;
  color: #2080f0;
}

.dir-sess-panel-title {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 12px;
  color: #555;
}

.dir-sess-loading {
  padding: 18px;
  display: flex;
  justify-content: center;
}

.dir-sess-list {
  overflow-y: auto;
  max-height: 300px;
  padding: 4px;
}

.dir-sess-item {
  padding: 8px 10px;
  border-radius: 8px;
  cursor: pointer;
}

.dir-sess-item:hover {
  background: #f5f7fa;
}

.dir-sess-item.active {
  background: #e8f5e9;
}

.dir-sess-title {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.dir-sess-title-text {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-size: 13px;
  font-weight: 500;
  color: #333;
}

.dir-sess-current {
  flex-shrink: 0;
  font-size: 11px;
  color: #18a058;
  background: #e8f5e9;
  border-radius: 4px;
  padding: 0 6px;
  line-height: 16px;
}

.dir-sess-meta {
  margin-top: 2px;
  font-size: 11px;
  color: #999;
}

.dir-sess-empty {
  padding: 18px;
  text-align: center;
  color: #aaa;
  font-size: 12px;
}

.dir-sess-panel-footer {
  border-top: 1px solid #f0f0f0;
  padding: 4px 8px;
  display: flex;
  justify-content: center;
  flex-shrink: 0;
}

.dir-sess-groups {
  overflow-y: auto;
  max-height: 340px;
}

.dir-sess-groups .dir-sess-list {
  max-height: none;
  padding: 0 4px 6px;
}

.dir-sess-group + .dir-sess-group {
  border-top: 1px solid #f5f5f5;
  padding-top: 4px;
}

.dir-sess-group-head {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 8px 10px 2px;
}

.dir-sess-group-folder {
  flex-shrink: 0;
  color: #888;
}

.dir-sess-group-dir {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 11.5px;
  color: #666;
  font-weight: 600;
}

.dir-sess-group-count {
  flex-shrink: 0;
  background: #f0f0f0;
  color: #888;
  font-size: 11px;
  border-radius: 8px;
  padding: 0 7px;
  line-height: 16px;
}

.right-sidebar-fab {
  display: none;
}

/* 移动端顶部工具条 - 桌面端隐藏（桌面侧栏常驻，没有抽屉态） */
.mobile-topbar {
  display: none;
}

/* Responsive: Mobile devices */
@media (max-width: 768px) {
  /* 目录会话面板：移动端把 absolute 覆盖成 fixed，钉到视口不占文档流。
     absolute 右溢会把文档 scrollWidth 撑超 100vw，进而污染 window.innerWidth/100vw
     （根因，见 <style> 块注释）；fixed 不占文档流、不撑宽、不产生横向滚动。
     底部 workdir-bar 即屏幕底部，面板向上弹，bottom 取 bar 上方约 40px 处浮起。 */
  .dir-sess-panel {
    position: fixed;
    left: 8px;
    right: 8px;
    bottom: 42px;
    width: auto;
    max-width: none;
    max-height: 60vh;
  }

  .chat-container {
    flex-direction: column;
    /* 顶部工具条常驻一行（会话标题 + 搜索/新建），其下为可展开的会话抽屉 */
    height: 100vh;
  }

  .session-sidebar {
    width: 100%;
    height: auto;
    /* 收起时抽屉里只有顶部工具条一行（44px + 1px 分隔线），48px 留一点余量 */
    max-height: 48px;
    border-right: none;
    border-bottom: 1px solid #e0e0e0;
    transition: max-height 0.3s ease;
    overflow: hidden;
  }

  .session-sidebar.mobile-expanded {
    max-height: 60vh;
  }
  
  .session-list {
    position: relative;
    top: 0;
    max-height: none;
    flex: 1;
  }

  /* 顶部工具条：标题 + 搜索/新建。收起时它就是抽屉的全部内容。
     position:relative 供标题做绝对居中（左右控件宽度不等，flex:1 居中会偏）。 */
  .mobile-topbar {
    position: relative;
    display: flex;
    align-items: center;
    min-height: 36px;
    padding: 3px 10px 3px 12px;
    box-sizing: border-box;
    flex-shrink: 0;
    cursor: pointer;
    user-select: none;
    -webkit-user-select: none;
    -webkit-tap-highlight-color: transparent;
  }

  /* 展开时与下方搜索行之间补一条发丝线（收起时侧栏自带 border-bottom，不叠加） */
  .session-sidebar.mobile-expanded .mobile-topbar {
    border-bottom: 1px solid #e0e0e0;
  }

  .mobile-topbar-caret {
    flex-shrink: 0;
    color: #999;
    transition: transform 0.3s ease;
  }

  .mobile-topbar-caret.open {
    transform: rotate(180deg);
  }

  /* 标题绝对居中：不随左侧箭头/右侧按钮的宽度差左右偏移。
     max-width 预留两侧控件空间（左箭头约 28px、右按钮组约 76px，各留余量），
     超长会话名省略号截断，不会压到两侧按钮。 */
  .mobile-topbar-title {
    position: absolute;
    left: 50%;
    top: 50%;
    transform: translate(-50%, -50%);
    max-width: calc(100% - 176px);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 14px;
    font-weight: 500;
    color: #333;
    text-align: center;
  }

  .mobile-topbar-actions {
    display: flex;
    align-items: center;
    gap: 2px;
    flex-shrink: 0;
    margin-left: auto; /* 标题改为绝对定位后，按钮组靠右对齐 */
  }

  .mobile-topbar-btn {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    padding: 0;
    border: none;
    background: transparent;
    border-radius: 8px;
    color: #555;
    cursor: pointer;
    -webkit-tap-highlight-color: transparent;
  }

  .mobile-topbar-btn:active {
    background: #ececec;
    color: #333;
  }

  /* 移动端屏幕窄，按钮往边角收一点，别压住消息正文 */
  .jump-bottom-btn {
    right: 12px;
    bottom: 12px;
  }

  .chat-main {
    flex: 1;
    min-height: 0;
    /* 覆盖桌面端 min-width:360px，避免窄屏(<360px)下撑宽页面产生横向滚动 */
    min-width: 0;
    width: 100%;
    max-width: 100%;
  }
  
  .messages {
    padding: 8px;
  }
  
  .message {
    gap: 6px;
  }
  
  .avatar {
    width: 28px;
    height: 28px;
    font-size: 14px;
  }
  
  .message-bubble {
    max-width: 85%;
    padding: 8px 10px;
    font-size: 14px;
  }
  
  .input-area {
    padding: 8px;
    padding-bottom: calc(8px + env(safe-area-inset-bottom));
  }

  /* 右侧边栏拉手：紧靠屏幕右边缘，一半嵌入边缘、一半露出（参考左侧会话手柄），
     几乎不遮挡聊天内容。竖条、左侧圆角，右侧与屏幕边缘齐平。 */
  .right-sidebar-fab {
    display: flex;
    align-items: center;
    justify-content: center;
    position: fixed;
    right: 0;
    top: 50%;
    transform: translate(50%, -50%);
    width: 34px;
    height: 42px;
    z-index: 1000;
    background: #fff;
    border: 1px solid #e0e0e0;
    border-right: none;
    border-radius: 10px;
    box-shadow: -2px 0 8px rgba(0, 0, 0, 0.12);
    color: #666;
    cursor: pointer;
    transition: right 0.3s ease;
  }

  .right-sidebar-fab:hover,
  .right-sidebar-fab:active,
  .right-sidebar-fab:focus {
    background: #f7f7f7;
    color: #333;
  }

  /* 右侧面板展开时，拉手跟随到面板左边缘（面板宽 280px），保持半嵌入姿态 */
  .right-sidebar-fab.open {
    right: 280px;
  }

  /* 移动端底部工具栏：压缩审批策略选择器，避免挤压工作目录/分身名 */
  .workdir-bar {
    padding: 0 8px;
  }
  .workdir-bar-approval {
    width: 88px;
    margin-left: 6px;
  }
  .workdir-bar-profile-name {
    max-width: 76px;
  }
}
</style>

<!--
  移动端 naive 弒层遮挡修复（真机问题：模型下拉左裁 / 目录面板右裁）。

  模型下拉（n-select，teleport 到 body）：主方案是 onModelMenuShow 按触发器位置动态算
  max-width 并经 menu-props.style 以 inline 写进菜单（375px 实测 left=61/right=356 完全屏内）；
  CSS 只是 JS 未执行时的兜底。为什么纯 CSS 不够：naive follower 放不下时 flip 到 bottom-end
  （右缘对齐触发器），可用空间取决于触发器位置（动态值），静态钳制值（如 100vw-16/40px）
  在触发器贴近屏幕中/右时必然左溢（375 实测 left=-4/-26）。曾试过 :has() 选择器——X5/XWeb
  等旧内核不支持，规则全灭，禁用。

  目录会话面板（.dir-sess-panel）：桌面端 absolute 锚在目录文字下方向上弹（anchor 够宽，
  正常不溢）。移动端是根因——absolute 右溢出把文档 scrollWidth 撑超 100vw（375 视口实测
  scrollW=404），进一步污染 window.innerWidth/100vw 导致所有基于视口的测量错乱。修复：移动端
  （@media max-width:768px）把面板改 position:fixed + left/right:8px 钉到视口，fixed 不占文档流、
  不撑宽、不产生横向滚动，scrollW 恢复 375、面板完整落屏内（无需 JS translateX 校正）。
-->
<style>
/* 会话重命名弹窗：n-modal 会被 teleport 到 body，身上没有 data-v-xxx，
   scoped 规则一条都不生效（曾导致 width:460px 失效、卡片被撑满整屏）。
   必须写在全局区。 */
.session-rename-modal {
  width: 460px;
  max-width: 94vw;
}

.model-switch-menu {
  max-width: min(calc(100vw - 88px), 660px);
}

/* 加号下拉里的「技能 / 连接器」行：左标题 + 右计数徽标。
   这类行由 render-label 函数生成、挂在 naive-ui 的渲染树里（无 __scopeId），
   scoped 规则对它一条都不生效，必须写在全局区。 */
.toolbar-menu-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 24px;
  min-width: 150px;
}

.toolbar-menu-label {
  flex: 1 1 auto;
}

.toolbar-menu-badge {
  flex: 0 0 auto;
  font-size: 11px;
  color: #9ca3af;
}

@media (max-width: 768px) {
  .n-base-select-menu,
  .n-popselect-menu,
  .n-dropdown-menu,
  .n-popover:not(.n-popover--raw) {
    max-width: calc(100vw - 40px);
  }
}

.model-switch-menu.n-base-select-menu .n-base-select-option .n-base-select-option__content {
  white-space: normal;
  overflow-wrap: anywhere;
  text-overflow: clip;
}

/* popselect 复用 NInternalSelectMenu，选项同样是 n-base-select-option__content；
   4 类选择器（0,4,0）压过 naive 的 nowrap 原规则（0,3,0） */
.n-popselect-menu.n-base-select-menu .n-base-select-option .n-base-select-option__content {
  white-space: normal;
  overflow-wrap: anywhere;
  text-overflow: clip;
}

/* 审批策略下拉（chat 工具条）：菜单 teleport 到 body，scoped 样式够不到，
   在这里按 approval-select-menu class 精确放开选项截断，保证"手动审批 -
   所有命令需确认"这类长文案完整展示。render-label 的内联截断样式同时挂在
   触发框和菜单选项上，触发框需要保留截断，菜单侧用 !important 覆盖（> 内联）。 */
.approval-select-menu.n-base-select-menu {
  max-width: calc(100vw - 24px);
}
.approval-select-menu .n-base-select-option .n-base-select-option__content {
  white-space: nowrap !important;
  overflow: visible !important;
  text-overflow: clip !important;
}
.approval-select-menu .n-base-select-option .approval-short-label__text {
  overflow: visible !important;
  text-overflow: clip !important;
}

/* 加号下拉菜单里的「计划模式」行已于 2026-09-29 冻结并全量删除，对应的
   .plan-mode-menu-item / .plan-mode-menu-icon / .plan-mode-menu-label 样式一并删除。
   若日后恢复该菜单项：它由 render 函数创建、菜单 teleport 到 body，身上没有
   data-v-xxx，样式必须写在全局区（不能放 <style scoped>）。 */
</style>