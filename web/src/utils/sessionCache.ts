/**
 * 侧栏"全量 web 会话"缓存 —— 模块级状态，跨路由切换存活。
 *
 * 为什么必须放在组件外面：ChatView 会随路由切换被卸载重建（本项目没有 keep-alive），
 * 把这份缓存挂在组件实例上，每次从别的页面回到"会话"都要重新全量拉取
 * （93 个会话 = 一次 limit=100 的补齐请求），并且会再铺一次骨架屏。
 * 用户看到的就是"每次进入对话页都重新加载渲染"。
 * 缓存提升到模块级后，回访直接命中内存：不发请求、不铺骨架、行数不变。
 *
 * 只服务 ChatView 的侧栏（分组 / 搜索），所以不放进 pinia；
 * 身份切换时由 api/client.ts 的 setAuthToken(null) 调用 resetSessionCache() 清空，
 * 避免上一个用户的会话残留到下一个用户。
 */
import { ref } from 'vue'
import type * as sessionsApi from '@/api/sessions'

// null = 尚未全量补齐（骨架屏的判据之一）
export const allWebSessions = ref<sessionsApi.Session[] | null>(null)

// 全量补齐进行中。注意它**不**驱动侧栏底部转圈（那是分页专用的 sessionsLoadingMore），
// 只在搜索态下作为"检索中"的提示。
export const fullSessionsLoading = ref(false)

// 补齐失败后要能退出等待：骨架屏的显示条件是"还在等全量"，
// 若失败后 allWebSessions 永远停在 null，侧栏就会一直卡在骨架上。
export const fullSessionsFailed = ref(false)

// 上次补齐成功的时间。缓存命中时仍要能自愈（别的端新建的会话也得出现），
// 但"每次回访都重拉"正是用户抱怨的那件事 —— 所以只在缓存明显过期时才静默刷新。
export const fullSessionsFetchedAt = ref(0)

export function resetSessionCache(): void {
  allWebSessions.value = null
  fullSessionsLoading.value = false
  fullSessionsFailed.value = false
  fullSessionsFetchedAt.value = 0
}
