import { describe, expect, it } from 'vitest'
import { dimensionsForViewport, dimensionsForWidth, sourceAspectRatio } from '../../utils/replay-player-geometry'

describe('replay player geometry', () => {
  it('falls back to a 16:9 source when no recorded viewport is known', () => {
    expect(sourceAspectRatio({})).toEqual({ width: 1280, height: 720 })
  })

  it('uses the recorded viewport when both dimensions are positive', () => {
    expect(sourceAspectRatio({ viewportWidth: 1024, viewportHeight: 900 })).toEqual({ width: 1024, height: 900 })
  })

  it('ignores a non-positive recorded dimension and derives height from the default ratio', () => {
    expect(sourceAspectRatio({ viewportWidth: 640, viewportHeight: 0 })).toEqual({ width: 640, height: 360 })
  })

  it('fits width only, deriving height from the recorded aspect ratio', () => {
    expect(dimensionsForWidth({ viewportWidth: 1280, viewportHeight: 720 }, 800)).toEqual({ width: 800, height: 450 })
  })

  it('never collapses to a zero or negative size for a tiny available width', () => {
    expect(dimensionsForWidth({}, 0)).toEqual({ width: 1, height: 1 })
  })

  it('fits within a viewport constrained by width, preserving aspect ratio', () => {
    // 16:9 source into a tall 1000x2000 viewport is width-bound.
    expect(dimensionsForViewport({ viewportWidth: 1280, viewportHeight: 720 }, 1000, 2000)).toEqual({ width: 1000, height: 562 })
  })

  it('fits within a viewport constrained by height, preserving aspect ratio', () => {
    // 16:9 source into a 800x300 viewport is height-bound.
    expect(dimensionsForViewport({ viewportWidth: 1280, viewportHeight: 720 }, 800, 300)).toEqual({ width: 533, height: 300 })
  })

  it('never returns a negative size when the available viewport is smaller than zero', () => {
    expect(dimensionsForViewport({}, -10, -10)).toEqual({ width: 1, height: 1 })
  })
})
