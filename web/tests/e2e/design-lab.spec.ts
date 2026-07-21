import { expect, test } from '@playwright/test'

const concepts = ['clearspace', 'dispatch', 'signal-observatory', 'project-deck', 'flowboard'] as const
const views = ['overview', 'feedback', 'feedback-detail'] as const

test('comparison index and route guards work', async ({ page }) => {
  await page.goto('/designs')
  await expect(page.getByRole('heading', { name: 'Five ways to operate on product feedback.' })).toBeVisible()
  await expect(page.locator('.concept-row')).toHaveCount(5)

  await page.goto('/designs/not-a-concept/overview')
  await expect(page).toHaveURL(/\/designs\?invalid=1$/)
  await expect(page.getByText('That design view does not exist.')).toBeVisible()
})

for (const concept of concepts) {
  test(`${concept} renders all representative views`, async ({ page }, testInfo) => {
    const browserErrors: string[] = []
    page.on('console', (message) => {
      if (message.type() === 'error') browserErrors.push(message.text())
    })
    page.on('pageerror', (error) => browserErrors.push(error.message))

    for (const view of views) {
      await page.goto(`/designs/${concept}/${view}`)
      await expect(page.locator('#concept-select')).toHaveValue(concept)
      await expect(page.locator('.view-switcher button[data-active="true"]')).toHaveText(
        view === 'feedback-detail' ? 'Detail' : view === 'feedback' ? 'Feedback' : 'Overview',
      )
      await expect(page.locator('body')).not.toContainText('Internal Server Error')
      await expect.poll(() => page.locator('img').evaluateAll((images) =>
        images.filter((image) => !(image as HTMLImageElement).complete || (image as HTMLImageElement).naturalWidth === 0)
          .map((image) => (image as HTMLImageElement).src),
      )).toEqual([])

      const dimensions = await page.evaluate(() => ({
        clientWidth: document.documentElement.clientWidth,
        scrollWidth: document.documentElement.scrollWidth,
        bodyHeight: document.body.getBoundingClientRect().height,
      }))
      expect(dimensions.scrollWidth).toBeLessThanOrEqual(dimensions.clientWidth + 1)
      expect(dimensions.bodyHeight).toBeGreaterThan(300)

      await page.screenshot({
        path: testInfo.outputPath(`${concept}-${view}.png`),
        fullPage: false,
        animations: 'disabled',
      })
    }

    expect(browserErrors).toEqual([])
  })
}

test('design switcher preserves the representative view', async ({ page }) => {
  await page.goto('/designs/clearspace/feedback')
  await page.locator('#concept-select').selectOption('dispatch')
  await expect(page).toHaveURL(/\/designs\/dispatch\/feedback$/)
  await page.locator('.view-switcher').getByRole('button', { name: 'Detail' }).click()
  await expect(page).toHaveURL(/\/designs\/dispatch\/feedback-detail$/)
})

test('core prototype interactions update visible state', async ({ page }, testInfo) => {
  await page.goto('/designs/clearspace/feedback')
  await page.getByRole('button', { name: 'Filters' }).click()
  await page.getByLabel('Project').selectOption('docs')
  await expect(page.getByText('FDB-1046')).toBeVisible()
  await expect(page.getByText('FDB-1048')).toHaveCount(0)

  await page.goto('/designs/dispatch/feedback')
  await page.locator('.feedback-stream article').nth(1).click()
  await expect(page.locator('.dispatch-inspector')).toContainText('FDB-1047')
  await page.locator('.feedback-stream article').nth(1).getByRole('checkbox').check()
  await expect(page.locator('.bulk-bar')).toContainText('1 selected')

  await page.goto('/designs/flowboard/feedback')
  if (testInfo.project.name === 'desktop') {
    await page.getByTitle('Table view').click()
    await expect(page.getByRole('table')).toBeVisible()
  } else {
    await expect(page.locator('.flow-board')).toBeVisible()
  }
})

test('mobile navigation and list adaptations remain usable', async ({ page }, testInfo) => {
  test.skip(testInfo.project.name !== 'mobile', 'Mobile-only interaction')

  await page.goto('/designs/clearspace/overview')
  await page.getByTitle('Open navigation').click()
  await expect(page.locator('.cs-sidebar')).toHaveAttribute('data-open', 'true')
  await expect(page.locator('.cs-sidebar').getByText('Acme Studio')).toBeVisible()

  await page.goto('/designs/dispatch/feedback')
  await expect(page.locator('.dispatch-inspector')).toBeHidden()
  await page.locator('.feedback-stream article').first().getByTitle('Open detail').click()
  await expect(page).toHaveURL(/\/designs\/dispatch\/feedback-detail\?id=FDB-1048$/)
})
