import type { ElementAttachment } from '../types'

const capturedStyles = [
  'display', 'position', 'color', 'background-color', 'font-family', 'font-size', 'font-weight',
  'line-height', 'text-align', 'border', 'border-radius', 'padding', 'margin', 'width', 'height',
]

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
      const styles = Object.fromEntries(capturedStyles.map((property) => [property, computed.getPropertyValue(property)]))
      const clone = candidate.cloneNode(true) as Element
      for (const field of clone.querySelectorAll('input, textarea, select')) {
        field.removeAttribute('value')
        field.textContent = ''
      }
      finish({ selector: selectorFor(candidate), html: clone.outerHTML.slice(0, 20000), styles })
    }
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') finish(null)
    }
    document.addEventListener('pointermove', onMove, true)
    document.addEventListener('click', onClick, true)
    document.addEventListener('keydown', onKey, true)
  })
}

function selectorFor(element: Element): string {
  if (element.id) return `#${CSS.escape(element.id)}`
  const parts: string[] = []
  let current: Element | null = element
  while (current && current !== document.body && parts.length < 6) {
    let part = current.tagName.toLowerCase()
    if (current.classList.length > 0) part += `.${CSS.escape(current.classList[0] ?? '')}`
    const siblings = current.parentElement ? Array.from(current.parentElement.children).filter((item) => item.tagName === current?.tagName) : []
    if (siblings.length > 1) part += `:nth-of-type(${siblings.indexOf(current) + 1})`
    parts.unshift(part)
    current = current.parentElement
  }
  return parts.join(' > ')
}
