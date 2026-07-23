import {
  buildSafeElementAttachment,
  sanitizeCapturedStyle,
} from '../../src/modules/element-attachment'

describe('privacy-safe element attachment', () => {
  beforeEach(() => {
    document.body.innerHTML = ''
  })

  it('keeps bounded structure while removing every source value and text node', () => {
    document.body.innerHTML = `
      <main>
        <section
          id="account-48291"
          class="customer-jane"
          data-email="jane.private@example.test"
          aria-label="Jane Private"
          title="reset-token-SECRET"
          onclick="fetch('/private')"
        >
          Jane Private jane.private@example.test
          <!-- bearer SECRET_COMMENT -->
          <script>window.secret = 'SECRET_SCRIPT'</script>
          <div>
            <span>Account 48291</span>
            <input name="email" value="jane.private@example.test">
            <img src="https://private.example/avatar/jane.png" alt="Jane Private">
            <a href="https://private.example/reset?token=SECRET_LINK">Reset</a>
          </div>
          <ul><li data-token="SECRET_DATA">Private item</li></ul>
        </section>
      </main>
    `
    const target = document.querySelector('section')!
    const attachment = buildSafeElementAttachment(target, styleReader({
      display: 'block',
      position: 'relative',
      color: 'rgb(18, 52, 86)',
      'background-color': 'rgba(0, 0, 0, 0)',
      width: '640px',
      height: 'auto',
    }))

    expect(attachment).toEqual({
      selector: 'main > section',
      html: '<section><div><span></span></div><ul><li></li></ul></section>',
      styles: {
        display: 'block',
        position: 'relative',
        color: 'rgb(18, 52, 86)',
        'background-color': 'rgba(0, 0, 0, 0)',
        width: '640px',
        height: 'auto',
      },
    })
    const serialized = JSON.stringify(attachment)
    for (const sentinel of [
      'Jane', 'jane.private', '48291', 'SECRET', 'private.example',
      'onclick', 'data-', 'aria-', 'title=', 'href=', 'src=', 'class=', 'id=',
    ]) {
      expect(serialized).not.toContain(sentinel)
    }
  })

  it('uses only tag and nth-of-type segments in selectors', () => {
    document.body.innerHTML = `
      <main id="customer-private">
        <div class="first"></div>
        <div class="account-secret"><span></span><span id="target-private"></span></div>
      </main>
    `
    const target = document.querySelector('#target-private')!
    const attachment = buildSafeElementAttachment(target, styleReader({ display: 'inline' }))

    expect(attachment?.selector).toBe('main > div:nth-of-type(2) > span:nth-of-type(2)')
    expect(attachment?.selector).not.toMatch(/[#.\[]/)
  })

  it('fails closed for unsafe roots or structures beyond the privacy bounds', () => {
    const input = document.createElement('input')
    input.value = 'jane.private@example.test'
    expect(buildSafeElementAttachment(input, styleReader({ display: 'inline-block' }))).toBeNull()

    const root = document.createElement('div')
    for (let index = 0; index < 140; index++) root.append(document.createElement('span'))
    expect(buildSafeElementAttachment(root, styleReader({ display: 'block' }))).toBeNull()
  })

  it.each([
    ['12px', '12px'],
    ['10000px', '10000px'],
    ['48291px', null],
    ['rgb(1, 2, 3)', 'rgb(1, 2, 3)'],
    ['solid', 'solid'],
    ['url(https://private.example/avatar.png)', null],
    ['var(--customer-secret)', null],
    ['"Jane Private"', null],
    ['calc(100% - 1px)', null],
    ['red; background-image:url(https://private.example)', null],
  ])('sanitizes a captured style value %s', (input, expected) => {
    expect(sanitizeCapturedStyle(input)).toBe(expected)
  })
})

function styleReader(values: Record<string, string>): Pick<CSSStyleDeclaration, 'getPropertyValue'> {
  return {
    getPropertyValue(property: string) {
      return values[property] ?? ''
    },
  }
}
