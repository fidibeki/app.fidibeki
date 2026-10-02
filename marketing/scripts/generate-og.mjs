// Render the marketing page's visual language as a 1200×630 social preview.
// Run manually after changing the composition or its catalog copy:
//   pnpm generate:og
// Commit the PNGs and manifest together. Content-hashed URLs work with /og's
// immutable cache policy; keep published files so cached pages retain an image.
import { createHash } from 'node:crypto'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { chromium } from '@playwright/test'
import { marketingLocales, messageKey } from '../utils/locale.ts'

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..')
const output = path.join(root, 'public', 'og')
const wordmark = await readFile(path.join(root, 'public', 'brand', 'wordmark-color@2x.png'))
const escapeHtml = value => value.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;').replaceAll('"', '&quot;')
const preview = {}

await mkdir(output, { recursive: true })
const browser = await chromium.launch()
try {
  const page = await browser.newPage({ viewport: { width: 1200, height: 630 }, deviceScaleFactor: 1 })
  for (const locale of marketingLocales) {
    const catalog = JSON.parse(await readFile(path.join(root, 'i18n', 'locales', locale.file), 'utf8'))
    const copy = source => {
      const value = catalog[messageKey(source)]
      if (!value) throw new Error(`Missing ${locale.code} social-preview copy: ${source}`)
      return escapeHtml(value)
    }
    await page.setContent(`<!doctype html>
<html lang="${locale.language}">
<head><meta charset="utf-8"><style>
  * { margin: 0; box-sizing: border-box; }
  body { width: 1200px; height: 630px; background: #fff; color: #191919; font-family: 'Helvetica Neue', Helvetica, Arial, sans-serif; padding: 52px 64px; }
  header { display: flex; align-items: center; justify-content: space-between; }
  .wordmark { height: 42px; width: auto; }
  .domain { font-size: 24px; color: #666; }
  main { margin-top: 75px; text-align: center; }
  h1 { font-size: 76px; line-height: 1.1; font-weight: 500; letter-spacing: -.035em; white-space: nowrap; }
  .intro { font-size: 28px; line-height: 1.35; color: #666; margin-top: 22px; }
  .composer { text-align: left; margin: 44px auto 0; max-width: 944px; height: 190px; border: 1px solid #d6d6d6; border-radius: 24px; padding: 28px 30px 24px; display: flex; flex-direction: column; justify-content: space-between; }
  .example { font-size: 29px; line-height: 1.35; }
  .composer-footer { display: flex; align-items: center; justify-content: space-between; }
  .try { display: flex; align-items: center; gap: 12px; color: #666; font-size: 23px; }
  .send { background: #191919; color: #fff; width: 56px; height: 56px; border-radius: 50%; display: grid; place-items: center; }
  svg { fill: none; stroke: currentColor; stroke-width: 1.7; stroke-linecap: round; stroke-linejoin: round; }
</style></head>
<body>
  <header><img class="wordmark" src="data:image/png;base64,${wordmark.toString('base64')}" alt="Fidibeki"><span class="domain">fidibeki.com</span></header>
  <main>
    <h1>${copy('What could be better?')}</h1>
    <p class="intro">${copy('Website feedback and live chat for SaaS teams.')}</p>
    <div class="composer">
      <p class="example">${copy("The checkout button isn't working on my phone.")}</p>
      <div class="composer-footer">
        <span class="try"><svg width="26" height="26" viewBox="0 0 24 24" aria-hidden="true"><path d="M21 11.5a8.4 8.4 0 0 1-.9 3.8 8.5 8.5 0 0 1-7.6 4.7 8.4 8.4 0 0 1-3.8-.9L3 21l1.9-5.7a8.4 8.4 0 0 1-.9-3.8 8.5 8.5 0 0 1 4.7-7.6 8.4 8.4 0 0 1 3.8-.9h.5a8.5 8.5 0 0 1 8 8v.5Z"/></svg>${copy('Try Fidibeki')}</span>
        <span class="send"><svg width="28" height="28" viewBox="0 0 24 24" aria-hidden="true"><path d="m5 12 7-7 7 7M12 5v14"/></svg></span>
      </div>
    </div>
  </main>
</body></html>`, { waitUntil: 'load' })
    await page.evaluate(async () => {
      await document.fonts.ready
      await Promise.all([...document.images].map(image => image.decode()))
      const heading = document.querySelector('h1')
      for (let size = 76; heading.scrollWidth > heading.clientWidth && size > 58; size--) heading.style.fontSize = `${size - 1}px`
      for (const selector of ['h1', '.intro', '.example', '.composer']) {
        const element = document.querySelector(selector)
        const rect = element.getBoundingClientRect()
        if (element.scrollWidth > element.clientWidth || rect.bottom > 594) throw new Error(`Social preview overflow: ${selector}`)
      }
    })
    const png = await page.screenshot({ type: 'png' })
    const hash = createHash('sha256').update(png).digest('hex').slice(0, 12)
    const filename = `fidibeki-${locale.code}-${hash}.png`
    await writeFile(path.join(output, filename), png)
    // Refresh the old unversioned URL for direct references. New metadata uses
    // only the hashed path, since clients may have cached the legacy response.
    if (locale.code === 'en') await writeFile(path.join(output, 'og-card.png'), png)
    preview[locale.code] = `/og/${filename}`
    console.log(`Wrote ${filename}`)
  }
  await writeFile(path.join(root, 'data', 'social-preview.json'), `${JSON.stringify(preview, null, 2)}\n`)
} finally {
  await browser.close()
}
