import type { WidgetAppearance, WidgetAppearanceOverrides } from '~/types/api'

export const defaultWidgetAppearance: WidgetAppearance = {
  position: 'bottom-right',
  theme: 'auto',
  accent_color: '#087f73',
}

export function cleanAppearanceOverrides(
  input: WidgetAppearanceOverrides,
  clearLogo = false,
): WidgetAppearanceOverrides {
  const output: WidgetAppearanceOverrides = {}
  if (input.position) output.position = input.position
  if (input.theme) output.theme = input.theme
  if (input.accent_color?.trim()) output.accent_color = input.accent_color.trim().toLowerCase()
  if (input.surface_color?.trim()) output.surface_color = input.surface_color.trim().toLowerCase()
  if (input.text_color?.trim()) output.text_color = input.text_color.trim().toLowerCase()
  if (clearLogo) output.logo_url = ''
  else if (input.logo_url?.trim()) output.logo_url = input.logo_url.trim()
  return output
}

export function resolveWidgetAppearance(...layers: Array<WidgetAppearanceOverrides | undefined>): WidgetAppearance {
  const resolved: WidgetAppearance = { ...defaultWidgetAppearance }
  for (const layer of layers) {
    if (!layer) continue
    if (layer.position) resolved.position = layer.position
    if (layer.theme) resolved.theme = layer.theme
    if (layer.accent_color) resolved.accent_color = layer.accent_color
    if (layer.surface_color) resolved.surface_color = layer.surface_color
    if (layer.text_color) resolved.text_color = layer.text_color
    if (layer.logo_url !== undefined) resolved.logo_url = layer.logo_url || undefined
  }
  return resolved
}
