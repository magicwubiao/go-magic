// marked 的 gfm linkify 有个对中文场景致命的问题：内置 URL 正则用 [^\s<]*
// 匹配、且只在 ASCII 标点处截断尾部，全角标点（（）【】「」，。；：？！等）
// 会被整体吞进 <a> —— 比如模型回复"已打开百度首页（www.baidu.com），页面
// 加载正常"，渲染出来的 href 里塞进了后半句话的 URL 编码，正文里 ）
// 和后面的文字也跟着变成链接色。
//
// 这里提供一个覆盖内置 url tokenizer 的对象，所有 marked 实例统一挂上：
//   marked.use({ tokenizer: cjkSafeUrlTokenizer, ... })
// 自定义正则显式排除全角标点；没匹配上时返回 undefined，marked 会回退到
// 内置实现（mailto: 邮箱链接等仍由原逻辑处理）。

import type { Tokens } from 'marked'

// URL 字符集：排除空白、ASCII 引号/反引号/尖括号，以及常见中文标点区间。
// \u3000-\u303F  CJK 符号和标点（、。〈〉《》【】〖〗等）
// \u2010-\u2027  一般标点补充（– — ‘ ’ “ ” … † 等）
// \uFF01-\uFF0F ％＆＇ 等全角标点；\uFF08\uFF09 全角括号；\uFF0C 全角逗号
// \uFF1A-\uFF1F ：；〈＝〉？等；\uFF3B-\uFF40 ［＼］等；\uFF5B-\uFF65 ｛｜｝～等
const URL_STOP = String.raw`\u3000-\u303F\u2010-\u2027\uFF01-\uFF0F\uFF08\uFF09\uFF0C\uFF1A-\uFF1F\uFF3B-\uFF40\uFF5B-\uFF65`
const URL_RE = new RegExp(`^(?:(?:ftp|https?)://|www\\.)[^\`"\\s<>${URL_STOP}]+`, 'i')

export const cjkSafeUrlTokenizer = {
  url(src: string): Tokens.Link | undefined {
    const cap = URL_RE.exec(src)
    if (!cap) return undefined
    let raw = cap[0]
    // 尾部 ASCII 标点截断（对齐内置 linkify 行为）：句号、逗号、引号等
    // 直接剥掉；右括号/方括号只在未配平时才剥（Wikipedia 式带括号路径要保留）
    raw = raw.replace(/[.,;:!?'"*_~]+$/, '')
    for (;;) {
      const open = raw.match(/\(/g)?.length ?? 0
      const close = raw.match(/\)/g)?.length ?? 0
      if (!(raw.endsWith(')') && close > open)) break
      raw = raw.slice(0, -1)
    }
    for (;;) {
      const open = raw.match(/\[/g)?.length ?? 0
      const close = raw.match(/\]/g)?.length ?? 0
      if (!(raw.endsWith(']') && close > open)) break
      raw = raw.slice(0, -1)
    }
    if (!raw) return undefined
    // 裸域名（www. 开头）按 gfm 惯例补 http:// 前缀
    const href = /^https?:\/\//i.test(raw) ? raw : `http://${raw}`
    // 与内置实现一致：链接文本不再走 inlineTokens —— raw 本身是 URL，
    // 再分词会重新命中 url tokenizer 造成无限递归
    return {
      type: 'link',
      raw,
      href,
      text: raw,
      tokens: [{ type: 'text', raw, text: raw }],
    }
  },
}
