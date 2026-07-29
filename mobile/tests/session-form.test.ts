import {
  EMPTY_FORM_STATE,
  MAX_TEXT_LENGTH,
  MobileClientError,
  createConsentInput,
  reconcileResponses,
  systemMessages,
  validateCaptureSession,
  validateForm,
} from '../src'
import { definition, session } from './fixtures'

describe('capture session and form contracts', () => {
  it('accepts the complete resolved native definition', () => {
    expect(validateCaptureSession(session())).toEqual(session())
  })

  it.each(['launcher', 'prompt', 'inline', 'reaction'])(
    'accepts active definition presentation metadata %s',
    (presentation) => {
      const candidate = structuredClone(session()) as unknown as {
        configuration: { definition: { presentation: string } }
      }
      candidate.configuration.definition.presentation = presentation
      expect(validateCaptureSession(candidate)).toEqual(candidate)
    },
  )

  it('accepts backend legacy/disabled serialization edge cases', () => {
    const legacy = structuredClone(session()) as unknown as {
      configuration: {
        definition: {
          recurrence_mode: string
          recurrence_days: null
          localization: Record<string, unknown>
          ai_processing: { enabled: boolean }
        }
      }
    }
    legacy.configuration.definition.recurrence_mode = 'session'
    legacy.configuration.definition.recurrence_days = null
    delete legacy.configuration.definition.localization.block_labels
    delete legacy.configuration.definition.localization.option_labels
    legacy.configuration.definition.ai_processing = { enabled: false }
    expect(validateCaptureSession(legacy)).toEqual(legacy)
    const webMedia = structuredClone(session())
    webMedia.configuration.definition.ai_processing!.input_categories = [
      'feedback_text',
      'screenshot',
      'audio',
      'video',
    ]
    expect(validateCaptureSession(webMedia)).toEqual(webMedia)
  })

  it('rejects unsupported blocks, duplicate keys, and unsafe consent links', () => {
    const current = session()
    const duplicate = session({
      configuration: {
        ...current.configuration,
        definition: definition({
          response_blocks: [
            { key: 'same', type: 'rating', required: true },
            { key: 'same', type: 'free_text', required: false },
          ],
          localization: {
            ...definition().localization,
            block_labels: { same: 'Same' },
          },
        }),
      },
    })
    expect(() => validateCaptureSession(duplicate)).toThrowError(MobileClientError)
    expect(() =>
      validateCaptureSession(
        session({
          configuration: {
            ...current.configuration,
            contact_notice: {
              ...current.configuration.contact_notice!,
              privacy_policy_url: 'http://example.test/privacy',
            },
          },
        }),
      ),
    ).toThrow('privacy policy URL must use HTTPS')
    expect(() =>
      validateCaptureSession(
        session({
          configuration: {
            ...current.configuration,
            contact_notice: {
              ...current.configuration.contact_notice!,
              privacy_policy_url: 'https://user:secret@example.test/privacy',
            },
          },
        }),
      ),
    ).toThrow('privacy policy URL must not include credentials')
    const unsafeAI = structuredClone(session())
    unsafeAI.configuration.definition.ai_processing!.privacy_policy_url = 'javascript:alert(1)'
    expect(() => validateCaptureSession(unsafeAI)).toThrow('AI privacy policy URL must use HTTPS')
    unsafeAI.configuration.definition.ai_processing!.privacy_policy_url = 'https://user:secret@example.test/privacy'
    expect(() => validateCaptureSession(unsafeAI)).toThrow('AI privacy policy URL must not include credentials')
  })

  it('validates every response block and optional reporter consent', () => {
    const current = definition()
    const missing = validateForm(current, {
      ...EMPTY_FORM_STATE,
      reporterEmail: 'not-an-email',
    })
    expect(missing.fields).toEqual({
      'responses.rating': 'required',
      'responses.choice': 'required',
      'responses.details': 'required',
      reporter_email: 'invalid',
      consent: 'required',
    })
    const validState = {
      responses: {
        rating: 5,
        reaction: 'happy',
        choice: 'a',
        details: 'The payment review was clear.',
      },
      comment: '',
      reporterEmail: 'reporter@example.test',
      consentGranted: true,
    }
    expect(validateForm(current, validState)).toEqual({
      valid: true,
      fields: {},
    })
    const currentSession = session()
    expect(
      createConsentInput(
        session({
          configuration: {
            ...currentSession.configuration,
            display_locale: 'pt-PT',
            contact_notice: {
              ...currentSession.configuration.contact_notice!,
              locale: 'pt-PT',
            },
          },
        }),
        validState,
      ),
    ).toEqual({
      purpose: 'feedback_updates',
      notice_version: '2026-07-controller-v1',
      controller_profile_version: 3,
      widget_definition_version: 2,
      locale: 'pt-PT',
      granted: true,
    })
    const contactDisabled = session()
    contactDisabled.configuration.definition.collect_contact = false
    expect(createConsentInput(contactDisabled, validState)).toBeUndefined()
  })

  it('preserves only compatible answers after a definition change', () => {
    const previous = definition().response_blocks
    const next = [previous[0]!, { key: 'choice', type: 'free_text' as const, required: true }, previous[3]!]
    expect(
      reconcileResponses(previous, next, {
        rating: 4,
        reaction: 'sad',
        choice: 'a',
        details: 'Keep this',
      }),
    ).toEqual({ rating: 4, details: 'Keep this' })
  })

  it('uses the shared 10,000-character free-text and comment bound', () => {
    const state = {
      responses: {
        rating: 5,
        choice: 'a',
        details: 'x'.repeat(MAX_TEXT_LENGTH),
      },
      comment: 'y'.repeat(MAX_TEXT_LENGTH),
      reporterEmail: '',
      consentGranted: false,
    }
    expect(validateForm(definition(), state)).toEqual({
      valid: true,
      fields: {},
    })
    expect(
      validateForm(definition(), {
        ...state,
        responses: {
          ...state.responses,
          details: 'x'.repeat(MAX_TEXT_LENGTH + 1),
        },
        comment: 'y'.repeat(MAX_TEXT_LENGTH + 1),
      }).fields,
    ).toEqual({
      'responses.details': 'invalid',
      comment: 'too_long',
    })
  })

  it('localizes system copy in English and Portuguese with English fallback', () => {
    expect(systemMessages('pt-AO').submitFailed).toContain('Não foi possível')
    expect(systemMessages('en-US').addScreenshot).toBe('Add screenshot')
    expect(systemMessages('fr-FR').addScreenshot).toBe('Add screenshot')
  })
})
