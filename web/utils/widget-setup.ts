import type { WidgetRuleInput } from '~/types/api'

export function defaultWidgetRule(): WidgetRuleInput {
  return {
    name: 'All pages', priority: 0, enabled: true, matcher_type: 'all_pages', matcher_value: null,
    trigger_type: 'delay', trigger_delay_seconds: 0, trigger_scroll_percent: null,
    can_rate: true, can_write_text: true, can_record_audio: false, can_record_video: false,
    can_attach_element: false, can_attach_screenshot: false, greeting: 'How was your experience?',
    position: 'bottom-right', theme: 'auto', accent_color: '#087f73', replay_mode: 'none',
    replay_sampling_rate: 0, replay_max_duration_seconds: 1800, replay_max_events: 50000,
    replay_mask_all_inputs: true, replay_mask_text_selector: null,
    recurrence_mode: 'cooldown', recurrence_days: 30,
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
