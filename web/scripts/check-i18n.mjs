// check-i18n.mjs —— 构建前拦两类 i18n 事故。
//
// 1. **编译不过的文案**：vue-i18n 用 message-compiler 把消息文本编译成 AST，
//    文本里的 `@` 会被当成链接消息前缀（`@:key` / `@.mod`），后面不是 `:` 或 `.`
//    就直接抛 `Invalid linked format`。这个错误发生在**渲染期**（t() 调用时），
//    不是构建期，所以构建能过、页面却整块挂掉——典型表现是「点按钮没弹窗」
//    （模态框内容渲染到那句文案时 patch 中断）。历史踩坑：
//    `@zed-industries/claude-code-acp`、`@某人`、`@note`。
//    同理 `{name}` 是插值、`|` 是复数分隔，字面量要避开。
//
// 2. **模板里引用了不存在的 key**：只会显示原始 key，不崩，但一样是 bug。
//
// 用法：node scripts/check-i18n.mjs（由 npm run check:i18n 调用）

import { baseCompile } from '@intlify/message-compiler'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join } from 'node:path'
import { createServer } from 'vite'

const ROOT = new URL('..', import.meta.url).pathname.replace(/^\/([A-Za-z]:)/, '$1')

const server = await createServer({
  server: { middlewareMode: true },
  appType: 'custom',
  logLevel: 'error',
})

const locales = {}
for (const [name, file] of [['zh', '/src/locales/zh.ts'], ['en', '/src/locales/en.ts']]) {
  const mod = await server.ssrLoadModule(file)
  locales[name] = mod.default ?? mod
}

const problems = []

// ---- 1. 逐条编译消息 ----
function flatten(obj, prefix, out) {
  for (const [k, v] of Object.entries(obj)) {
    const key = prefix ? `${prefix}.${k}` : k
    if (v && typeof v === 'object') flatten(v, key, out)
    else if (typeof v === 'string') out.set(key, v)
  }
  return out
}

for (const [locale, tree] of Object.entries(locales)) {
  const messages = flatten(tree[locale] ?? tree, '', new Map())
  for (const [key, text] of messages) {
    try {
      baseCompile(text, {
        onError: (err) => {
          problems.push(`[${locale}] ${key}: ${err.message}\n      ${text}`)
        },
      })
    } catch (err) {
      problems.push(`[${locale}] ${key}: ${err.message}\n      ${text}`)
    }
  }
}

// ---- 2. 扫描源码里静态写死的 key ----
function walk(dir, files = []) {
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry)
    if (statSync(full).isDirectory()) walk(full, files)
    else if (/\.(vue|ts)$/.test(entry)) files.push(full)
  }
  return files
}

const zhKeys = flatten(locales.zh.zh ?? locales.zh, '', new Map())
const used = new Set()
for (const file of walk(join(ROOT, 'src'))) {
  if (file.includes(`${join('src', 'locales')}`)) continue
  const src = readFileSync(file, 'utf8')
  // 只认 `t('a.b')` / `$t('a.b')` 这类静态调用；动态拼接的（模板字符串）天然查不到。
  for (const m of src.matchAll(/\$?\bt\(\s*'([A-Za-z0-9_.-]+)'/g)) used.add(m[1])
}

for (const key of [...used].sort()) {
  // 以点结尾的是前缀拼接（t('goals.statusOptions.' + status)），子键由运行时决定。
  if (key.endsWith('.')) continue
  if (!zhKeys.has(key)) problems.push(`missing key: ${key} (used in src/, absent from locales/zh.ts)`)
}

await server.close()

if (problems.length) {
  console.error(`i18n check failed: ${problems.length} problem(s)\n`)
  for (const p of problems) console.error('  - ' + p)
  process.exit(1)
}
console.log(`i18n check ok: ${used.size} keys referenced, all messages compile`)
