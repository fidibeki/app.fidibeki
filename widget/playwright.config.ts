import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './tests/e2e',
  timeout: 30_000,
  fullyParallel: true,
  reporter: [['list']],
  use: {
    baseURL: 'http://127.0.0.1:4173',
    trace: 'retain-on-failure',
  },
  webServer: {
    command: 'node tests/server.mjs',
    url: 'http://127.0.0.1:4173/sample.html',
    reuseExistingServer: true,
  },
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' } },
    // Firefox coverage is scoped to the attachment pipeline spec: that is the surface with
    // browser-dependent MediaRecorder container behavior (see issue #56). Every other spec
    // continues to run only on the primary Chromium path.
    {
      name: 'firefox',
      use: { browserName: 'firefox' },
      testMatch: /attachments\.spec\.ts/,
    },
  ],
})
