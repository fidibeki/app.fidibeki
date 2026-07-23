import { readdirSync, readFileSync } from 'node:fs'
import { extname, join, relative } from 'node:path'
import { describe, expect, it } from 'vitest'
import { findVisibleLiterals } from '../../utils/i18n-quality'

const webRoot = join(import.meta.dirname, '../..')
const scanRoots = ['components', 'layouts', 'pages']
const excludedDirectories = new Set(['design', 'studies', 'designs'])
const allowedLiterals = new Set([
  '#feedback',
  '.private, [data-sensitive]',
  '*.preview.example.com',
  'ACME',
  'Africa/Luanda',
  'en',
  'example.com&#10;*.preview.example.com',
  'Europe/Lisbon',
  'Fidibeki',
  'https://cdn.example.com/logo.svg',
  'https://example.com',
  'https://example.com/docs?source=test',
  "condition.matcher_type === 'url_regex' ? '^https://example\\\\.com/docs' : 'https://example.com/docs'",
  "condition.matcher_type === 'url_regex' ? '^https://example\\\\.com/account' : 'https://example.com/account'",
  'https://example.com/privacy',
  'https://example.com/terms',
  'Slack',
  'pt-ao',
  'reporter@example.com',
  'security@company.com',
  'support@example.com',
  'Teams',
  'UTC',
  'Webhook',
  'https://hooks.example.com/...',
])

function vueFiles(directory: string): string[] {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name)
    if (entry.isDirectory()) {
      return excludedDirectories.has(entry.name) ? [] : vueFiles(path)
    }
    return extname(entry.name) === '.vue' ? [path] : []
  })
}

describe('localized source coverage', () => {
  it('keeps visible production copy in the message catalogs', () => {
    const violations = scanRoots
      .flatMap(root => vueFiles(join(webRoot, root)))
      .concat(join(webRoot, 'app.vue'))
      .sort()
      .flatMap(file => findVisibleLiterals(readFileSync(file, 'utf8')).map(literal => ({
        file: relative(webRoot, file),
        ...literal,
      })))
      .filter(literal => !allowedLiterals.has(literal.value))
      .map(literal => `${literal.file}:${literal.line} [${literal.kind}] ${literal.value}`)

    expect(violations).toEqual([])
  })
})
