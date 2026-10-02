import { messageKey } from '~/utils/locale'

export function useMarketingI18n() {
  const { locale, t, te } = useI18n()
  function copy(
    value: unknown,
    values: Record<string, string | number> = {},
  ): string {
    if (value == null) return ''
    if (typeof value === 'number')
      return new Intl.NumberFormat(
        locale.value === 'pt' ? 'pt-PT' : locale.value,
      ).format(value)
    const source = String(value)
    const key = messageKey(source)
    if (te(key)) return t(key, values)
    if (/^\$\d+(?:\.\d+)?$/.test(source) && locale.value !== 'en') {
      return new Intl.NumberFormat(
        locale.value === 'pt' ? 'pt-PT' : locale.value,
        {
          style: 'currency',
          currency: 'USD',
          minimumFractionDigits: 2,
        },
      ).format(Number(source.slice(1)))
    }
    return source
  }
  return { copy, locale }
}
