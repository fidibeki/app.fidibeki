import { canonicalPage } from '../targeting'
import {
  PRIVATE_TEXT_ATTRIBUTE, isPrivateReplayElement, maskReplayText, redactReplayText,
  sanitizeReplayAttributes, sanitizeReplayCSS, sanitizeReplayStyleMutation,
} from './replay-content'

/**
 * Replay content sanitization. Preserve presentation while redacting inputs, recognized
 * contact details, and private regions before any event is buffered or uploaded.
 */

// Matches rrweb-snapshot's MaskTextFn shape structurally; declared locally because
// rrweb-snapshot is only a transitive dependency (via rrweb) and is not resolvable for types.
type MaskTextFn = (text: string, element: HTMLElement | null) => string

/** Legacy marker retained for compatibility; it never overrides private-region masking. */
export const SAFE_TEXT_ATTRIBUTE = 'data-fidibeki-safe-text'

/** Run the selective redactor on every text node, including ordinary UI labels. */
const MASK_EVERY_ELEMENT_SELECTOR = '*'

const EVENT_TYPE_DOM_CONTENT_LOADED = 0
const EVENT_TYPE_LOAD = 1
const EVENT_TYPE_FULL_SNAPSHOT = 2
const EVENT_TYPE_INCREMENTAL_SNAPSHOT = 3
const EVENT_TYPE_META = 4
const EVENT_TYPE_CUSTOM = 5
const INCREMENTAL_SOURCE_MUTATION = 0
const INCREMENTAL_SOURCE_INPUT = 5
const SAFE_INCREMENTAL_SOURCES = new Set([0, 1, 2, 3, 4, 5, 6, 7, 8, 12, 13, 14, 16])
const ELEMENT_NODE_TYPE = 2
const TEXT_NODE_TYPE = 3
const CDATA_NODE_TYPE = 4
const COMMENT_NODE_TYPE = 5

/** Canonicalizes a URL to origin + normalized pathname, stripping credentials, query, and hash. */
export function canonicalizeReplayURL(rawURL: string): string | null {
  return canonicalPage(rawURL)?.target ?? null
}

/** Every text node must pass through the selective redactor. */
export function replayMaskTextSelector(): string {
  return MASK_EVERY_ELEMENT_SELECTOR
}

/** Apply contact redaction to every text node and full masking only to private regions. */
export function createReplayMaskTextFn(selector?: string | null): MaskTextFn {
  return (text, element) => {
    // rrweb also calls this hook for CSS character-data mutations. CSS is parsed
    // separately before buffering; contact matching here would corrupt numeric values.
    if (element?.tagName.toLowerCase() === 'style') return text
    return isPrivateReplayElement(element, selector) ? maskReplayText(text) : redactReplayText(text)
  }
}

export type ReplayNodeResolver = (id: number) => Node | null

function resolveReplayElement(id: unknown, resolveNode?: ReplayNodeResolver): Element | null {
  if (!resolveNode || typeof id !== 'number' || !Number.isInteger(id) || id < 0) return null
  try {
    const node = resolveNode(id)
    return node?.nodeType === 1 ? node as Element : null
  } catch {
    return null
  }
}

function sanitizeAttributeMap(
  attributes: unknown,
  element: Element | null = null,
  tagName = '',
  privateRegion = false,
): Record<string, unknown> {
  if (element && attributes && typeof attributes === 'object' && 'style' in attributes) {
    // rrweb style diffs are objects, but the live inline declaration is the complete state.
    const raw = attributes as Record<string, unknown>
    if (typeof raw.style === 'object' && raw.style !== null) {
      raw.style = element.getAttribute('style') ?? ''
    }
  }
  return sanitizeReplayAttributes(attributes, element?.tagName.toLowerCase() ?? tagName, privateRegion)
}

function textElement(id: unknown, resolveNode?: ReplayNodeResolver): Element | null {
  if (typeof id !== 'number' || !resolveNode) return null
  try {
    const node = resolveNode(id)
    return node?.nodeType === 1 ? node as Element : node?.parentElement ?? null
  } catch {
    return null
  }
}

function sanitizeSnapshotNode(
  node: unknown,
  privateAncestor = false,
  resolveNode?: ReplayNodeResolver,
  selector?: string | null,
  parentTag = '',
): boolean {
  if (!node || typeof node !== 'object') return false
  const candidate = node as Record<string, unknown>
  const element = textElement(candidate.id, resolveNode)
  let privateRegion = privateAncestor || isPrivateReplayElement(element, selector)
  const tag = typeof candidate.tagName === 'string' ? candidate.tagName.toLowerCase() : parentTag
  // Serialized markers also cover detached nodes and verifier-only contract fixtures.
  if (candidate.type === ELEMENT_NODE_TYPE) {
    const attrs = candidate.attributes as Record<string, unknown> | undefined
    privateRegion ||= !!attrs && [PRIVATE_TEXT_ATTRIBUTE, 'data-private', 'data-sensitive'].some(key => key in attrs)
    privateRegion ||= ['input', 'textarea', 'select'].includes(tag)
    candidate.attributes = sanitizeAttributeMap(candidate.attributes, element, tag, privateRegion)
  }
  if (candidate.type === TEXT_NODE_TYPE && typeof candidate.textContent === 'string') {
    candidate.textContent = tag === 'style' || candidate.isStyle === true
      ? sanitizeReplayCSS(candidate.textContent)
      : tag === 'script' ? ''
      : privateRegion ? maskReplayText(candidate.textContent) : redactReplayText(candidate.textContent)
  }
  if (candidate.type === CDATA_NODE_TYPE || candidate.type === COMMENT_NODE_TYPE) candidate.textContent = ''
  if (Array.isArray(candidate.childNodes)) {
    for (const child of candidate.childNodes) {
      if (!sanitizeSnapshotNode(child, privateRegion, resolveNode, selector, tag)) return false
    }
  }
  return true
}

interface AttributeMutationLike {
  id?: unknown
  attributes?: unknown
}

interface TextMutationLike {
  id?: unknown
  value?: unknown
}

interface AddedNodeMutationLike {
  parentId?: unknown
  node?: unknown
}

interface ReplayEventLike {
  type?: unknown
  data?: {
    href?: unknown
    node?: unknown
    source?: unknown
    attributes?: unknown
    adds?: unknown
    texts?: unknown
    text?: unknown
    tag?: unknown
    payload?: unknown
  }
}

/**
 * True for incremental events that reference no serialized node: rrweb stamps mirror id -1
 * on interactions inside blocked regions (e.g. the Fidibeki widget's own UI). They cannot
 * replay, so callers drop them silently as policy rather than reporting a failure.
 */
export function isUnserializedReplayEvent(event: unknown): boolean {
  if (!event || typeof event !== 'object') return false
  const candidate = event as ReplayEventLike
  return candidate.type === EVENT_TYPE_INCREMENTAL_SNAPSHOT &&
    (candidate.data as { id?: unknown } | undefined)?.id === -1
}

/**
 * Sanitizes one serialized rrweb event in place: preserves page presentation, redacts personal
 * information, and pins navigation URLs to the safe session projection. Returns `null`
 * when the event cannot be safely processed so the caller fails closed.
 */
export function sanitizeReplayEvent(
  event: unknown,
  sessionPageURL: string,
  resolveNode?: ReplayNodeResolver,
  selector?: string | null,
): unknown | null {
  if (!event || typeof event !== 'object') return null
  try {
    const canonicalSessionPageURL = canonicalizeReplayURL(sessionPageURL)
    if (!canonicalSessionPageURL || canonicalSessionPageURL !== sessionPageURL) return null
    const candidate = event as ReplayEventLike
    if (
      candidate.type === EVENT_TYPE_DOM_CONTENT_LOADED ||
      candidate.type === EVENT_TYPE_LOAD
    ) return candidate
    if (candidate.type === EVENT_TYPE_META) {
      if (!candidate.data || typeof candidate.data.href !== 'string') return null
      const canonical = canonicalizeReplayURL(candidate.data.href)
      if (!canonical) return null
      candidate.data.href = canonicalSessionPageURL
      return candidate
    }
    if (candidate.type === EVENT_TYPE_FULL_SNAPSHOT) {
      if (!candidate.data || !sanitizeSnapshotNode(candidate.data.node, false, resolveNode, selector)) return null
      return candidate
    }
    if (candidate.type === EVENT_TYPE_INCREMENTAL_SNAPSHOT) {
      if (!candidate.data || typeof candidate.data.source !== 'number') return null
      if (!SAFE_INCREMENTAL_SOURCES.has(candidate.data.source)) return null
      if (candidate.data.source === INCREMENTAL_SOURCE_MUTATION) {
        if (Array.isArray(candidate.data.attributes)) {
          for (const entry of candidate.data.attributes as AttributeMutationLike[]) {
            entry.attributes = sanitizeAttributeMap(
              entry.attributes,
              resolveReplayElement(entry.id, resolveNode),
              '',
              isPrivateReplayElement(textElement(entry.id, resolveNode), selector),
            )
          }
        }
        if (Array.isArray(candidate.data.texts)) {
          for (const entry of candidate.data.texts as TextMutationLike[]) {
            if (typeof entry.value === 'string') {
              const element = textElement(entry.id, resolveNode)
              entry.value = element?.tagName.toLowerCase() === 'style'
                ? sanitizeReplayCSS(entry.value)
                : createReplayMaskTextFn(selector)(entry.value, element as HTMLElement | null)
            }
          }
        }
        if (Array.isArray(candidate.data.adds)) {
          for (const entry of candidate.data.adds as AddedNodeMutationLike[]) {
            const privateParent = isPrivateReplayElement(textElement(entry.parentId, resolveNode), selector)
            if (!sanitizeSnapshotNode(entry.node, privateParent, resolveNode, selector)) return null
          }
        }
      } else if (
        candidate.data.source === INCREMENTAL_SOURCE_INPUT &&
        typeof candidate.data.text === 'string'
      ) {
        candidate.data.text = maskReplayText(candidate.data.text)
      }
      if ([8, 13, 16].includes(candidate.data.source)) {
        if (!sanitizeReplayStyleMutation(candidate.data as Record<string, unknown>)) return null
      }
      return candidate
    }
    if (candidate.type === EVENT_TYPE_CUSTOM) {
      if (
        !candidate.data ||
        candidate.data.tag !== 'navigation' ||
        !candidate.data.payload ||
        typeof candidate.data.payload !== 'object'
      ) return null
      const payload = candidate.data.payload as Record<string, unknown>
      if (typeof payload.url !== 'string') return null
      const canonical = canonicalizeReplayURL(payload.url)
      if (!canonical) return null
      candidate.data.payload = { url: canonicalSessionPageURL }
      return candidate
    }
    return null
  } catch {
    return null
  }
}
