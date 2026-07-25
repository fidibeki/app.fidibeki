import { readFileSync } from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'
import { plans, trialDays } from '~/data/pricing'

// The marketing site must never advertise prices or limits that differ from
// what the backend actually enforces. These tests parse the Go plan catalog
// and fail if the two drift apart.

const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..', '..', '..')
const plansGo = readFileSync(
  path.join(repoRoot, 'backend', 'internal', 'config', 'plans.go'),
  'utf8',
)
const billingGo = readFileSync(
  path.join(repoRoot, 'backend', 'internal', 'billing', 'models.go'),
  'utf8',
)

function backendPlanBlock(goPlanName: string) {
  const marker = `tenant.Plan${goPlanName}: {`
  const start = plansGo.indexOf(marker)
  expect(start, `plan block for ${goPlanName}`).toBeGreaterThan(-1)
  // A plan entry ends at the next top-level map key (one tab of indentation)
  // or at the closing brace of the planDefinitions map.
  const nextKey = plansGo.indexOf('\n\ttenant.Plan', start + marker.length)
  const mapEnd = plansGo.indexOf('\n}', start + marker.length)
  const candidates = [nextKey, mapEnd].filter((index) => index !== -1)
  const end = candidates.length ? Math.min(...candidates) : undefined
  return plansGo.slice(start, end)
}

function extractInt(block: string, pattern: RegExp, label: string) {
  const match = block.match(pattern)
  expect(match, label).not.toBeNull()
  return Number(match![1])
}

const goNames: Record<string, string> = {
  starter: 'Starter',
  growth: 'Growth',
  scale: 'Scale',
  enterprise: 'Enterprise',
}

describe('pricing data mirrors the backend plan catalog', () => {
  for (const plan of plans) {
    const block = backendPlanBlock(goNames[plan.id])

    it(`matches ${plan.id} price`, () => {
      if (plan.monthlyPriceCents === null) {
        expect(block).not.toContain('MonthlyPriceCents')
      } else {
        expect(
          extractInt(block, /MonthlyPriceCents:\s*intPointer\((\d+)\)/, 'monthly price'),
        ).toBe(plan.monthlyPriceCents)
      }
    })

    it(`matches ${plan.id} yearly price`, () => {
      if (plan.yearlyPriceCents === null) {
        expect(block).not.toContain('YearlyPriceCents')
      } else {
        expect(
          extractInt(block, /YearlyPriceCents:\s*intPointer\((\d+)\)/, 'yearly price'),
        ).toBe(plan.yearlyPriceCents)
      }
    })

    it(`keeps ${plan.id} yearly price at or below the advertised discount`, () => {
      if (plan.yearlyPriceCents === null || plan.monthlyPriceCents === null || plan.yearlyDiscountPercent === null) return
      const fullYear = plan.monthlyPriceCents * 12
      const advertisedCeiling = fullYear * (1 - plan.yearlyDiscountPercent / 100)
      expect(plan.yearlyPriceCents).toBeLessThanOrEqual(advertisedCeiling)
      expect(plan.yearlyPriceCents % 12).toBe(0)
    })

    it(`matches ${plan.id} yearly discount`, () => {
      if (plan.yearlyDiscountPercent === null) {
        expect(block).not.toContain('YearlyDiscountPercent')
      } else {
        expect(
          extractInt(block, /YearlyDiscountPercent:\s*intPointer\((\d+)\)/, 'yearly discount'),
        ).toBe(plan.yearlyDiscountPercent)
      }
    })

    it(`matches ${plan.id} limits`, () => {
      expect(extractInt(block, /Projects:\s*(-?\d+)/, 'projects')).toBe(plan.limits.projects)
      expect(extractInt(block, /FeedbackPerMonth:\s*(-?\d+)/, 'feedback per month')).toBe(
        plan.limits.feedbackPerMonth,
      )
      expect(extractInt(block, /Users:\s*(-?\d+)/, 'users')).toBe(plan.limits.users)
      expect(extractInt(block, /StorageDays:\s*(-?\d+)/, 'storage days')).toBe(
        plan.limits.storageDays,
      )
    })

    it(`matches ${plan.id} feature flags`, () => {
      const flags: Record<keyof typeof plan.features, RegExp> = {
        ai: /AI:\s*true/,
        sessionReplay: /Replay:\s*true/,
        slack: /Slack:\s*true/,
        video: /Video:\s*true/,
        audio: /Audio:\s*true/,
        domCapture: /DOMCapture:\s*true/,
        customDomain: /CustomDomain:\s*true/,
      }
      for (const [key, pattern] of Object.entries(flags) as [
        keyof typeof plan.features,
        RegExp,
      ][]) {
        expect(pattern.test(block), `${plan.id}.${key}`).toBe(plan.features[key])
      }
    })
  }

  it('matches the trial length', () => {
    const match = billingGo.match(/TrialDuration\s*=\s*(\d+)\s*\*\s*24\s*\*\s*time\.Hour/)
    expect(match).not.toBeNull()
    expect(Number(match![1])).toBe(trialDays)
  })

  it('only self-serves the plans the backend exposes', () => {
    const selfServe = plans.filter((plan) => plan.selfServe).map((plan) => goNames[plan.id])
    const listMatch = plansGo.match(/SelfServicePlans[\s\S]*?\{([\s\S]*?)\}/)
    expect(listMatch).not.toBeNull()
    for (const name of selfServe) {
      expect(listMatch![1]).toContain(`tenant.Plan${name}`)
    }
    expect(listMatch![1]).not.toContain('tenant.PlanEnterprise')
  })
})
