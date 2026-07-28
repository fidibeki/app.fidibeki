import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './tests/e2e',
  fullyParallel: true,
  use: {
    ...devices['Desktop Chrome'],
    baseURL: 'http://127.0.0.1:4174',
  },
  webServer: {
    // Run the production Nitro bundle. A dev server shares .nuxt state with a
    // preceding build and can miss Nuxt's generated #app-manifest alias.
    command: 'node tests/e2e/ci-server.mjs',
    url: 'http://127.0.0.1:4174/login',
    reuseExistingServer: !process.env.CI,
  },
})
