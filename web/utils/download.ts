export function parseSignedDownloadURL(value: string, baseURL: string): string {
  const url = new URL(value, baseURL)
  if ((url.protocol !== 'https:' && url.protocol !== 'http:') || url.username || url.password) {
    throw new Error('signed download URL is invalid')
  }
  return url.toString()
}

export function triggerSignedDownload(value: string, filename: string) {
  const href = parseSignedDownloadURL(value, window.location.origin)
  const anchor = document.createElement('a')
  anchor.href = href
  anchor.download = filename
  anchor.rel = 'noopener noreferrer'
  anchor.referrerPolicy = 'no-referrer'
  document.body.append(anchor)
  anchor.click()
  anchor.remove()
}

export function triggerBlobDownload(blob: Blob, filename: string) {
  const href = URL.createObjectURL(blob)
  const anchor = document.createElement('a')
  anchor.href = href
  anchor.download = filename
  document.body.append(anchor)
  anchor.click()
  anchor.remove()
  URL.revokeObjectURL(href)
}
