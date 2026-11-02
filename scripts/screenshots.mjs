// README screenshots and a browser smoke test, against a demo instance (DEMO_DATA=true seeds a made-up
// library: abstract covers, placeholder videos that do not play):
//
//   DATA_DIR=/tmp/psp-demo DEMO_DATA=true AUTH_MODE=none PORT=18227 ./pocket-sphere
//   CDP=http://127.0.0.1:19291 BASE=http://127.0.0.1:18227 node scripts/screenshots.mjs   # any Chrome with remote debugging
//   SHOTS=0 … node scripts/screenshots.mjs                                                  # checks only
// playwright-core comes from web/node_modules, or set PW_FROM to a package.json next to an install of it.
import { createRequire } from 'node:module'
import assert from 'node:assert/strict'

const require = createRequire(process.env.PW_FROM || new URL('../web/package.json', import.meta.url))
let pw
try { pw = require('playwright') } catch { pw = require('playwright-core') }
const { chromium } = pw
const base = process.env.BASE || 'http://127.0.0.1:18227'
const shots = process.env.SHOTS !== '0'
const dir = process.env.OUT ? process.env.OUT.replace(/\/?$/, '/') : new URL('../docs/screenshots/', import.meta.url).pathname
const browser = process.env.CDP ? await chromium.connectOverCDP(process.env.CDP) : await chromium.launch()
const errors = []
// placeholder videos cannot play: keep the player calm for the pictures
const calm = '*, *::before, *::after { transition: none !important; animation: none !important; } .player-error, .toasts, .toast, .pbadge, .player-vr, .player-hint { display: none !important; }'

async function session({ phone = false, dark = false } = {}) {
  const ctx = await browser.newContext({
    viewport: phone ? { width: 390, height: 844 } : { width: 1440, height: 900 },
    deviceScaleFactor: phone ? 3 : 2, isMobile: phone, hasTouch: phone, colorScheme: dark ? 'dark' : 'light',
  })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e}`))
  page.on('console', m => { if (m.type() === 'error' && !/Failed to load resource|MEDIA_ELEMENT|NotSupported/.test(m.text())) errors.push(m.text()) })
  return { ctx, page }
}
async function go(page, path, wait = 1200) {
  await page.goto(base + path, { waitUntil: 'networkidle' })
  await page.addStyleTag({ content: calm })
  await page.evaluate(() => document.fonts.ready)
  await page.waitForTimeout(wait)
}
async function shot(page, name) {
  if (!shots) return
  // the placeholder videos have no picture: show the scene's cover in the player instead
  await page.evaluate(() => document.querySelectorAll('.player-stage').forEach(st => { const v = st.querySelector('video'); if (v?.poster) { st.style.background = `center / cover no-repeat url(${v.poster})`; v.style.opacity = '0' } }))
  await page.screenshot({ path: dir + name + '.png' })
}
const api = (page, p) => page.evaluate(u => fetch(u).then(r => r.json()), p)

const { ctx: c0, page: p0 } = await session()
await p0.goto(base + '/')
let list = await api(p0, '/api/v1/scenes?per=100')
for (let i = 0; i < 30 && list.total < 36; i++) { await p0.waitForTimeout(500); list = await api(p0, '/api/v1/scenes?per=100') }
assert.ok(list.total >= 36, 'demo scenes')
const vr = list.items.find(s => s.is_vr)
await c0.close()

{
  const { ctx, page } = await session()
  await go(page, '/')
  assert.ok(await page.getByText('Continue watching').first().isVisible(), 'home renders')
  await shot(page, 'desktop-home')
  await go(page, '/scenes?sort=date')
  await shot(page, 'desktop-scenes')
  await go(page, `/scenes/${vr.id}`, 2000)
  assert.ok(await page.locator('.scene-title').isVisible(), 'scene page renders')
  await shot(page, 'desktop-scene')
  await go(page, '/performers')
  await shot(page, 'desktop-performers')
  await go(page, '/downloads')
  assert.ok(await page.getByText('Rooftop Evening').isVisible(), 'downloads render')
  await shot(page, 'desktop-downloads')
  await go(page, '/stats?days=30')
  await shot(page, 'desktop-stats')
  await go(page, '/settings/headset')
  await shot(page, 'desktop-headset')
  await ctx.close()
}
{
  const { ctx, page } = await session({ dark: true })
  await go(page, '/scenes?vr=yes')
  await shot(page, 'desktop-vr-dark')
  await ctx.close()
}
{
  const { ctx, page } = await session({ phone: true })
  await go(page, '/')
  await shot(page, 'phone-home')
  await go(page, `/scenes/${vr.id}`, 2000)
  await shot(page, 'phone-scene')
  await ctx.close()
}
if (errors.length) { console.error(errors.join('\n')); process.exit(1) }
console.log('ok')
process.exit(0)
