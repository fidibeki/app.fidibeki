import { MobileClientError, type PreparedScreenshot, type ScreenshotAsset } from './types'
import { createUUID, type RandomBytes } from './uuid'

export const MAX_SCREENSHOT_BYTES = 10 * 1024 * 1024
const SUPPORTED_TYPES = new Set(['image/png', 'image/jpeg', 'image/webp'])

async function disposeOnce(asset: ScreenshotAsset, state: { disposed: boolean }): Promise<void> {
  if (state.disposed) return
  state.disposed = true
  await asset.dispose?.()
}

export async function prepareScreenshot(
  asset: ScreenshotAsset,
  randomBytes?: RandomBytes,
): Promise<PreparedScreenshot> {
  const state = { disposed: false }
  try {
    if (!SUPPORTED_TYPES.has(asset.contentType) || asset.size < 1 || asset.size > MAX_SCREENSHOT_BYTES) {
      throw new MobileClientError('screenshot_error', 'Invalid screenshot declaration')
    }
    const blob = await asset.read()
    if (
      blob.size !== asset.size ||
      blob.type.toLowerCase() !== asset.contentType ||
      !SUPPORTED_TYPES.has(blob.type.toLowerCase())
    ) {
      throw new MobileClientError('screenshot_error', 'Screenshot data does not match its declaration')
    }
    return {
      clientAttachmentId: createUUID(randomBytes),
      contentType: asset.contentType,
      size: asset.size,
      blob,
      dispose: () => disposeOnce(asset, state),
    }
  } catch (error) {
    await disposeOnce(asset, state)
    if (error instanceof MobileClientError) throw error
    throw new MobileClientError('screenshot_error', 'Screenshot could not be read', { cause: error })
  }
}
