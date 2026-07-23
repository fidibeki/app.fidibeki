import type { WidgetAppearance, WidgetAppearanceOverrides } from '~/types/api'

export const defaultWidgetAppearance: WidgetAppearance = {
  position: 'bottom-right',
  theme: 'auto',
  accent_color: '#087f73',
  accent_foreground: '#ffffff',
}

const accentForegroundCandidates = ['#171a19', '#ffffff', '#000000'] as const
const minimumTextContrast = 4.5

export function resolveAccentForeground(accent: string): string {
  const normalized = accent.trim().toLowerCase()
  if (!/^#[0-9a-f]{6}$/.test(normalized)) return '#ffffff'
  return accentForegroundCandidates.find(
    foreground => contrastRatio(normalized, foreground) >= minimumTextContrast,
  ) || '#000000'
}

export function contrastRatio(left: string, right: string): number {
  const leftLuminance = relativeLuminance(left)
  const rightLuminance = relativeLuminance(right)
  return (Math.max(leftLuminance, rightLuminance) + 0.05)
    / (Math.min(leftLuminance, rightLuminance) + 0.05)
}

function relativeLuminance(color: string): number {
  const normalized = color.trim().toLowerCase()
  if (!/^#[0-9a-f]{6}$/.test(normalized)) return 0
  const components = [1, 3, 5].map((offset) => {
    const component = Number.parseInt(normalized.slice(offset, offset + 2), 16) / 255
    return component <= 0.04045
      ? component / 12.92
      : ((component + 0.055) / 1.055) ** 2.4
  })
  return 0.2126 * components[0]! + 0.7152 * components[1]! + 0.0722 * components[2]!
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

export function resolveWidgetAppearance(
  ...layers: Array<WidgetAppearanceOverrides | WidgetAppearance | undefined>
): WidgetAppearance {
  const resolved: WidgetAppearance = { ...defaultWidgetAppearance }
  for (const layer of layers) {
    if (!layer) continue
    if (layer.position) resolved.position = layer.position
    if (layer.theme) resolved.theme = layer.theme
    if (layer.accent_color) {
      resolved.accent_color = layer.accent_color
      resolved.accent_foreground = 'accent_foreground' in layer && layer.accent_foreground
        ? layer.accent_foreground
        : resolveAccentForeground(layer.accent_color)
    }
    if (layer.surface_color) resolved.surface_color = layer.surface_color
    if (layer.text_color) resolved.text_color = layer.text_color
    if (layer.logo_url !== undefined) resolved.logo_url = layer.logo_url || undefined
  }
  return resolved
}
