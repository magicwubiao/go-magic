// 移除零宽字符（U+200B / U+2060 / U+FEFF）。
//
// 背景：后端对"只发起工具调用、正文为空"的 assistant 回合，向 provider 发送
// 零宽空格占位符以满足智谱 GLM 1214 校验。模型会照抄自己历史里的占位符并
// 回显到回复中；marked 开启 breaks:true 后，只含零宽字符的行并不是空行
// （行内有字符），每行会渲染成一个行高的 <br> —— 模型大量回显时表现为
// 大段空白。渲染前剥掉这些不可见字符即可，剥完变空的行会被 markdown
// 自然折叠成一个段落分隔，不再堆叠高度。
//
// 注意：U+200C / U+200D 在部分文字系统（如波斯语）和 emoji 序列中有语义，
// 故意不剥。
export function stripZeroWidth(s: string): string {
  // 快速路径：绝大多数内容不含零宽字符
  if (!/[\u200b\u2060\ufeff]/.test(s)) return s
  return s.replace(/[\u200b\u2060\ufeff]/g, '')
}
