import { createRegionalFormatters } from '~/utils/regional-format'

export function useRegionalFormat() {
  const nuxtApp = useNuxtApp()
  const { effectiveLocale, timezone } = useRegionalContext()
  const formatter = computed(() => createRegionalFormatters({
    locale: effectiveLocale.value,
    timezone: timezone.value,
  }))

  return {
    formatter: readonly(formatter),
    date: (value: string | number | Date, options?: Intl.DateTimeFormatOptions) =>
      formatter.value.date(value, options),
    dateTime: (value: string | number | Date, options?: Intl.DateTimeFormatOptions) =>
      formatter.value.dateTime(value, options),
    integer: (value: number, options?: Intl.NumberFormatOptions) =>
      formatter.value.integer(value, options),
    decimal: (value: number, options?: Intl.NumberFormatOptions) =>
      formatter.value.decimal(value, options),
    currency: (value: number, currency: string, options?: Intl.NumberFormatOptions) =>
      formatter.value.currency(value, currency, options),
    percent: (value: number, options?: Intl.NumberFormatOptions) =>
      formatter.value.percent(value, options),
    relativeTime: (
      value: number,
      unit: Intl.RelativeTimeFormatUnit,
      options?: Intl.RelativeTimeFormatOptions,
    ) => formatter.value.relativeTime(value, unit, options),
    list: (values: readonly string[], options?: Intl.ListFormatOptions) =>
      formatter.value.list(values, options),
    plural: (
      key: string,
      count: number,
      parameters: Record<string, unknown> = {},
    ) => nuxtApp.$i18n.t(key, { ...parameters, count }, count),
  }
}
