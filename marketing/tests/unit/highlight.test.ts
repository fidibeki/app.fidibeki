import { describe, expect, it } from 'vitest'
import { docs } from '../../data/docs'
import { features } from '../../data/features'
import { integrations } from '../../data/integrations'
import { escapeHTML, highlightCode } from '../../utils/highlight'

describe('code highlighting', () => {
  it('tokenizes the embed snippet the site publishes', () => {
    const html = highlightCode('<script async src="https://widget.fidibeki.com/v1.js"></script>', 'html')
    expect(html).toContain('hljs-tag')
    expect(html).toContain('hljs-string')
  })

  it('maps the labels the content is authored with', () => {
    expect(highlightCode('<b>hi</b>', 'html')).toContain('hljs-tag')
    expect(highlightCode('const a = 1', 'js')).toContain('hljs-keyword')
  })

  // The result goes to v-html. highlight.js escapes what it tokenizes, so the
  // branch worth protecting is the other one: a language with no grammar must
  // not arrive as live markup.
  it('escapes a snippet it has no grammar for', () => {
    const rendered = highlightCode('<img src=x onerror=alert(1)>', 'text')
    expect(rendered).not.toContain('<img')
    expect(rendered).toContain('&lt;img')
  })

  it('escapes the four characters that change how markup parses', () => {
    expect(escapeHTML('<a href="x">&</a>'))
      .toBe('&lt;a href=&quot;x&quot;&gt;&amp;&lt;/a&gt;')
  })

  // A language nobody registered renders as flat text, which is the thing that
  // prompted this in the first place. Asserting against the snippets the site
  // actually publishes catches both halves: a missing grammar, and a snippet
  // labelled with a language it is not written in.
  it('highlights every code block the site publishes', () => {
    const blocks = [...features, ...docs, ...integrations]
      .flatMap(entry => entry.sections)
      .filter(section => section.code)
      .map(section => section.code!)
    expect(blocks.length).toBeGreaterThan(0)

    for (const block of blocks) {
      const rendered = highlightCode(block.snippet, block.language)
      // 'text' is the deliberate opt-out for output that is not source.
      if (block.language === 'text') {
        expect(rendered, block.snippet.slice(0, 40)).not.toContain('hljs-')
        continue
      }
      expect(rendered, `${block.language}: ${block.snippet.slice(0, 40)}`).toContain('hljs-')
    }
  })
})
