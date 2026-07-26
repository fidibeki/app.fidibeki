import { defineConfig, devices } from '@playwright/test'

const port = process.env.PLAYWRIGHT_PORT || '4321'
const serverCommand = process.env.CI
  ? 'node tests/e2e/ci-web-server.mjs'
  : `NUXT_IGNORE_LOCK=1 NUXT_PUBLIC_TURNSTILE_SITE_KEY=1x00000000000000000000AA ./node_modules/.bin/nuxt dev --host 127.0.0.1 --port ${port}`

export default defineConfig({
  testDir: './tests/e2e',
  fullyParallel: true,
  // GitHub's standard runner has four cores and the suite is API-mocked, so the
  // browsers are the only real load. Two workers left half the runner idle.
  workers: process.env.CI ? 4 : undefined,
  retries: 0,
  reporter: 'list',
  expect: {
    timeout: process.env.CI ? 15_000 : 5_000,
  },
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    trace: 'retain-on-failure',
  },
  webServer: process.env.PLAYWRIGHT_EXTERNAL ? undefined : {
    command: serverCommand,
    url: `http://127.0.0.1:${port}/designs`,
    reuseExistingServer: true,
    timeout: 120_000,
  },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'], viewport: { width: 1440, height: 1000 } } },
    { name: 'mobile', use: { ...devices['Pixel 7'], viewport: { width: 390, height: 844 } } },
  ],
})
