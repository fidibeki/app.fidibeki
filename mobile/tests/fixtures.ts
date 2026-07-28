import type {
  MobileCaptureSession,
  MobileWidgetDefinition,
  StorageAdapter,
} from '../src'

export class MemoryStorage implements StorageAdapter {
  readonly values = new Map<string, string>()

  async getItem(key: string): Promise<string | null> {
    return this.values.get(key) ?? null
  }

  async setItem(key: string, value: string): Promise<void> {
    this.values.set(key, value)
  }

  async removeItem(key: string): Promise<void> {
    this.values.delete(key)
  }
}

export function definition(
  overrides: Partial<MobileWidgetDefinition> = {},
): MobileWidgetDefinition {
  return {
    id: 'rule-1',
    definition_key: 'mobile-example',
    definition_version: 2,
    presentation: 'launcher',
    launcher_variant: 'floating_button',
    objective: 'continuous_feedback',
    recurrence_mode: 'session',
    response_blocks: [
      { key: 'rating', type: 'rating', required: true },
      {
        key: 'reaction',
        type: 'reaction',
        required: false,
        options: [
          { value: 'happy' },
          { value: 'sad' },
        ],
      },
      {
        key: 'choice',
        type: 'single_choice',
        required: true,
        options: [
          { value: 'a' },
          { value: 'b' },
        ],
      },
      { key: 'details', type: 'free_text', required: true },
    ],
    localization: {
      launcher_label: 'Send feedback',
      prompt: 'How did checkout go?',
      comment_placeholder: 'Anything else?',
      contact_label: 'Email for updates',
      consent_label: 'I agree to receive updates',
      submit_label: 'Send feedback',
      success_message: 'Thanks for the feedback.',
      privacy_purpose_summary: 'We use your email to send feedback updates.',
      privacy_lawful_basis_summary: 'Processing is based on consent.',
      privacy_retention_summary: 'Evidence is retained with the feedback.',
      block_labels: {
        rating: 'Rating',
        reaction: 'Reaction',
        choice: 'Choose one',
        details: 'Details',
      },
      option_labels: {
        reaction: { happy: 'Happy', sad: 'Sad' },
        choice: { a: 'Alpha', b: 'Beta' },
      },
    },
    allow_comment: true,
    collect_contact: true,
    can_attach_screenshot: true,
    ai_processing: {
      enabled: true,
      provider: 'google_gemini',
      privacy_policy_url: 'https://example.test/privacy',
      policy_version: '2026-07-24',
      input_categories: ['feedback_text', 'screenshot'],
    },
    appearance: {},
    ...overrides,
  }
}

export function session(
  overrides: Partial<MobileCaptureSession> = {},
): MobileCaptureSession {
  return {
    token: 'fmc_test_capture_session_token_123456789',
    submit_expires_at: '2099-07-28T12:00:00.000Z',
    configuration: {
      schema_version: 1,
      project_id: 'project-1',
      display_locale: 'en',
      definition: definition(),
      controller_profile: {
        version: 3,
        name: 'Example Company',
        privacy_contact_email: 'privacy@example.test',
        privacy_rights_email: 'rights@example.test',
        privacy_policy_url: 'https://example.test/privacy',
      },
      contact_notice: {
        notice_version: '2026-07-controller-v1',
        controller_profile_version: 3,
        widget_definition_version: 2,
        locale: 'en',
        controller_name: 'Example Company',
        privacy_contact_email: 'privacy@example.test',
        privacy_rights_email: 'rights@example.test',
        privacy_policy_url: 'https://example.test/privacy',
        purpose_summary: 'We use your email to send feedback updates.',
        lawful_basis_summary: 'Processing is based on consent.',
        retention_summary: 'Evidence is retained with the feedback.',
      },
    },
    ...overrides,
  }
}
