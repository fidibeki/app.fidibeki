import { readFileSync } from 'node:fs'
import { globSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

describe('forbidden native behavior', () => {
  it('contains no WebView, analytics, device identifier, private key, or permission API', () => {
    const root = fileURLToPath(new URL('../src', import.meta.url))
    const source = globSync('**/*.{ts,tsx}', { cwd: root }).map((path) =>
      readFileSync(join(root, path), 'utf8'),
    )
    const implementation = source.join('\n')
    for (const forbidden of [
      'react-native-webview',
      '<WebView',
      'CameraRoll',
      'requestCameraPermission',
      'requestMultiple',
      'advertisingId',
      'vendorIdentifier',
      'deviceId',
      'pk_live_',
      'mobile_capture:create',
      '/analytics',
      '/interactions',
      '/impressions',
      'FidibekiFeedbackLauncher',
      'fidibeki-launcher',
    ]) {
      expect(implementation).not.toContain(forbidden)
    }
  })
})
