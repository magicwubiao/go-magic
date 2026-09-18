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

export async function createSession(workDir?: string, workspaceId?: string): Promise<Session> {
  const body: Record<string, string> = {}
  if (workDir) body.work_dir = workDir
  if (workspaceId) body.workspace_id = workspaceId
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

export async function listFSEntries(path?: string, sessionId?: string, workspaceId?: string, showHidden = false): Promise<{ current: string; entries: FSEntry[] }> {
  const params = new URLSearchParams()
  if (path) params.set('path', path)
  if (sessionId) params.set('session_id', sessionId)
  if (workspaceId) params.set('workspace_id', workspaceId)
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

export async function readFSFile(path: string, sessionId?: string, workspaceId?: string): Promise<string> {
  const params = new URLSearchParams()
  params.set('path', path)
  if (sessionId) params.set('session_id', sessionId)
  if (workspaceId) params.set('workspace_id', workspaceId)
  const token = getAuthToken()
  const headers: Record<string, string> = {}
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }
  const res = await fetch(`/api/fs/read?${params.toString()}`, { headers })
  if (!res.ok) {
    throw new Error(`Failed to read file: ${res.statusText}`)
  }
  return res.text()
}

export function getFSAuthHeaders(): Record<string, string> {
  const token = getAuthToken()
  const headers: Record<string, string> = {}
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }
  return headers
}

export function getFSReadUrl(path: string, sessionId?: string, workspaceId?: string): string {
  const params = new URLSearchParams()
  params.set('path', path)
  if (sessionId) params.set('session_id', sessionId)
  if (workspaceId) params.set('workspace_id', workspaceId)
  const token = getAuthToken()
  // TODO: 安全风险 - token 出现在 URL 中会被浏览器历史/Referer/日志记录
  // 后续应改用一次性短 token 或 fetch-event-source 库支持 header 传 token
  if (token) params.set('token', token)
  return `/api/fs/read?${params.toString()}`
}

export function getFSDownloadUrl(path: string, sessionId?: string, workspaceId?: string): string {
  const params = new URLSearchParams()
  params.set('path', path)
  if (sessionId) params.set('session_id', sessionId)
  if (workspaceId) params.set('workspace_id', workspaceId)
  const token = getAuthToken()
  // TODO: 安全风险 - token 出现在 URL 中会被浏览器历史/Referer/日志记录
  // 后续应改用一次性短 token 或 fetch-event-source 库支持 header 传 token
  if (token) params.set('token', token)
  return `/api/fs/download?${params.toString()}`
}

export async function deleteFSPath(path: string, sessionId?: string, workspaceId?: string): Promise<void> {
  const body: Record<string, string> = { path }
  if (sessionId) body.session_id = sessionId
  if (workspaceId) body.workspace_id = workspaceId
  return request('/fs/delete', {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export async function renameFSPath(path: string, newName: string, sessionId?: string, workspaceId?: string): Promise<{ path: string; name: string }> {
  const body: Record<string, string> = { path, new_name: newName }
  if (sessionId) body.session_id = sessionId
  if (workspaceId) body.workspace_id = workspaceId
  return request('/fs/rename', {
    method: 'POST',
    body: JSON.stringify(body),
  })
}

export async function writeFSFile(path: string, content: string, sessionId?: string, workspaceId?: string): Promise<{ path: string; size: number }> {
  const body: Record<string, string> = { path, content }
  if (sessionId) body.session_id = sessionId
  if (workspaceId) body.workspace_id = workspaceId
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

export function getFSZipUrl(path: string, sessionId?: string, workspaceId?: string, showHidden = false): string {
  const params = new URLSearchParams()
  params.set('path', path)
  if (sessionId) params.set('session_id', sessionId)
  if (workspaceId) params.set('workspace_id', workspaceId)
  if (showHidden) params.set('hidden', '1')
  const token = getAuthToken()
  // TODO: 安全风险 - token 出现在 URL 中会被浏览器历史/Referer/日志记录
  // 后续应改用一次性短 token 或 fetch-event-source 库支持 header 传 token
  if (token) params.set('token', token)
  return `/api/fs/zip?${params.toString()}`
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

export async function createDir(parent: string, name: string, sessionId?: string, workspaceId?: string): Promise<{ path: string; name: string }> {
  const body: Record<string, string> = { parent, name }
  if (sessionId) body.session_id = sessionId
  if (workspaceId) body.workspace_id = workspaceId
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
  result.url = addTokenToUrl(result.url)
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

export function addTokenToUrl(url: string): string {
  const token = getAuthToken()
  if (!token) return url
  // TODO: 安全风险 - token 出现在 URL 中会被浏览器历史/Referer/日志记录
  // 后续应改用一次性短 token 或 fetch-event-source 库支持 header 传 token
  const sep = url.includes('?') ? '&' : '?'
  return `${url}${sep}token=${encodeURIComponent(token)}`
}

// 去掉 URL 里已有的 token 参数。落库的上传引用可能带着签发时的 token
// （上传响应就是 addTokenToUrl 处理过的），重新签发时若不剥掉，?token= 会
// 叠加两个值，服务端只取第一个——凭据一旦轮换，旧 token 就让缩略图 401。
function stripUrlToken(url: string): string {
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

// attachmentSrc 把消息里带回的附件地址变成能直接塞进 <img src> / <a href> 的
// 地址。data:/blob:/外链原样返回；站内 /api/uploads/... 挂在 requireAuth 后面，
// 浏览器发不出 Authorization 头，只能走 ?token=（与上传返回的 url 同一套机制）。
export function attachmentSrc(url?: string): string {
  if (!url) return ''
  if (/^(data:|blob:|https?:)/i.test(url)) return url
  return addTokenToUrl(stripUrlToken(url))
}

export async function listFiles(): Promise<FileItem[]> {
  const res = await request<{ files: FileItem[] }>('/files')
  return (res.files || []).map(f => ({
    ...f,
    url: addTokenToUrl(f.url),
  }))
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
// 渲染时由 attachmentSrc 补上认证 token。
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