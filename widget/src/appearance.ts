const foregroundCandidates = ['#171a19', '#ffffff', '#000000'] as const
const minimumTextContrast = 4.5

export function resolveAccentForeground(accent: string): string {
  const normalized = accent.trim().toLowerCase()
  if (!/^#[0-9a-f]{6}$/.test(normalized)) return '#ffffff'
  return foregroundCandidates.find(
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
