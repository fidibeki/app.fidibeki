import { describe, expect, it } from 'vitest'
import { cleanAppearanceOverrides, resolveWidgetAppearance } from '../../utils/widget-appearance'

describe('widget appearance hierarchy', () => {
  it('resolves workspace, project, and definition overrides in order', () => {
    expect(resolveWidgetAppearance(
      { theme: 'dark', accent_color: '#005f56', logo_url: 'https://example.com/workspace.svg' },
      { surface_color: '#101817', text_color: '#f7faf9' },
      { position: 'bottom-left', logo_url: '' },
    )).toEqual({
      position: 'bottom-left', theme: 'dark', accent_color: '#005f56',
      surface_color: '#101817', text_color: '#f7faf9', logo_url: undefined,
    })
  })

  it('omits blank values while preserving an explicit logo reset', () => {
    expect(cleanAppearanceOverrides({ accent_color: ' #005F56 ', logo_url: ' ' }, true))
      .toEqual({ accent_color: '#005f56', logo_url: '' })
  })
})
