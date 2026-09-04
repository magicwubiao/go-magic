import { request, getAuthToken } from './client'

export interface UploadedFile {
  id: string
  name: string
  filename: string
  url: string
  size: number
  data?: string  // base64 data URL for reliable message sending
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
export interface FileOp {
  action: string
  path: string
  param?: string
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

export async function uploadFile(file: File): Promise<UploadedFile> {
  const formData = new FormData()
  formData.append('file', file)

  const token = getAuthToken()
  const headers: Record<string, string> = {}
  if (token) {
    headers['Authorization'] = `Bearer ${token}`
  }

  // Read file as base64 for reliable message sending
  const base64Data = await fileToBase64(file)

  const response = await fetch('/api/upload', {
    method: 'POST',
    headers,
    body: formData,
  })

  if (!response.ok) {
    throw new Error(`Upload failed: ${response.statusText}`)
  }

  const result: UploadedFile = await response.json()
  result.url = addTokenToUrl(result.url)
  // Store base64 data for message sending
  result.data = base64Data
  return result
}

// Convert File to base64 data URL
async function fileToBase64(file: File): Promise<string> {
  if (!(file instanceof Blob)) {
    return ''
  }
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(reader.result as string)
    reader.onerror = reject
    reader.readAsDataURL(file)
  })
}

export interface FileItem {
  filename: string
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

export async function listFiles(): Promise<FileItem[]> {
  const res = await request<{ files: FileItem[] }>('/files')
  return (res.files || []).map(f => ({
    ...f,
    url: addTokenToUrl(f.url),
  }))
}

export async function deleteFile(filename: string): Promise<void> {
  return request(`/files/${filename}`, { method: 'DELETE' })
}

export function streamChat(sessionId: string, content: string, images?: string[], files?: UploadedFile[]): EventSource {
  const token = getAuthToken()
  let url = `/api/sessions/${sessionId}/stream?content=${encodeURIComponent(content)}`
  if (images && images.length) {
    url += `&images=${encodeURIComponent(JSON.stringify(images))}`
  }
  if (files && files.length) {
    // Send file info with base64 content embedded directly
    // This avoids file system access issues on the backend
    const filesData = files.map(f => ({
      name: f.name,
      filename: f.filename,
      url: f.url,
      // Include data URL if available (for immediate use)
      data: f.data || null
    }))
    url += `&files=${encodeURIComponent(JSON.stringify(filesData))}`
  }
  if (token) {
    // TODO: 安全风险 - token 出现在 URL 中会被浏览器历史/Referer/日志记录
    // EventSource 不支持自定义 header，后续应改用 fetch-event-source 库或一次性短 token
    url += `&token=${encodeURIComponent(token)}`
  }
  return new EventSource(url)
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