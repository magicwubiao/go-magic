// i18n prod-compiler check.
//
// Usage (from web/):  node scripts/i18n-check.mjs
//
// Why: vue-i18n's DEV compiler only warns on malformed messages (e.g. a bare
// '@' that is parsed as a linked format, or a stray '{'), but the PROD
// compiler throws — and a throw at render time blanks the whole UI. This
// script bundles zh.ts/en.ts with esbuild and compiles EVERY unique message
// string through the PROD compiler, failing loudly on any syntax error.
//
// It sidesteps key-path resolution (keys may themselves contain dots, e.g.
// skills.hubSources['skills.sh']) by wrapping each value under synthetic
// top-level keys and calling t() on them. zh/en key asymmetry only warns —
// vue-i18n falls back to the other locale gracefully.
import { createRequire } from 'node:module'
import { fileURLToPath, pathToFileURL } from 'node:url'
import { build } from 'esbuild'

const require = createRequire(import.meta.url)
const root = fileURLToPath(new URL('../', import.meta.url)) // web/
const outdir = fileURLToPath(new URL('../.i18n-check/', import.meta.url))

await build({
  entryPoints: [root + 'src/locales/zh.ts', root + 'src/locales/en.ts'],
  outdir,
  bundle: true,
  format: 'esm',
  platform: 'node',
  logLevel: 'error',
})
const zh = (await import(pathToFileURL(outdir + 'zh.js').href)).default
const en = (await import(pathToFileURL(outdir + 'en.js').href)).default

const { createI18n } = require('vue-i18n/dist/vue-i18n.cjs.prod.js')

// Collect every leaf string of a locale tree.
function leafStrings(obj, out = []) {
  for (const v of Object.values(obj)) {
    if (typeof v === 'string') out.push(v)
    else if (v && typeof v === 'object') leafStrings(v, out)
  }
  return out
}

const zhStrings = [...new Set(leafStrings(zh))]
const enStrings = [...new Set(leafStrings(en))]

function keySet(obj, prefix = '', out = new Set()) {
  for (const [k, v] of Object.entries(obj)) {
    const path = prefix ? `${prefix}.${k}` : k
    if (v && typeof v === 'object' && !Array.isArray(v)) keySet(v, path, out)
    else out.add(path)
  }
  return out
}
const enKeys = keySet(en)
const zhKeys = keySet(zh)
let asymmetric = 0
for (const k of zhKeys) if (!enKeys.has(k)) { console.warn(`[warn] en.ts missing key: ${k}`); asymmetric++ }
for (const k of enKeys) if (!zhKeys.has(k)) { console.warn(`[warn] zh.ts missing key: ${k}`); asymmetric++ }

// Compile-check all unique strings of both locales (dedupe across locales).
const all = new Map() // text -> locales containing it
for (const s of zhStrings) all.set(s, (all.get(s) || '') + 'zh,')
for (const s of enStrings) all.set(s, (all.get(s) || '') + 'en')

const texts = [...all.keys()]
const fakeCtx = { name: 'X', count: 1, platform: 'X', n: 1, title: 'X' }
const BATCH = 500
let checked = 0
for (let start = 0; start < texts.length; start += BATCH) {
  const slice = texts.slice(start, start + BATCH)
  const messages = { en: Object.fromEntries(slice.map((s, i) => [`m${i}`, s])) }
  const t = createI18n({ legacy: false, locale: 'en', messages }).global.t
  for (let i = 0; i < slice.length; i++) {
    try {
      t(`m${i}`, fakeCtx)
      checked++
    } catch (e) {
      console.error(`FAIL (in ${all.get(texts[start + i])}): ${JSON.stringify(texts[start + i])}`)
      console.error(`  -> ${e.message}`)
      process.exit(1)
    }
  }
}
console.log(
  `OK: ${checked}/${texts.length} unique messages passed the prod compiler ` +
  `(${zhStrings.length} zh, ${enStrings.length} en), ${asymmetric} asymmetric key warnings`,
)
