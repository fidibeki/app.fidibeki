import { readFileSync } from 'node:fs'
import { describe, expect, it } from 'vitest'
import {
  MobileClient,
  MobileClientError,
  canonicalRequest,
  normalizeScreen,
  recurrenceStorageKey,
  type MobileFeedbackRequest,
  validateCaptureSession,
} from '../src'

function fixture<T>(name: string): T {
  const url = new URL(`../../contracts/mobile-capture/fixtures/${name}`, import.meta.url)
  return JSON.parse(readFileSync(url, 'utf8')) as T
}

function unknownFieldTarget(
  session: Record<string, unknown>,
  target: string,
): Record<string, unknown> {
  const configuration = session.configuration as Record<string, unknown>
  const definition = configuration.definition as Record<string, unknown>
  const blocks = definition.response_blocks as Array<Record<string, unknown>>
  const targets: Record<string, Record<string, unknown>> = {
    session,
    configuration,
    definition,
    response_block: blocks[0]!,
    response_option: (blocks[1]!.options as Array<Record<string, unknown>>)[0]!,
    localization: definition.localization as Record<string, unknown>,
    appearance: definition.appearance as Record<string, unknown>,
    ai_processing: definition.ai_processing as Record<string, unknown>,
    controller_profile: configuration.controller_profile as Record<string, unknown>,
    contact_notice: configuration.contact_notice as Record<string, unknown>,
  }
  const selected = targets[target]
  if (!selected) throw new Error(`Unknown fixture target: ${target}`)
  return selected
}

describe('shared mobile capture contracts', () => {
  it('accepts the versioned valid session fixture', () => {
    const parsed = validateCaptureSession(fixture('session-valid.json'))
    expect(parsed.configuration.schema_version).toBe(1)
    expect(parsed.configuration.definition.response_blocks).toHaveLength(4)
  })

  it('rejects an unsupported session schema', () => {
    expect(() => validateCaptureSession(fixture('session-invalid-schema.json'))).toThrowError(
      MobileClientError,
    )
  })

  it('rejects shared unknown fields at every contract object', () => {
    const contract = fixture<{
      unknown_field: string
      cases: Array<{ fixture: string; target: string }>
    }>('session-unknown-fields.json')

    for (const testCase of contract.cases) {
      const session = fixture<Record<string, unknown>>(testCase.fixture)
      unknownFieldTarget(session, testCase.target)[contract.unknown_field] = 'must be rejected'
      expect(
        () => validateCaptureSession(session),
        `target ${testCase.target}`,
      ).toThrowError(MobileClientError)
    }
  })

  it('sanitizes untrusted validation response content', async () => {
    const privateResponse = 'private-token reporter@example.invalid original-feedback'
    const client = new MobileClient({
      apiBaseUrl: 'https://api.example.test',
      fetch: async () =>
        new Response(
          JSON.stringify({
            error: {
              code: 'validation_failed',
              message: privateResponse,
              details: [
                { field: 'responses.feedback', message: privateResponse },
                { field: 'capture_token', message: privateResponse },
              ],
            },
          }),
          { status: 422 },
        ),
    })
    const request = fixture<{ request: MobileFeedbackRequest }>('feedback-submit-valid.json').request
    const failure = client.submitFeedback(
      validateCaptureSession(fixture('session-valid.json')),
      request,
    )

    await expect(failure).rejects.toMatchObject({
      message: 'Feedback is invalid',
      fields: { 'responses.feedback': 'invalid' },
    })
    await failure.catch((error: unknown) => {
      expect(String(error)).not.toContain(privateResponse)
      expect((error as MobileClientError).fields).not.toHaveProperty('capture_token')
    })
  })

  it('normalizes shared screen cases and uses the normalized recurrence component', () => {
    const contract = fixture<{
      cases: Array<{ input: string; expected: string; encoded_component: string }>
    }>('screen-normalization.json')
    for (const testCase of contract.cases) {
      expect(normalizeScreen(testCase.input)).toBe(testCase.expected)
      expect(recurrenceStorageKey('project', 'definition', testCase.input)).toContain(
        testCase.encoded_component,
      )
    }
  })

  it('uses the shared strict request and accepted response', async () => {
    const contract = fixture<{
      request: MobileFeedbackRequest
      accepted: {
        feedback_id: string
        feedback_public_id: string
        attachments: []
      }
    }>('feedback-submit-valid.json')
    expect(JSON.parse(canonicalRequest(contract.request))).toEqual(contract.request)

    const client = new MobileClient({
      apiBaseUrl: 'https://api.example.test',
      fetch: async () => new Response(JSON.stringify(contract.accepted), { status: 201 }),
    })
    await expect(
      client.submitFeedback(
        validateCaptureSession(fixture('session-valid.json')),
        contract.request,
      ),
    ).resolves.toMatchObject({ feedback_public_id: 'FDBK-FIXTURE' })
  })

  it('accepts the shared dynamic form and consent contract', () => {
    const session = validateCaptureSession(fixture('session-contact-valid.json'))
    const request = fixture<MobileFeedbackRequest>('feedback-submit-consent.json')

    expect(session.configuration.contact_notice?.locale).toBe('pt-PT')
    expect(session.configuration.definition.ai_processing?.enabled).toBe(true)
    expect(JSON.parse(canonicalRequest(request))).toEqual(request)
  })

  it('accepts the shared screenshot declaration and upload grant', async () => {
    const contract = fixture<{
      request: MobileFeedbackRequest
      accepted: {
        feedback_id: string
        feedback_public_id: string
        attachments: Array<{
          attachment_id: string
          client_attachment_id: string
          presigned_url: string
          method: 'PUT'
          headers: Record<string, string>
          complete_token: string
          expires_at: string
        }>
      }
    }>('feedback-submit-screenshot.json')

    expect(JSON.parse(canonicalRequest(contract.request))).toEqual(contract.request)
    const client = new MobileClient({
      apiBaseUrl: 'https://api.example.test',
      fetch: async () => new Response(JSON.stringify(contract.accepted), { status: 201 }),
    })
    await expect(
      client.submitFeedback(
        validateCaptureSession(fixture('session-valid.json')),
        contract.request,
      ),
    ).resolves.toMatchObject({
      feedback_public_id: 'FDBK-SCREENSHOT',
      attachments: [{ method: 'PUT' }],
    })
  })

  it('maps every shared backend error fixture', async () => {
    const request = fixture<{ request: MobileFeedbackRequest }>('feedback-submit-valid.json').request
    const contract = fixture<{
      cases: Array<{
        status: number
        body: unknown
        expected_code: string
        retryable: boolean
      }>
    }>('feedback-submit-errors.json')

    for (const testCase of contract.cases) {
      const client = new MobileClient({
        apiBaseUrl: 'https://api.example.test',
        fetch: async () =>
          new Response(JSON.stringify(testCase.body), {
            status: testCase.status,
          }),
      })
      await expect(
        client.submitFeedback(
          validateCaptureSession(fixture('session-valid.json')),
          request,
        ),
      ).rejects.toMatchObject({
        code: testCase.expected_code,
        retryable: testCase.retryable,
      })
    }
  })
})
