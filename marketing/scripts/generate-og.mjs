// Renders the social-preview card (public/og/og-card.png, 1200×630) from a
// small branded HTML template using the Playwright Chromium that the e2e
// suite already depends on. Run manually when the brand or tagline changes:
//   node scripts/generate-og.mjs
import { mkdir, readFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from '@playwright/test'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const target = path.join(root, 'public', 'og', 'og-card.png')

const wordmark = await readFile(path.join(root, 'public', 'brand', 'wordmark-white@2x.png'))
const font = await readFile(path.join(root, 'public', 'fonts', 'bricolage-grotesque-latin.woff2'))

const html = `<!doctype html>
<html>
<head>
<style>
  @font-face {
    font-family: 'Bricolage Grotesque';
    src: url(data:font/woff2;base64,${font.toString('base64')}) format('woff2');
    font-weight: 200 800;
  }
  * { margin: 0; box-sizing: border-box; }
  body {
    width: 1200px;
    height: 630px;
    background: #0b1210;
    background-image: radial-gradient(70% 90% at 50% -10%, rgba(62, 197, 178, 0.18), transparent 70%),
      linear-gradient(rgba(238, 244, 241, 0.04) 1px, transparent 1px),
      linear-gradient(90deg, rgba(238, 244, 241, 0.04) 1px, transparent 1px);
    background-size: auto, 48px 48px, 48px 48px;
    font-family: 'Bricolage Grotesque', sans-serif;
    color: #eef4f1;
    display: flex;
    flex-direction: column;
    justify-content: center;
    padding: 0 96px;
  }
  img.wordmark { height: 56px; width: auto; align-self: flex-start; }
  h1 {
    margin-top: 44px;
    font-size: 74px;
    font-weight: 640;
    line-height: 1.06;
    letter-spacing: -0.015em;
    max-width: 950px;
  }
  h1 span { color: #3ec5b2; }
  .loop {
    margin-top: 44px;
    display: flex;
    gap: 14px;
    font-size: 26px;
    font-weight: 600;
    color: #93a7a0;
  }
  .loop b { color: #3ec5b2; font-weight: 600; }
</style>
</head>
<body>
  <img class="wordmark" src="data:image/png;base64,${wordmark.toString('base64')}" />
  <h1>Product feedback that <span>closes the loop</span></h1>
  <div class="loop">
    <b>Capture</b><span>→</span><b>Analyze</b><span>→</span><b>Decide</b><span>→</span><b>Track</b><span>→</span><b>Communicate</b>
  </div>
</body>
</html>`

await mkdir(path.dirname(target), { recursive: true })
const browser = await chromium.launch()
const page = await browser.newPage({ viewport: { width: 1200, height: 630 } })
await page.setContent(html, { waitUntil: 'networkidle' })
await page.screenshot({ path: target })
await browser.close()
console.log(`Wrote ${target}`)
