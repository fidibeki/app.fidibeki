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
				'contracts/widget-objective-policy.ts',
				'src/modules/replay.ts',
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
