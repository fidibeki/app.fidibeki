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

/**
 * Attribute allowlist for serialized replay nodes. This is deliberately an allowlist
 * (structural/presentational attributes with a small enumerable value space) rather than a
 * denylist of "known identity" attributes, because rrweb 2.1.1 has no per-attribute masking
 * hook: anything not recognized as safe (id, name, value, title, alt, aria-label, href, src,
 * data-*, and any future/unknown attribute) is dropped rather than passed through.
 */
const SAFE_STRUCTURAL_ATTRIBUTES = new Set([
  'class', 'style', 'type', 'role', 'dir', 'lang', 'hidden', 'disabled', 'readonly',
  'checked', 'selected', 'multiple', 'required', 'open', 'tabindex', 'colspan', 'rowspan',
  'scope', 'width', 'height', 'min', 'max', 'step', 'wrap', 'target', 'rel', 'media',
  'aria-hidden', 'aria-expanded', 'aria-checked', 'aria-selected', 'aria-disabled',
  'aria-current', 'aria-pressed', 'aria-modal', 'aria-live', 'aria-atomic',
  'aria-orientation', 'aria-valuemin', 'aria-valuemax', 'aria-valuenow',
])

const EVENT_TYPE_FULL_SNAPSHOT = 2
const EVENT_TYPE_INCREMENTAL_SNAPSHOT = 3
const EVENT_TYPE_META = 4
const INCREMENTAL_SOURCE_MUTATION = 0
const ELEMENT_NODE_TYPE = 2
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

function sanitizeAttributeMap(attributes: unknown): Record<string, unknown> {
  const sanitized: Record<string, unknown> = {}
  if (!attributes || typeof attributes !== 'object') return sanitized
  for (const [key, value] of Object.entries(attributes as Record<string, unknown>)) {
    if (SAFE_STRUCTURAL_ATTRIBUTES.has(key.toLowerCase())) sanitized[key] = value
  }
  return sanitized
}

interface SnapshotNodeLike {
  type?: unknown
  attributes?: unknown
  childNodes?: unknown
  textContent?: unknown
}

function sanitizeSnapshotNode(node: unknown): void {
  if (!node || typeof node !== 'object') return
  const candidate = node as SnapshotNodeLike & Record<string, unknown>
  if (candidate.type === ELEMENT_NODE_TYPE) {
    candidate.attributes = sanitizeAttributeMap(candidate.attributes)
  }
  // HTML comments are not covered by rrweb's text-masking pipeline (that only applies to
  // genuine Text nodes), so they are blanked here instead of passed through verbatim.
  if (candidate.type === COMMENT_NODE_TYPE && typeof candidate.textContent === 'string') {
    candidate.textContent = ''
  }
  if (Array.isArray(candidate.childNodes)) {
    for (const child of candidate.childNodes) sanitizeSnapshotNode(child)
  }
}

interface AttributeMutationLike {
  attributes?: unknown
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
  }
}

/**
 * Sanitizes one serialized rrweb event in place for anonymity: canonicalizes the internal
 * Meta event's `href`, and strips non-allowlisted attributes from full-snapshot and
 * newly-added DOM nodes (rrweb has no built-in hook for this). Returns `null` when the event
 * cannot be safely processed so the caller fails closed and drops it instead of uploading it.
 */
export function sanitizeReplayEvent(event: unknown): unknown | null {
  if (!event || typeof event !== 'object') return null
  try {
    const candidate = event as ReplayEventLike
    if (candidate.type === EVENT_TYPE_META && candidate.data) {
      if (typeof candidate.data.href === 'string') {
        const canonical = canonicalizeReplayURL(candidate.data.href)
        if (!canonical) return null
        candidate.data.href = canonical
      }
      return candidate
    }
    if (candidate.type === EVENT_TYPE_FULL_SNAPSHOT && candidate.data) {
      sanitizeSnapshotNode(candidate.data.node)
      return candidate
    }
    if (
      candidate.type === EVENT_TYPE_INCREMENTAL_SNAPSHOT &&
      candidate.data?.source === INCREMENTAL_SOURCE_MUTATION
    ) {
      if (Array.isArray(candidate.data.attributes)) {
        for (const entry of candidate.data.attributes as AttributeMutationLike[]) {
          entry.attributes = sanitizeAttributeMap(entry.attributes)
        }
      }
      if (Array.isArray(candidate.data.adds)) {
        for (const entry of candidate.data.adds as AddedNodeMutationLike[]) {
          sanitizeSnapshotNode(entry.node)
        }
      }
      return candidate
    }
    return candidate
  } catch {
    return null
  }
}
