import type { WidgetLocalization, WidgetRule } from './types'

export interface WidgetSystemCopy {
  close: string
  feedback: string
  sending: string
  sendError: string
  attachmentError: string
  emailPlaceholder: string
  invalidEmail: string
  emailRequired: string
  required: string
}

export interface ResolvedWidgetLocalization {
  locale: string
  content: WidgetLocalization
  system: WidgetSystemCopy
}

const systemCopy: Record<string, WidgetSystemCopy> = {
  en: {
    close: 'Close feedback', feedback: 'Feedback', sending: 'Sending...',
    sendError: 'Could not send feedback. Please try again.',
    attachmentError: 'One or more attachments could not be uploaded.',
    emailPlaceholder: 'you@example.com', invalidEmail: 'Enter a valid email address.',
    emailRequired: 'Enter an email address to receive updates.',
    required: 'Please complete the required response.',
  },
  pt: {
    close: 'Fechar feedback', feedback: 'Feedback', sending: 'A enviar...',
    sendError: 'Não foi possível enviar o feedback. Tenta novamente.',
    attachmentError: 'Não foi possível carregar um ou mais anexos.',
    emailPlaceholder: 'voce@exemplo.com', invalidEmail: 'Introduz um endereço de email válido.',
    emailRequired: 'Introduz um endereço de email para receber atualizações.',
    required: 'Preenche a resposta obrigatória.',
  },
}

export function resolveWidgetLocalization(
  rule: WidgetRule,
  projectDefault = 'en',
  preferredLocales: readonly string[] = [],
): ResolvedWidgetLocalization {
  const entries = Object.entries(rule.localizations ?? {})
  const locales = new Map(entries.map(([locale, content]) => [normalizeLocale(locale), { locale, content }]))
  const fallback = findLocale(locales, [projectDefault]) ?? entries[0]
  const selected = findLocale(locales, preferredLocales) ?? fallback
  const locale = selected?.[0] ?? (normalizeLocale(projectDefault) || 'en')
  const content = selected?.[1] ?? legacyLocalization(rule)
  const language = primaryLanguage(locale)
  return { locale, content, system: systemCopy[language] ?? systemCopy.en! }
}

function findLocale(
  locales: Map<string, { locale: string; content: WidgetLocalization }>,
  requested: readonly string[],
): [string, WidgetLocalization] | null {
  for (const candidate of requested) {
    const normalized = normalizeLocale(candidate)
    if (!normalized) continue
    const exact = locales.get(normalized)
    if (exact) return [exact.locale, exact.content]
    const primary = primaryLanguage(normalized)
    const generic = locales.get(primary)
    if (generic) return [generic.locale, generic.content]
  }
  return null
}

function legacyLocalization(rule: WidgetRule): WidgetLocalization {
  return {
    launcher_label: 'Feedback',
    prompt: rule.greeting ?? 'How was your experience?',
    comment_placeholder: 'Tell us more',
    contact_label: 'Email address',
    consent_label: 'Email me about this feedback',
    submit_label: 'Send feedback',
    success_message: 'Your feedback was sent.',
  }
}

function normalizeLocale(locale: string): string {
  return locale.trim().replace(/_/g, '-').toLowerCase()
}

function primaryLanguage(locale: string): string {
  return normalizeLocale(locale).split('-', 1)[0] ?? 'en'
}
