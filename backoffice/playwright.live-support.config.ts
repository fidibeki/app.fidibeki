import { defineConfig, devices } from '@playwright/test'

// Runs against the real Go API, Redis, PostgreSQL and the two Nuxt proxies.
// See docs/live-support.md for the isolated local stack and launch commands.
export default defineConfig({
  testDir: './tests/live-support',
  workers: 1,
  timeout: 60_000,
  expect: { timeout: 10_000 },
  reporter: 'list',
  use: { ...devices['Desktop Chrome'], trace: 'retain-on-failure' },
})
