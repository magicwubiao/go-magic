/**
 * 引导（steer）气泡的去重规则。
 *
 * 背景：一条引导从发出到"落地"要经过**两条独立的异步通道**，它们的到达顺序
 * 不确定（服务端先广播 guide_added、再回 HTTP 响应，但两条通道各自的投递与
 * 浏览器任务调度会把它变成竞态）：
 *
 *   1. 提交响应（POST /sessions/{id}/messages {guide:true}）——带服务端分配的 id；
 *   2. SSE 广播 guide_added —— 同一个 id，作用于本会话**所有**监听连接。
 *
 * 三个入口都在往 `state.messages` 里塞"用户气泡"：
 *   - 提交前的乐观气泡（id = `guide_local_<n>`）；
 *   - 响应返回后把乐观气泡固化为 `user_<服务端id>`；
 *   - 广播到达后补一条 `user_<服务端id>`（另一台设备/标签页发的引导靠它显示）。
 *
 * 原先三条路径各自"只判断自己的键"，于是 广播先到 → 补一条 `user_<id>`；
 * 响应后到 → 又把乐观气泡改名成同一个 `user_<id>` → **同一句话两个气泡**
 * （用户看到"引导发了两次"，且两条消息 id 相同，Vue 的 :key 也会重复）。
 *
 * 因此把判定收敛到这里，只做纯函数：输入一个消息列表 + 事件字段，输出
 * "该对列表做什么"（改名 / 丢弃 / 新增 / 什么都不做），由调用方执行。
 * 两个方向的顺序都必须收敛到同一条消息上（幂等）。
 */

/** 服务端消息 id 在前端气泡上的统一前缀（与 promoteQueuedToMessage 的键一致）。 */
export const USER_ID_PREFIX = 'user_'

/** 乐观引导气泡的本地 id 前缀（尚未拿到服务端 id 的气泡）。 */
export const GUIDE_LOCAL_PREFIX = 'guide_local_'

/** 参与去重判定所需的最小消息形状（只读 id / content，不依赖 Vue 响应式）。 */
export interface GuideBubbleLike {
  id: string
  content?: string
}

/** 服务端 id → 气泡 id。 */
export function guideBubbleId(serverId: string): string {
  return USER_ID_PREFIX + serverId
}

/** 本地占位 id（还没拿到服务端 id 的乐观项）：它们绝不会出现在服务端历史里。 */
function isLocalPlaceholderId(id: string): boolean {
  return id.startsWith(GUIDE_LOCAL_PREFIX) || id.startsWith('local_')
}

/**
 * 按 id 查找消息下标。
 *
 * 同时认两种形态：`user_<id>`（前端内存态的键）与 `<id>`（服务端落库形态的
 * 键——引导注入时落库的消息带的就是服务端原始 id）。刷新页面时 messages 由
 * 服务端历史整体重建，此时匹配不上 `user_<id>` 就会重复补一条气泡。
 *
 * 本地占位 id 只认 `user_<id>` 一种形态：占位项（排队项的 local_* turnId、
 * 乐观引导气泡的 guide_local_*）在服务端从未存在过，拿它们去和 messages 里的
 * 原始 id 比对只会产生误判（把还没固化的气泡当成"已存在"而跳过固化）。
 */
export function indexOfMessage(messages: readonly GuideBubbleLike[], id: string): number {
  if (!id) return -1
  const prefixed = USER_ID_PREFIX + id
  if (isLocalPlaceholderId(id)) {
    return messages.findIndex(m => m.id === prefixed)
  }
  return messages.findIndex(m => m.id === id || m.id === prefixed)
}

/** 查找"尚未固化的乐观引导气泡"（内容一致才算同一句话）。 */
export function indexOfPendingGuide(messages: readonly GuideBubbleLike[], content: string): number {
  if (!content) return -1
  return messages.findIndex(m => m.id.startsWith(GUIDE_LOCAL_PREFIX) && m.content === content)
}

/** applyGuideEvent 的处置计划。 */
export type GuideEventPlan =
  | { kind: 'noop' }
  | { kind: 'adopt'; index: number; id: string }
  | { kind: 'rename'; index: number; id: string }
  | { kind: 'drop'; index: number }
  | { kind: 'push' }

/**
 * guide_added 广播到达时的处置计划。
 *
 * - 该 id 已在列表里（响应先到并固化，或另一条广播）→ noop，绝不新增；
 * - 本标签页的乐观气泡还在（内容一致）→ adopt：把它原地认领为服务端 id，
 *   既不新增气泡，也不会留下一个永远不会被固化的 `guide_local_*` 孤儿；
 * - 都没有 → push（另一台设备发的引导）。
 */
export function planGuideBroadcast(
  messages: readonly GuideBubbleLike[],
  serverId: string,
  content: string,
): GuideEventPlan {
  if (!serverId) return { kind: 'noop' }
  if (indexOfMessage(messages, serverId) >= 0) return { kind: 'noop' }
  const pending = indexOfPendingGuide(messages, content)
  if (pending >= 0) return { kind: 'adopt', index: pending, id: guideBubbleId(serverId) }
  return { kind: 'push' }
}

/**
 * 提交响应到达时的处置计划（localId = 乐观气泡的本地 id）。
 *
 * - 乐观气泡已不在（广播先到并认领了它）→ noop；
 * - 该服务端 id 的气泡已存在（广播先到并自己 push 了一条）→ drop：乐观气泡
 *   是重复项，删掉它；
 * - 否则 → rename：把乐观气泡固化为 `user_<服务端id>`。
 */
export function planGuideSettle(
  messages: readonly GuideBubbleLike[],
  localId: string,
  serverId: string,
): GuideEventPlan {
  const mine = messages.findIndex(m => m.id === localId)
  if (mine < 0) return { kind: 'noop' }
  if (!serverId) return { kind: 'noop' }
  if (indexOfMessage(messages, serverId) >= 0) return { kind: 'drop', index: mine }
  return { kind: 'rename', index: mine, id: guideBubbleId(serverId) }
}
