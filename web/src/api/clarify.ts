// 澄清提问（AI 需求不明确时向用户发起选择/补充说明）API。
// 服务端：POST /api/clarify/{id}/answer、POST /api/clarify/{id}/dismiss、GET /api/clarify/pending
// 注意：必须经 request() 携带 Bearer auth_token（裸 fetch 无 Authorization → 401）。

import { request } from './client'

const BASE_PATH = '/clarify'

export interface PendingClarifyRaw {
  id: string
  session_id: string
  question: string
  options: string[]
  context: string
  multi_select: boolean
  header: string
  created_at: string
  expires_at: string
}

// 答复一次澄清：choices 承载多选选项文本（单选给单项），note 为自由补充说明。
// 选项与说明至少给一个。
export async function answerClarify(
  id: string,
  payload: { choice?: string; choices?: string[]; note?: string },
): Promise<{ success: boolean }> {
  return request<{ success: boolean }>(`${BASE_PATH}/${encodeURIComponent(id)}/answer`, {
    method: 'POST',
    body: JSON.stringify(payload),
  })
}

// 关闭（取消）一次澄清：唤醒挂起的 clarify 工具并报"用户已关闭"，
// 模型收到工具错误后自行决定继续或收尾。
export async function dismissClarify(id: string): Promise<{ success: boolean }> {
  return request<{ success: boolean }>(`${BASE_PATH}/${encodeURIComponent(id)}/dismiss`, {
    method: 'POST',
  })
}

// 拉取指定会话的待答复澄清（页面刷新 / SSE 断连后的恢复）。
export async function getPendingClarifies(sessionId?: string): Promise<PendingClarifyRaw[]> {
  const q = sessionId ? `?session_id=${encodeURIComponent(sessionId)}` : ''
  const data = await request<{ pending: PendingClarifyRaw[] }>(`${BASE_PATH}/pending${q}`)
  return data?.pending || []
}
