import { request, getAuthToken } from './client'

export interface UploadedFile {
  id: string
  name: string
  filename: string
  url: string
  size: number
  // MIME 类型。上传接口会回传，队列快照恢复的附件也带；渲染层据此判断
  // 是否渲染缩略图（见 isImageAttachment）。缺省时该函数会退回按扩展名判断。
  mime?: string
  data?: string  // base64 data URL for reliable message sending
  // 前端附加（仅内存，不序列化、不上传）：上传时的原始 File 对象。
  // 图片走多模态通道时直接用 FileReader 读取，绕开带鉴权的 /api/uploads。
  _native?: File
}

export interface Session {
  id: string
  title: string
  source: string
  model: string
  work_dir?: string
  work_dir_user_set?: boolean
  profile?: string
  started_at: number
  last_active: number
  is_active: boolean
  message_count: number
  input_tokens: number
  output_tokens: number
  preview: string
}

// 文件操作记录：一次工具调用对单个文件的动作。action 与后端 FileOp.Action 对齐，
// 取值 read/write/delete/list/search/batch/access；path 可为绝对路径或会话工作目录相对路径。
// diff：后端快照跟踪器生成的 unified diff 文本（write/delete 且文本可读时携带，
// 空/缺省表示二进制、超限或内容未变，前端不展示可展开的 diff 行）。
export interface FileOp {
  action: string
  path: string
  param?: string
  diff?: string
}

export interface Message {
  id: string
  session_id: string
  role: 'user' | 'assistant' | 'system' | 'tool'
  content: string
  timestamp: string
  tool_calls?: unknown[]
  tool_name?: string
  tool_call_id?: string
  images?: string[]
  files?: Partial<UploadedFile>[]
  // 后端落库返回：assistant 消息对应的本轮全部文件操作（含 read 等非变更动作）。
  // 刷新/重开后由该字段驱动"变更的文件"展示；流式进行中数据源是 tool_calls_snapshot[].file_ops。
  file_ops?: FileOp[]
  // 前端附加：assistant 回复对应本轮执行的工具调用摘要（UI 展示用）。
  // 后端 /sessions/{id}/messages 暂不返回该字段，前端在 streaming 结束时写入内存快照。
  tool_calls_snapshot?: unknown[]
  // 前端附加：与 tool_calls_snapshot 搭配的时间线，
  // 让历史消息中"思考文本 ↔ 工具块"的穿插顺序能和 streaming 时完全一致。
  // 元素 = {kind:'text', end:number} | {kind:'tool', toolCallId:string}
  streaming_timeline_snapshot?: unknown[]
}

export async function getSessions(limit: number = 20, offset: number = 0): Promise<{ sessions: Session[]; total: number }> {
  const res = await request<{ sessions: Session[]; total: number }>(`/sessions?limit=${limit}&offset=${offset}`)
  return { sessions: res.sessions || [], total: res.total || 0 }
}

// 按工作目录分组的会话（服务端只统计"用户显式设置过工作目录"的 web 会话，
// 组内/组间按最近活动倒序），供底部状态栏"按目录查看会话"面板使用。
export interface SessionDirGroup {
  dir: string
  sessions: Session[]
}

export async function getSessionDirGroups(): Promise<{ groups: SessionDirGroup[]; totalSessions: number }> {
  const res = await request<{ groups?: SessionDirGroup[]; total_sessions?: number }>('/sessions/dir-groups')
  return { groups: res?.groups || [], totalSessions: res?.total_sessions || 0 }
}

export async function getSession(id: string): Promise<{ session_id: string; messages: Message[] }> {
  return request(`/sessions/${id}/messages`)
}

export async function createSession(workDir?: string): Promise<Session> {
  const body: Record<string, string> = {}
  if (workDir) body.work_dir = workDir
  return request('/sessions', {
    method: 'POST',
    body: Object.keys(body).length > 0 ? JSON.stringify(body) : undefined,
  })
}

export async function deleteSession(id: string, deleteFiles: boolean = false): Promise<void> {
  return request(`/sessions/${id}`, { 
    method: 'DELETE',
    body: JSON.stringify({ delete_files: deleteFiles })
  })
}

export async function renameSession(id: string, name: string): Promise<void> {
  return request(`/sessions/${id}`, {
    method: 'PUT',
    body: JSON.stringify({ name }),
  })
}

export async function updateSessionWorkDir(id: string, workDir: string): Promise<void> {
  return request(`/sessions/${id}`, {
    method: 'PUT',
    body: JSON.stringify({ work_dir: workDir }),
  })
}

export interface DirEntry {
  path: string
  name: string
  is_dir: boolean
}

export async function listDirs(path?: string): Promise<{ current: string; dirs: DirEntry[] }> {
  const query = path ? `?path=${encodeURIComponent(path)}` : ''
  return request(`/fs/dirs${query}`)
}

// Previously used working directories (global config + user-set session dirs),
// most recently used first; used to recommend directories in the dir picker.
export async function listWorkDirHistory(): Promise<string[]> {
  const res = await request<{ dirs?: string[] }>('/fs/workdir-history')
  return res?.dirs || []
}

// Open a local directory in the OS file explorer (Windows Explorer / Finder / xdg-open).
export async function openFolderInExplorer(path: string): Promise<void> {
  await request('/fs/open-folder', {
    method: 'POST',
    body: JSON.stringify({ path }),
  })
}

export interface FSEntry {
  path: string
  name: string
  is_dir: boolean
  size: number
  modified: number
  hidden?: boolean
}

export async function listFSEntries(path?: string, sessionId?: string, showHidden = false): Promise<{ current: string; entries: FSEntry[] }> {
  const params = new URLSearchParams()
  if (path) params.set('path', path)
  if (sessionId) params.set('session_id', sessionId)
  if (showHidden) params.set('hidden', '1')
  const query = params.toString() ? `?${params.toString()}` : ''
  const res = await request<{ current?: string; entries?: FSEntry[]; error?: string }>(`/fs/list${query}`)
  // Older backends may reply 200 with {"error": ...}; treat that as a failure
  // instead of an empty listing so the UI keeps its previous state.
  if (res && typeof res === 'object' && res.error) {
    throw new Error(res.error)
  }
  return { current: res?.current || '', entries: res?.entries || [] }
}

/**
 * 预览读取失败的分类。调用方据此决定「显示错误」还是「降级为二进制占位」，
 * 不再靠解析正文里的 {"binary":true} 去猜。
 */
export type FSPreviewErrorKind = 'binary' | 'too-large' | 'not-found' | 'other'

export class FSPreviewError extends Error {
  readonly kind: FSPreviewErrorKind
  readonly status: number

  constructor(message: string, kind: FSPreviewErrorKind, status: number) {
    super(message)
    this.name = 'FSPreviewError'
    this.kind = kind
    this.status = status
  }
}

function classifyReadFailure(status: number, binary: boolean): FSPreviewErrorKind {
  if (binary) return 'binary'
  if (status === 413) return 'too-large'
  if (status === 404) return 'not-found'
  return 'other'
}

/**
 * 读取文件原始字节。
 *
 * 返回 ArrayBuffer 而不是字符串，是为了把编码判定交给调用方：服务端一律按
 * UTF-8 应答，而 GBK/GB18030 的中文文件直接 text() 会整篇乱码。
 *
 * 失败时抛 FSPreviewError。服务端已改为用真实状态码（413 过大 / 415 二进制 /
 * 404 不存在）加 JSON 错误体应答，这里必须按状态码判定——只读正文的话，
 * {"error":"file too large for preview (>2MB)"} 会被当成文件内容渲染出来。
 */
export async function readFSFileBytes(path: string, sessionId?: string): Promise<{ buffer: ArrayBuffer; contentType: string }> {
  const params = new URLSearchParams()
  params.set('path', path)
  if (sessionId) params.set('session_id', sessionId)
  const res = await fetch(`/api/fs/read?${params.toString()}`, { headers: getFSAuthHeaders() })

  if (!res.ok) {
    let detail = ''
    let binary = false
    try {
      const body = (await res.json()) as { error?: string; binary?: boolean }
      detail = body?.error || ''
      binary = body?.binary === true
    } catch {
      // 非 JSON 错误体：退回状态文本
    }
    throw new FSPreviewError(
      detail || `Failed to read file: ${res.statusText || res.status}`,
      classifyReadFailure(res.status, binary),
      res.status,
    )
  }

  const buffer = await res.arrayBuffer()

  // 兼容旧后端：二进制文件曾以 200 + {"binary":true,"error":...} 应答（现已改为 415）。
  // 只认这个不可能出现在真实文本文件里的固定形状，避免把内容恰好是
  // {"error": "..."} 的合法 JSON 文件误判成错误。
  if (buffer.byteLength > 0 && buffer.byteLength <= 1024) {
    const head = new TextDecoder('utf-8').decode(buffer)
    if (head.includes('"binary"') && head.includes('"error"')) {
      try {
        const stub = JSON.parse(head) as { binary?: boolean; error?: string }
        if (stub?.binary === true) {
          throw new FSPreviewError(stub.error || 'binary file', 'binary', 200)
        }
      } catch (e) {
        if (e instanceof FSPreviewError) throw e
      }
    }
  }

  return { buffer, contentType: res.headers.get('Content-Type') || '' }
}

export function getFSAuthHeaders(): Record<string, string> {
  const token = getAuthToken()
  const headers: Record<string, string> = {}
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }
  return headers
}

/**
 * 换取「单文件内联读取」票据地址（用于 <img src>、新标签页打开）。
 *
 * 不再把登录 token 拼进 query：那会让同一串能打开全部接口的凭据留在浏览器
 * 历史、Referer 与反代日志里。改为服务端签发一张只解锁这一个文件的票据。
 * 换取动作本身走带 Authorization 头的 fetch，因此凭据不进 URL。
 */
export async function getFSReadUrl(path: string, sessionId?: string): Promise<string> {
  return signFSTicket({ scope: 'read', path, sessionId })
}

// createFSServeUrl 申请静态网页预览地址（POST /api/fs/sign）。
//
// 返回的地址形如 /api/fs/serve/<签名>/<入口>，签名覆盖「托管目录 + 过期时间」
// 并且写在**路径**里。这一点是必需的，不是风格选择：
//   1. index.html 里的相对引用（assets/style.css）按 RFC 3986 §5.3 只做路径
//      合并，query 会被整段丢弃 —— 原先挂在 ?path=&token= 上的托管根和凭据
//      会一起消失，于是每个 css/js/图片都变成 401；
//   2. iframe 内的子资源请求（img/link/script）带不上 Authorization header；
//   3. sandbox 收紧后（刻意不保留 allow-same-origin）cookie 同样不会随子资源
//      发送 —— 已实测确认：带 allow-same-origin 时子资源收到 cookie，去掉后
//      为空。而正是这一步阻断了被预览页面读取 localStorage.auth_token。
// 路径段是唯一能被相对引用自动继承、又不依赖任何浏览器凭据的位置。
//
// 请求本身走带 Authorization 头的 fetch，因此返回的地址里不含任何凭据，
// 可以放心交给被预览的页面。
export async function createFSServeUrl(path: string, sessionId?: string): Promise<string> {
  return signFSTicket({ scope: 'serve', path, sessionId })
}

/** 票据作用域：每个作用域只解锁一类动作，服务端严格校验，不可互相顶替。 */
export type FSTicketScope = 'serve' | 'read' | 'download' | 'zip' | 'uploads' | 'events'

/**
 * signFSTicket 用登录凭据换一张作用域受限的签名票据地址（POST /api/fs/sign）。
 *
 * 这是「浏览器发不出 Authorization 头」那类请求（<img src> / <a href> /
 * EventSource）唯一的凭据来源。它替代了过去的 ?token=<登录凭据>：
 *
 *   - 只解锁被声明的**单个动作 + 单个文件**，而不是整套 API；
 *   - 有硬性过期时间（一次性动作 1 小时；长驻的 serve/events 1 天）；
 *   - 密钥由 authToken 单向派生，从票据无法反推登录凭据。
 *
 * 换取动作本身走带 Authorization 头的 fetch，所以登录凭据永远不进 URL。
 */
export async function signFSTicket(params: {
  scope: FSTicketScope
  path?: string
  sessionId?: string
  hidden?: boolean
}): Promise<string> {
  // events 不需要 path；uploads 的 path 是上传根内的相对路径，与工作区路径
  // 语义不同。这里原样透传，由服务端按 scope 解释，避免前端复制一遍路径规则。
  const body: Record<string, unknown> = { scope: params.scope }
  if (params.path !== undefined) body.path = params.path
  if (params.sessionId) body.session_id = params.sessionId
  if (params.hidden) body.hidden = true

  const res = await fetch('/api/fs/sign', {
    method: 'POST',
    headers: { ...getFSAuthHeaders(), 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  })
  if (!res.ok) {
    let detail = ''
    try {
      detail = ((await res.json()) as { error?: string })?.error || ''
    } catch {
      // 非 JSON 错误体：退回状态文本
    }
    throw new Error(detail || `Failed to sign ticket: ${res.statusText || res.status}`)
  }

  const data = (await res.json()) as { url?: string }
  if (!data?.url) {
    throw new Error('Failed to sign ticket')
  }
  return data.url
}

/**
 * 换取「单文件附件下载」票据地址（用于 <a href>，浏览器发不出 Authorization）。
 */
export async function getFSDownloadUrl(path: string, sessionId?: string): Promise<string> {
  return signFSTicket({ scope: 'download', path, sessionId })
}

export async function deleteFSPath(path: string, sessionId?: string): Promise<void> {
  const body: Record<string, string> = { path }
  if (sessionId) body.session_id = sessionId
  return request('/fs/delete', {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export async function renameFSPath(path: string, newName: string, sessionId?: string): Promise<{ path: string; name: string }> {
  const body: Record<string, string> = { path, new_name: newName }
  if (sessionId) body.session_id = sessionId
  return request('/fs/rename', {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export async function writeFSFile(path: string, content: string, sessionId?: string): Promise<{ path: string; size: number }> {
  const body: Record<string, string> = { path, content }
  if (sessionId) body.session_id = sessionId
  return request('/fs/write', {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export interface FSUploadResult {
  name: string
  path: string
  size: number
}

export async function uploadFSToDir(dirPath: string, files: File[], sessionId?: string): Promise<{ uploaded: FSUploadResult[]; count: number }> {
  const formData = new FormData()
  for (const file of files) {
    formData.append('files', file)
  }

  const params = new URLSearchParams()
  params.set('path', dirPath)
  if (sessionId) params.set('session_id', sessionId)

  const token = getAuthToken()
  const headers: Record<string, string> = {}
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  const response = await fetch(`/api/fs/upload?${params.toString()}`, {
    method: 'POST',
    headers,
    body: formData,
  })

  if (!response.ok) {
    const text = await response.text()
    throw new Error(text || `Upload failed: ${response.statusText}`)
  }

  return response.json()
}

/**
 * 换取「打包下载」票据地址。
 *
 * showHidden 必须写进票据载荷，不能只作为 query 参数：否则拿到票据的人可以
 * 自行翻转 hidden，去取本不该给他的隐藏文件。所以这里把它一起交给签发端。
 */
export async function getFSZipUrl(path: string, sessionId?: string, showHidden = false): Promise<string> {
  return signFSTicket({ scope: 'zip', path, sessionId, hidden: showHidden })
}

export interface ShareResponse {
  token: string
  url: string
  path: string
  name: string
  is_dir: boolean
  expires_at: number
}

export async function createShare(path: string, seconds: number = 3600): Promise<ShareResponse> {
  return request<ShareResponse>('/fs/share', {
    method: 'POST',
    body: JSON.stringify({ path, seconds }),
  })
}

export async function createDir(parent: string, name: string, sessionId?: string): Promise<{ path: string; name: string }> {
  const body: Record<string, string> = { parent, name }
  if (sessionId) body.session_id = sessionId
  return request('/fs/mkdir', {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export async function sendMessage(sessionId: string, content: string): Promise<void> {
  return request(`/sessions/${sessionId}/messages`, {
    method: 'POST',
    body: JSON.stringify({ content }),
  })
}

export async function uploadFile(file: File, sessionId?: string): Promise<UploadedFile> {
  const formData = new FormData()
  formData.append('file', file)
  if (sessionId) {
    formData.append('session_id', sessionId)
  }

  const token = getAuthToken()
  const headers: Record<string, string> = {}
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  const response = await fetch('/api/upload', {
    method: 'POST',
    headers,
    body: formData,
  })

  if (!response.ok) {
    // Surface the machine-readable code (X-Error-Code) plus the raw detail so
    // callers can translate the error via i18n instead of showing the raw
    // English backend message.
    const detail = (await response.text().catch(() => '')) || response.statusText
    const err = new Error(detail.trim()) as Error & { code?: string; status?: number }
    err.code = response.headers.get('X-Error-Code') || ''
    err.status = response.status
    throw err
  }

  const result: UploadedFile = await response.json()
  // 不再往 url 上拼登录凭据：落库的引用保持裸地址，展示时由
  // resolveAttachmentSrc 换取作用域受限的上传票据。
  // Server already persisted the file; we no longer need base64 on the wire
  // because the SSE endpoint now reads files from disk by filename. Leaving
  // the data field in the type for backward compatibility but clearing it
  // so we don't carry megabytes of base64 through the app state.
  result.data = ''
  return result
}

export interface FileItem {
  filename: string
  disk?: string
  session_id?: string
  size: number
  url: string
  updated: string
}

// 去掉 URL 里历史遗留的 token 参数。
//
// 旧实现的上传响应会被 addTokenToUrl 处理，落库的引用因而可能带着 ?token=。
// 签发票据前必须剥掉：uploads 票据的 path 是上传根内的相对路径，残留的 query
// 会被当成路径的一部分而导致解析失败。
function stripLegacyUrlToken(url: string): string {
  if (!/[?&]token=/.test(url)) return url
  const hashAt = url.indexOf('#')
  const hash = hashAt >= 0 ? url.slice(hashAt) : ''
  const base = hashAt >= 0 ? url.slice(0, hashAt) : url
  const qAt = base.indexOf('?')
  if (qAt < 0) return url
  const params = new URLSearchParams(base.slice(qAt + 1))
  params.delete('token')
  const qs = params.toString()
  return base.slice(0, qAt) + (qs ? `?${qs}` : '') + hash
}

// 附件票据缓存。同一条聊天记录里的同一张图会被反复渲染，而签发一次票据是
// 一次网络往返，不能每渲染一次就签一次。服务端一次性票据有效期 1 小时，
// 缓存取半小时，留足余量避免把临期票据塞进 DOM。
const attachmentTicketCache = new Map<string, { url: string; expiresAt: number }>()
const ATTACHMENT_TICKET_CACHE_MS = 30 * 60 * 1000

/**
 * resolveAttachmentSrc 把消息里带回的附件地址换成能直接塞进 <img src> /
 * <a href> 的地址。
 *
 * data:/blob:/外链原样返回；站内 /api/uploads/... 在认证后面，而浏览器发不出
 * Authorization 头——这正是票据要解决的问题：换一张 scope=uploads 的票据，
 * 只解锁这一个附件。票据异步换取，结果按源地址缓存。
 */
export async function resolveAttachmentSrc(url?: string): Promise<string> {
  if (!url) return ''
  if (/^(data:|blob:|https?:)/i.test(url)) return url
  const key = stripLegacyUrlToken(url)
  const hit = attachmentTicketCache.get(key)
  if (hit && hit.expiresAt > Date.now()) return hit.url
  const signed = await signFSTicket({ scope: 'uploads', path: key })
  attachmentTicketCache.set(key, { url: signed, expiresAt: Date.now() + ATTACHMENT_TICKET_CACHE_MS })
  return signed
}

export async function listFiles(): Promise<FileItem[]> {
  // FileItem.url 保持裸地址（不再拼登录凭据）；需要展示时由调用方通过
  // resolveAttachmentSrc 换取票据。
  const res = await request<{ files: FileItem[] }>('/files')
  return res.files || []
}

export async function deleteFile(sessionId: string | undefined, disk: string): Promise<void> {
  const seg = sessionId ? `${sessionId}/${disk}` : disk
  return request(`/files/${seg}`, { method: 'DELETE' })
}

export interface ChatStreamEvent {
  data: string
}

/**
 * ChatStream is a thin EventSource-compatible shim over fetch + ReadableStream.
 *
 * The original implementation used EventSource (GET + token in URL + base64
 * file contents in URL), which leaked the auth token into browser history,
 * Referer, and reverse-proxy access logs, and routinely exceeded URL-length
 * limits once any non-trivial file was attached. The backend now exposes a
 * POST endpoint that takes a JSON body in the request and authenticates via
 * the Authorization header, so this class reproduces the EventSource surface
 * area (addEventListener / close) on top of fetch().
 */
export class ChatStream {
  private listeners: { [type: string]: Array<(ev: ChatStreamEvent) => void> } = {}
  private errorListeners: Array<(err: unknown) => void> = []
  private closed = false
  private abortController: AbortController | null = null

  constructor(sessionId: string, body: {
    content: string
    images?: string[]
    // 每张图片对应的已上传 /api/uploads/ 路径（与 images 同序）。
    // 后端用它作为持久化引用，避免把 data URL base64 写进会话库。
    imageUrls?: string[]
    // 每张图片的原始文件名（与 images 同序）。后端落库时记下来，会话回放才
    // 能在缩略图旁边显示用户认得出的名字，而不是一串 uuid。
    imageNames?: string[]
    files?: Array<Pick<UploadedFile, 'name' | 'filename' | 'url'>>
    // 「重新发送」的原排队项 id。带上它 = 这是一次重发：服务端先摘掉原条目
    // 再入队，避免同内容消息被查重合并成一条（点了重发却毫无变化）。
    retry_of?: string
  }, attach = false) {
    const token = getAuthToken()
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
      Accept: 'text/event-stream',
    }
    if (token) {
      headers['Authorization'] = `Bearer ${token}`
    }

    this.abortController = new AbortController()

    // attach 模式只挂事件总线、不提交消息，服务端据此跳过"内容不能为空"校验。
    const url = attach
      ? `/api/sessions/${encodeURIComponent(sessionId)}/stream?attach=1`
      : `/api/sessions/${encodeURIComponent(sessionId)}/stream`

    fetch(url, {
      method: 'POST',
      headers,
      body: JSON.stringify(body),
      signal: this.abortController.signal,
    })
      .then(async (res) => {
        if (!res.ok || !res.body) {
          const text = await res.text().catch(() => res.statusText)
          this.dispatchError(new Error(`stream failed: ${res.status} ${text}`))
          return
        }
        const reader = res.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''
        try {
          while (!this.closed) {
            const { value, done } = await reader.read()
            if (done) break
            buffer += decoder.decode(value, { stream: true })
            // SSE frames are separated by a blank line.
            let idx: number
            while ((idx = buffer.indexOf('\n\n')) !== -1) {
              const frame = buffer.slice(0, idx)
              buffer = buffer.slice(idx + 2)
              const dataLines: string[] = []
              for (const line of frame.split('\n')) {
                if (line.startsWith('data:')) {
                  dataLines.push(line.slice(5).trimStart())
                }
              }
              if (dataLines.length) {
                this.dispatch('message', { data: dataLines.join('\n') })
              }
            }
          }
          // Drain trailing frame (server may have ended without blank line).
          if (buffer.trim()) {
            const lines = buffer.split('\n')
            const dataLines: string[] = []
            for (const line of lines) {
                if (line.startsWith('data:')) {
                  dataLines.push(line.slice(5).trimStart())
                }
              }
            if (dataLines.length) {
              this.dispatch('message', { data: dataLines.join('\n') })
            }
          }
        } catch (err) {
          if (!this.closed) {
            this.dispatchError(err)
          }
        }
      })
      .catch((err) => {
        if (!this.closed) {
          this.dispatchError(err)
        }
      })
  }

  addEventListener(type: 'message', cb: (ev: ChatStreamEvent) => void): void
  addEventListener(type: 'error', cb: (err: unknown) => void): void
  addEventListener(type: string, cb: (ev: any) => void): void {
    if (type === 'error') {
      this.errorListeners.push(cb)
    } else {
      ;(this.listeners[type] ||= []).push(cb)
    }
  }

  set onmessage(cb: ((ev: ChatStreamEvent) => void) | null) {
    this.listeners['message'] = cb ? [cb] : []
  }

  set onerror(cb: ((err: unknown) => void) | null) {
    this.errorListeners = cb ? [cb] : []
  }

  close(): void {
    if (this.closed) return
    this.closed = true
    if (this.abortController) {
      this.abortController.abort()
    }
  }

  private dispatch(type: string, ev: ChatStreamEvent): void {
    const arr = this.listeners[type]
    if (!arr) return
    for (const cb of arr) {
      try {
        cb(ev)
      } catch (e) {
        console.error('ChatStream listener threw:', e)
      }
    }
  }

  private dispatchError(err: unknown): void {
    for (const cb of this.errorListeners) {
      try {
        cb(err)
      } catch (e) {
        console.error('ChatStream error listener threw:', e)
      }
    }
  }
}

// 非流式提交：把消息放进服务端会话队列后立即返回（不等结果）。
// 用于"当前回合进行中再发一条"的场景——此时已有 SSE 连接在推当前回合的
// 输出，再开一条流会打断渲染，因此复用现有连接提交、由它回传 queued 事件。
export async function submitMessage(
  sessionId: string,
  content: string,
  images?: string[],
  files?: UploadedFile[],
  imageUrls?: string[],
  imageNames?: string[],
  retryOf?: string,
): Promise<{ id: string; queued: boolean; duplicate: boolean }> {
  const slimFiles = files?.map(f => ({ name: f.name, filename: f.filename, url: f.url }))
  return request(`/sessions/${encodeURIComponent(sessionId)}/messages`, {
    method: 'POST',
    body: JSON.stringify({
      content,
      images,
      imageUrls,
      imageNames,
      files: slimFiles,
      retry_of: retryOf || undefined,
    }),
  })
}

// 引导提交：回合进行中把这条消息注入运行中的回合——模型在下一次 LLM 调用前
// 看到它并调整方向，生成不被打断。服务端在无法注入时回落为普通入队：返回体
// 里 guided 缺失/false 且带 queued/duplicate，调用方据此把乐观气泡转回排队项。
export async function submitGuide(
  sessionId: string,
  content: string,
): Promise<{ guided?: boolean; id: string; queued?: boolean; duplicate?: boolean; content?: string }> {
  return request(`/sessions/${encodeURIComponent(sessionId)}/messages`, {
    method: 'POST',
    body: JSON.stringify({ content, guide: true }),
  })
}

export interface QueuedTurnInfo {
  id: string
  content: string
  created_at: number
  /** 1 起，1 表示下一个执行 */
  position: number
  /**
   * 这条排队消息携带的附件。服务端回的是落库形态（file 部件，只含
   * name/mime/url，不含 inline base64），前端把它当作 UploadedFile 的
   * 子集使用即可。
   */
  attachments?: QueuedAttachment[]
}

/** 排队消息附件的轻量形态（服务端快照回传的形态）。 */
export interface QueuedAttachment {
  type: string
  file?: {
    name?: string
    mime_type?: string
    url?: string
  }
}

// queuedAttachmentsFromParts 把服务端回传的 content parts 收敛成前端统一的
// 附件结构。url 是持久化引用（/api/uploads/...），页面刷新后仍然有效 ——
// 渲染时由 resolveAttachmentSrc 换取 uploads 票据（登录凭据不进 URL）。
//
// 刻意回 Partial<UploadedFile> 而不是 UploadedFile：服务端快照里本来就没有
// id/size（uploads 元数据不在队列里），硬凑两个假字段只会在下游某处被当成
// 真值使用。渲染层（isImageAttachment / attachmentLabel）接受的正是 Partial。
export function queuedAttachmentsFromParts(parts?: QueuedAttachment[]): Partial<UploadedFile>[] {
  if (!parts || !parts.length) return []
  const out: Partial<UploadedFile>[] = []
  for (const part of parts) {
    const f = part?.file
    if (!f || !f.url) continue
    out.push({
      name: f.name || '',
      filename: f.url,
      url: f.url,
      mime: f.mime_type || '',
    })
  }
  return out
}

export interface SessionRunningState {
  running: boolean
  active_id: string
  queue_depth: number
  queued: QueuedTurnInfo[]
}

// 探测会话回合是否仍在服务端执行。移动端浏览器切后台会杀掉 SSE 连接，
// 但服务端回合与连接已解耦、会继续跑完落库；前端用此接口轮询恢复。
// 同时返回排队消息列表：用户在一个回合进行中继续发消息会进入服务端队列，
// 刷新页面后需要靠它恢复"排队中"的界面状态。
export async function getSessionRunning(sessionId: string): Promise<SessionRunningState> {
  const res = await request<{
    session_id: string
    running: boolean
    active_id?: string
    queue_depth?: number
    queued?: QueuedTurnInfo[]
  }>(`/sessions/${encodeURIComponent(sessionId)}/running`)
  return {
    running: !!res.running,
    active_id: res.active_id || '',
    queue_depth: res.queue_depth ?? (res.queued?.length || 0),
    queued: res.queued || [],
  }
}

// 显式取消会话正在执行的回合。回合已与连接解耦，前端 abort 本地流
// 不再能停止服务端执行，用户点"停止"时必须调用此接口。
// 服务端语义是"停止这一切"：正在执行的回合被取消，排队消息一并丢弃。
export async function cancelGeneration(sessionId: string): Promise<{ cancelled: boolean; dropped: number }> {
  const res = await request<{ cancelled?: boolean; dropped?: number }>(
    `/sessions/${encodeURIComponent(sessionId)}/cancel`,
    { method: 'POST' },
  )
  return { cancelled: !!res.cancelled, dropped: res.dropped || 0 }
}

// clearQueuedTurns 清空待发队列，但保留正在执行的回合。
//
// 与 cancelGeneration 的区别：这里不取消运行中的回合，用户想撤掉后面排着的
// 一串、但仍然想看当前这条回答时用它。返回被丢弃的条数。
export async function clearQueuedTurns(
  sessionId: string,
): Promise<{ dropped: number; queueDepth: number }> {
  const res = await request<{ dropped?: number; queue_depth?: number }>(
    `/sessions/${encodeURIComponent(sessionId)}/queue/clear`,
    { method: 'POST' },
  )
  return { dropped: res.dropped || 0, queueDepth: res.queue_depth || 0 }
}

// getQueuedTurnContent 取一条排队消息的完整原文（非预览）。
//
// /running 里的 content 是截断到 120 字的预览，只够在排队列表里显示一行。
// "编辑后重发"要把原文回填进输入框，刷新页面后本地已无原件，必须问服务端。
// 返回 null 表示该条已不在队列里（已被认领执行或已被删除）。
export async function getQueuedTurnContent(
  sessionId: string,
  turnId: string,
): Promise<{ content: string; attachments: Partial<UploadedFile>[] } | null> {
  try {
    const res = await request<{ content?: string; attachments?: QueuedAttachment[] }>(
      `/sessions/${encodeURIComponent(sessionId)}/queue/${encodeURIComponent(turnId)}/content`,
    )
    return {
      content: res.content || '',
      attachments: queuedAttachmentsFromParts(res.attachments),
    }
  } catch {
    // 404 = 该条已经转入执行（或已删除）：调用方把它当"来不及编辑"处理。
    return null
  }
}

// attachStream 把一条 SSE 连接挂到会话事件总线上，不发送任何新消息。
// 用于断线恢复（手机切后台被杀连接）或回合进行中打开页面续接实时输出。
// 连接上没有回合在跑时服务端会立即回 stream_started{started:false} 并保持
// 心跳，不会发 done——前端据此判定"当前无回合"。
export function attachStream(sessionId: string): ChatStream {
  return new ChatStream(sessionId, { content: '' }, true)
}

// removeQueuedTurn 丢弃一条尚未执行的排队消息。
// 与 cancelGeneration 的区别：只作用于点名的那一条，正在执行的回合与其它
// 排队消息都不受影响。removed=false 说明该条已被 worker 认领开始执行
// （来不及删了），前端据此提示用户改用停止。
export async function removeQueuedTurn(
  sessionId: string,
  turnId: string,
): Promise<{ removed: boolean; queueDepth: number }> {
  const res = await request<{ removed?: boolean; queue_depth?: number }>(
    `/sessions/${encodeURIComponent(sessionId)}/queue/${encodeURIComponent(turnId)}`,
    { method: 'DELETE' },
  )
  return { removed: !!res.removed, queueDepth: res.queue_depth || 0 }
}

// moveQueuedTurn 把一条排队消息拖到队列中的新位置（0 起，0 = 下一个执行）。
// 只改变等待顺序，正在执行的回合不受影响。服务端会把越界下标夹到有效区间，
// 因此这里可以直接传拖动落点的下标。
// moved=false 且 started=true 表示该条已被认领执行，前端应把它撤出排队区。
export async function moveQueuedTurn(
  sessionId: string,
  turnId: string,
  to: number,
): Promise<{ moved: boolean; started: boolean; queueDepth: number }> {
  const res = await request<{ moved?: boolean; started?: boolean; queue_depth?: number }>(
    `/sessions/${encodeURIComponent(sessionId)}/queue/${encodeURIComponent(turnId)}/position`,
    { method: 'PUT', body: JSON.stringify({ to }) },
  )
  return {
    moved: !!res.moved,
    started: !!res.started,
    queueDepth: res.queue_depth || 0,
  }
}

// updateQueuedTurn 修改一条排队消息的内容（编辑后重发）。
// started=true 表示该条已经被认领执行，服务端拒绝修改。
export async function updateQueuedTurn(
  sessionId: string,
  turnId: string,
  content: string,
  images?: string[],
  files?: UploadedFile[],
  imageUrls?: string[],
  imageNames?: string[],
): Promise<{ updated: boolean; started: boolean }> {
  const slimFiles = files?.map(f => ({ name: f.name, filename: f.filename, url: f.url }))
  const res = await request<{ updated?: boolean; started?: boolean }>(
    `/sessions/${encodeURIComponent(sessionId)}/queue/${encodeURIComponent(turnId)}`,
    {
      method: 'PUT',
      body: JSON.stringify({ content, images, imageUrls, imageNames, files: slimFiles }),
    },
  )
  return { updated: !!res.updated, started: !!res.started }
}

export function streamChat(sessionId: string, content: string, images?: string[], files?: UploadedFile[], imageUrls?: string[], imageNames?: string[], retryOf?: string, attach = false): ChatStream {  // The server now resolves file content from the uploads directory by
  // filename; we only need to ship the file metadata (name, filename, url),
  // never the base64 contents.
  const slimFiles = files?.map(f => ({
    name: f.name,
    filename: f.filename,
    url: f.url,
  }))
  return new ChatStream(sessionId, {
    content,
    images,
    imageUrls,
    imageNames,
    files: slimFiles,
    // 重发：告诉服务端先摘掉原排队项，否则两条同内容消息会被查重合并，
    // 表现为"点了重发但队列没变化"。见 handleSessionStream。
    retry_of: retryOf || undefined,
  }, attach)
}

export interface SessionGoal {
  id: string
  title: string
  status: string
  progress: number
}

export async function getSessionGoals(sessionId: string): Promise<SessionGoal[]> {
  const res = await request<{ session_id: string; goals: SessionGoal[] }>(`/sessions/${sessionId}/goals`)
  return res.goals || []
}

export async function unlinkSessionGoal(goalId: string, sessionId: string): Promise<void> {
  return request(`/goals/${goalId}/unlink`, {
    method: 'POST',
    body: JSON.stringify({ session_id: sessionId }),
  })
}