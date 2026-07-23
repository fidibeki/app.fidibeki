import { siteName } from '~/data/site'

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

export function useSiteMeta(options: SiteMetaOptions) {
  const config = useRuntimeConfig()
  const siteUrl = config.public.siteUrl.replace(/\/$/, '')
  const canonical = options.path === '/' ? `${siteUrl}/` : `${siteUrl}${options.path}`
  const fullTitle =
    options.path === '/' ? `${siteName} — ${options.title}` : `${options.title} · ${siteName}`

  const jsonLd: Record<string, unknown>[] = [...(options.structuredData ?? [])]

  if (options.breadcrumbs?.length) {
    jsonLd.push({
      '@context': 'https://schema.org',
      '@type': 'BreadcrumbList',
      itemListElement: options.breadcrumbs.map((item, index) => ({
        '@type': 'ListItem',
        position: index + 1,
        name: item.name,
        item: `${siteUrl}${item.path === '/' ? '/' : item.path}`,
      })),
    })
  }

  useHead({
    title: fullTitle,
    link: [{ rel: 'canonical', href: canonical }],
    meta: [
      { name: 'description', content: options.description },
      { property: 'og:title', content: fullTitle },
      { property: 'og:description', content: options.description },
      { property: 'og:url', content: canonical },
      { property: 'og:type', content: 'website' },
      { property: 'og:site_name', content: siteName },
      { property: 'og:image', content: `${siteUrl}/og/og-card.png` },
      { name: 'twitter:card', content: 'summary_large_image' },
      { name: 'twitter:title', content: fullTitle },
      { name: 'twitter:description', content: options.description },
      { name: 'twitter:image', content: `${siteUrl}/og/og-card.png` },
    ],
    script: jsonLd.map((data) => ({
      type: 'application/ld+json',
      innerHTML: JSON.stringify(data),
    })),
  })
}
