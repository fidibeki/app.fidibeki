// Keep these recognizers and attribute sets in sync with replay/content.go.
const personalPatterns = [
  /[A-Z0-9.!#$%&'*+/=?^_`{|}~-]+@[A-Z0-9-]+(?:\.[A-Z0-9-]+)+/gi,
  /\+?\d[\d ().-]{7,}\d/g,
]

export const PRIVATE_TEXT_ATTRIBUTE = 'data-fidibeki-mask'
export const privateTextSelector = [
  '[data-fidibeki-mask]', '[data-private]', '[data-sensitive]', '.rr-mask',
  'input', 'textarea', 'select', '[contenteditable]:not([contenteditable="false"])',
  '[autocomplete="name"]', '[autocomplete="given-name"]', '[autocomplete="family-name"]',
  '[itemtype$="/Person"] [itemprop="name"]', '[itemprop="email"]', '[itemprop="telephone"]',
  '.user-name', '.user-email', '.profile-name', '.customer-name', '.reporter-name',
].join(',')

export function maskReplayText(text: string): string {
  return text.replace(/\S/g, '*')
}

export function redactReplayText(text: string): string {
  for (const pattern of personalPatterns) {
    text = text.replace(pattern, match => {
      if (/^(?:\d{4}-\d{2}-\d{2}|\d{2}-\d{2}-\d{4})$/.test(match)) return match
      return match.includes('@') || match.replace(/\D/g, '').length >= 8 ? maskReplayText(match) : match
    })
  }
  return text
}

export function isPrivateReplayElement(element: Element | null, selector?: string | null): boolean {
  for (let current = element; current; ) {
    if (current.closest(privateTextSelector)) return true
    if (selector) {
      try {
        if (current.closest(selector)) return true
      } catch {
        // An invalid configured selector must never silently expose private text.
        return true
      }
    }
    const root = current.getRootNode()
    current = root.nodeType === 11 && 'host' in root ? (root as ShadowRoot).host : null
  }
  return false
}

export function safeReplayAssetURL(value: string): boolean {
  if (/^#[a-zA-Z_][\w-]*$/.test(value)) return true
  try {
    const url = new URL(value)
    const path = decodeURIComponent(url.pathname)
    return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password &&
      !url.search && !url.hash && redactReplayText(path) === path &&
      !/(?:avatar|profile|user|account|private|upload|attachment|reporter|customer)/i.test(path) &&
      /\.(?:css|woff2?|ttf|otf|png|jpe?g|gif|webp|avif|svg|ico)$/i.test(path)
  } catch {
    return false
  }
}

const cssURL = /url\(\s*(?:"([^"\n]*)"|'([^'\n]*)'|([^)'"\s]*))\s*\)/gi

function decodedCSS(value: string): string {
  return value.replace(/\/\*[\s\S]*?\*\//g, '').replace(
    /\\([0-9a-f]{1,6}\s?|.)/gi,
    (_, escaped: string) => /^[0-9a-f]/i.test(escaped)
      ? String.fromCodePoint(Math.min(parseInt(escaped.trim(), 16) || 0xfffd, 0x10ffff))
      : escaped,
  )
}

function safeCSSValue(value: string): boolean {
  const decoded = decodedCSS(value)
  if (decoded.replace(personalPatterns[0]!, '') !== decoded || /(?:expression|image-set|attr)\s*\(|javascript:|@import/i.test(decoded)) return false
  let safe = true
  const withoutURLs = decoded.replace(cssURL, (_, double: string, single: string, bare: string) => {
    const url = double ?? single ?? bare
    if (url !== '' && !safeReplayAssetURL(url)) safe = false
    return ''
  })
  return safe && !/url\s*\(/i.test(withoutURLs)
}

function sanitizeDeclarations(style: CSSStyleDeclaration): string {
  const sanitized = document.createElement('span').style
  sanitized.cssText = style.cssText
  for (let index = 0; index < style.length; index++) {
    const property = style.item(index)
    const value = style.getPropertyValue(property)
    const privateContent = property === 'content' && !/^(?:none|normal|"[ \t]*"|'[ \t]*')$/.test(value)
    if (value.includes('\\') || !safeCSSValue(value) || privateContent || /^(?:-moz-binding|behavior)$/i.test(property) ||
      (property.startsWith('--') && /(?:name|email|user|account|token|secret|address|phone|reporter)/i.test(property))) {
      sanitized.removeProperty(property)
    }
  }
  return sanitized.cssText
}

/** Use the browser CSS parser, preserving the cascade, variables, media rules and fonts. */
export function sanitizeReplayCSS(value: string, inline = false): string {
  if (new TextEncoder().encode(value).byteLength > 1024 * 1024) return ''
  try {
    if (inline) {
      const style = document.createElement('span').style
      style.cssText = value
      return sanitizeDeclarations(style)
    }
    const sheet = new CSSStyleSheet()
    sheet.replaceSync(value)
    const sanitizeRule = (rule: CSSRule): string => {
      // Imports could load unsanitized CSS later. rrweb already inlines readable sheets.
      if (rule.type === 3) return ':not(*) { }'
      const source = rule.cssText
      const header = source.slice(0, source.indexOf('{')).trim()
      if (!safeCSSValue(header)) return ':not(*) { }'
      const nested = 'cssRules' in rule
        ? Array.from((rule as CSSGroupingRule).cssRules).map(sanitizeRule).join(' ')
        : ''
      if ('style' in rule) {
        const declarations = sanitizeDeclarations((rule as CSSStyleRule).style)
        return `${header} { ${declarations}${nested ? ` ${nested}` : ''} }`
      }
      if ('cssRules' in rule) {
        return `${header} { ${nested} }`
      }
      // Layer ordering statements contain no fetchable resources or generated content.
      return /^@layer\s+[\w., -]+;$/.test(source) ? source : ':not(*) { }'
    }
    return Array.from(sheet.cssRules).map(sanitizeRule).join('\n')
  } catch {
    return ''
  }
}

export function sanitizeReplayStyleMutation(data: Record<string, unknown>): boolean {
  const rules = (entries: unknown) => {
    if (!Array.isArray(entries)) return false
    for (const entry of entries) {
      if (!entry || typeof entry.rule !== 'string') return false
      // Retain rule slots so later CSSOM updates still address the right index.
      entry.rule = sanitizeReplayCSS(entry.rule) || ':not(*) { }'
      if (entry.index === undefined) entry.index = 0
    }
    return true
  }
  if (data.source === 8) {
    if (data.adds !== undefined && !rules(data.adds)) return false
    for (const key of ['replace', 'replaceSync']) {
      if (data[key] !== undefined) {
        if (typeof data[key] !== 'string') return false
        data[key] = sanitizeReplayCSS(data[key])
      }
    }
  } else if (data.source === 13) {
    const change = (data.set ?? data.remove) as Record<string, unknown> | undefined
    if (!change || typeof change.property !== 'string' || !/^-{0,2}[a-zA-Z][\w-]*$/.test(change.property)) return false
    if (data.set) {
      if (typeof change.value !== 'string') return false
      const style = document.createElement('span').style
      style.cssText = sanitizeReplayCSS(`${change.property}: ${change.value}`, true)
      const value = style.getPropertyValue(change.property)
      if (value) data.set = { property: change.property, value, ...(change.priority === 'important' ? { priority: 'important' } : {}) }
      else {
        delete data.set
        data.remove = { property: change.property }
      }
    }
  } else if (data.source === 16 && data.styles !== undefined) {
    if (!Array.isArray(data.styles)) return false
    for (const style of data.styles) if (!style || !rules(style.rules)) return false
  }
  return true
}

const presentationAttributes = new Set([
  'id', 'class', 'type', 'role', 'dir', 'lang', 'hidden', 'open', 'disabled', 'checked', 'selected',
  'width', 'height', 'colspan', 'rowspan', 'span', 'size', 'rows', 'cols', 'wrap', 'slot',
  'viewbox', 'd', 'fill', 'fill-rule', 'fill-opacity', 'stroke', 'stroke-width', 'stroke-linecap',
  'stroke-linejoin', 'stroke-dasharray', 'stroke-dashoffset', 'stroke-opacity', 'clip-rule',
  'cx', 'cy', 'r', 'rx', 'ry', 'x', 'y', 'x1', 'x2', 'y1', 'y2', 'points', 'transform',
  'preserveaspectratio', 'xmlns', 'offset', 'stop-color', 'stop-opacity', 'gradientunits',
  'gradienttransform', 'patternunits', 'patterntransform', 'clip-path', 'mask',
  'aria-expanded', 'aria-hidden', 'aria-checked', 'aria-selected', 'aria-disabled',
  'data-theme', 'data-state', 'data-side', 'data-orientation', 'data-active', 'data-selected', 'aria-current',
  'rr_width', 'rr_height', 'rr_scrollleft', 'rr_scrolltop', 'rr_open_mode',
  'rr_mediastate', 'rr_mediacurrenttime', 'rr_mediaplaybackrate', 'rr_mediamuted', 'rr_medialoop', 'rr_mediavolume',
])

export function sanitizeReplayAttributes(
  attributes: unknown, tagName = '', privateRegion = false,
): Record<string, unknown> {
  const result: Record<string, unknown> = {}
  if (!attributes || typeof attributes !== 'object') return result
  for (const [key, raw] of Object.entries(attributes)) {
    const name = key.toLowerCase()
    if (name === PRIVATE_TEXT_ATTRIBUTE || name === 'data-fidibeki-safe-text') {
      result[key] = raw === null ? null : ''
    } else if (name === '_csstext' && ['style', 'link'].includes(tagName)) {
      result[key] = typeof raw === 'string' ? sanitizeReplayCSS(raw) : ''
    } else if (name === 'style') {
      // rrweb may emit a per-property style diff. The caller replaces it with live cssText.
      result[key] = typeof raw === 'string' ? sanitizeReplayCSS(raw, true) : ''
    } else if (name === 'value' || ['alt', 'title', 'aria-label', 'aria-description'].includes(name)) {
      result[key] = typeof raw === 'string' ? maskReplayText(raw) : null
    } else if (name === 'placeholder') {
      result[key] = typeof raw === 'string' ? redactReplayText(raw) : null
    } else if (['src', 'href', 'xlink:href', 'poster'].includes(name)) {
      const asset = ['img', 'image', 'use', 'video'].includes(tagName)
      result[key] = asset && !privateRegion && typeof raw === 'string' && safeReplayAssetURL(raw) ? raw : null
    } else if (presentationAttributes.has(name) || /^data-v-[\da-f]+(?:-s)?$/.test(name)) {
      if (raw === null || typeof raw === 'boolean' || typeof raw === 'number') result[key] = raw
      else if (typeof raw === 'string' && safeCSSValue(raw)) result[key] = raw
    }
  }
  if (privateRegion) result[PRIVATE_TEXT_ATTRIBUTE] = ''
  return result
}
