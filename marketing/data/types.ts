export interface DetailSection {
  heading: string
  body: string
  bullets?: string[]
  code?: { language: string; snippet: string }
}

export interface FeatureEntry {
  slug: string
  name: string
  loopStage: 'Capture' | 'Analyze' | 'Decide' | 'Track' | 'Communicate'
  tagline: string
  summary: string
  icon: string
  availability: string
  highlights: string[]
  sections: DetailSection[]
  related: string[]
}

export interface IntegrationEntry {
  slug: string
  name: string
  status: 'available' | 'planned'
  tagline: string
  summary: string
  icon: string
  availability: string
  events?: string[]
  sections: DetailSection[]
}

export interface UseCaseEntry {
  slug: string
  name: string
  audience: string
  tagline: string
  summary: string
  icon: string
  featureSlugs: string[]
  sections: DetailSection[]
}

export interface DocEntry {
  slug: string
  title: string
  description: string
  minutes: number
  sections: DetailSection[]
}

export interface PlanEntry {
  id: 'starter' | 'growth' | 'scale' | 'enterprise'
  name: string
  monthlyPriceCents: number | null
  yearlyPriceCents: number | null
  yearlyDiscountPercent: number | null
  pricingModel: 'per_seat' | 'custom'
  priceUnit: 'seat' | null
  blurb: string
  selfServe: boolean
  limits: {
    projects: number
    feedbackPerMonth: number
    users: number
    storageDays: number
  }
  features: {
    ai: boolean
    testimonials: boolean
    sessionReplay: boolean
    slack: boolean
    video: boolean
    audio: boolean
    domCapture: boolean
    customDomain: boolean
  }
  highlighted?: boolean
}
