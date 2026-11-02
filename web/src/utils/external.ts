/**
 * 用系统默认浏览器打开外部链接。
 *
 * 为什么不直接用 window.open：打包成桌面版（Tauri + WebView2）后，页面里的
 * window.open 会被当作弹窗处理——尤其是在 await 之后调用（OAuth 要先做发现/
 * 注册，几秒钟过去了），WebView 直接拦掉，壳层的 on_new_window 根本收不到
 * 事件，用户看到的就是"点了没反应"。浏览器里因为弹出策略宽松一些所以正常，
 * 这就是"网页里能用、桌面版不能"的典型成因。
 *
 * 这里改成合成的 <a target="_blank"> 点击：走真实的导航/新窗口请求，Tauri
 * 的 on_new_window 能拦到并交给系统浏览器（见 go-magic-desktop main.rs）；
 * 浏览器里行为与 window.open(..., '_blank') 一致。
 *
 * 注意：真正需要"一定能弹出来"的授权页，权威路径是后端进程调用系统浏览器
 * （internal/server/openurl.go）——那条路不依赖页面激活状态。本函数用于
 * 页面上的兜底链接/说明文档这类由用户主动点击的跳转。
 */
export function openExternal(url?: string): void {
  if (!url) return
  try {
    const a = document.createElement('a')
    a.href = url
    a.target = '_blank'
    a.rel = 'noopener noreferrer'
    a.style.display = 'none'
    document.body.appendChild(a)
    a.click()
    a.remove()
  } catch {
    // 极端环境（没有 document）下退回 window.open，至少不比现状差。
    window.open(url, '_blank', 'noopener,noreferrer')
  }
}
