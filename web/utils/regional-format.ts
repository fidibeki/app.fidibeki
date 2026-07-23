import {
  normalizeSupportedLocale,
  normalizeSupportedTimezone,
  type SupportedLocale,
} from '~/utils/regional'

export interface RegionalFormatterOptions {
  locale?: string | null
  timezone?: string | null
  placeholder?: string
}

export interface RegionalFormatters {
  locale: SupportedLocale
  timezone: string
  date: (value: string | number | Date, options?: Intl.DateTimeFormatOptions) => string
  dateTime: (value: string | number | Date, options?: Intl.DateTimeFormatOptions) => string
  integer: (value: number, options?: Intl.NumberFormatOptions) => string
  decimal: (value: number, options?: Intl.NumberFormatOptions) => string
  currency: (value: number, currency: string, options?: Intl.NumberFormatOptions) => string
  percent: (value: number, options?: Intl.NumberFormatOptions) => string
  relativeTime: (
    value: number,
    unit: Intl.RelativeTimeFormatUnit,
    options?: Intl.RelativeTimeFormatOptions,
  ) => string
  list: (values: readonly string[], options?: Intl.ListFormatOptions) => string
  pluralCategory: (
    value: number,
    options?: Intl.PluralRulesOptions,
  ) => Intl.LDMLPluralRule
}

const dateTimeFormatters = new Map<string, Intl.DateTimeFormat>()
const numberFormatters = new Map<string, Intl.NumberFormat>()
const relativeTimeFormatters = new Map<string, Intl.RelativeTimeFormat>()
const listFormatters = new Map<string, Intl.ListFormat>()
const pluralRules = new Map<string, Intl.PluralRules>()

function cacheKey(locale: string, timezone: string, options: object) {
  return `${locale}|${timezone}|${JSON.stringify(options)}`
}

function cachedDateTime(
  locale: string,
  timezone: string,
  options: Intl.DateTimeFormatOptions,
) {
  const resolvedOptions = { ...options, timeZone: timezone }
  const key = cacheKey(locale, timezone, resolvedOptions)
  let formatter = dateTimeFormatters.get(key)
  if (!formatter) {
    formatter = new Intl.DateTimeFormat(locale, resolvedOptions)
    dateTimeFormatters.set(key, formatter)
  }
  return formatter
}

function cachedNumber(locale: string, options: Intl.NumberFormatOptions) {
  const key = cacheKey(locale, '', options)
  let formatter = numberFormatters.get(key)
  if (!formatter) {
    formatter = new Intl.NumberFormat(locale, options)
    numberFormatters.set(key, formatter)
  }
  return formatter
}

function cachedRelativeTime(locale: string, options: Intl.RelativeTimeFormatOptions) {
  const key = cacheKey(locale, '', options)
  let formatter = relativeTimeFormatters.get(key)
  if (!formatter) {
    formatter = new Intl.RelativeTimeFormat(locale, options)
    relativeTimeFormatters.set(key, formatter)
  }
  return formatter
}

function cachedList(locale: string, options: Intl.ListFormatOptions) {
  const key = cacheKey(locale, '', options)
  let formatter = listFormatters.get(key)
  if (!formatter) {
    formatter = new Intl.ListFormat(locale, options)
    listFormatters.set(key, formatter)
  }
  return formatter
}

function cachedPluralRules(locale: string, options: Intl.PluralRulesOptions) {
  const key = cacheKey(locale, '', options)
  let formatter = pluralRules.get(key)
  if (!formatter) {
    formatter = new Intl.PluralRules(locale, options)
    pluralRules.set(key, formatter)
  }
  return formatter
}

function validDate(value: string | number | Date) {
  const date = value instanceof Date ? value : new Date(value)
  return Number.isNaN(date.getTime()) ? null : date
}

export function createRegionalFormatters(
  options: RegionalFormatterOptions = {},
): RegionalFormatters {
  const locale = normalizeSupportedLocale(options.locale) || 'en'
  const timezone = normalizeSupportedTimezone(options.timezone) || 'UTC'
  const placeholder = options.placeholder || '—'

  return {
    locale,
    timezone,
    date(value, formatOptions = {}) {
      const date = validDate(value)
      if (!date) return placeholder
      return cachedDateTime(locale, timezone, {
        dateStyle: 'medium',
        ...formatOptions,
      }).format(date)
    },
    dateTime(value, formatOptions = {}) {
      const date = validDate(value)
      if (!date) return placeholder
      return cachedDateTime(locale, timezone, {
        dateStyle: 'medium',
        timeStyle: 'short',
        ...formatOptions,
      }).format(date)
    },
    integer(value, formatOptions = {}) {
      return cachedNumber(locale, {
        ...formatOptions,
        maximumFractionDigits: 0,
      }).format(value)
    },
    decimal(value, formatOptions = {}) {
      return cachedNumber(locale, {
        maximumFractionDigits: 2,
        ...formatOptions,
      }).format(value)
    },
    currency(value, currency, formatOptions = {}) {
      return cachedNumber(locale, {
        style: 'currency',
        currency,
        ...formatOptions,
      }).format(value)
    },
    percent(value, formatOptions = {}) {
      return cachedNumber(locale, {
        style: 'percent',
        maximumFractionDigits: 1,
        ...formatOptions,
      }).format(value)
    },
    relativeTime(value, unit, formatOptions = {}) {
      return cachedRelativeTime(locale, {
        numeric: 'auto',
        ...formatOptions,
      }).format(value, unit)
    },
    list(values, formatOptions = {}) {
      return cachedList(locale, {
        style: 'long',
        type: 'conjunction',
        ...formatOptions,
      }).format([...values])
    },
    pluralCategory(value, formatOptions = {}) {
      return cachedPluralRules(locale, formatOptions).select(value)
    },
  }
}
