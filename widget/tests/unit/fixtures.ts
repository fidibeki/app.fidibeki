import type { WidgetRule } from '../../src/types'

export function rule(overrides: Partial<WidgetRule> = {}): WidgetRule {
  return {
    id: 'rule-id',
    name: 'Test rule',
    priority: 0,
    matcher_type: 'all_pages',
    matcher_value: null,
    trigger_type: 'delay',
    trigger_delay_seconds: 0,
    trigger_scroll_percent: null,
    can_rate: true,
    can_write_text: true,
    can_record_audio: false,
    can_record_video: false,
    can_attach_element: false,
    can_attach_screenshot: false,
    greeting: null,
    position: 'bottom-left',
    theme: 'light',
    accent_color: null,
    replay_mode: 'none',
    replay_sampling_rate: 0,
    replay_max_duration_seconds: 1800,
    replay_max_events: 50000,
    replay_mask_all_inputs: true,
    replay_mask_text_selector: null,
    ...overrides,
  }
}
