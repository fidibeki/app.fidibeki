import {
  localizedAPIError,
  localizedAPIFieldErrors,
  type APIErrorTranslator,
} from '~/utils/safe-api-error'

export function useLocalizedAPIError() {
  const { $i18n } = useNuxtApp()
  const translate: APIErrorTranslator = (key, parameters = {}) =>
    $i18n.t(key, parameters)

  return {
    message: (error: unknown, fallbackKey = 'errors.generic') =>
      localizedAPIError(error, translate, fallbackKey),
    fields: (error: unknown) => localizedAPIFieldErrors(error, translate),
  }
}
