import { mkdtemp, mkdir, rm, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import path from 'node:path'
import { afterAll, beforeAll, describe, expect, it } from 'vitest'
// eslint-disable-next-line import/no-relative-packages
import { buildSitemap, collectRoutes } from '../../scripts/generate-sitemap.mjs'

let fixtureDir: string

beforeAll(async () => {
  fixtureDir = await mkdtemp(path.join(tmpdir(), 'fidibeki-sitemap-'))
  await writeFile(path.join(fixtureDir, 'index.html'), '<html></html>')
  await writeFile(path.join(fixtureDir, '200.html'), '<html></html>')
  await writeFile(path.join(fixtureDir, '404.html'), '<html></html>')
  await mkdir(path.join(fixtureDir, 'pricing'), { recursive: true })
  await writeFile(
    path.join(fixtureDir, 'pricing', 'index.html'),
    '<html></html>',
  )
  await mkdir(path.join(fixtureDir, 'features', 'session-replay'), {
    recursive: true,
  })
  await writeFile(
    path.join(fixtureDir, 'features', 'session-replay', 'index.html'),
    '<html></html>',
  )
  await mkdir(path.join(fixtureDir, '_nuxt'), { recursive: true })
  await writeFile(path.join(fixtureDir, '_nuxt', 'app.js'), 'console.log(1)')
})

afterAll(async () => {
  await rm(fixtureDir, { recursive: true, force: true })
})

describe('sitemap generation', () => {
  it('collects only html routes', async () => {
    const routes = await collectRoutes(fixtureDir)
    expect(routes.sort()).toEqual([
      '/',
      '/200.html',
      '/404.html',
      '/features/session-replay',
      '/pricing',
    ])
  })

  it('excludes error documents and emits valid xml', async () => {
    const routes = await collectRoutes(fixtureDir)
    const xml = buildSitemap(routes, 'https://fidibeki.com')
    expect(xml).toContain('<?xml version="1.0" encoding="UTF-8"?>')
    expect(xml).toContain('<loc>https://fidibeki.com/</loc>')
    expect(xml).toContain('<loc>https://fidibeki.com/pricing</loc>')
    expect(xml).toContain(
      '<loc>https://fidibeki.com/features/session-replay</loc>',
    )
    expect(xml).not.toContain('404.html')
    expect(xml).not.toContain('200.html')
  })
})

it('lists only existing language counterparts, including self and x-default', () => {
  const xml = buildSitemap(
    ['/pricing', '/pt/pricing', '/es/pricing', '/fr/pricing', '/pricing'],
    'https://fidibeki.com',
  )
  expect(xml.match(/<url>/g)).toHaveLength(4)
  for (const entry of xml.matchAll(/<url>([\s\S]*?)<\/url>/g)) {
    expect(entry[1]).toContain(
      'hreflang="en" href="https://fidibeki.com/pricing"',
    )
    expect(entry[1]).toContain(
      'hreflang="pt-PT" href="https://fidibeki.com/pt/pricing"',
    )
    expect(entry[1]).toContain(
      'hreflang="es" href="https://fidibeki.com/es/pricing"',
    )
    expect(entry[1]).toContain(
      'hreflang="fr" href="https://fidibeki.com/fr/pricing"',
    )
    expect(entry[1]).toContain(
      'hreflang="x-default" href="https://fidibeki.com/pricing"',
    )
  }
  const partial = buildSitemap(
    ['/pricing', '/pt/pricing'],
    'https://fidibeki.com',
  )
  expect(partial).not.toContain('/fr/pricing')
})
