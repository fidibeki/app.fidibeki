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
const MAX_PRESENTATION_STYLE_BYTES = 4 * 1024
const MAX_PRESENTATION_VALUE_BYTES = 256
const replayTextEncoder = new TextEncoder()

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

type PresentationValue = (value: string) => string | null

interface PresentationProperty {
  name: string
  capture: PresentationValue
  valid: (value: string) => boolean
  omit?: ReadonlySet<string>
  include?: (computed: CSSStyleDeclaration) => boolean
}

function keywordValue(...values: string[]): PresentationValue {
  const allowed = new Set(values)
  return value => {
    const canonical = value.trim().toLowerCase()
    return allowed.has(canonical) ? canonical : null
  }
}

function finiteNumberValue(
  minimum = Number.NEGATIVE_INFINITY,
  maximum = Number.POSITIVE_INFINITY,
): PresentationValue {
  const format = /^-?(?:0|[1-9]\d*)(?:\.\d+)?$/
  return value => {
    const canonical = value.trim().toLowerCase()
    if (!format.test(canonical)) return null
    const number = Number(canonical)
    return Number.isFinite(number) && number >= minimum && number <= maximum ? canonical : null
  }
}

function lengthValue(...keywords: string[]): PresentationValue {
  const allowed = new Set(keywords)
  const format = /^-?(?:0|[1-9]\d*)(?:\.\d+)?(?:px|%)$/
  return value => {
    const canonical = value.trim().toLowerCase()
    return allowed.has(canonical) || format.test(canonical) ? canonical : null
  }
}

function lengthListValue(minimum: number, maximum: number, ...keywords: string[]): PresentationValue {
  const normalizeLength = lengthValue(...keywords)
  return value => {
    const parts = value.trim().split(/\s+/)
    if (parts.length < minimum || parts.length > maximum) return null
    const normalized = parts.map(part => normalizeLength(part))
    return normalized.every(part => part !== null) ? normalized.join(' ') : null
  }
}

function colorValue(value: string): string | null {
  const canonical = value.trim().toLowerCase()
  if (canonical === 'transparent') return canonical
  const match = canonical.match(
    /^(rgba?)\((\d{1,3}), (\d{1,3}), (\d{1,3})(?:, ((?:0|1)(?:\.\d+)?))?\)$/,
  )
  if (!match) return null
  const channels = match.slice(2, 5).map(Number)
  if (channels.some(channel => channel < 0 || channel > 255)) return null
  if (match[1] === 'rgb' && match[5] !== undefined) return null
  if (match[1] === 'rgba') {
    if (match[5] === undefined) return null
    const alpha = Number(match[5])
    if (!Number.isFinite(alpha) || alpha < 0 || alpha > 1) return null
  }
  return canonical
}

function fontFamilyValue(value: string): string | null {
  const canonical = value.trim().toLowerCase()
  if (canonical === 'serif' || canonical === 'sans-serif' || canonical === 'monospace') {
    return canonical
  }
  if (canonical.includes('mono')) return 'monospace'
  if (canonical.includes('sans')) return 'sans-serif'
  if (canonical.includes('serif')) return 'serif'
  return canonical === '' ? null : 'sans-serif'
}

function canonicalFontFamilyValue(value: string): string | null {
  const canonical = value.trim().toLowerCase()
  return canonical === 'serif' || canonical === 'sans-serif' || canonical === 'monospace'
    ? canonical
    : null
}

function transformValue(value: string): string | null {
  const canonical = value.trim().toLowerCase()
  if (canonical === 'none') return canonical
  const match = canonical.match(/^(matrix|matrix3d)\(([^)]+)\)$/)
  if (!match) return null
  const values = match[2]?.split(', ')
  const expected = match[1] === 'matrix' ? 6 : 16
  if (!values || values.length !== expected) return null
  const numberValue = finiteNumberValue()
  return values.every(entry => numberValue(entry) !== null) ? canonical : null
}

function aspectRatioValue(value: string): string | null {
  const canonical = value.trim().toLowerCase()
  if (canonical === 'auto') return canonical
  const parts = canonical.split(' / ')
  const numberValue = finiteNumberValue(0)
  return parts.length === 2 && parts.every(part => numberValue(part) !== null)
    ? canonical
    : null
}

function trackListValue(value: string): string | null {
  const canonical = value.trim().toLowerCase()
  if (canonical === 'none') return canonical
  const parts = canonical.split(/\s+/)
  if (parts.length === 0 || parts.length > 64) return null
  const trackValue = lengthValue('auto', 'min-content', 'max-content')
  return parts.every(part => trackValue(part) !== null) ? parts.join(' ') : null
}

const zeroLengths = new Set(['0px', '0%'])
const autoValue = new Set(['auto'])
const normalValue = new Set(['normal'])
const noneValue = new Set(['none'])

function presentationProperty(
  name: string,
  capture: PresentationValue,
  options: {
    omit?: ReadonlySet<string>
    valid?: PresentationValue
    include?: (computed: CSSStyleDeclaration) => boolean
  } = {},
): PresentationProperty {
  const valid = options.valid ?? capture
  return {
    name,
    capture,
    valid: value => valid(value) === value,
    ...(options.omit ? { omit: options.omit } : {}),
    ...(options.include ? { include: options.include } : {}),
  }
}

const displayValue = keywordValue(
  'none', 'contents', 'block', 'inline', 'inline-block', 'flow-root',
  'flex', 'inline-flex', 'grid', 'inline-grid', 'table', 'inline-table',
  'table-row-group', 'table-header-group', 'table-footer-group', 'table-row',
  'table-cell', 'table-column-group', 'table-column', 'table-caption', 'list-item',
)
const positionValue = keywordValue('static', 'relative', 'absolute', 'fixed', 'sticky')
const borderStyleValue = keywordValue(
  'none', 'hidden', 'dotted', 'dashed', 'solid', 'double',
  'groove', 'ridge', 'inset', 'outset',
)
const alignmentValue = keywordValue(
  'normal', 'stretch', 'start', 'end', 'center', 'flex-start', 'flex-end',
  'self-start', 'self-end', 'baseline',
)
const flexDisplay = (computed: CSSStyleDeclaration) =>
  computed.display === 'flex' || computed.display === 'inline-flex'
const gridDisplay = (computed: CSSStyleDeclaration) =>
  computed.display === 'grid' || computed.display === 'inline-grid'
const borderVisible = (side: string) => (computed: CSSStyleDeclaration) =>
  computed.getPropertyValue(`border-${side}-style`) !== 'none'

const presentationProperties: readonly PresentationProperty[] = [
  presentationProperty('display', displayValue),
  presentationProperty('position', positionValue, { omit: new Set(['static']) }),
  presentationProperty('box-sizing', keywordValue('content-box', 'border-box'), {
    omit: new Set(['content-box']),
  }),
  presentationProperty('top', lengthValue('auto'), { omit: autoValue }),
  presentationProperty('right', lengthValue('auto'), { omit: autoValue }),
  presentationProperty('bottom', lengthValue('auto'), { omit: autoValue }),
  presentationProperty('left', lengthValue('auto'), { omit: autoValue }),
  presentationProperty('z-index', value =>
    keywordValue('auto')(value) ?? finiteNumberValue()(value), { omit: autoValue }),
  presentationProperty('visibility', keywordValue('visible', 'hidden', 'collapse')),
  presentationProperty('opacity', finiteNumberValue(0, 1), { omit: new Set(['1']) }),
  presentationProperty('width', lengthValue('auto'), { omit: autoValue }),
  presentationProperty('height', lengthValue('auto'), { omit: autoValue }),
  presentationProperty('min-width', lengthValue('auto'), { omit: new Set(['auto', ...zeroLengths]) }),
  presentationProperty('min-height', lengthValue('auto'), { omit: new Set(['auto', ...zeroLengths]) }),
  presentationProperty('max-width', lengthValue('none'), { omit: noneValue }),
  presentationProperty('max-height', lengthValue('none'), { omit: noneValue }),
  presentationProperty('margin-top', lengthValue('auto')),
  presentationProperty('margin-right', lengthValue('auto')),
  presentationProperty('margin-bottom', lengthValue('auto')),
  presentationProperty('margin-left', lengthValue('auto')),
  presentationProperty('padding-top', lengthValue()),
  presentationProperty('padding-right', lengthValue()),
  presentationProperty('padding-bottom', lengthValue()),
  presentationProperty('padding-left', lengthValue()),
  presentationProperty('overflow-x', keywordValue('visible', 'hidden', 'clip', 'scroll', 'auto')),
  presentationProperty('overflow-y', keywordValue('visible', 'hidden', 'clip', 'scroll', 'auto')),
  presentationProperty('aspect-ratio', aspectRatioValue, { omit: autoValue }),
  ...(['top', 'right', 'bottom', 'left'] as const).map(side =>
    presentationProperty(`border-${side}-width`, lengthValue(), {
      omit: zeroLengths,
      include: borderVisible(side),
    })),
  ...(['top', 'right', 'bottom', 'left'] as const).map(side =>
    presentationProperty(`border-${side}-style`, borderStyleValue)),
  ...(['top', 'right', 'bottom', 'left'] as const).map(side =>
    presentationProperty(`border-${side}-color`, colorValue, { include: borderVisible(side) })),
  ...(['top-left', 'top-right', 'bottom-right', 'bottom-left'] as const).map(corner =>
    presentationProperty(`border-${corner}-radius`, lengthListValue(1, 2))),
  presentationProperty('outline-width', lengthValue(), { omit: zeroLengths }),
  presentationProperty('outline-style', borderStyleValue, { omit: noneValue }),
  presentationProperty('outline-color', colorValue, {
    include: computed => computed.outlineStyle !== 'none',
  }),
  presentationProperty(
    'flex-direction',
    keywordValue('row', 'row-reverse', 'column', 'column-reverse'),
    { omit: new Set(['row']), include: flexDisplay },
  ),
  presentationProperty('flex-wrap', keywordValue('nowrap', 'wrap', 'wrap-reverse'), {
    omit: new Set(['nowrap']), include: flexDisplay,
  }),
  presentationProperty('flex-grow', finiteNumberValue(0), { include: flexDisplay }),
  presentationProperty('flex-shrink', finiteNumberValue(0), { include: flexDisplay }),
  presentationProperty('flex-basis', lengthValue('auto', 'content'), { include: flexDisplay }),
  presentationProperty('order', finiteNumberValue(), { omit: new Set(['0']), include: flexDisplay }),
  presentationProperty(
    'justify-content',
    keywordValue(
      'normal', 'start', 'end', 'center', 'flex-start', 'flex-end',
      'left', 'right', 'space-between', 'space-around', 'space-evenly', 'stretch',
    ),
    { omit: normalValue },
  ),
  presentationProperty('align-items', alignmentValue, { omit: normalValue }),
  presentationProperty('align-content', alignmentValue, { omit: normalValue }),
  presentationProperty('align-self', keywordValue(
    'auto', 'normal', 'stretch', 'start', 'end', 'center', 'flex-start', 'flex-end',
    'self-start', 'self-end', 'baseline',
  ), { omit: new Set(['auto', 'normal']) }),
  presentationProperty('column-gap', lengthValue('normal'), {
    omit: new Set(['normal', ...zeroLengths]),
  }),
  presentationProperty('row-gap', lengthValue('normal'), {
    omit: new Set(['normal', ...zeroLengths]),
  }),
  presentationProperty('grid-auto-flow', keywordValue(
    'row', 'column', 'dense', 'row dense', 'column dense',
  ), { omit: new Set(['row']), include: gridDisplay }),
  presentationProperty('grid-template-columns', trackListValue, {
    omit: noneValue, include: gridDisplay,
  }),
  presentationProperty('grid-template-rows', trackListValue, {
    omit: noneValue, include: gridDisplay,
  }),
  presentationProperty('grid-auto-columns', trackListValue, {
    omit: autoValue, include: gridDisplay,
  }),
  presentationProperty('grid-auto-rows', trackListValue, {
    omit: autoValue, include: gridDisplay,
  }),
  presentationProperty('background-color', colorValue),
  presentationProperty('color', colorValue),
  presentationProperty('font-family', fontFamilyValue, { valid: canonicalFontFamilyValue }),
  presentationProperty('font-size', lengthValue()),
  presentationProperty('font-style', keywordValue('normal', 'italic', 'oblique')),
  presentationProperty('font-weight', value =>
    keywordValue('normal', 'bold', 'bolder', 'lighter')(value) ??
    finiteNumberValue(1, 1000)(value)),
  presentationProperty('line-height', value =>
    keywordValue('normal')(value) ?? lengthValue()(value)),
  presentationProperty('letter-spacing', value =>
    keywordValue('normal')(value) ?? lengthValue()(value)),
  presentationProperty('text-align', keywordValue(
    'start', 'end', 'left', 'right', 'center', 'justify', 'match-parent',
  )),
  presentationProperty('white-space', keywordValue(
    'normal', 'pre', 'nowrap', 'pre-wrap', 'pre-line', 'break-spaces',
  )),
  presentationProperty('text-transform', keywordValue(
    'none', 'capitalize', 'uppercase', 'lowercase', 'full-width', 'full-size-kana',
  )),
  presentationProperty('object-fit', keywordValue(
    'fill', 'contain', 'cover', 'none', 'scale-down',
  ), { omit: new Set(['fill']) }),
  presentationProperty('object-position', lengthListValue(2, 2), {
    omit: new Set(['50% 50%']),
  }),
  presentationProperty('border-collapse', keywordValue('separate', 'collapse'), {
    omit: new Set(['separate']),
  }),
  presentationProperty('table-layout', keywordValue('auto', 'fixed'), { omit: autoValue }),
  presentationProperty('transform', transformValue, { omit: noneValue }),
  presentationProperty('transform-origin', lengthListValue(2, 3), {
    include: computed => computed.transform !== 'none',
  }),
]

const presentationPropertyIndex = new Map(
  presentationProperties.map((property, index) => [property.name, index]),
)

/** Validates the complete canonical presentation value accepted by the backend contract. */
export function isValidReplayPresentationStyle(style: string): boolean {
  if (replayTextEncoder.encode(style).byteLength > MAX_PRESENTATION_STYLE_BYTES) return false
  if (style === '') return true
  if (style.endsWith(';')) return false
  let previousIndex = -1
  for (const declaration of style.split(';')) {
    const separator = declaration.indexOf(':')
    if (separator <= 0 || separator !== declaration.lastIndexOf(':')) return false
    const name = declaration.slice(0, separator)
    const value = declaration.slice(separator + 1)
    if (!name || !value || replayTextEncoder.encode(value).byteLength > MAX_PRESENTATION_VALUE_BYTES) {
      return false
    }
    const index = presentationPropertyIndex.get(name)
    if (index === undefined || index <= previousIndex) return false
    const property = presentationProperties[index]
    if (!property?.valid(value)) return false
    previousIndex = index
  }
  return true
}

/** Projects a live element's computed style into the anonymous replay presentation contract. */
export function createReplayPresentationStyle(element: Element): string | null {
  try {
    const view = element.ownerDocument.defaultView
    if (!view) return null
    const computed = view.getComputedStyle(element)
    const declarations: string[] = []
    let bytes = 0
    for (const property of presentationProperties) {
      if (property.include && !property.include(computed)) continue
      const value = property.capture(computed.getPropertyValue(property.name))
      if (value === null || property.omit?.has(value)) continue
      const declaration = `${property.name}:${value}`
      const declarationBytes = replayTextEncoder.encode(
        `${declarations.length > 0 ? ';' : ''}${declaration}`,
      ).byteLength
      if (bytes + declarationBytes > MAX_PRESENTATION_STYLE_BYTES) break
      declarations.push(declaration)
      bytes += declarationBytes
    }
    const style = declarations.join(';')
    return isValidReplayPresentationStyle(style) ? style : null
  } catch {
    return null
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
): Record<string, unknown> {
  const sanitized: Record<string, unknown> = {}
  if (!attributes || typeof attributes !== 'object') return sanitized
  for (const [key, value] of Object.entries(attributes as Record<string, unknown>)) {
    if (key.toLowerCase() !== SAFE_TEXT_ATTRIBUTE) continue
    sanitized[SAFE_TEXT_ATTRIBUTE] = value === null ? null : ''
  }
  if (element) {
    const style = createReplayPresentationStyle(element)
    if (style) sanitized.style = style
  }
  return sanitized
}

interface SnapshotNodeLike {
  type?: unknown
  attributes?: unknown
  childNodes?: unknown
  textContent?: unknown
}

function sanitizeSnapshotNode(
  node: unknown,
  safeTextAncestor = false,
  resolveNode?: ReplayNodeResolver,
): boolean {
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
    candidate.attributes = sanitizeAttributeMap(
      candidate.attributes,
      resolveReplayElement(candidate.id, resolveNode),
    )
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
      if (!sanitizeSnapshotNode(child, safeText, resolveNode)) return false
    }
  }
  return true
}

interface AttributeMutationLike {
  id?: unknown
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
 * Sanitizes one serialized rrweb event in place for anonymity: pins Meta/navigation URLs to
 * the server-verifiable session projection, removes original attributes, and may replace them
 * with computed presentation from mirrored live nodes. Returns `null` when the event cannot be
 * safely processed so the caller fails closed.
 */
export function sanitizeReplayEvent(
  event: unknown,
  sessionPageURL: string,
  resolveNode?: ReplayNodeResolver,
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
      if (!candidate.data || !sanitizeSnapshotNode(candidate.data.node, false, resolveNode)) return null
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
            )
          }
        }
        if (Array.isArray(candidate.data.texts)) {
          for (const entry of candidate.data.texts as TextMutationLike[]) {
            if (typeof entry.value === 'string') entry.value = maskReplayText(entry.value)
          }
        }
        if (Array.isArray(candidate.data.adds)) {
          for (const entry of candidate.data.adds as AddedNodeMutationLike[]) {
            if (!sanitizeSnapshotNode(entry.node, false, resolveNode)) return null
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
