const { chromium } = require('playwright-core')
const fs = require('fs')

const EDGE = 'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe'
const TOKEN = fs.readFileSync('C:/Users/Administrator/.magic/.auth_token', 'utf8').trim()

;(async () => {
  const browser = await chromium.launch({ executablePath: EDGE, headless: true })
  const ctx = await browser.newContext({ viewport: { width: 1280, height: 900 } })
  const page = await ctx.newPage()
  await page.goto('http://localhost:5000/', { waitUntil: 'domcontentloaded', timeout: 30000 }).catch(() => {})
  await page.evaluate((tk) => localStorage.setItem('auth_token', tk), TOKEN)
  await page.goto('http://localhost:5000/#/chat', { waitUntil: 'networkidle', timeout: 30000 }).catch(() => {})
  await page.waitForTimeout(1500)

  // 点击"go magic"会话（历史上含工具调用回合）
  const click = await page.evaluate(() => {
      const items = [...document.querySelectorAll('*')].filter(el => el.children.length === 0 && el.textContent.trim() === 'go magic')
      for (const el of items) {
        let p = el; for (let i = 0; i < 4 && p; i++) { if (p.click && p.tagName !== 'BODY') { p.click(); return true; } p = p.parentElement; }
      }
      return false
  })
  console.log('clicked:', click)
  await page.waitForTimeout(2500)

  // 先收集折叠态所有 tool-call-card 宽度 + 容器宽度
  const measure = async () => page.evaluate(() => {
    const cards = [...document.querySelectorAll('.tool-call-card')].map((el, i) => {
      const h = el.querySelector('.tool-call-header')
      const r = el.getBoundingClientRect()
      const hr = h ? h.getBoundingClientRect() : null
      return {
        i,
        cardW: Math.round(r.width * 10) / 10,
        cardH: Math.round(r.height * 10) / 10,
        headerW: hr ? Math.round(hr.width * 10) / 10 : null,
        headerH: hr ? Math.round(hr.height * 10) / 10 : null,
      }
    })
    const bodies = [...document.querySelectorAll('.message-body.assistant-body')].map((el) => {
      const r = el.getBoundingClientRect()
      return Math.round(r.width * 10) / 10
    })
    const userBodies = [...document.querySelectorAll('.message-body.user-body, .message.user .message-body')].map((el) => {
      const r = el.getBoundingClientRect()
      return Math.round(r.width * 10) / 10
    })
    return { cards, assistantBodyWidths: bodies, userBodyWidths: userBodies }
  })

  const folded = await measure()
  console.log('FOLDED:', JSON.stringify(folded, null, 2))

  // 展开所有 tool-call-card
  const expand = await page.evaluate(() => {
    let n = 0
    for (const h of document.querySelectorAll('.tool-call-card > .tool-call-header, .tool-call-card button.tool-call-header')) { h.click(); n++ }
    for (const h of document.querySelectorAll('.tool-call-group-header')) { h.click(); n++ }
    return n
  })
  console.log('expanded headers:', expand)
  await page.waitForTimeout(1200)

  const expanded = await measure()
  console.log('EXPANDED:', JSON.stringify(expanded, null, 2))

  await page.screenshot({ path: 'D:/project/go/go-magic/web/.ui-check-expanded.png', fullPage: false })
  await browser.close()
})().catch((e) => { console.error(e); process.exit(1) })