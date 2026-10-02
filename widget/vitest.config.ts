import { defineConfig } from 'vitest/config'

export default defineConfig({
  test: {
    environment: 'happy-dom',
    globals: true,
    include: ['tests/unit/**/*.test.ts'],
    coverage: {
      provider: 'v8',
      allowExternal: true,
      include: [
        'src/analytics.ts', 'src/analytics-outbox.ts', 'src/config.ts', 'src/targeting.ts',
        'src/triggers.ts', 'src/suppression.ts', 'src/metadata.ts',
        'src/policy.ts',
        'src/submission-errors.ts',
        'src/privacy-location.ts',
        'src/replay-consent.ts',
        'src/consent-prompt.ts',
        'contracts/widget-objective-policy.ts',
        'src/modules/replay.ts',
        'src/modules/privacy.ts',
        'src/modules/replay-content.ts',
        'src/modules/element-attachment.ts',
        'src/modules/media-mime.ts',
      ],
      reporter: ['text', 'json-summary'],
      thresholds: {
        statements: 80,
        branches: 75,
        functions: 80,
        lines: 80,
      },
    },
  },
})
