import { describe, expect, it } from 'vitest'
import { defaultWidgetRule, widgetEmbedCode } from '../../utils/widget-setup'

describe('widget setup helpers', () => {
  it('creates a valid conservative first rule', () => {
    expect(defaultWidgetRule()).toMatchObject({
      enabled: true,
      matcher_type: 'all_pages',
      trigger_type: 'delay',
      trigger_delay_seconds: 0,
      can_rate: true,
      can_write_text: true,
      can_record_audio: false,
      can_record_video: false,
      replay_mode: 'none',
      recurrence_mode: 'cooldown',
      recurrence_days: 30,
      definition_status: 'active',
      presentation: 'launcher',
      objective: 'continuous_feedback',
      response_blocks: [
        { key: 'rating', type: 'rating', required: false },
        { key: 'feedback', type: 'free_text', required: false },
      ],
      collect_contact: false,
    })
  })

  it('uses the configured environment widget host', () => {
    expect(widgetEmbedCode('pk_live_key', 'staging.fidibeki.com'))
      .toContain('https://widget.staging.fidibeki.com/v1/loader.js')
    expect(widgetEmbedCode('pk_live_key', 'localhost'))
      .toContain('http://widget.localhost:8088/v1/loader.js')
  })
})
