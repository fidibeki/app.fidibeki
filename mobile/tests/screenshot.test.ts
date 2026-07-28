import { MAX_SCREENSHOT_BYTES, MobileClientError, prepareScreenshot } from '../src'

const random = (target: Uint8Array) => {
  target.fill(7)
  return target
}

describe('host-owned screenshot preparation', () => {
  it('reads a supported declaration and never retains the local URI', async () => {
    const dispose = vi.fn()
    const blob = new Blob(['png'], { type: 'image/png' })
    const prepared = await prepareScreenshot(
      {
        uri: 'file:///private/user/screenshot.png',
        contentType: 'image/png',
        size: blob.size,
        read: async () => blob,
        dispose,
      },
      random,
    )
    expect(JSON.stringify(prepared)).not.toContain('file:///')
    await prepared.dispose()
    await prepared.dispose()
    expect(dispose).toHaveBeenCalledOnce()
  })

  it.each([
    ['invalid MIME', 'image/gif', 3],
    ['empty', 'image/png', 0],
    ['oversized', 'image/png', MAX_SCREENSHOT_BYTES + 1],
  ])('rejects and disposes %s declarations', async (_name, contentType, size) => {
    const dispose = vi.fn()
    await expect(
      prepareScreenshot(
        {
          uri: 'local://asset',
          contentType: contentType as 'image/png',
          size,
          read: async () => new Blob(['png'], { type: 'image/png' }),
          dispose,
        },
        random,
      ),
    ).rejects.toBeInstanceOf(MobileClientError)
    expect(dispose).toHaveBeenCalledOnce()
  })

  it('rejects read/type/size mismatches and provider read errors with cleanup', async () => {
    const mismatchDispose = vi.fn()
    await expect(
      prepareScreenshot(
        {
          uri: 'local://asset',
          contentType: 'image/png',
          size: 10,
          read: async () => new Blob(['jpeg'], { type: 'image/jpeg' }),
          dispose: mismatchDispose,
        },
        random,
      ),
    ).rejects.toThrow('does not match')
    expect(mismatchDispose).toHaveBeenCalledOnce()

    const errorDispose = vi.fn()
    await expect(
      prepareScreenshot(
        {
          uri: 'local://asset',
          contentType: 'image/webp',
          size: 10,
          read: async () => {
            throw new Error('permission denied by host')
          },
          dispose: errorDispose,
        },
        random,
      ),
    ).rejects.toThrow('could not be read')
    expect(errorDispose).toHaveBeenCalledOnce()
  })
})
