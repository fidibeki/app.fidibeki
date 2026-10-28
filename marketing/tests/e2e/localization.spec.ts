import { expect, test } from '@playwright/test'

const pricingLocales = [
  {
    path: '',
    lang: 'en',
    project: '1 project',
    projects: '4 projects',
    member: '1 team member',
    members: '3 team members',
    retention: '30 days of stored replays & attachments',
    days: '30 days',
    feedback: '2,500 feedback / month',
  },
  {
    path: '/pt',
    lang: 'pt-PT',
    project: '1 projeto',
    projects: '4 projetos',
    member: '1 membro da equipa',
    members: '3 membros da equipa',
    retention: 'Gravações e anexos guardados durante 30 dias',
    days: '30 dias',
    feedback: '2500 respostas / mês',
  },
  {
    path: '/es',
    lang: 'es',
    project: '1 proyecto',
    projects: '4 proyectos',
    member: '1 miembro del equipo',
    members: '3 miembros del equipo',
    retention: 'Grabaciones y archivos adjuntos guardados durante 30 días',
    days: '30 días',
    feedback: '2500 comentarios / mes',
  },
  {
    path: '/fr',
    lang: 'fr',
    project: '1 projet',
    projects: '4 projets',
    member: "1 membre de l'équipe",
    members: "3 membres de l'équipe",
    retention: 'Enregistrements et pièces jointes conservés pendant 30 jours',
    days: '30 jours',
    feedback: '2 500 retours / mois',
  },
]

for (const locale of pricingLocales) {
  test(`pricing limits use complete localized sentences: ${locale.lang}`, async ({
    page,
  }) => {
    await page.goto(`${locale.path}/pricing`)
    await expect(page.locator('html')).toHaveAttribute('lang', locale.lang)
    const starter = page.locator('.plan-card').nth(0)
    const growth = page.locator('.plan-card').nth(1)
    await expect(starter.locator('.plan-limits li')).toHaveText([
      locale.project,
      locale.lang === 'en'
        ? '500 feedback / month'
        : locale.lang === 'pt-PT'
          ? '500 respostas / mês'
          : locale.lang === 'es'
            ? '500 comentarios / mes'
            : '500 retours / mois',
      locale.member,
      locale.retention,
    ])
    await expect(growth.locator('.plan-limits li').nth(0)).toHaveText(
      locale.projects,
    )
    await expect(growth.locator('.plan-limits li').nth(1)).toHaveText(
      locale.feedback,
    )
    await expect(growth.locator('.plan-limits li').nth(2)).toHaveText(
      locale.members,
    )
    await expect(
      page.locator('.comparison-table tbody tr').nth(0).locator('td').nth(0),
    ).toHaveText(locale.project)
    await expect(
      page.locator('.comparison-table tbody tr').nth(3).locator('td').nth(0),
    ).toHaveText(locale.days)
    const limits = await page
      .locator('.plan-limits')
      .evaluateAll((nodes) =>
        nodes.map((node) => node.getBoundingClientRect().top),
      )
    expect(Math.max(...limits) - Math.min(...limits)).toBeLessThan(2)
    if (locale.lang !== 'en') {
      await expect(page.locator('h1')).not.toContainText('free trial')
      await expect(page.locator('.plans-note')).not.toContainText('free trial')
      await expect(page.locator('.faq-answer').first()).not.toContainText(
        'Every new workspace',
      )
      await expect(page.locator('.plan-grid')).not.toContainText(
        /\b(?:Unlimited|days|projects|members|month)\b/,
      )
    }
  })
}

test('one click updates the current section on localized routes', async ({
  page,
}) => {
  await page.goto('/pt/pricing')
  const nav = page.locator('.home-nav')
  await nav.locator('a[href="/pt/features"]').click()
  await expect(page).toHaveURL(/\/pt\/features$/)
  await expect(nav.locator('a[href="/pt/features"]')).toHaveAttribute(
    'aria-current',
    'page',
  )
  await expect(nav.locator('a[href="/pt/pricing"]')).not.toHaveAttribute(
    'aria-current',
  )
  await page.goBack()
  await expect(page).toHaveURL(/\/pt\/pricing$/)
  await expect(nav.locator('a[href="/pt/pricing"]')).toHaveAttribute(
    'aria-current',
    'page',
  )
  await page.goForward()
  await expect(nav.locator('a[href="/pt/features"]')).toHaveAttribute(
    'aria-current',
    'page',
  )
})

test('language selector preserves the page, query, hash and selection on reload', async ({
  page,
}) => {
  await page.goto('/docs/installation?ref=language-test#allowed-domains')
  await page.locator('.header-language button').click()
  await page.locator('.header-language a[lang="fr"]').click()
  await expect(page).toHaveURL(
    /\/fr\/docs\/installation\?ref=language-test#allowed-domains$/,
  )
  await expect(page.locator('html')).toHaveAttribute('lang', 'fr')
  await expect(page.locator('.header-language button')).toContainText('FR')
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('lang', 'fr')
  expect(
    (await page.context().cookies()).find(
      (cookie) => cookie.name === 'fidibeki-locale',
    )?.value,
  ).toBe('fr')
})

for (const locale of ['pt', 'es', 'fr']) {
  test(`${locale} layouts fit mobile, tablet and desktop`, async ({ page }) => {
    for (const width of [320, 390, 834, 1024, 1440]) {
      await page.setViewportSize({ width, height: 1000 })
      for (const route of ['', '/pricing', '/docs', '/features']) {
        await page.goto(`/${locale}${route}`)
        const dimensions = await page.evaluate(() => ({
          viewport: document.documentElement.clientWidth,
          page: document.documentElement.scrollWidth,
        }))
        expect(
          dimensions.page,
          `${locale}${route} at ${width}`,
        ).toBeLessThanOrEqual(dimensions.viewport + 1)
        if (route === '' && width >= 834) {
          const cards = await page
            .locator('.home-editorial-pair .home-editorial > a')
            .evaluateAll((nodes) =>
              nodes.map((node) => node.getBoundingClientRect().height),
            )
          expect(cards).toHaveLength(2)
          expect(Math.abs(cards[0]! - cards[1]!)).toBeLessThan(2)
        }
      }
    }
  })
}

test('switching language updates page metadata and pricing content in the same navigation', async ({
  page,
}) => {
  await page.goto('/fr/pricing')
  const frenchDescription = await page
    .locator('meta[name="description"]')
    .getAttribute('content')
  for (const [lang, prefix, title, question] of [
    ['en', '', 'Pricing', 'Every new workspace'],
    ['pt-PT', '/pt', 'Preços', 'Cada novo espaço de trabalho'],
  ] as const) {
    await page.locator('.header-language button').click()
    await page.locator(`.header-language a[lang="${lang}"]`).click()
    await expect(page).toHaveURL(new RegExp(`${prefix}/pricing$`))
    await expect(page).toHaveTitle(new RegExp(title))
    await expect(page.locator('link[rel="canonical"]')).toHaveAttribute(
      'href',
      `https://fidibeki.com${prefix}/pricing`,
    )
    await expect(page.locator('meta[name="description"]')).not.toHaveAttribute(
      'content',
      frenchDescription!,
    )
    await expect(page.locator('.faq-answer').first()).toContainText(question)
    await expect
      .poll(async () =>
        page
          .locator('script[type="application/ld+json"]')
          .evaluateAll(
            (nodes) =>
              nodes
                .map((node) => JSON.parse(node.textContent || '{}'))
                .find((data) => data['@type'] === 'SoftwareApplication')?.url,
          ),
      )
      .toBe(`https://fidibeki.com${prefix || '/'}`)
  }
})
