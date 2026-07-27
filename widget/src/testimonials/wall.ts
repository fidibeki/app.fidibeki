import { applyHostTheme, resolveTheme, subscribeToHostDataTheme, subscribeToSystemTheme } from '../theme-resolver'
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
  :host(:not([data-fb-theme="light"])) {
    --fw-surface: #121a17;
    --fw-ink: #eef4f1;
    --fw-muted: #93a7a0;
    --fw-line: #22302a;
  }
}

:host([data-fb-theme="dark"]) {
  --fw-surface: #121a17;
  --fw-ink: #eef4f1;
  --fw-muted: #93a7a0;
  --fw-line: #22302a;
}

/* The grid is also what an unrecognised layout renders as, which is deliberate:
   a wall set to "custom" still has a script tag on somebody's page, and that
   page should degrade to a wall rather than to a blank space. */
.wall {
  display: grid;
  gap: var(--fw-gap);
  grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
  margin: 0;
  padding: 0;
  list-style: none;
}

/* Masonry is real column flow rather than a grid with staggered rows, because
   the whole point is that a two-line quote does not reserve the height of a
   ten-line one. */
.wall[data-layout="masonry"] {
  display: block;
  column-width: 300px;
  column-gap: var(--fw-gap);
}

.wall[data-layout="masonry"] > li {
  break-inside: avoid;
  margin: 0 0 var(--fw-gap);
}

.wall[data-layout="masonry"] .card { height: auto; }

.wall[data-layout="carousel"] {
  display: flex;
  /* Each card is as tall as what is in it. Stretched to match the tallest, a
     one-line testimonial beside a video becomes a card that is mostly empty
     space, and the strip reads as a row of gaps. */
  align-items: flex-start;
  overflow-x: auto;
  scroll-snap-type: x mandatory;
  scroll-padding: 0 2px;
  -webkit-overflow-scrolling: touch;
}

.wall[data-layout="carousel"] > li {
  flex: 0 0 min(340px, 85%);
  scroll-snap-align: start;
}

/* Marquee: one row duplicated and translated by exactly its own width, which is
   what makes the seam invisible. */
.marquee {
  overflow: hidden;
  -webkit-mask-image: linear-gradient(90deg, transparent, #000 5%, #000 95%, transparent);
  mask-image: linear-gradient(90deg, transparent, #000 5%, #000 95%, transparent);
}

.marquee__track {
  display: flex;
  gap: var(--fw-gap);
  width: max-content;
  animation: fw-marquee var(--fw-marquee-duration, 48s) linear infinite;
}

/* Content that moves on its own has to be stoppable. Hover covers the pointer,
   focus-within covers the keyboard, and the container is focusable so there is
   a way to reach it. */
.marquee:hover .marquee__track,
.marquee:focus-within .marquee__track { animation-play-state: paused; }

.marquee:focus-visible { outline: 2px solid var(--fw-accent); outline-offset: 2px; }

.wall[data-layout="marquee"] {
  display: flex;
  align-items: flex-start;
  gap: var(--fw-gap);
  width: max-content;
}

.wall[data-layout="marquee"] > li { flex: 0 0 320px; }

/* A row of cards is only as short as its tallest card, and an upright phone
   recording is over 500px tall at this width — which would leave every quote
   beside it stranded at the top of a band the height of a video player. Both
   horizontal layouts are meant to be a strip on a page, so the footage is
   bounded here and the row stays a strip. */
.wall[data-layout="marquee"] video,
.wall[data-layout="carousel"] video { max-height: 220px; }

/* The track holds two lists separated by one gap, so a full cycle is half the
   track plus half a gap. */
@keyframes fw-marquee {
  from { transform: translateX(0); }
  to { transform: translateX(calc(-50% - var(--fw-gap) / 2)); }
}

@media (prefers-reduced-motion: reduce) {
  .marquee {
    overflow-x: auto;
    -webkit-mask-image: none;
    mask-image: none;
  }
  .marquee__track { animation: none; }
  .marquee__clone { display: none; }
}

.wall[data-layout="list"] { grid-template-columns: minmax(0, 1fr); }

/* A list is one testimonial per row, so there is width to spare: the author
   moves out beside the quote instead of sitting under it, which is what stops
   this being a grid with one column. */
@container (min-width: 560px) {
  .wall[data-layout="list"] .card {
    flex-direction: row;
    align-items: flex-start;
    gap: 22px;
  }

  .wall[data-layout="list"] figcaption {
    order: -1;
    flex: 0 0 168px;
    flex-direction: column;
    align-items: flex-start;
    gap: 8px;
    margin: 0;
    padding: 0;
  }

  .wall[data-layout="list"] .avatar {
    width: 44px;
    height: 44px;
    flex-basis: 44px;
  }
}

/* Spotlight drops the card chrome entirely: a pull quote under a hero is
   typography, not a box. */
.wall[data-layout="single"] {
  grid-template-columns: minmax(0, 1fr);
  gap: 40px;
}

.wall[data-layout="single"] .card {
  align-items: center;
  gap: 18px;
  max-width: 680px;
  height: auto;
  margin: 0 auto;
  padding: 4px 0;
  background: none;
  border: 0;
  text-align: center;
}

.wall[data-layout="single"] .body { flex: 0 1 auto; align-items: center; }

.wall[data-layout="single"] blockquote {
  font-size: clamp(18px, 3.4cqi, 24px);
  font-weight: 500;
  line-height: 1.45;
}

.wall[data-layout="single"] .headline { font-size: 17px; }

.wall[data-layout="single"] video { max-width: 520px; }

.card {
  display: flex;
  flex-direction: column;
  gap: 12px;
  box-sizing: border-box;
  height: 100%;
  /* A figure carries a UA margin of 1em 40px. Left alone it eats 80px of every
     card's width and stacks 40px on each side of the gap the layout asked for,
     so two neighbouring cards sit 96px apart instead of 16. */
  margin: 0;
  padding: 18px;
  background: var(--fw-surface);
  border: 1px solid var(--fw-line);
  border-radius: var(--fw-radius);
}

/* Everything that is not the author, kept together so a layout can move the
   author relative to it without having to place each child by hand. */
.body {
  display: flex;
  flex: 1;
  flex-direction: column;
  gap: 12px;
  min-width: 0;
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

/* Attribution. It is a part() like everything else so a customer can move or
   restyle it to sit with their own footer, rather than being stuck with ours in
   the middle of their page. */
.badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  margin-top: 14px;
  color: var(--fw-muted);
  font-size: 11px;
  text-decoration: none;
}

.badge:hover,
.badge:focus-visible { color: var(--fw-accent); }

.badge svg {
  width: 12px;
  height: 12px;
  flex: 0 0 12px;
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
        <div class="body" part="body">
          ${payload.show_rating ? renderStars(card.rating) : ''}
          ${card.headline ? `<p class="headline" part="headline">${escapeHTML(card.headline)}</p>` : ''}
          ${video ? renderVideo(video, poster, author) : ''}
          <blockquote part="quote">${escapeHTML(card.quote)}</blockquote>
          ${card.tags?.length
            ? `<ul class="tags" part="tags">${card.tags.map(tag => `<li>${escapeHTML(tag)}</li>`).join('')}</ul>`
            : ''}
        </div>
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
 * A marquee is the row duplicated and translated by exactly its own width, which
 * is what makes the seam invisible.
 *
 * The copy is inert and hidden from assistive technology, so a screen reader
 * announces each quote once rather than twice, and tabbing through the page does
 * not land on the same company link two times.
 *
 * The duration scales with the card count so a wall of twenty does not race past
 * at four times the speed of a wall of five.
 */
/** Where the badge points. Not configurable: it is the attribution. */
const BADGE_HREF = 'https://fidibeki.com'

/**
 * "Proofed by Fidibeki", with the brand mark.
 *
 * The link is rel="noopener" but not nofollow: this is a visible, honest
 * attribution on a wall the customer chose to publish, not a keyword stuffed
 * into someone else's footer.
 */
function renderBadge(): string {
  return `
    <a class="badge" part="badge" href="${BADGE_HREF}" target="_blank" rel="noopener">
      <svg viewBox="0 0 100 100" aria-hidden="true" focusable="false">
        <path fill="currentColor" d="M28.036,91.512c-0.38,0-0.764-0.104-1.096-0.324c-0.708-0.467-1.048-1.332-0.844-2.155l3.552-14.353H15.872c-0.944,0-1.764-0.656-1.956-1.584L2,16.368c-0.116-0.556,0.004-1.132,0.34-1.588c0.332-0.456,0.84-0.752,1.404-0.813L91.828,4.5c0.607-0.072,1.22,0.156,1.648,0.592c0.428,0.436,0.631,1.056,0.547,1.66l-7.836,58.191c-0.136,0.992-0.979,1.736-1.983,1.736H61.216L29.256,91.1C28.892,91.376,28.464,91.512,28.036,91.512L28.036,91.512z" />
      </svg>
      Proofed by Fidibeki
    </a>
  `
}

function renderMarquee(items: string, count: number, label: string): string {
  const seconds = Math.max(20, count * 7)
  return `
    <div class="marquee" part="wall" role="region" tabindex="0" aria-label="${label}"
         style="--fw-marquee-duration: ${seconds}s">
      <div class="marquee__track">
        <ul class="wall" data-layout="marquee">${items}</ul>
        <ul class="wall marquee__clone" data-layout="marquee" aria-hidden="true" inert>${items}</ul>
      </div>
    </div>
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
  // The last payload, kept so a presentational change can repaint without
  // asking the network for a wall it already has.
  private payload: WallPayload | null = null
  private origin = ''
  // Cleanups for the live theme subscriptions (system + host data-theme).
  private themeCleanups: Array<() => void> = []

  constructor() {
    super()
    this.root = this.attachShadow({ mode: 'open' })
  }

  connectedCallback(): void {
    void this.load()
    // Re-resolve and re-apply the theme when the OS or the host page changes its
    // active colour while the wall is connected. The CSS already has
    // `:host([theme="dark"])` and `:host([theme="light"])`, so applying the
    // attribute is enough — no innerHTML rewrite needed.
    const handleThemeChange = () => {
      if (!this.payload) return
      this.applyTheme()
    }
    this.themeCleanups.push(subscribeToSystemTheme(handleThemeChange))
    this.themeCleanups.push(subscribeToHostDataTheme(handleThemeChange))
  }

  disconnectedCallback(): void {
    for (const cleanup of this.themeCleanups) cleanup()
    this.themeCleanups = []
  }

  /**
   * Only `wall`, `testimonial` and `host` name a different payload; `layout` and
   * `theme` are a repaint of the one already held.
   *
   * Routing both through load() looks harmless and is not: load() returns early
   * when the endpoint has not changed, so a page that flips `theme` with its own
   * dark mode toggle would set the attribute, observe nothing happen, and have
   * no way to tell that observedAttributes had listed an attribute the element
   * declined to act on.
   */
  attributeChangedCallback(name: string): void {
    if (!this.isConnected) return
    if (name === 'wall' || name === 'testimonial' || name === 'host') {
      void this.load()
      return
    }
    if (this.payload) this.render(this.payload, this.origin)
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
      this.payload = payload
      this.origin = endpoint.origin
      this.render(payload, endpoint.origin)
    } catch {
      this.renderEmpty()
    }
  }

  private renderEmpty(): void {
    this.payload = null
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

    const label = escapeHTML(payload.name || 'Testimonials')
    const items = cards.map(card => renderCard(card, payload, origin)).join('')
    // A single card has nothing to scroll past, so it is rendered as a plain row
    // rather than as an animation of one thing going round for ever.
    const body = layout === 'marquee' && cards.length > 1
      ? renderMarquee(items, cards.length, label)
      : `<ul class="wall" part="wall" data-layout="${escapeHTML(layout)}" aria-label="${label}">${items}</ul>`

    this.root.innerHTML = `
      <style>${STYLES}${variables ? `:host{${variables}}` : ''}</style>
      ${body}
      ${renderBadge()}
    `
    this.applyTheme()
  }

  /**
   * Resolves the theme via the priority order (custom element attribute >
   * host `data-theme` > server `payload.theme.mode` > system) and applies it to
   * the host as `theme="light"|"dark"`, which the CSS selectors
   * `:host([theme="dark"])` and `:host([theme="light"])` already honour.
   *
   * Guards against the feedback loop: setting `theme` triggers
   * `attributeChangedCallback`, which re-renders, which would call
   * `applyTheme` again. The guard skips the write when the resolved value
   * matches the attribute already on the host.
   */
  private applyTheme(): void {
    const fallback = this.payload?.theme?.mode ?? 'auto'
    const resolved = resolveTheme({
      elementAttr: this.getAttribute('theme'),
      fallback,
    })
    if (this.getAttribute('data-fb-theme') === resolved) return
    applyHostTheme(this, resolved)
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
