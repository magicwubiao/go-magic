#!/usr/bin/env node
/**
 * naive-ui 组件注册一致性守卫（按文件）。
 *
 * 背景：naive-ui 从"整包全局注册"（app.use(naive)）改成了各 SFC 自己 import 自己
 * 用到的组件 —— 只有这样 Rollup 才能把"只有一个页面用"的重组件（date-picker、
 * data-table、upload……）拆进那个页面的 chunk，首屏不再预加载 1.34MB 的 naive-ui。
 *
 * 代价是**每个用到 n-* 标签的文件必须自己 import 对应组件**。漏掉不会构建失败、
 * 类型检查也过（本项目 tsconfig 没开 strictTemplates），运行时只有一条
 * "Failed to resolve component" 警告，组件静默不渲染 —— 页面看起来只是"少了一块"。
 * 所以用这个脚本兜底：逐文件把模板标签和局部 import 做比对。
 *
 * 检查项：
 *   1. 模板里出现 <n-xxx> 但该文件没 import 对应组件 → 报错退出
 *   2. import 的名字在 naive-ui 里根本不存在（拼写错误）→ 报错退出
 *   3. import 了但文件里完全没提到（含 h(NXxx) 形式的用法）→ 提示（tsc 的
 *      noUnusedLocals 也会报，这里只是让它更早、更直白）
 *
 * 用法：node scripts/check-naive-components.mjs   （或 npm run check:naive）
 */
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const here = path.dirname(fileURLToPath(import.meta.url))
const webRoot = path.resolve(here, '..')
const srcDir = path.join(webRoot, 'src')
const naiveUiDir = path.join(webRoot, 'node_modules', 'naive-ui', 'es')

const SKIP_DIRS = new Set(['node_modules', 'dist', '.git'])

function walk(dir, out = []) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    if (entry.isDirectory()) {
      if (!SKIP_DIRS.has(entry.name)) walk(path.join(dir, entry.name), out)
    } else if (/\.(vue|ts)$/.test(entry.name)) {
      out.push(path.join(dir, entry.name))
    }
  }
  return out
}

/** kebab 标签名 → naive-ui 的 PascalCase 组件名（n-data-table → NDataTable） */
function pascal(tag) {
  return (
    'N' +
    tag
      .replace(/^n-/, '')
      .split('-')
      .map((s) => s.charAt(0).toUpperCase() + s.slice(1))
      .join('')
  )
}

// naive-ui 实际导出的名字（抓拼写错误）。只读一遍，缓存。
const exported = new Set()
if (fs.existsSync(naiveUiDir)) {
  const stack = [naiveUiDir]
  while (stack.length) {
    const dir = stack.pop()
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const p = path.join(dir, entry.name)
      if (entry.isDirectory()) {
        stack.push(p)
      } else if (entry.name.endsWith('.mjs')) {
        for (const m of fs.readFileSync(p, 'utf8').matchAll(/\b(N[A-Z][A-Za-z0-9]*)\b/g)) {
          exported.add(m[1])
        }
      }
    }
  }
}

const tagRe = /<n-([a-z0-9]+(?:-[a-z0-9]+)*)/g
// 逐行锚定的 import 解析：不能用 `import {[\s\S]*?} from 'naive-ui'` 这种懒惰匹配，
// 它会从文件里第一个 import 开始吞到 naive-ui 那一行，把别的模块的名字也算进来。
const importRe = /^import\s+(?:type\s+)?([\s\S]*?)\s*from\s*'([^']+)'/gm

const problems = []
const warnings = []
let files = 0
let tagTotal = 0

for (const file of walk(srcDir)) {
  const src = fs.readFileSync(file, 'utf8')
  const rel = path.relative(webRoot, file).replace(/\\/g, '/')

  // 模板里用到的组件
  const tags = new Set()
  for (const m of src.matchAll(tagRe)) tags.add(pascal('n-' + m[1]))
  if (!tags.size) continue
  files++

  // 本文件从 naive-ui 引入的（值）名字
  const imported = new Set()
  for (const m of src.matchAll(importRe)) {
    if (m[2] !== 'naive-ui') continue
    if (/^import\s+type\b/.test(m[0])) continue
    const braces = m[1].match(/\{([\s\S]*)\}/)
    if (!braces) continue
    for (const part of braces[1].split(',')) {
      const name = part.trim().split(/\s+as\s+/)[0].trim()
      if (name) imported.add(name)
    }
  }

  for (const name of [...tags].sort()) {
    tagTotal++
    if (!imported.has(name)) problems.push(`${rel}: <${kebab(name)}> 用了但没 import ${name}`)
    else if (exported.size && !exported.has(name)) problems.push(`${rel}: naive-ui 没有导出 ${name}（拼写错误？）`)
  }

  // 引入了但在文件里完全没出现 → 大概率是废 import。
  // 注意模板里是 kebab 形式（<n-spin>），而 import 的是 PascalCase（NSpin），
  // 所以两种写法都要算：import 语句本身会让 PascalCase 至少出现 1 次。
  for (const name of [...imported].sort()) {
    if (!/^N[A-Z]/.test(name)) continue
    const pascalCount = [...src.matchAll(new RegExp(`\\b${name}\\b`, 'g'))].length
    const kebabCount = src.split(`<${kebab(name)}`).length - 1
    if (pascalCount <= 1 && kebabCount === 0) warnings.push(`${rel}: import 了 ${name} 但文件里没用到`)
  }
}

function kebab(name) {
  return 'n-' + name.replace(/^N/, '').replace(/([a-z0-9])([A-Z])/g, '$1-$2').toLowerCase()
}

if (problems.length) {
  console.error('✗ naive-ui 组件未按文件引入（运行时组件不会渲染，构建与 tsc 都不会报）：')
  for (const p of problems) console.error('    ' + p)
}
if (warnings.length) {
  console.warn('⚠ 可疑的无效 import：')
  for (const w of warnings) console.warn('    ' + w)
}

if (problems.length) {
  console.error(`\n共 ${problems.length} 处问题。在对应文件里补 import { NXxx } from 'naive-ui' 后重试。`)
  process.exit(1)
}

console.log(
  `✓ naive-ui 按文件引入一致：${files} 个文件、${tagTotal} 处 <n-*> 组件均已局部 import` +
    (warnings.length ? `（${warnings.length} 处可疑 import，见上）` : '')
)
