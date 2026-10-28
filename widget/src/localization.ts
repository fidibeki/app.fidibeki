import type { AIInputCategory, WidgetLocalization, WidgetRule } from './types'

export interface WidgetSystemCopy {
  close: string
  formLabel: string
  feedback: string
  feedbackPlaceholder: string
  rating: string
  ratingOutOfFive: string
  attachElement: string
  elementAttached: string
  sending: string
  sendError: string
  attachmentError: string
  emailPlaceholder: string
  invalidEmail: string
  emailRequired: string
  consentNotice: string
  controllerNotice: string
  lawfulBasis: string
  retention: string
  privacyContact: string
  rightsContact: string
  privacyPolicy: string
  managedBy: string
  privacyChoices: string
  closePrivacyChoices: string
  consentRetention: string
  withdrawn: string
  withdrawError: string
  aiDisclosure: string
  aiScope: string
  aiInputNames: Record<AIInputCategory, string>
  required: string
  retry: string
  retryInSecond: string
  retryInSeconds: string
  submissionRateLimited: string
  uploadRateLimited: string
  replayRateLimited: string
  spamRejected: string
  planQuotaExhausted: string
  uploadExpired: string
  uploadInvalid: string
  uploadUnsafe: string
  providerUnavailable: string
}

export interface ResolvedWidgetLocalization {
  locale: string
  content: WidgetLocalization
  system: WidgetSystemCopy
}

const systemCopy: Record<string, WidgetSystemCopy> = {
  en: {
    close: 'Close feedback', formLabel: 'Feedback form', feedback: 'Feedback',
    feedbackPlaceholder: 'Tell us what worked or what got in the way',
    rating: 'Rating', ratingOutOfFive: '{value} out of 5',
    attachElement: 'Attach page element', elementAttached: 'Element attached',
    sending: 'Sending...',
    sendError: 'Could not send feedback. Please try again.',
    attachmentError: 'One or more attachments could not be uploaded.',
    emailPlaceholder: 'you@example.com', invalidEmail: 'Enter a valid email address.',
    emailRequired: 'Enter an email address to receive updates.',
    consentNotice: "We'll only use your email for updates about this feedback. Unsubscribe anytime.",
    controllerNotice: 'How {controller} uses your data',
    lawfulBasis: 'Legal basis', retention: 'Retention',
    privacyContact: 'Privacy contact', rightsContact: 'Rights contact',
    privacyPolicy: 'Privacy policy',
    managedBy: 'Managed by',
    privacyChoices: 'Privacy choices',
    closePrivacyChoices: 'Close privacy choices',
    consentRetention: 'Retained for up to {days} days.',
    withdrawn: 'Consent withdrawn.',
    withdrawError: 'Your choice could not be saved. Please try again.',
    aiDisclosure: 'Selected feedback content may be processed by {provider} for advisory analysis.',
    aiScope: 'Content types: {categories}.',
    aiInputNames: {
      feedback_text: 'written feedback', screenshot: 'screenshots',
      audio: 'audio recordings', video: 'video recordings',
    },
    required: 'Please complete the required response.',
    retry: 'Retry',
    retryInSecond: 'Try again in {seconds} second.',
    retryInSeconds: 'Try again in {seconds} seconds.',
    submissionRateLimited: 'Too many feedback attempts.',
    uploadRateLimited: 'Attachments are temporarily busy.',
    replayRateLimited: 'Session recording is temporarily busy.',
    spamRejected: 'This feedback could not be accepted.',
    planQuotaExhausted: 'This workspace has reached its feedback limit.',
    uploadExpired: 'The attachment upload expired.',
    uploadInvalid: 'An attachment could not be validated.',
    uploadUnsafe: 'An attachment could not be accepted.',
    providerUnavailable: 'Processing is temporarily unavailable.',
  },
  pt: {
    close: 'Fechar feedback', formLabel: 'Formulário de feedback', feedback: 'Feedback',
    feedbackPlaceholder: 'Conta-nos o que funcionou ou o que dificultou a experiência',
    rating: 'Classificação', ratingOutOfFive: '{value} de 5',
    attachElement: 'Anexar elemento da página', elementAttached: 'Elemento anexado',
    sending: 'A enviar...',
    sendError: 'Não foi possível enviar o feedback. Tenta novamente.',
    attachmentError: 'Não foi possível carregar um ou mais anexos.',
    emailPlaceholder: 'voce@exemplo.com', invalidEmail: 'Introduz um endereço de email válido.',
    emailRequired: 'Introduz um endereço de email para receber atualizações.',
    consentNotice: 'Utilizamos o teu email apenas para atualizações sobre este feedback. Cancela quando quiseres.',
    controllerNotice: 'Como {controller} utiliza os teus dados',
    lawfulBasis: 'Base legal', retention: 'Retenção',
    privacyContact: 'Contacto de privacidade', rightsContact: 'Direitos e pedidos',
    privacyPolicy: 'Política de privacidade',
    managedBy: 'Gerido por',
    privacyChoices: 'Escolhas de privacidade',
    closePrivacyChoices: 'Fechar escolhas de privacidade',
    consentRetention: 'Conservado até {days} dias.',
    withdrawn: 'Consentimento retirado.',
    withdrawError: 'Não foi possível guardar a escolha. Tenta novamente.',
    aiDisclosure: 'O conteúdo de feedback selecionado pode ser processado pelo {provider} para análise consultiva.',
    aiScope: 'Tipos de conteúdo: {categories}.',
    aiInputNames: {
      feedback_text: 'feedback escrito', screenshot: 'capturas de ecrã',
      audio: 'gravações de áudio', video: 'gravações de vídeo',
    },
    required: 'Preenche a resposta obrigatória.',
    retry: 'Tentar novamente',
    retryInSecond: 'Tenta novamente dentro de {seconds} segundo.',
    retryInSeconds: 'Tenta novamente dentro de {seconds} segundos.',
    submissionRateLimited: 'Foram feitas demasiadas tentativas de feedback.',
    uploadRateLimited: 'Os anexos estão temporariamente indisponíveis.',
    replayRateLimited: 'A gravação da sessão está temporariamente indisponível.',
    spamRejected: 'Não foi possível aceitar este feedback.',
    planQuotaExhausted: 'Este espaço de trabalho atingiu o limite de feedback.',
    uploadExpired: 'O envio do anexo expirou.',
    uploadInvalid: 'Não foi possível validar um anexo.',
    uploadUnsafe: 'Não foi possível aceitar um anexo.',
    providerUnavailable: 'O processamento está temporariamente indisponível.',
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
  const locale = selected?.[0] ?? compatibleSystemLocale(preferredLocales, projectDefault)
  const content = selected?.[1] ?? legacyLocalization(rule)
  const language = primaryLanguage(locale)
  return { locale, content, system: systemCopy[language] ?? systemCopy.en! }
}

export function resolveWidgetLocalizationForPage(
  rule: WidgetRule,
  projectDefault = 'en',
  pageLanguage: string | null = null,
  browserLocales: readonly string[] = [],
): ResolvedWidgetLocalization {
  const declaredLanguage = normalizePageLanguage(pageLanguage)
  return resolveWidgetLocalization(
    rule,
    projectDefault,
    declaredLanguage ? [declaredLanguage] : browserLocales,
  )
}

export function normalizePageLanguage(locale: string | null | undefined): string | null {
  const candidate = locale?.trim()
  if (!candidate) return null
  try {
    return Intl.getCanonicalLocales(candidate)[0]?.toLowerCase() ?? null
  } catch {
    return null
  }
}

function compatibleSystemLocale(requested: readonly string[], projectDefault: string): string {
  for (const candidate of [...requested, projectDefault]) {
    const normalized = normalizeLocale(candidate)
    if (normalized && systemCopy[primaryLanguage(normalized)]) return normalized
  }
  return 'en'
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
    consent_label: 'Email me updates about this feedback',
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
