import type { WidgetRuleInput } from '~/types/api'

export function defaultWidgetRule(): WidgetRuleInput {
  return {
    name: 'All pages', priority: 0, enabled: false, matcher_type: 'all_pages', matcher_value: null,
    trigger_type: 'delay', trigger_delay_seconds: 0, trigger_scroll_percent: null,
    can_rate: true, can_write_text: true, can_record_audio: false, can_record_video: false,
    can_attach_element: false, can_attach_screenshot: false, greeting: 'How was your experience?',
    position: 'bottom-right', theme: 'auto', accent_color: '#087f73', replay_mode: 'none',
    replay_sampling_rate: 0, replay_max_duration_seconds: 1800, replay_max_events: 50000,
    replay_mask_all_inputs: true, replay_mask_text_selector: null,
    replay_record_canvas: false, replay_canvas_fps: 2,
    replay_consent: {
      mode: 'none', notice_version: '', controller_name: '', privacy_policy_url: '',
      supported_locales: [], retention_days: 0, expiry_days: 0, notices: {},
    },
    analytics_consent: {
      mode: 'none', notice_version: '', controller_name: '', privacy_policy_url: '',
      supported_locales: [], retention_days: 0, expiry_days: 30, notices: {},
    },
    recurrence_mode: 'cooldown', recurrence_days: 30,
    definition_key: 'all-pages-feedback', definition_status: 'draft', presentation: 'launcher',
    objective: 'continuous_feedback',
    response_blocks: [
      { key: 'rating', type: 'rating', required: false },
      { key: 'feedback', type: 'free_text', required: false },
    ],
    localizations: {
      en: {
        launcher_label: 'Feedback', prompt: 'How was your experience?',
        comment_placeholder: 'Tell us more', contact_label: 'Email address',
        consent_label: 'Email me about this feedback', submit_label: 'Send feedback',
        success_message: 'Your feedback was sent.',
        privacy_purpose_summary: '', privacy_lawful_basis_summary: '',
        privacy_retention_summary: '',
      },
    },
    allow_comment: false, collect_contact: false,
    ai_processing: { enabled: false },
    appearance_overrides: {},
  }
}

export function widgetOrigin(baseDomain: string) {
  return baseDomain === 'localhost'
    ? 'http://widget.localhost:8088'
    : `https://widget.${baseDomain}`
}

export function widgetEmbedCode(publicKey: string, baseDomain: string) {
  return `<script src="${widgetOrigin(baseDomain)}/v1/loader.js" data-fidibeki-key="${publicKey}" async><\/script>`
}
