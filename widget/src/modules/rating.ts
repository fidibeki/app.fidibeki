interface RatingCopy {
  rating: string
  ratingOutOfFive: string
}

export function createRatingControl(copy: RatingCopy): {
  element: HTMLElement
  value(): number | null
  localize(copy: RatingCopy): void
} {
  let selected: number | null = null
  const fieldset = document.createElement('fieldset')
  fieldset.style.cssText = 'border:0;padding:0;margin:0;min-width:0'
  const legend = document.createElement('legend')
  legend.textContent = copy.rating
  legend.style.cssText = 'margin:0 0 7px;font:650 12px/1.2 sans-serif;color:var(--fb-muted)'
  const row = document.createElement('div')
  row.style.cssText = 'display:grid;grid-template-columns:repeat(5,36px);gap:5px'
  fieldset.append(legend, row)
  const buttons: HTMLButtonElement[] = []
  for (let value = 1; value <= 5; value++) {
    const button = document.createElement('button')
    button.type = 'button'
    button.textContent = '★'
    button.title = ratingTitle(copy, value)
    button.setAttribute('aria-label', button.title)
    button.style.cssText = 'width:36px;height:36px;border:1px solid var(--fb-line);border-radius:5px;background:transparent;color:var(--fb-muted);font-size:21px;cursor:pointer'
    button.addEventListener('click', () => {
      selected = value
      for (const [index, candidate] of Array.from(row.children).entries()) {
        const active = index < value
        ;(candidate as HTMLButtonElement).style.color = active ? 'var(--fb-accent)' : 'var(--fb-muted)'
        candidate.setAttribute('aria-pressed', String(index + 1 === value))
      }
    })
    row.append(button)
    buttons.push(button)
  }
  return {
    element: fieldset,
    value: () => selected,
    localize(next) {
      legend.textContent = next.rating
      for (const [index, button] of buttons.entries()) {
        button.title = ratingTitle(next, index + 1)
        button.setAttribute('aria-label', button.title)
      }
    },
  }
}

function ratingTitle(copy: RatingCopy, value: number): string {
  return copy.ratingOutOfFive.replace('{value}', String(value))
}
