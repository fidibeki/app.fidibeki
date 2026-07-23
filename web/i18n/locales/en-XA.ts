import { pseudoLocalizeCatalog } from '../../utils/i18n-quality'
import { englishMessages } from './en'

export default defineI18nLocale(() => pseudoLocalizeCatalog(englishMessages))
