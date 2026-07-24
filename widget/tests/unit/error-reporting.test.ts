import type { ErrorEvent } from '@sentry/browser'
import { REDACTED_TELEMETRY_VALUE, scrubEvent } from '../../src/modules/error-reporting'

describe('Sentry event scrubbing', () => {
  it('removes request (URL/path/query/hash), headers, cookies, body, user, contexts, and tags', () => {
    const event: ErrorEvent = {
      type: undefined,
      user: { email: 'person@example.com', id: 'user-id', ip_address: '203.0.113.9' },
      request: {
        url: 'https://example.com/checkout/reporter-42?token=secret#step-2',
        data: { text: 'private feedback' },
        headers: { Authorization: 'Bearer secret', Cookie: 'session=secret' },
        cookies: { session: 'secret' },
      },
      contexts: { custom: { customer_field: 'sensitive' } },
      tags: { customer_id: 'acme' },
      sdk: {
        name: 'sentry.javascript.browser',
        version: '10.67.0',
        integrations: ['customer-person@example.com'],
      },
    }
    const scrubbed = scrubEvent(event)
    expect(scrubbed.user).toBeUndefined()
    expect(scrubbed.request).toBeUndefined()
    expect(scrubbed.contexts).toBeUndefined()
    expect(scrubbed.tags).toBeUndefined()
    expect(scrubbed.sdk).toBeUndefined()
    expect(JSON.stringify(scrubbed)).not.toMatch(/person@example\.com|secret|203\.0\.113\.9|private feedback|acme/)
  })

  it('drops raw breadcrumb messages and data regardless of key name', () => {
    const event: ErrorEvent = {
      type: undefined,
      breadcrumbs: [
        { category: 'fetch', message: 'GET /v1/widget/submit?key=abc123', data: { note: 'reporter typed this' } },
        { category: 'ui.click', data: { nested: { arbitrary_field: 'still sensitive' } } },
      ],
    }
    const scrubbed = scrubEvent(event)
    expect(scrubbed.breadcrumbs).toBeUndefined()
  })

  it('sanitizes a thrown Error message so it cannot leak reporter/attachment content', () => {
    const event: ErrorEvent = {
      type: undefined,
      message: 'attachment pipeline failed for reporter upload named my-passport-scan.png',
      exception: {
        values: [{
          type: 'AttachmentPipelineError',
          value: 'attachment pipeline failed: reporter feedback text was "I earn 3000 EUR and my email is a@b.com"',
          stacktrace: {
            frames: [{
              filename: 'error-reporting.js',
              function: 'reportError',
              module: 'widget',
              lineno: 12,
              colno: 7,
              in_app: true,
              abs_path: 'https://person@example.com/private/error-reporting.js?token=secret',
              context_line: 'const email = "person@example.com"',
              pre_context: ['const token = "before-secret"'],
              post_context: ['send("after-secret")'],
              vars: { authorization: 'Bearer secret' },
              module_metadata: { reporter_text: 'private feedback' },
            }],
            frames_omitted: [1, 2],
          },
        }],
      },
    }
    const scrubbed = scrubEvent(event)
    expect(scrubbed.message).toBe(REDACTED_TELEMETRY_VALUE)
    expect(scrubbed.exception!.values![0]!.value).toBe(REDACTED_TELEMETRY_VALUE)
    expect(scrubbed.exception!.values![0]!.type).toBe('AttachmentPipelineError')
    expect(scrubbed.exception!.values![0]!.stacktrace?.frames?.[0]).toEqual({
      filename: 'error-reporting.js',
      function: 'reportError',
      module: 'widget',
      lineno: 12,
      colno: 7,
      in_app: true,
    })
    expect(scrubbed.exception!.values![0]!.stacktrace?.frames_omitted).toEqual([1, 2])
    expect(JSON.stringify(scrubbed)).not.toMatch(
      /passport-scan|3000 EUR|a@b\.com|before-secret|after-secret|Bearer secret|private feedback/,
    )
  })

  it('sanitizes a thrown raw string so it cannot leak host-page or token content', () => {
    const event: ErrorEvent = {
      type: undefined,
      exception: {
        values: [{ type: 'Error', value: 'Uncaught: token=abc123 on https://customer-site.example/private-page' }],
      },
    }
    const scrubbed = scrubEvent(event)
    expect(scrubbed.exception!.values![0]!.value).toBe(REDACTED_TELEMETRY_VALUE)
    expect(JSON.stringify(scrubbed)).not.toMatch(/abc123|customer-site\.example/)
  })

  it('drops mechanism.data but keeps mechanism.type/handled', () => {
    const event: ErrorEvent = {
      type: undefined,
      exception: {
        values: [{
          type: 'TypeError',
          mechanism: { type: 'generic', handled: true, data: { customer_field: 'person@example.com' } },
        }],
      },
    }
    const scrubbed = scrubEvent(event)
    const mechanism = scrubbed.exception!.values![0]!.mechanism
    expect(mechanism).toEqual({ type: 'generic', handled: true })
  })

  it('keeps only the bounded phase/project_id/widget_rule_id operational context, dropping anything else', () => {
    const event: ErrorEvent = {
      type: undefined,
      extra: {
        phase: 'submission',
        project_id: 'project-1',
        widget_rule_id: 'rule-1',
        reporter_text: 'should never survive',
        attachment_detail: 'should never survive either',
      },
    }
    const scrubbed = scrubEvent(event)
    expect(scrubbed.extra).toEqual({ phase: 'submission', project_id: 'project-1', widget_rule_id: 'rule-1' })
  })

  it('rejects unknown phases and malformed or oversized operational identifiers', () => {
    const event: ErrorEvent = {
      type: undefined,
      extra: {
        phase: 'reporter typed private feedback',
        project_id: 'project/with/query?token=secret',
        widget_rule_id: 'x'.repeat(129),
      },
    }
    expect(scrubEvent(event).extra).toBeUndefined()
  })

  it('omits extra entirely when no allowed context key is present', () => {
    const event: ErrorEvent = { type: undefined, extra: { unrelated: 'value' } }
    const scrubbed = scrubEvent(event)
    expect(scrubbed.extra).toBeUndefined()
  })

  it('preserves useful allowed evidence: type, stacktrace, timestamp, release, environment', () => {
    const event: ErrorEvent = {
      type: undefined,
      event_id: 'abc123',
      timestamp: 1721818800,
      level: 'error',
      release: 'widget@1.0.0',
      environment: 'staging',
      exception: {
        values: [{
          type: 'TypeError',
          stacktrace: { frames: [{ filename: 'widget.js', function: 'mount', lineno: 10, in_app: true }] },
        }],
      },
    }
    const scrubbed = scrubEvent(event)
    expect(scrubbed.event_id).toBe('abc123')
    expect(scrubbed.timestamp).toBe(1721818800)
    expect(scrubbed.level).toBe('error')
    expect(scrubbed.release).toBe('widget@1.0.0')
    expect(scrubbed.environment).toBe('staging')
    expect(scrubbed.exception!.values![0]!.type).toBe('TypeError')
    expect(scrubbed.exception!.values![0]!.stacktrace?.frames?.[0]?.function).toBe('mount')
  })
})
