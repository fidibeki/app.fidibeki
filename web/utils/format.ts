import { createRegionalFormatters } from '~/utils/regional-format'

const productFormat = createRegionalFormatters({ locale: 'en', timezone: 'UTC' })

export function formatEuro(cents: number | null | undefined): string {
  if (cents == null) return 'Managed contract'
  return productFormat.currency(cents / 100, 'EUR', {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })
}

export function formatMonthlyEuro(cents: number | null | undefined): string {
  const price = formatEuro(cents)
  return cents == null ? price : `${price}/month`
}

export function formatWholeNumber(value: number): string {
  return productFormat.integer(value)
}

export function formatDate(value: string | number | Date): string {
  return productFormat.date(value)
}

export function formatDateTime(value: string | number | Date): string {
  return productFormat.dateTime(value)
}
