/**
 * 供应商名称与保存前校验的纯函数。
 *
 * 为什么单独抽出来：模型管理页原本只能从内置目录里"挑"名字，而目录里
 * 只有一个 `custom` 条目，添加过之后又被"已配置的不再出现在下拉里"过滤掉
 * → 用户自定义供应商实际上只能加一个。放开自由命名之后，名称合法性、重名、
 * base_url / models 必填这些判定必须可测：web/ 没有测试框架，回归由
 * `.workbuddy/probe/provider-name-test.mjs` 用项目自带的 typescript 转译后直跑。
 */

/**
 * 供应商名既是 `config.providers` 的 map key，又会拼进 `/api/providers/{name}`
 * 的 URL 路径（服务端 `strings.SplitN(path, "/", 2)` 取第一段当 name），
 * 所以只允许 URL 安全、无歧义的字符集；首字符限字母数字，避免 `.`/`-` 开头
 * 这类看着像路径片段的名称。
 */
export const PROVIDER_NAME_PATTERN = /^[A-Za-z0-9][A-Za-z0-9._-]*$/
export const PROVIDER_NAME_MAX_LEN = 64

/** 保存校验问题；数组顺序即提示优先级（一次只展示第一条）。 */
export type ProviderSaveProblem = 'name' | 'nameExists' | 'baseUrl' | 'models'

export interface ProviderDraft {
  name: string
  baseUrl: string
  models: string[]
  /** 编辑已有供应商时跳过重名校验（自己当然和自己同名）。 */
  isEditing: boolean
}

export interface ProviderPreset {
  baseUrl: string
}

export function normalizeProviderName(raw: string | null | undefined): string {
  return (raw ?? '').trim()
}

export function isValidProviderName(raw: string | null | undefined): boolean {
  const name = normalizeProviderName(raw)
  return (
    name.length > 0 &&
    name.length <= PROVIDER_NAME_MAX_LEN &&
    PROVIDER_NAME_PATTERN.test(name)
  )
}

/** 过滤掉动态输入里的空行，避免空模型 ID 进配置。 */
export function normalizeModels(models: string[] | null | undefined): string[] {
  return (models ?? []).map((m) => m.trim()).filter(Boolean)
}

/**
 * 判定"这次能不能保存"，返回全部问题而不只是第一个——调用点取第一条提示，
 * 探针则能一次把组合情况钉死。
 */
export function planProviderSave(
  draft: ProviderDraft,
  presets: Record<string, ProviderPreset | undefined>,
  configuredNames: string[],
): { ok: boolean; problems: ProviderSaveProblem[] } {
  const name = normalizeProviderName(draft.name)
  const problems: ProviderSaveProblem[] = []

  if (!isValidProviderName(name)) {
    problems.push('name')
  } else if (!draft.isEditing && configuredNames.includes(name)) {
    // 重名不该被静默接受：saveProvider 先 GET 再决定 POST/PUT，撞名会直接
    // 覆盖掉已有供应商的 key 与模型列表。
    problems.push('nameExists')
  }

  // 内置目录（含 SiliconFlow 这类前端附加项）都自带官方地址；没有地址来源的
  // 就是自定义供应商，必须让用户填，否则请求会打到空 URL。
  const presetBaseUrl = normalizeProviderName(presets[name]?.baseUrl)
  if (!presetBaseUrl && !normalizeProviderName(draft.baseUrl)) {
    problems.push('baseUrl')
  }

  // 后端 createProviderForName 取不到第一个模型会直接报错，空模型列表存下去
  // 等于留了个切过去就不可用的坏配置。
  if (normalizeModels(draft.models).length === 0) {
    problems.push('models')
  }

  return { ok: problems.length === 0, problems }
}
