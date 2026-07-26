import { SUPPORTED_SCHEMA_VERSION, isWallPayload } from './types'
import type { TestimonialAuthor, TestimonialCard, WallPayload } from './types'

// Styles live inside the shadow root, so the host page cannot leak in and we
// cannot leak out. Everything a customer might reasonably want to restyle is a
// custom property with a sensible default, and every meaningful node carries a
// part() so ::part() works without forking the component.
const STYLES = `
:host {
  --fw-accent: #087f73;
  --fw-radius: 10px;
  --fw-surface: #ffffff;
  --fw-ink: #17211f;
  --fw-muted: #697672;
  --fw-line: #d9e1de;
  --fw-gap: 16px;
  --fw-font: system-ui, -apple-system, "Segoe UI", sans-serif;
  display: block;
  color: var(--fw-ink);
  font-family: var(--fw-font);
  container-type: inline-size;
}

:host([hidden]) { display: none; }

@media (prefers-color-scheme: dark) {
  :host(:not([theme="light"])) {
    --fw-surface: #121a17;
    --fw-ink: #eef4f1;
    --fw-muted: #93a7a0;
    --fw-line: #22302a;
  }
}

:host([theme="dark"]) {
  --fw-surface: #121a17;
  --fw-ink: #eef4f1;
  --fw-muted: #93a7a0;
  --fw-line: #22302a;
}

.wall {
  display: grid;
  gap: var(--fw-gap);
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  margin: 0;
  padding: 0;
  list-style: none;
}

.wall[data-layout="list"],
.wall[data-layout="single"] { grid-template-columns: minmax(0, 1fr); }

.wall[data-layout="carousel"] {
  display: flex;
  overflow-x: auto;
  scroll-snap-type: x mandatory;
  -webkit-overflow-scrolling: touch;
}

.wall[data-layout="carousel"] > li {
  flex: 0 0 min(340px, 85%);
  scroll-snap-align: start;
}

.card {
  display: flex;
  flex-direction: column;
  gap: 12px;
  box-sizing: border-box;
  height: 100%;
  padding: 18px;
  background: var(--fw-surface);
  border: 1px solid var(--fw-line);
  border-radius: var(--fw-radius);
}

.headline {
  margin: 0;
  font-size: 15px;
  font-weight: 700;
  line-height: 1.3;
}

blockquote {
  margin: 0;
  font-size: 15px;
  line-height: 1.55;
  overflow-wrap: anywhere;
}

.rating {
  display: flex;
  gap: 2px;
  color: var(--fw-accent);
  font-size: 13px;
  letter-spacing: 1px;
}

figcaption {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: auto;
  padding-top: 4px;
}

.avatar {
  width: 36px;
  height: 36px;
  flex: 0 0 36px;
  object-fit: cover;
  background: var(--fw-line);
  border-radius: 50%;
}

.who { min-width: 0; }

.name {
  display: block;
  font-size: 13px;
  font-style: normal;
  font-weight: 650;
}

.role {
  display: block;
  color: var(--fw-muted);
  font-size: 12px;
  overflow-wrap: anywhere;
}

.role a { color: inherit; }

video {
  width: 100%;
  background: #000;
  border-radius: calc(var(--fw-radius) - 4px);
  /* The fallback only applies when the payload carried no dimensions. Each card
     sets its own ratio inline, so a recording made on a phone held upright is
     shown upright rather than letterboxed into a shape it was never in. */
  aspect-ratio: 16 / 9;
  /* A portrait clip is otherwise as tall as the card is wide, which on a
     three-column wall pushes everything below it off the screen. */
  max-height: 70vh;
  object-fit: contain;
}

.tags {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
  margin: 0;
  padding: 0;
  list-style: none;
}

.tags li {
  padding: 2px 8px;
  border: 1px solid var(--fw-line);
  border-radius: 999px;
  color: var(--fw-muted);
  font-size: 11px;
}

@media (prefers-reduced-motion: no-preference) {
  .card { transition: border-color 150ms ease; }
}
`

/** Escapes text before it reaches innerHTML. */
function escapeHTML(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}

/**
 * Only http(s) URLs are allowed through. A testimonial is author-supplied
 * content published on someone else's site; letting a `javascript:` company URL
 * reach an href would make us the delivery vehicle.
 */
function safeURL(value: string | undefined, origin: string): string {
  if (!value) return ''
  try {
    const resolved = new URL(value, origin)
    return resolved.protocol === 'http:' || resolved.protocol === 'https:' ? resolved.href : ''
  } catch {
    return ''
  }
}

function renderStars(rating: number | null | undefined): string {
  if (!rating || rating < 1) return ''
  const rounded = Math.min(5, Math.round(rating))
  return `<div class="rating" part="rating" role="img" aria-label="${rounded} out of 5">${
    '★'.repeat(rounded)
  }${'☆'.repeat(5 - rounded)}</div>`
}

/**
 * A video element sized to the recording rather than to an assumption.
 *
 * The dimensions come from the rendition the API actually serves, so the box is
 * correct before a byte of video has loaded: no letterboxing of a portrait clip,
 * and no jump when the real size arrives. A payload without them falls back to
 * the stylesheet's 16:9, which is the old behaviour rather than a broken one.
 */
function renderVideo(source: string, poster: string, author: TestimonialAuthor): string {
  const width = Number(author.video_width) || 0
  const height = Number(author.video_height) || 0
  const ratio = width > 0 && height > 0 ? ` style="aspect-ratio: ${width} / ${height}"` : ''
  const still = poster ? ` poster="${escapeHTML(poster)}"` : ''
  return `<video part="video" controls preload="none" playsinline` +
    `${ratio}${still} src="${escapeHTML(source)}"></video>`
}

function renderCard(card: TestimonialCard, payload: WallPayload, origin: string): string {
  const author = card.author || {}
  const avatar = payload.show_avatar ? safeURL(author.avatar_url, origin) : ''
  const video = payload.show_video ? safeURL(author.video_url, origin) : ''
  const poster = payload.show_video ? safeURL(author.poster_url, origin) : ''
  const companyURL = payload.show_company ? safeURL(author.company_url, origin) : ''
  const company = payload.show_company ? author.company : ''

  const roleParts: string[] = []
  if (author.role) roleParts.push(escapeHTML(author.role))
  if (company) {
    roleParts.push(companyURL
      ? `<a href="${escapeHTML(companyURL)}" rel="nofollow noopener" target="_blank">${escapeHTML(company)}</a>`
      : escapeHTML(company))
  }

  return `
    <li part="card-wrapper">
      <figure class="card" part="card">
        ${payload.show_rating ? renderStars(card.rating) : ''}
        ${card.headline ? `<p class="headline" part="headline">${escapeHTML(card.headline)}</p>` : ''}
        ${video ? renderVideo(video, poster, author) : ''}
        <blockquote part="quote">${escapeHTML(card.quote)}</blockquote>
        ${card.tags?.length
          ? `<ul class="tags" part="tags">${card.tags.map(tag => `<li>${escapeHTML(tag)}</li>`).join('')}</ul>`
          : ''}
        <figcaption part="author">
          ${avatar ? `<img class="avatar" part="avatar" src="${escapeHTML(avatar)}" alt="" loading="lazy" width="36" height="36" />` : ''}
          <span class="who">
            ${author.name ? `<cite class="name" part="name">${escapeHTML(author.name)}</cite>` : ''}
            ${roleParts.length ? `<span class="role" part="role">${roleParts.join(' · ')}</span>` : ''}
          </span>
        </figcaption>
      </figure>
    </li>
  `
}

/**
 * `<fidibeki-wall wall="wl_...">` renders a published wall.
 *
 * A failure renders nothing rather than an error box: this element lives on a
 * customer's marketing page, and a broken widget shouting about itself is worse
 * than a quiet gap.
 */
export class FidibekiWall extends HTMLElement {
  static get observedAttributes(): string[] {
    return ['wall', 'testimonial', 'layout', 'theme', 'host']
  }

  private root: ShadowRoot
  private loaded = ''

  constructor() {
    super()
    this.root = this.attachShadow({ mode: 'open' })
  }

  connectedCallback(): void {
    void this.load()
  }

  attributeChangedCallback(): void {
    if (this.isConnected) void this.load()
  }

  private endpoint(): { url: string; origin: string } | null {
    const wall = this.getAttribute('wall') || this.getAttribute('testimonial')
    if (!wall) return null
    const host = this.getAttribute('host') || this.defaultHost()
    return { url: `${host}/v1/public/walls/${encodeURIComponent(wall)}`, origin: host }
  }

  /**
   * Falls back to the origin the loader script itself came from, so a customer
   * pastes one script tag and nothing else.
   */
  private defaultHost(): string {
    const script = document.currentScript as HTMLScriptElement | null
    const source = script?.src || FidibekiWall.scriptOrigin
    if (!source) return ''
    try {
      return new URL(source).origin
    } catch {
      return ''
    }
  }

  static scriptOrigin = ''

  private async load(): Promise<void> {
    const endpoint = this.endpoint()
    if (!endpoint || endpoint.url === this.loaded) return
    this.loaded = endpoint.url
    try {
      const response = await fetch(endpoint.url, { credentials: 'omit' })
      if (!response.ok) {
        this.renderEmpty()
        return
      }
      const payload: unknown = await response.json()
      if (!isWallPayload(payload) || payload.schema_version > SUPPORTED_SCHEMA_VERSION) {
        this.renderEmpty()
        return
      }
      this.render(payload, endpoint.origin)
    } catch {
      this.renderEmpty()
    }
  }

  private renderEmpty(): void {
    this.root.innerHTML = ''
  }

  private render(payload: WallPayload, origin: string): void {
    const layout = this.getAttribute('layout') || payload.layout || 'grid'
    const cards = this.hasAttribute('testimonial') ? payload.cards.slice(0, 1) : payload.cards
    if (!cards.length) {
      this.renderEmpty()
      return
    }
    const theme = payload.theme || {}
    const variables = [
      theme.accent ? `--fw-accent:${CSS.escape(theme.accent)}` : '',
      theme.radius ? `--fw-radius:${CSS.escape(theme.radius)}` : '',
      theme.font_family ? `--fw-font:${CSS.escape(theme.font_family)}` : '',
    ].filter(Boolean).join(';')

    this.root.innerHTML = `
      <style>${STYLES}${variables ? `:host{${variables}}` : ''}</style>
      <ul class="wall" part="wall" data-layout="${escapeHTML(layout)}"
          aria-label="${escapeHTML(payload.name || 'Testimonials')}">
        ${cards.map(card => renderCard(card, payload, origin)).join('')}
      </ul>
    `
  }
}

/** Registers both elements. Re-registration is a no-op so a page can safely
 * include the script twice. */
export function defineTestimonialElements(scriptOrigin = ''): void {
  if (scriptOrigin) FidibekiWall.scriptOrigin = scriptOrigin
  if (typeof customElements === 'undefined') return
  if (!customElements.get('fidibeki-wall')) {
    customElements.define('fidibeki-wall', FidibekiWall)
  }
  if (!customElements.get('fidibeki-testimonial')) {
    // A single quote under a hero is the commonest first integration, more so
    // than a full wall, so it is a first-class element rather than a wall with
    // an awkward attribute.
    customElements.define('fidibeki-testimonial', class extends FidibekiWall {})
  }
}
