import { defineStore } from 'pinia'
import { ref } from 'vue'
import * as roomsApi from '@/api/rooms'
import type { Room, RoomMessage, RoomSendResult } from '@/api/rooms'

export const useRoomsStore = defineStore('rooms', () => {
  const rooms = ref<Room[]>([])
  const activeRoomId = ref<string | null>(null)
  const messages = ref<RoomMessage[]>([])
  const loading = ref(false)
  const sending = ref(false)

  // AbortController for the in-flight blocking round so the user can cancel.
  let sendAbort: AbortController | null = null

  // Live-progress polling for the in-flight round. The backend runs members
  // one after another and only returns from /send when the whole round is
  // done — minutes later — so without polling the dashboard looks frozen
  // exactly while the bots are working.
  const POLL_INTERVAL_MS = 2000

  function isAbortError(e: unknown): boolean {
    return e instanceof DOMException && e.name === 'AbortError'
  }

  function delay(ms: number): Promise<void> {
    return new Promise(resolve => setTimeout(resolve, ms))
  }

  async function loadRooms(): Promise<void> {
    loading.value = true
    try {
      rooms.value = await roomsApi.getRooms()
    } catch {
      rooms.value = []
    } finally {
      loading.value = false
    }
  }

  async function createRoom(data: Partial<Room>): Promise<Room> {
    const room = await roomsApi.createRoom(data)
    rooms.value.push(room)
    return room
  }

  async function selectRoom(id: string): Promise<void> {
    activeRoomId.value = id
    // Clear previous room messages first so switching never flashes stale chat.
    messages.value = []
    loading.value = true
    try {
      const [msgs] = await Promise.all([roomsApi.getRoomMessages(id)])
      messages.value = msgs
    } catch {
      messages.value = []
    } finally {
      loading.value = false
    }
  }

  function getActiveRoom(): Room | null {
    if (!activeRoomId.value) return null
    return rooms.value.find(r => r.id === activeRoomId.value) || null
  }

  /**
   * Deliver a message to the room. The backend blocks until the coordinated
   * round finishes, so the user message is optimistically appended locally and
   * replaced/kept by the authoritative results afterwards.
   *
   * Because a round is serial over its members (each with its own turn
   * budget), we ALSO poll the room log while it runs — see watchRound. That is
   * what makes members show up one by one instead of the chat appearing to
   * hang for minutes.
   *
   * payload carries uploaded attachment refs (vision/files channels, same
   * shape as the bot chat payload).
   */
  async function sendMessage(
    message: string,
    target?: string,
    payload?: roomsApi.RoomSendPayload,
    attachments?: roomsApi.RoomAttachment[]
  ): Promise<RoomSendResult | null> {
    if (!activeRoomId.value || sending.value) return null
    const roomId = activeRoomId.value
    sending.value = true
    sendAbort = new AbortController()

    const localId = 'local_' + Date.now()
    messages.value.push({
      id: localId,
      from: '@user',
      content: message,
      timestamp: Date.now(),
      attachments,
    })
    // 回合期间持续拉取房间日志（乐观气泡在服务端回传该条消息后自动让位）。
    let watching = true
    const probe = { id: localId, text: message }
    void (async () => {
      while (watching) {
        await delay(POLL_INTERVAL_MS)
        if (!watching) return
        try {
          const [msgs, status] = await Promise.all([
            roomsApi.getRoomMessages(roomId),
            roomsApi.getRoomStatus(roomId),
          ])
          if (!watching) return
          if (activeRoomId.value === roomId) applyServerMessages(msgs, probe)
          // 阻塞的 POST 已经返回时由它的收尾逻辑负责；否则 running=false
          // 说明这一轮跑完了 —— 但 POST 还在路上，交给它做最终合并。
          if (!status.running) return
        } catch {
          /* 轮询失败不打断回合，下一拍重试 */
        }
      }
    })()

    try {
      const res = await roomsApi.sendRoomMessage(roomId, message, target, sendAbort.signal, payload)
      messages.value = messages.value.filter(m => m.id !== localId)
      for (const m of res.messages) {
        if (!messages.value.some(x => x.id === m.id)) {
          messages.value.push(m)
        }
      }
      return res
    } catch (e) {
      if (isAbortError(e)) {
        // 我们本地不等了（取消，或等满这次调用的上限），但后端那一轮通常
        // 还在跑：保留已经轮询到的成员回复，继续盯到这一轮真正结束，而不是
        // 把用户的这条消息打成错误。
        // 先停掉上面那个轮询协程，否则两个循环会各拉一份房间日志。
        watching = false
        await watchUntilRoundDone(roomId, probe)
        return null
      }
      // On failure, replace the optimistic bubble with a visible error marker.
      messages.value = messages.value.map(m =>
        m.id === localId ? { ...m, content: `⚠️ ${m.content}` } : m
      )
      throw e
    } finally {
      watching = false
      sendAbort = null
      sending.value = false
    }
  }

  /**
   * Merge a server room log into the current list. The optimistic bubble is
   * kept (appended last, where it belongs — nothing else can have arrived
   * before the server echoes it) until the server echoes that same message.
   */
  function applyServerMessages(incoming: RoomMessage[], probe: { id: string; text: string }): void {
    const echoed = incoming.some(m => isUserMessage(m) && m.content === probe.text)
    if (echoed) {
      messages.value = incoming
      return
    }
    const local = messages.value.find(m => m.id === probe.id)
    messages.value = local ? [...incoming, local] : incoming
  }

  function isUserMessage(m: RoomMessage): boolean {
    // Same predicate BotsView uses to render the human's own bubbles.
    return m.from === '@user' || m.from === 'user' || m.from.startsWith('user:')
  }

  /**
   * Poll until this room's round is no longer running. Used after we stopped
   * waiting on the blocking send so the reply of the last member still lands
   * in the UI.
   */
  async function watchUntilRoundDone(roomId: string, probe: { id: string; text: string }): Promise<void> {
    const deadline = Date.now() + 30 * 60 * 1000
    while (Date.now() < deadline) {
      try {
        const [msgs, status] = await Promise.all([
          roomsApi.getRoomMessages(roomId),
          roomsApi.getRoomStatus(roomId),
        ])
        if (activeRoomId.value === roomId) applyServerMessages(msgs, probe)
        if (!status.running) return
      } catch {
        /* keep polling */
      }
      await delay(POLL_INTERVAL_MS)
    }
  }

  function cancelSend(): void {
    sendAbort?.abort()
  }

  /**
   * Stop the in-flight round for real: tell the backend to abort (remaining
   * members are skipped and the currently speaking turn is canceled), then
   * drop the local wait on the blocking send. cancelSend() alone only tears
   * down the fetch — the round kept running server-side, which is exactly
   * the "群聊没法停止" trap. Polling continues until the backend reports the
   * round is really over, so partial replies still land in the UI.
   */
  async function stopRound(): Promise<void> {
    const roomId = activeRoomId.value
    cancelSend()
    if (!roomId) return
    try {
      await roomsApi.stopRoomRound(roomId)
    } catch {
      /* 本地已不再等待；后端不可达时停止请求本身也无法送达 */
    }
  }

  async function refreshMessages(): Promise<void> {
    if (!activeRoomId.value) return
    try {
      messages.value = await roomsApi.getRoomMessages(activeRoomId.value)
    } catch {
      /* keep current messages */
    }
  }

  async function updateRoom(roomId: string, data: Partial<Room>): Promise<Room> {
    const updated = await roomsApi.updateRoom(roomId, data)
    const idx = rooms.value.findIndex(r => r.id === roomId)
    if (idx >= 0) rooms.value[idx] = updated
    return updated
  }

  async function deleteRoom(roomId: string): Promise<void> {
    await roomsApi.deleteRoom(roomId)
    rooms.value = rooms.value.filter(r => r.id !== roomId)
    if (activeRoomId.value === roomId) {
      activeRoomId.value = null
      messages.value = []
    }
  }

  return {
    rooms,
    activeRoomId,
    messages,
    loading,
    sending,
    loadRooms,
    createRoom,
    selectRoom,
    getActiveRoom,
    sendMessage,
    cancelSend,
    stopRound,
    refreshMessages,
    updateRoom,
    deleteRoom,
  }
})
