import { expect, test } from '@playwright/test'

test('quick start shows the full snippet and copies usable HTML', async ({
  page,
  context,
}) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  await page.goto('/docs')
  const example = page.getByRole('region', { name: 'Code example' })
  await expect(example).toContainText('data-fidibeki-key="pk_live_..."')
  const size = await example.evaluate((element) => ({
    width: element.clientWidth,
    content: element.scrollWidth,
  }))
  expect(size.content).toBeLessThanOrEqual(size.width + 1)
  await page.getByRole('button', { name: 'Copy code' }).click()
  await expect(
    page.getByRole('button', { name: 'Copied', exact: true }),
  ).toBeVisible()
  const copied = await page.evaluate(() => navigator.clipboard.readText())
  expect(copied).toBe(await example.textContent())
  expect(copied).toContain('src="https://widget.fidibeki.com/v1/loader.js"')
  expect(copied).toContain('</script>')
  await page.getByRole('link', { name: 'Read the installation guide' }).click()
  await expect(page).toHaveURL(/\/docs\/installation$/)
})

test('quick start offers manual copying if clipboard access fails', async ({
  page,
}) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', {
      value: {
        writeText: async () => {
          throw new DOMException('Permission denied', 'NotAllowedError')
        },
      },
    })
  })
  await page.goto('/docs')
  await page.getByRole('button', { name: 'Copy code' }).click()
  await expect(page.getByRole('status')).toHaveText(
    "Couldn't copy. Select and copy the code above.",
  )
  await expect(page.getByRole('status')).toBeVisible()
  await expect(
    page.getByRole('region', { name: 'Code example' }),
  ).toContainText('data-fidibeki-key="pk_live_..."')
})

test('doc contents jump to the heading and related guides navigate correctly', async ({
  page,
  isMobile,
}) => {
  await page.goto('/docs/installation')
  const contents = isMobile
    ? page.getByRole('navigation', { name: 'Article sections' })
    : page.getByRole('navigation', { name: 'On this page', exact: true })
  if (isMobile) await page.locator('.mobile-contents summary').click()
  await contents.getByRole('link', { name: '3. Embed the loader' }).click()
  await expect(page).toHaveURL(/#3-embed-the-loader$/)
  const heading = page.getByRole('heading', { name: '3. Embed the loader' })
  await expect(heading).toBeInViewport()
  await expect
    .poll(async () => (await heading.boundingBox())?.y ?? 0)
    .toBeGreaterThan(72)
  await page
    .getByRole('link', { name: 'Configure widget rules', exact: true })
    .click()
  await expect(page.locator('h1')).toHaveText('Configure widget rules')
  await expect(page).toHaveURL(/\/docs\/widget-rules$/)
})

test('integration examples retain their correct plan and destination', async ({
  page,
}) => {
  await page.goto('/integrations')
  const example = page.getByRole('region', {
    name: 'Integration example',
    exact: true,
  })
  await example.getByRole('button', { name: 'Jira', exact: true }).click()
  await expect(
    example.getByRole('button', { name: 'Jira', exact: true }),
  ).toHaveAttribute('aria-pressed', 'true')
  await expect(
    example.getByRole('region', { name: 'Jira example', exact: true }),
  ).toBeVisible()
  await expect(example.locator('.connection-caption')).toContainText(
    'All plans',
  )
  await expect(example.locator('.connection-caption a')).toHaveAttribute(
    'href',
    '/integrations/jira',
  )
})

test('pricing comparison remains reachable on narrow screens', async ({
  page,
}) => {
  await page.goto('/pricing')
  await expect(
    page.getByRole('heading', { name: 'Starter', exact: true }),
  ).toBeVisible()
  const comparison = page.getByRole('region', { name: /Plan comparison/ })
  await comparison.scrollIntoViewIfNeeded()
  await expect(
    comparison.getByRole('columnheader', { name: 'Enterprise', exact: true }),
  ).toBeAttached()
  await comparison.evaluate((element) => {
    element.scrollLeft = element.scrollWidth
  })
  await expect(
    comparison.getByRole('columnheader', { name: 'Enterprise', exact: true }),
  ).toBeInViewport()
})

test('pricing questions open one at a time with keyboard access', async ({
  page,
}) => {
  await page.goto('/pricing')
  const section = page.getByRole('region', { name: 'Pricing questions' })
  const trial = section
    .locator('details')
    .filter({ hasText: 'How does the free trial work?' })
  const yearly = section
    .locator('details')
    .filter({ hasText: 'Is yearly billing available?' })
  await expect(trial).toHaveAttribute('open', '')
  await expect(yearly).not.toHaveAttribute('open', '')
  const toggle = yearly.locator('summary')
  await toggle.focus()
  await toggle.press('Enter')
  await expect(yearly).toHaveAttribute('open', '')
  await expect(trial).not.toHaveAttribute('open', '')
  await expect(yearly.locator('.faq-answer')).toBeVisible()
  await expect(yearly).toContainText(
    '7% on Starter and 14% on Growth and Scale',
  )
  await toggle.press('Space')
  await expect(yearly).not.toHaveAttribute('open', '')
})
