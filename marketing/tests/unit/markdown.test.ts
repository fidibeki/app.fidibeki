import { describe, expect, it } from 'vitest'
import { pageMarkdown } from '../../scripts/generate-markdown.mjs'

const html = `<html><header>Header</header><main><nav>Breadcrumbs</nav><h1>Plans</h1><p>Actual prices &amp; limits.</p><table><tr><th>Limit</th><th>Starter</th></tr><tr><th>Projects</th><td>1</td></tr><tr><th>AI</th><td><svg aria-hidden="true"></svg><span class="visually-hidden">Not included</span></td></tr></table><pre><code>const test = "literal";</code></pre><a href="/pt/docs">Docs</a><form>Demo draft</form><button>Next</button></main><footer>Footer</footer></html>`

describe('Markdown representations', () => {
  it('preserves facts, accessible feature values and code while removing page chrome', () => {
    const md = pageMarkdown(
      html,
      'https://fidibeki.com/pt/pricing',
      new Set(['/pt/docs']),
    )
    expect(md).toContain('# Plans')
    expect(md).toContain('Actual prices & limits.')
    expect(md).toContain('| Projects | 1 |')
    expect(md).toContain('| AI | Not included |')
    expect(md).toContain('const test = "literal";')
    expect(md).toContain('[Docs](https://fidibeki.com/pt/docs.md)')
    expect(md).toContain('Source: https://fidibeki.com/pt/pricing')
    expect(md).not.toMatch(/Header|Footer|Breadcrumbs|Demo draft|Next/)
  })
  it('fails if a page has no main content instead of silently publishing an empty document', () => {
    expect(() =>
      pageMarkdown('<html>Missing</html>', 'https://fidibeki.com'),
    ).toThrow('Missing main content')
  })
})
