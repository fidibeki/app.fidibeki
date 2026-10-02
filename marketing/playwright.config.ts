import { defineConfig, devices } from '@playwright/test'

const port = Number(process.env.PLAYWRIGHT_PORT || 4311)

export default defineConfig({
  testDir: './tests/e2e',
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  reporter: 'list',
  use: {
    baseURL: `http://127.0.0.1:${port}`,
    trace: 'on-first-retry',
  },
  projects: [
    {
      name: 'desktop',
      use: {
        ...devices['Desktop Chrome'],
        viewport: { width: 1440, height: 1000 },
      },
    },
    {
      // Chromium with a tablet viewport; WebKit is not installed in this repo's
      // Playwright cache and the responsive checks only need the dimensions.
      name: 'tablet',
      testMatch: /responsive\.spec\.ts/,
      use: {
        ...devices['Desktop Chrome'],
        viewport: { width: 834, height: 1112 },
      },
    },
    {
      name: 'mobile',
      testMatch: /(responsive|navigation|homepage|marketing)\.spec\.ts/,
      use: { ...devices['Pixel 7'] },
    },
  ],
  webServer: {
    command: 'node scripts/serve-static.mjs',
    port,
    env: { PORT: String(port) },
    reuseExistingServer: !process.env.CI,
  },
})
