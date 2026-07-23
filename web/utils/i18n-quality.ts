export type MessageCatalog = {
  [key: string]: string | MessageCatalog
}

const pseudoCharacters: Record<string, string> = {
  A: 'ÅÅ',
  B: 'ß',
  C: 'Ç',
  D: 'Ð',
  E: 'ËË',
  F: 'F',
  G: 'G',
  H: 'H',
  I: 'ÏÏ',
  J: 'J',
  K: 'K',
  L: '£',
  M: 'M',
  N: 'Ñ',
  O: 'ØØ',
  P: 'Þ',
  Q: 'Q',
  R: 'R',
  S: 'Š',
  T: 'T',
  U: 'ÜÜ',
  V: 'V',
  W: 'W',
  X: 'X',
  Y: 'ÝÝ',
  Z: 'Ž',
  a: 'åå',
  b: 'þ',
  c: 'ç',
  d: 'ð',
  e: 'ëë',
  f: 'ƒ',
  g: 'g',
  h: 'h',
  i: 'ïï',
  j: 'j',
  k: 'k',
  l: '£',
  m: 'm',
  n: 'ñ',
  o: 'øø',
  p: 'þ',
  q: 'q',
  r: 'r',
  s: 'š',
  t: 't',
  u: 'üü',
  v: 'v',
  w: 'w',
  x: 'x',
  y: 'ýý',
  z: 'ž',
}

function pseudoLocalizeBranch(message: string) {
  const source = message.trim()
  const transformed = source.replace(/\{[^{}]*\}|[A-Za-z]/g, token =>
    token.startsWith('{') ? token : pseudoCharacters[token] || token,
  )
  const padding = '~'.repeat(Math.max(3, Math.ceil(source.length * 0.3)))
  return `[${transformed} ${padding}]`
}

export function pseudoLocalizeMessage(message: string) {
  return message
    .split('|')
    .map(pseudoLocalizeBranch)
    .join(' | ')
}

export function pseudoLocalizeCatalog(catalog: MessageCatalog): MessageCatalog {
  return Object.fromEntries(
    Object.entries(catalog).map(([key, value]) => [
      key,
      typeof value === 'string' ? pseudoLocalizeMessage(value) : pseudoLocalizeCatalog(value),
    ]),
  )
}

export function flattenCatalog(catalog: MessageCatalog, prefix = ''): Map<string, string> {
  const entries = new Map<string, string>()
  for (const [key, value] of Object.entries(catalog)) {
    const path = prefix ? `${prefix}.${key}` : key
    if (typeof value === 'string') {
      entries.set(path, value)
    } else {
      for (const [nestedKey, nestedValue] of flattenCatalog(value, path)) {
        entries.set(nestedKey, nestedValue)
      }
    }
  }
  return entries
}

export function interpolationTokens(message: string) {
  return [...message.matchAll(/\{[^{}]*\}/g)].map(match => match[0]).sort()
}

export function catalogParity(reference: MessageCatalog, candidate: MessageCatalog) {
  const referenceEntries = flattenCatalog(reference)
  const candidateEntries = flattenCatalog(candidate)
  const referenceKeys = new Set(referenceEntries.keys())
  const candidateKeys = new Set(candidateEntries.keys())

  return {
    missing: [...referenceKeys].filter(key => !candidateKeys.has(key)).sort(),
    extra: [...candidateKeys].filter(key => !referenceKeys.has(key)).sort(),
    interpolationMismatches: [...referenceEntries].flatMap(([key, referenceMessage]) => {
      const candidateMessage = candidateEntries.get(key)
      if (candidateMessage === undefined) return []
      const referenceTokens = interpolationTokens(referenceMessage)
      const candidateTokens = interpolationTokens(candidateMessage)
      return referenceTokens.join('\0') === candidateTokens.join('\0')
        ? []
        : [{ key, referenceTokens, candidateTokens }]
    }),
  }
}

export interface VisibleLiteral {
  kind: 'attribute' | 'expression' | 'script' | 'text'
  line: number
  value: string
}

const visibleAttributes = new Set(['alt', 'aria-label', 'placeholder', 'title'])

function lineAt(source: string, index: number) {
  return source.slice(0, index).split('\n').length
}

function humanText(value: string) {
  const normalized = value.trim()
  if (/^#[\da-f]{3,8}$/i.test(normalized)) return false
  return /[A-Za-zÀ-ÿ]{2}/.test(normalized.replace(/&(?:#\d+|#x[\da-f]+|\w+);/gi, ''))
}

export function findVisibleLiterals(source: string): VisibleLiteral[] {
  const templateMatch = /<template(?:\s[^>]*)?>([\s\S]*?)<\/template>/.exec(source)
  if (!templateMatch || templateMatch.index === undefined) return []

  const template = templateMatch[1]
  const templateOffset = templateMatch.index + templateMatch[0].indexOf(template)
  const masked = [...template]
  const literals: VisibleLiteral[] = []

  const scriptPatterns = [
    /useHead\(\s*\{[\s\S]{0,240}?title\s*:\s*(['"])(.*?)\1/g,
    /(?:notice|errorMessage|successMessage)\.value\s*=\s*(['"])(.*?)\1/g,
    /(?:window\.)?(?:alert|confirm)\(\s*(['"])(.*?)\1/g,
  ]
  for (const pattern of scriptPatterns) {
    for (const match of source.matchAll(pattern)) {
      const value = match[2].trim()
      const isCatalogKey = /^[\w-]+(?:\.[\w-]+)+$/.test(value)
      if (humanText(value) && !isCatalogKey) {
        literals.push({
          kind: 'script',
          line: lineAt(source, match.index),
          value,
        })
      }
    }
  }

  const mask = (start: number, end: number, boundary = false) => {
    for (let index = start; index < end; index += 1) {
      if (masked[index] !== '\n') masked[index] = ' '
    }
    if (boundary && masked[start] !== '\n') masked[start] = '\0'
  }

  for (const match of template.matchAll(/<!--[\s\S]*?-->|{{[\s\S]*?}}/g)) {
    if (match[0].startsWith('{{')) {
      const expression = match[0].slice(2, -2)
      for (const branch of expression.matchAll(/(?:^|[?:]|\|\||\?\?)\s*(['"])(.*?)\1/g)) {
        const value = branch[2].trim()
        if (humanText(value)) {
          literals.push({
            kind: 'expression',
            line: lineAt(source, templateOffset + match.index + branch.index),
            value,
          })
        }
      }
    }
    mask(match.index, match.index + match[0].length)
  }

  const tags: Array<{ end: number, source: string, start: number }> = []
  for (let start = 0; start < template.length; start += 1) {
    if (
      template[start] !== '<'
      || masked[start] === ' '
      || !/[A-Za-z/!]/.test(template[start + 1] || '')
    ) continue
    let quote = ''
    let end = start + 1
    for (; end < template.length; end += 1) {
      const character = template[end]
      if (quote) {
        if (character === quote && template[end - 1] !== '\\') quote = ''
      } else if (character === '"' || character === "'") {
        quote = character
      } else if (character === '>') {
        end += 1
        break
      }
    }
    tags.push({ start, end, source: template.slice(start, end) })
    start = end - 1
  }

  for (const tag of tags) {
    for (const attribute of tag.source.matchAll(/(?:^|\s)([:@\w-]+)\s*=\s*(["'])(.*?)\2/g)) {
      const name = attribute[1]
      const value = attribute[3].trim()
      const baseName = name.replace(/^:/, '')
      const boundLiteral = name.startsWith(':')
        && visibleAttributes.has(baseName)
        && [...value.matchAll(/(['"`])((?:\\.|(?!\1).)*)\1/g)]
          .some((match) => {
            const before = value.slice(0, match.index)
            const isCatalogKey = (
              /(?:\bt|\$t|\bplural)\s*\(/.test(value)
              && /^[\w-]+(?:\.[\w-]+)+$/.test(match[2])
            ) || /(?:\bt|\$t|\bplural)\s*\(\s*$/.test(before)
            return !isCatalogKey && humanText(match[2])
          })
      if ((visibleAttributes.has(name) && value && humanText(value)) || boundLiteral) {
        literals.push({
          kind: 'attribute',
          line: lineAt(source, templateOffset + tag.start + attribute.index),
          value,
        })
      }
    }
    mask(tag.start, tag.end, true)
  }

  masked.join('').split('\n').forEach((line, index) => {
    line.split('\0').forEach((segment) => {
      const value = segment.replace(/\s+/g, ' ').trim()
      if (value && humanText(value)) {
        literals.push({
          kind: 'text',
          line: lineAt(source, templateOffset) + index,
          value,
        })
      }
    })
  })

  return literals.sort((left, right) => left.line - right.line || left.kind.localeCompare(right.kind))
}
