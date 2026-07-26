import hljs from 'highlight.js/lib/core'
import javascript from 'highlight.js/lib/languages/javascript'
import xml from 'highlight.js/lib/languages/xml'

// Only the grammars the backoffice actually shows. The package's barrel export
// carries around 190 of them, which is a lot of parser to ship in order to
// colour a script tag.
hljs.registerLanguage('xml', xml)
hljs.registerLanguage('javascript', javascript)

/** Our language labels are not hljs grammar names. */
const grammars: Record<string, string> = {
  html: 'xml',
  xml: 'xml',
  js: 'javascript',
  javascript: 'javascript',
}

/**
 * Turns a snippet into markup safe to hand to v-html.
 *
 * Both branches escape. highlight.js escapes the source it tokenizes — that is
 * what lets arbitrary code go through it at all — so the only tags in a
 * highlighted result are the spans it added. A language with no grammar is
 * escaped here instead, which is the branch that matters: without it, a snippet
 * labelled "text" would reach v-html as live markup.
 */
export function highlightCode(code: string, language: string): string {
  const grammar = grammars[language]
  if (!grammar) return escapeHTML(code)
  return hljs.highlight(code, { language: grammar, ignoreIllegals: true }).value
}

export function escapeHTML(value: string): string {
  return value
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
}
