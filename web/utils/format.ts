const productLocale = 'en-US'
const productTimeZone = 'UTC'

export function formatEuro(cents: number | null | undefined): string {
  if (cents == null) return 'Managed contract'
  return new Intl.NumberFormat(productLocale, {
    style: 'currency',
    currency: 'EUR',
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  }).format(cents / 100)
}

export function formatMonthlyEuro(cents: number | null | undefined): string {
  const price = formatEuro(cents)
  return cents == null ? price : `${price}/month`
}

export function formatWholeNumber(value: number): string {
  return new Intl.NumberFormat(productLocale, { maximumFractionDigits: 0 }).format(value)
}

export function formatDate(value: string | number | Date): string {
  return new Intl.DateTimeFormat(productLocale, {
    dateStyle: 'medium',
    timeZone: productTimeZone,
  }).format(new Date(value))
}

export function formatDateTime(value: string | number | Date): string {
  return new Intl.DateTimeFormat(productLocale, {
    dateStyle: 'medium',
    timeStyle: 'short',
    timeZone: productTimeZone,
  }).format(new Date(value))
}
