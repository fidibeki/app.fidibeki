import type { ElementAttachment } from '../types'

const capturedStyles = [
  'display', 'position', 'color', 'background-color', 'font-size', 'font-weight',
  'line-height', 'text-align', 'box-sizing', 'width', 'height', 'min-width', 'min-height',
  'max-width', 'max-height', 'padding-top', 'padding-right', 'padding-bottom', 'padding-left',
  'margin-top', 'margin-right', 'margin-bottom', 'margin-left',
  'border-top-width', 'border-right-width', 'border-bottom-width', 'border-left-width',
  'border-top-style', 'border-right-style', 'border-bottom-style', 'border-left-style',
  'border-top-color', 'border-right-color', 'border-bottom-color', 'border-left-color',
  'border-top-left-radius', 'border-top-right-radius', 'border-bottom-right-radius',
  'border-bottom-left-radius',
]

const allowedTags = new Set([
  'address', 'article', 'aside', 'blockquote', 'dd', 'div', 'dl', 'dt', 'figcaption',
  'figure', 'footer', 'h1', 'h2', 'h3', 'h4', 'h5', 'h6', 'header', 'li', 'main',
  'nav', 'ol', 'p', 'section', 'span', 'table', 'tbody', 'td', 'tfoot', 'th',
  'thead', 'tr', 'ul',
])
const allowedStyleKeywords = new Set([
  'absolute', 'auto', 'block', 'bold', 'bolder', 'border-box', 'center', 'contents',
  'dashed', 'dotted', 'double', 'end', 'fixed', 'flex', 'grid', 'groove', 'hidden',
  'inherit', 'initial', 'inline', 'inline-block', 'inline-flex', 'inline-grid', 'inset',
  'left', 'lighter', 'none', 'normal', 'outset', 'relative', 'revert', 'ridge', 'right',
  'solid', 'start', 'static', 'sticky', 'table', 'table-cell', 'table-row', 'transparent',
  'unset',
])
const maximumNodes = 128
const maximumDepth = 12
const maximumSelectorSegments = 6

export function chooseElement(excludedHost: HTMLElement): Promise<ElementAttachment | null> {
  return new Promise((resolve) => {
    const overlay = document.createElement('div')
    overlay.style.cssText = 'position:fixed;z-index:2147482999;pointer-events:none;border:2px solid #147d64;background:rgba(20,125,100,.1);display:none'
    document.body.append(overlay)
    let candidate: Element | null = null

    const finish = (attachment: ElementAttachment | null) => {
      document.removeEventListener('pointermove', onMove, true)
      document.removeEventListener('click', onClick, true)
      document.removeEventListener('keydown', onKey, true)
      overlay.remove()
      resolve(attachment)
    }
    const onMove = (event: PointerEvent) => {
      const target = event.target
      if (!(target instanceof Element) || target === excludedHost || excludedHost.contains(target)) return
      candidate = target
      const bounds = target.getBoundingClientRect()
      overlay.style.display = 'block'
      overlay.style.left = `${bounds.left}px`
      overlay.style.top = `${bounds.top}px`
      overlay.style.width = `${bounds.width}px`
      overlay.style.height = `${bounds.height}px`
    }
    const onClick = (event: MouseEvent) => {
      event.preventDefault()
      event.stopImmediatePropagation()
      if (!candidate) return finish(null)
      const computed = getComputedStyle(candidate)
      finish(buildSafeElementAttachment(candidate, computed))
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') finish(null)
    }
    document.addEventListener('pointermove', onMove, true)
    document.addEventListener('click', onClick, true)
    document.addEventListener('keydown', onKey, true)
  })
}

export function buildSafeElementAttachment(
  element: Element,
  computed: Pick<CSSStyleDeclaration, 'getPropertyValue'> = getComputedStyle(element),
): ElementAttachment | null {
  const state = { nodes: 0, unsafe: false }
  const sanitized = sanitizeElement(element, element.ownerDocument, state, 1)
  if (!sanitized || state.unsafe) return null

  const styles: Record<string, string> = {}
  for (const property of capturedStyles) {
    const value = sanitizeCapturedStyle(computed.getPropertyValue(property))
    if (value !== null && value !== '') styles[property] = value
  }
  return {
    selector: selectorFor(element),
    html: sanitized.outerHTML,
    styles,
  }
}

export function sanitizeCapturedStyle(input: string): string | null {
  const value = input.trim().toLowerCase()
  if (value === '') return ''
  if (value.length > 100 || /[\u0000-\u001f\u007f"'`;:{}[\]\\]/.test(value)) return null
  if (/(?:url|var|attr|calc|env|expression)\s*\(/.test(value)) return null
  if (allowedStyleKeywords.has(value)) return value
  if (/^#[0-9a-f]{3,8}$/.test(value)) return value
  if (/^rgba?\(\s*\d{1,3}(?:\.\d+)?%?\s*,\s*\d{1,3}(?:\.\d+)?%?\s*,\s*\d{1,3}(?:\.\d+)?%?(?:\s*,\s*(?:0(?:\.\d+)?|1(?:\.0+)?))?\s*\)$/.test(value)) {
    return value
  }
  const length = String.raw`-?(?:\d+(?:\.\d+)?|\.\d+)(?:px|%|em|rem|vh|vw)?`
  if (new RegExp(`^${length}(?:\\s+${length}){0,3}$`).test(value)) return value
  return null
}

function sanitizeElement(
  source: Element,
  documentObject: Document,
  state: { nodes: number; unsafe: boolean },
  depth: number,
): Element | null {
  const tag = source.tagName.toLowerCase()
  if (!allowedTags.has(tag)) return null
  state.nodes++
  if (state.nodes > maximumNodes || depth > maximumDepth) {
    state.unsafe = true
    return null
  }
  const output = documentObject.createElement(tag)
  for (const child of source.children) {
    const sanitized = sanitizeElement(child, documentObject, state, depth + 1)
    if (state.unsafe) return null
    if (sanitized) output.append(sanitized)
  }
  return output
}

function selectorFor(element: Element): string {
  const parts: string[] = []
  let current: Element | null = element
  const body = element.ownerDocument.body
  while (current && current !== body && parts.length < maximumSelectorSegments) {
    let part = current.tagName.toLowerCase()
    const siblings = current.parentElement ? Array.from(current.parentElement.children).filter((item) => item.tagName === current?.tagName) : []
    if (siblings.length > 1) part += `:nth-of-type(${siblings.indexOf(current) + 1})`
    parts.unshift(part)
    current = current.parentElement
  }
  return parts.join(' > ')
}
