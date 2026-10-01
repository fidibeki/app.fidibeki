import { expect, test } from '@playwright/test'

// The walkthrough runs locally. It never sends feedback or starts a real chat.
test('one send explains the whole loop automatically without losing the report', async ({
  page,
}) => {
  const mutations: string[] = []
  page.on('request', (request) => {
    if (['POST', 'PUT', 'PATCH', 'DELETE'].includes(request.method()))
      mutations.push(request.url())
  })
  await page.clock.install()
  await page.goto('/')
  const decline = page.getByRole('button', { name: 'Decline', exact: true })
  if (await decline.isVisible()) await decline.click()
  const input = page.getByRole('textbox', {
    name: 'Try an example feedback report',
  })
  const preview = page.getByRole('button', { name: 'Preview feedback flow' })
  const title = page.getByRole('heading', {
    name: 'What could be better?',
    exact: true,
  })
  const initialTitleY = (await title.boundingBox())!.y
  await expect(preview).toBeDisabled()
  await input.fill('   ')
  await expect(preview).toBeDisabled()
  await page
    .getByRole('button', { name: 'Suggest an idea', exact: true })
    .click()
  await expect(input).toBeFocused()
  await preview.click()
  const journey = page.getByRole('region', { name: 'Your feedback journey' })
  await expect(journey).toBeFocused()
  expect(Math.abs((await title.boundingBox())!.y - initialTitleY)).toBeLessThan(
    2,
  )
  await expect(journey.locator('h2')).toHaveCount(3)
  await expect(journey.locator('blockquote')).toContainText('save my filters')
  await expect(journey).toHaveAttribute('data-step', '0')
  await page.clock.runFor(2300)
  await expect(journey).toHaveAttribute('data-step', '1')
  await page.clock.runFor(5000)
  await expect(journey).toHaveAttribute('data-playback', 'complete')
  await expect(journey.locator('.journey-reply')).toHaveAttribute(
    'aria-hidden',
    'false',
  )
  await expect(journey.locator('.journey-reply')).toContainText(
    'You can now save your filters',
  )
  await expect(journey.locator('blockquote')).toContainText('save my filters')
  await expect(
    page.getByRole('link', { name: 'Try it with your product' }),
  ).toHaveAttribute('href', /\/signup$/)
  await page.getByRole('button', { name: 'Try another example' }).click()
  await expect(input).toBeFocused()
  await expect(input).toHaveValue(
    "I'd love to save my filters so I can pick up where I left off.",
  )
  expect(mutations).toEqual([])
})

test('animation can pause, resume and replay', async ({ page }) => {
  await page.clock.install()
  await page.goto('/')
  const decline = page.getByRole('button', { name: 'Decline', exact: true })
  if (await decline.isVisible()) await decline.click()
  await page.getByRole('button', { name: 'Report a bug', exact: true }).click()
  await page.getByRole('button', { name: 'Preview feedback flow' }).click()
  const journey = page.getByRole('region', { name: 'Your feedback journey' })
  await page.getByRole('button', { name: 'Pause animation' }).click()
  const pausedStep = await journey.getAttribute('data-step')
  await page.clock.runFor(12000)
  await expect(journey).toHaveAttribute('data-step', pausedStep!)
  await expect(journey).toHaveAttribute('data-playback', 'paused')
  await page.getByRole('button', { name: 'Resume animation' }).click()
  await page.clock.runFor(8000)
  await expect(journey).toHaveAttribute('data-playback', 'complete')
  await expect(
    page.getByRole('button', { name: 'Replay animation' }),
  ).toBeFocused()
  await page.getByRole('button', { name: 'Replay animation' }).click()
  await expect(journey).toHaveAttribute('data-step', '0')
  await expect(journey).toHaveAttribute('data-playback', 'playing')
  await page.getByRole('button', { name: 'Edit your feedback' }).click()
  await page.clock.runFor(12000)
  await expect(journey).toHaveCount(0)
  await expect(page.getByRole('textbox')).toHaveValue(
    "The checkout button isn't working on my phone.",
  )
})

test('reduced motion shows a complete question-and-answer story immediately', async ({
  page,
}) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/')
  const decline = page.getByRole('button', { name: 'Decline', exact: true })
  if (await decline.isVisible()) await decline.click()
  await page
    .getByRole('button', { name: 'Ask a question', exact: true })
    .click()
  await page.getByRole('button', { name: 'Preview feedback flow' }).click()
  const journey = page.getByRole('region', { name: 'Your feedback journey' })
  await expect(journey).toHaveAttribute('data-playback', 'complete')
  await expect(
    journey.getByRole('heading', { name: 'The answer comes back to you.' }),
  ).toBeVisible()
  await expect(journey.locator('.journey-reply')).toContainText(
    'invite your teammates',
  )
  await expect(
    page.getByRole('button', { name: 'Pause animation' }),
  ).toHaveCount(0)
  await expect(journey).not.toContainText('connect the report to an issue')
  expect(
    await journey
      .locator('.journey-reply')
      .evaluate((element) => getComputedStyle(element).transitionDuration),
  ).toBe('0s')
})

test('the animation waits while the demonstration is offscreen', async ({
  page,
}) => {
  await page.clock.install()
  await page.goto('/')
  const decline = page.getByRole('button', { name: 'Decline', exact: true })
  if (await decline.isVisible()) await decline.click()
  await page.getByRole('button', { name: 'Report a bug', exact: true }).click()
  await page.getByRole('button', { name: 'Preview feedback flow' }).click()
  const journey = page.getByRole('region', { name: 'Your feedback journey' })
  await page.evaluate(() =>
    window.scrollTo({ top: document.body.scrollHeight, behavior: 'instant' }),
  )
  await expect(journey).not.toBeInViewport()
  await page.clock.runFor(100)
  const offscreenStep = await journey.getAttribute('data-step')
  await page.clock.runFor(12000)
  await expect(journey).toHaveAttribute('data-step', offscreenStep!)
  await page.evaluate(() => window.scrollTo({ top: 0, behavior: 'instant' }))
  await expect(journey).toBeInViewport()
  await page.clock.runFor(8000)
  await expect(journey).toHaveAttribute('data-playback', 'complete')
})

test('long feedback stays contained and playback controls remain available', async ({
  page,
}) => {
  await page.goto('/')
  const decline = page.getByRole('button', { name: 'Decline', exact: true })
  if (await decline.isVisible()) await decline.click()
  const report = 'a'.repeat(500)
  await page
    .getByRole('textbox', { name: 'Try an example feedback report' })
    .fill(report)
  await page.getByRole('button', { name: 'Preview feedback flow' }).click()
  await expect(page.locator('.journey-message blockquote')).toHaveText(report)
  const overflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  )
  expect(overflow).toBeLessThanOrEqual(1)
  await expect(
    page.getByRole('button', { name: 'Pause animation' }),
  ).toBeInViewport()
})

test('integration previews show the right workflow, destination and plan', async ({
  page,
}) => {
  await page.goto('/')
  const decline = page.getByRole('button', { name: 'Decline', exact: true })
  if (await decline.isVisible()) await decline.click()
  const choices = page.getByRole('group', { name: 'Preview an integration' })
  for (const [label, slug, link, plan] of [
    ['Slack', 'slack', 'Connect Slack', 'Scale & Enterprise'],
    [
      'Teams',
      'microsoft-teams',
      'Connect Microsoft Teams',
      'Scale & Enterprise',
    ],
    ['Jira', 'jira', 'Connect Jira', 'All plans'],
    ['Linear', 'linear', 'Connect Linear', 'All plans'],
    ['Webhooks', 'webhooks', 'Explore webhooks', 'Scale & Enterprise'],
  ]) {
    const button = choices.getByRole('button', { name: label, exact: true })
    await button.focus()
    await page.keyboard.press('Enter')
    await expect(button).toHaveAttribute('aria-pressed', 'true')
    await expect(
      page.getByRole('region', { name: `${label} example`, exact: true }),
    ).toBeVisible()
    await expect(
      page.getByRole('link', { name: link, exact: true }),
    ).toHaveAttribute('href', `/integrations/${slug}`)
    await expect(page.locator('.connection-caption')).toContainText(plan!)
  }
  await expect(page.locator('.connection-event code')).toHaveText(
    'feedback.created',
  )
  const overflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  )
  expect(overflow).toBeLessThanOrEqual(1)
})

test('mobile menu closes with Escape and returns keyboard focus', async ({
  page,
  viewport,
}) => {
  test.skip(!viewport || viewport.width >= 900, 'mobile only')
  await page.goto('/')
  const menu = page.getByRole('button', { name: 'Open menu', exact: true })
  await menu.click()
  await page
    .getByRole('navigation', { name: 'Mobile' })
    .getByRole('link', { name: 'Pricing', exact: true })
    .focus()
  await page.keyboard.press('Escape')
  await expect(page.getByRole('navigation', { name: 'Mobile' })).toBeHidden()
  await expect(menu).toBeFocused()
})

test('homepage keeps its header and controls usable at 320px', async ({
  page,
}) => {
  await page.setViewportSize({ width: 320, height: 568 })
  await page.goto('/')
  const header = page.locator('.home-header')
  expect((await header.boundingBox())!.height).toBeLessThanOrEqual(76)
  const overflow = await page.evaluate(
    () =>
      document.documentElement.scrollWidth -
      document.documentElement.clientWidth,
  )
  expect(overflow).toBeLessThanOrEqual(1)
  await expect(
    header.getByRole('link', { name: 'Start free trial' }),
  ).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'Open menu', exact: true }),
  ).toBeVisible()
})
