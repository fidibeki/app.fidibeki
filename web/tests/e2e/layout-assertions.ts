import type { Page } from '@playwright/test'

export async function visibleLayoutDefects(page: Page) {
  return page.evaluate(() => {
    const defects: string[] = []
    const describe = (element: HTMLElement) =>
      `${element.tagName.toLowerCase()}:${element.textContent?.trim().replace(/\s+/g, ' ').slice(0, 40)}`
    const documentElement = document.documentElement
    if (documentElement.scrollWidth > documentElement.clientWidth + 1) {
      defects.push(`document overflow ${documentElement.scrollWidth}/${documentElement.clientWidth}`)
    }

    const elements = [...document.querySelectorAll<HTMLElement>('h1, h2, h3, p, a, button, label')]
      .filter((element) => {
        const style = getComputedStyle(element)
        const bounds = element.getBoundingClientRect()
        const closedDetails = element.closest('details:not([open])')
        if (closedDetails && !element.closest('summary')) return false
        if (innerWidth <= 700 && element.closest('.app-global-rail:not([data-open="true"])')) {
          return false
        }
        return style.display !== 'none' && style.visibility !== 'hidden'
          && bounds.width > 0 && bounds.height > 0
      })

    for (const element of elements) {
      const style = getComputedStyle(element)
      const bounds = element.getBoundingClientRect()
      const name = describe(element)
      if (bounds.left < -1 || bounds.right > innerWidth + 1) {
        defects.push(`${name} outside viewport`)
      }
      if (
        (style.overflowX === 'hidden' || style.overflowX === 'clip')
        && element.scrollWidth > element.clientWidth + 1
      ) {
        defects.push(`${name} horizontally clipped`)
      }
      if (
        (style.overflowY === 'hidden' || style.overflowY === 'clip')
        && element.scrollHeight > element.clientHeight + 1
      ) {
        defects.push(`${name} vertically clipped`)
      }
    }

    for (let leftIndex = 0; leftIndex < elements.length; leftIndex += 1) {
      const left = elements[leftIndex]
      const leftBounds = left.getBoundingClientRect()
      for (let rightIndex = leftIndex + 1; rightIndex < elements.length; rightIndex += 1) {
        const right = elements[rightIndex]
        if (left.contains(right) || right.contains(left)) continue
        const rightBounds = right.getBoundingClientRect()
        const overlapWidth = Math.min(leftBounds.right, rightBounds.right)
          - Math.max(leftBounds.left, rightBounds.left)
        const overlapHeight = Math.min(leftBounds.bottom, rightBounds.bottom)
          - Math.max(leftBounds.top, rightBounds.top)
        if (overlapWidth > 2 && overlapHeight > 2) {
          defects.push(`${describe(left)} overlaps ${describe(right)}`)
        }
      }
    }
    return defects
  })
}
