import { defineStore } from 'pinia'
import { ref } from 'vue'

/**
 * 预览目标。
 *
 * 两种来源二选一，与 FilePreviewPanel 的 props 对齐：
 *  - fsPath + sessionId：工作区/会话文件
 *  - url：上传附件等直接地址
 */
export interface PreviewTarget {
  name?: string
  typeName?: string
  size?: number
  url?: string
  fsPath?: string
  sessionId?: string
}

/**
 * 聊天页的预览状态（全局单例）。
 *
 * 为什么不是各个组件各自持有 show 标志：预览面板在 ChatView 里只挂载一份，
 * 而打开入口散落在 ChatView（消息附件）和 RightSidebar（文件树）两处。
 * 共用一份状态才能保证「同一时刻只预览一个文件」，也不会出现两个面板叠着出现。
 *
 * FilesView / BotsView 不经过这里——那两个页面没有「边预览边对话」的诉求，
 * 继续用 FilePreviewDialog 的弹窗形态。
 */
export const usePreviewStore = defineStore('preview', () => {
  const visible = ref(false)
  const target = ref<PreviewTarget | null>(null)

  function open(next: PreviewTarget) {
    // 同一个文件再次点击不重新加载：面板内部按 fsPath/url 变化才触发 load，
    // 重复赋值会因对象引用不同而白等一次异步票据
    const cur = target.value
    const same = cur
      && cur.fsPath === next.fsPath
      && cur.url === next.url
      && cur.sessionId === next.sessionId
    if (!same) target.value = next
    visible.value = true
  }

  function close() {
    visible.value = false
  }

  function toggle(next: PreviewTarget) {
    if (visible.value && target.value?.fsPath === next.fsPath && target.value?.url === next.url) {
      close()
      return
    }
    open(next)
  }

  return { visible, target, open, close, toggle }
})
