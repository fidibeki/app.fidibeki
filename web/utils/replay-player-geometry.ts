export interface ReplaySource {
  viewportWidth?: number
  viewportHeight?: number
}

export interface ReplayDimensions {
  width: number
  height: number
}

const DEFAULT_SOURCE_WIDTH = 1280
const DEFAULT_ASPECT_RATIO = 0.5625

export function sourceAspectRatio(source: ReplaySource): ReplayDimensions {
  const width = source.viewportWidth && source.viewportWidth > 0 ? source.viewportWidth : DEFAULT_SOURCE_WIDTH
  const height = source.viewportHeight && source.viewportHeight > 0
    ? source.viewportHeight
    : Math.round(width * DEFAULT_ASPECT_RATIO)
  return { width, height }
}

/** Fits the recorded aspect ratio to an available width only, letting height follow. */
export function dimensionsForWidth(source: ReplaySource, availableWidth: number): ReplayDimensions {
  const { width: sourceWidth, height: sourceHeight } = sourceAspectRatio(source)
  const width = Math.max(1, Math.floor(availableWidth))
  return { width, height: Math.max(1, Math.round(width * sourceHeight / sourceWidth)) }
}

/** Fits the recorded aspect ratio inside an available width AND height, e.g. a fullscreen viewport. */
export function dimensionsForViewport(source: ReplaySource, availableWidth: number, availableHeight: number): ReplayDimensions {
  const { width: sourceWidth, height: sourceHeight } = sourceAspectRatio(source)
  const widthScale = Math.max(0, availableWidth) / sourceWidth
  const heightScale = Math.max(0, availableHeight) / sourceHeight
  const scale = Math.min(widthScale, heightScale)
  return {
    width: Math.max(1, Math.floor(sourceWidth * scale)),
    height: Math.max(1, Math.floor(sourceHeight * scale)),
  }
}
