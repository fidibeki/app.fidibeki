import { describe, expect, it } from 'vitest'
import { escapeHTML, highlightCode } from '../../utils/highlight'

describe('code highlighting', () => {
  it('tokenizes the embed snippet we hand people', () => {
    const html = highlightCode('<script async src="https://widget.test/v1.js"></script>', 'html')
    expect(html).toContain('hljs-tag')
    expect(html).toContain('hljs-name')
    expect(html).toContain('hljs-string')
  })

  it('maps our language labels onto hljs grammar names', () => {
    // The content is authored as "html" and "js"; hljs calls them xml and
    // javascript. A label that misses the map falls through to plain text, so
    // this is the difference between highlighted and not.
    expect(highlightCode('<b>hi</b>', 'html')).toContain('hljs-tag')
    expect(highlightCode('const a = 1', 'js')).toContain('hljs-keyword')
    expect(highlightCode('const a = 1', 'javascript')).toContain('hljs-keyword')
  })

  // The result goes to v-html. highlight.js escapes what it tokenizes, so the
  // branch worth protecting is the other one: a language with no grammar must
  // not arrive as live markup.
  it('escapes a snippet it has no grammar for', () => {
    const rendered = highlightCode('<img src=x onerror=alert(1)>', 'text')
    expect(rendered).not.toContain('<img')
    expect(rendered).toContain('&lt;img')
  })

  it('escapes rather than renders even when the grammar is known', () => {
    const rendered = highlightCode('<img src=x onerror=alert(1)>', 'html')
    // Every angle bracket from the source survives as an entity; the only real
    // tags are hljs spans.
    expect(rendered).not.toContain('<img')
    expect(rendered).toContain('&lt;')
  })

  it('escapes the four characters that change how markup parses', () => {
    expect(escapeHTML('<a href="x">&</a>'))
      .toBe('&lt;a href=&quot;x&quot;&gt;&amp;&lt;/a&gt;')
  })
})
