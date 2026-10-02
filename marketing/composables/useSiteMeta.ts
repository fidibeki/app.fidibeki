import { siteName } from '~/data/site'
import { localizedPath, markdownPath, marketingLocales } from '~/utils/locale'

interface BreadcrumbItem {
  name: string
  path: string
}

interface SiteMetaOptions {
  title: string
  description: string
  path: string
  breadcrumbs?: BreadcrumbItem[]
  /** Additional JSON-LD objects to embed on this page. */
  structuredData?: Record<string, unknown>[]
}

export function useSiteMeta(source: SiteMetaOptions | (() => SiteMetaOptions)) {
  const { copy, locale } = useMarketingI18n()
  const config = useRuntimeConfig()
  const siteUrl = config.public.siteUrl.replace(/\/$/, '')
  useHead(() => {
    const options = typeof source === 'function' ? source() : source
    const pagePath = localizedPath(options.path, locale.value)
    const canonical = `${siteUrl}${pagePath}`
    const fullTitle =
      options.path === '/'
        ? `${siteName} — ${copy(options.title)}`
        : `${copy(options.title)} · ${siteName}`

    const language = marketingLocales.find(
      (entry) => entry.code === locale.value,
    )!.language
    function localizeSchema(value: unknown, key = ''): unknown {
      if (Array.isArray(value))
        return value.map((entry) => localizeSchema(entry))
      if (value && typeof value === 'object')
        return Object.fromEntries(
          Object.entries(value).map(([field, entry]) => [
            field,
            localizeSchema(entry, field),
          ]),
        )
      if (typeof value !== 'string') return value
      if (['name', 'headline', 'description'].includes(key)) return copy(value)
      if (key === 'url' && value.startsWith(`${siteUrl}/`)) {
        return `${siteUrl}${localizedPath(value.slice(siteUrl.length), locale.value)}`
      }
      return value
    }
    const jsonLd: Record<string, unknown>[] = (
      options.structuredData ?? []
    ).map((data) => ({
      ...(localizeSchema(data) as Record<string, unknown>),
      inLanguage: language,
    }))

    if (options.breadcrumbs?.length) {
      jsonLd.push({
        '@context': 'https://schema.org',
        '@type': 'BreadcrumbList',
        itemListElement: options.breadcrumbs.map((item, index) => ({
          '@type': 'ListItem',
          position: index + 1,
          name: copy(item.name),
          item: `${siteUrl}${localizedPath(item.path, locale.value)}`,
        })),
      })
    }

    return {
      title: fullTitle,
      link: [
        { rel: 'canonical', href: canonical },
        {
          rel: 'alternate',
          type: 'text/markdown',
          href: `${siteUrl}${markdownPath(pagePath)}`,
        },
        { rel: 'describedby', type: 'text/plain', href: `${siteUrl}/llms.txt` },
      ],
      meta: [
        { name: 'description', content: copy(options.description) },
        { property: 'og:title', content: fullTitle },
        { property: 'og:description', content: copy(options.description) },
        { property: 'og:url', content: canonical },
        { property: 'og:type', content: 'website' },
        { property: 'og:site_name', content: siteName },
        { property: 'og:image', content: `${siteUrl}/og/og-card.png` },
        { property: 'og:image:width', content: '1200' },
        { property: 'og:image:height', content: '630' },
        {
          property: 'og:image:alt',
          content: copy('Fidibeki: website feedback and conversations'),
        },
        { name: 'twitter:card', content: 'summary_large_image' },
        { name: 'twitter:title', content: fullTitle },
        { name: 'twitter:description', content: copy(options.description) },
        { name: 'twitter:image', content: `${siteUrl}/og/og-card.png` },
        {
          name: 'twitter:image:alt',
          content: copy('Fidibeki: website feedback and conversations'),
        },
      ],
      script: jsonLd.map((data) => ({
        type: 'application/ld+json',
        innerHTML: JSON.stringify(data),
      })),
    }
  })
}
