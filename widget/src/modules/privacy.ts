import { canonicalPage } from '../targeting'

/**
 * Shared anonymity primitives for replay capture (issue #53). These are intentionally
 * generic over rrweb's serialized event shape rather than tied to any one rule/config so
 * a future automatic feedback/element-context capture path (issue #54) can reuse them.
 */

// Matches rrweb-snapshot's MaskTextFn shape structurally; declared locally because
// rrweb-snapshot is only a transitive dependency (via rrweb) and is not resolvable for types.
type MaskTextFn = (text: string, element: HTMLElement | null) => string

/** Explicit, safe opt-in for text known not to identify a person (e.g. static marketing copy). */
export const SAFE_TEXT_ATTRIBUTE = 'data-fidibeki-safe-text'

/** rrweb's own maskTextSelector/maskTextClass require customer configuration to activate;
 * matching every element makes masking unconditional so anonymity does not depend on it. */
const MASK_EVERY_ELEMENT_SELECTOR = '*'

const EVENT_TYPE_DOM_CONTENT_LOADED = 0
const EVENT_TYPE_LOAD = 1
const EVENT_TYPE_FULL_SNAPSHOT = 2
const EVENT_TYPE_INCREMENTAL_SNAPSHOT = 3
const EVENT_TYPE_META = 4
const EVENT_TYPE_CUSTOM = 5
const INCREMENTAL_SOURCE_MUTATION = 0
const INCREMENTAL_SOURCE_INPUT = 5
const SAFE_INCREMENTAL_SOURCES = new Set([0, 1, 2, 3, 4, 5, 6, 7, 12, 14])
const ELEMENT_NODE_TYPE = 2
const TEXT_NODE_TYPE = 3
const CDATA_NODE_TYPE = 4
const COMMENT_NODE_TYPE = 5

/** Canonicalizes a URL to origin + normalized pathname, stripping credentials, query, and hash. */
export function canonicalizeReplayURL(rawURL: string): string | null {
  return canonicalPage(rawURL)?.target ?? null
}

/** The selector rrweb must use so every element is subject to text masking by default. */
export function replayMaskTextSelector(): string {
  return MASK_EVERY_ELEMENT_SELECTOR
}

function isWithinSafeTextRegion(element: Element | null): boolean {
  return element !== null && element.closest(`[${SAFE_TEXT_ATTRIBUTE}]`) !== null
}

/** Masks every text node by default; only text inside an explicit safe opt-in passes through. */
export function createReplayMaskTextFn(): MaskTextFn {
  return (text, element) => (isWithinSafeTextRegion(element) ? text : text.replace(/\S/g, '*'))
}

function maskReplayText(value: string): string {
  return value.replace(/\S/g, '*')
}

function sanitizeAttributeMap(attributes: unknown): Record<string, unknown> {
  const sanitized: Record<string, unknown> = {}
  if (!attributes || typeof attributes !== 'object') return sanitized
  for (const [key, value] of Object.entries(attributes as Record<string, unknown>)) {
    if (key.toLowerCase() !== SAFE_TEXT_ATTRIBUTE) continue
    sanitized[SAFE_TEXT_ATTRIBUTE] = value === null ? null : ''
  }
  return sanitized
}

interface SnapshotNodeLike {
  type?: unknown
  attributes?: unknown
  childNodes?: unknown
  textContent?: unknown
}

function sanitizeSnapshotNode(node: unknown, safeTextAncestor = false): boolean {
  if (!node || typeof node !== 'object') return false
  const candidate = node as SnapshotNodeLike & Record<string, unknown>
  let safeText = safeTextAncestor
  if (candidate.type === ELEMENT_NODE_TYPE) {
    const attributes = candidate.attributes
    safeText = safeTextAncestor || (
      attributes !== null &&
      typeof attributes === 'object' &&
      Object.keys(attributes as Record<string, unknown>)
        .some(key => key.toLowerCase() === SAFE_TEXT_ATTRIBUTE)
    )
    candidate.attributes = sanitizeAttributeMap(candidate.attributes)
  }
  if (candidate.type === TEXT_NODE_TYPE && typeof candidate.textContent === 'string' && !safeText) {
    candidate.textContent = maskReplayText(candidate.textContent)
  }
  // CDATA and HTML comments are not covered by rrweb's text-masking pipeline.
  if (
    (candidate.type === CDATA_NODE_TYPE || candidate.type === COMMENT_NODE_TYPE) &&
    typeof candidate.textContent === 'string'
  ) {
    candidate.textContent = ''
  }
  if (Array.isArray(candidate.childNodes)) {
    for (const child of candidate.childNodes) {
      if (!sanitizeSnapshotNode(child, safeText)) return false
    }
  }
  return true
}

interface AttributeMutationLike {
  attributes?: unknown
}

interface TextMutationLike {
  value?: unknown
}

interface AddedNodeMutationLike {
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
 * Sanitizes one serialized rrweb event in place for anonymity: pins Meta/navigation URLs to
 * the server-verifiable session projection, and strips non-allowlisted attributes from
 * full-snapshot and newly-added DOM nodes (rrweb has no built-in hook for this). Returns
 * `null` when the event cannot be safely processed so the caller fails closed.
 */
export function sanitizeReplayEvent(event: unknown, sessionPageURL: string): unknown | null {
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
      if (!candidate.data || !sanitizeSnapshotNode(candidate.data.node)) return null
      return candidate
    }
    if (candidate.type === EVENT_TYPE_INCREMENTAL_SNAPSHOT) {
      if (!candidate.data || typeof candidate.data.source !== 'number') return null
      if (!SAFE_INCREMENTAL_SOURCES.has(candidate.data.source)) return null
      if (candidate.data.source === INCREMENTAL_SOURCE_MUTATION) {
        if (Array.isArray(candidate.data.attributes)) {
          for (const entry of candidate.data.attributes as AttributeMutationLike[]) {
            entry.attributes = sanitizeAttributeMap(entry.attributes)
          }
        }
        if (Array.isArray(candidate.data.texts)) {
          for (const entry of candidate.data.texts as TextMutationLike[]) {
            if (typeof entry.value === 'string') entry.value = maskReplayText(entry.value)
          }
        }
        if (Array.isArray(candidate.data.adds)) {
          for (const entry of candidate.data.adds as AddedNodeMutationLike[]) {
            if (!sanitizeSnapshotNode(entry.node)) return null
          }
        }
      } else if (
        candidate.data.source === INCREMENTAL_SOURCE_INPUT &&
        typeof candidate.data.text === 'string'
      ) {
        candidate.data.text = maskReplayText(candidate.data.text)
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
