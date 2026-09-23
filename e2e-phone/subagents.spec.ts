// Subagents stay out of the drawer's list. They hang off the Session that
// called them: a card per Task call, a sheet from the header, and a way back
// from the subagent to its caller.
import { subagentScenario } from '../e2e/fake-daemon/subagents'
import { expect, openDrawer, pairPhone, pickSession, test } from './fixtures'

const { main, explorer, reviewer, sessions } = subagentScenario()

test.use({ scenario: { sessions } })

test('subagents are not listed; the calling Session shows each as a card', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  const list = await openDrawer(page)
  await expect(list.getByRole('button', { name: /Plan the release/ })).toBeVisible()
  await expect(list.getByRole('button', { name: /Explorer:/ })).toHaveCount(0)
  await expect(list.getByRole('button', { name: /Reviewer:/ })).toHaveCount(0)

  await pickSession(page, /Plan the release/)
  const transcript = page.getByRole('log', { name: 'Transcript' })
  const explorerCard = transcript.getByRole('group', { name: 'Explorer: Map the mobile app' })
  await expect(explorerCard).toContainText('Completed')
  await expect(explorerCard).toContainText('12 tools')
  await expect(explorerCard).toContainText('2m 14s')
  await explorerCard.getByRole('button', { name: 'Details' }).click()
  await expect(explorerCard.getByRole('region', { name: 'Prompt' })).toContainText(
    'Explore apps/phone and report its screens.',
  )
  const report = explorerCard.getByRole('region', { name: 'Report' })
  await expect(report).toContainText('The app has three screens')
  await expect(report).not.toContainText('**')

  const reviewerCard = transcript.getByRole('group', { name: 'Reviewer: Review the diff' })
  await expect(reviewerCard.getByRole('status')).toContainText('Running')

  await explorerCard.getByRole('button', { name: 'Open subagent session' }).click()
  await expect(transcript).toContainText('The app has three screens: list, session, settings.')
  await expect(page.getByRole('navigation', { name: 'Session hierarchy' })).toBeVisible()
})

test('the header leads to each subagent and back to the calling Session', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  await pickSession(page, /Plan the release/)
  await page.getByRole('button', { name: '2 subagents, 1 running' }).click()
  const sheet = page.getByRole('dialog', { name: 'Subagents' })
  await sheet.getByRole('button', { name: reviewer.title }).click()
  await expect(sheet).toBeHidden()

  const trail = page.getByRole('navigation', { name: 'Session hierarchy' })
  await expect(trail.getByRole('button', { name: reviewer.title })).toBeVisible()

  // The title switches between subagents of the same caller.
  await trail.getByRole('button', { name: reviewer.title }).click()
  await page
    .getByRole('dialog', { name: 'Subagents' })
    .getByRole('radio', { name: explorer.title })
    .click()
  await expect(trail.getByRole('button', { name: explorer.title })).toBeVisible()

  await trail.getByRole('button', { name: main.title }).click()
  await expect(page.getByRole('navigation', { name: 'Session hierarchy' })).toHaveCount(0)
  await expect(page.getByRole('heading', { name: main.title })).toBeVisible()

  // Inside a subagent the calling Session stays the selected row.
  await explorerCardOpen(page)
  const list = await openDrawer(page)
  const row = list.getByRole('button', { name: /Plan the release/ })
  await expect(row).toHaveAttribute('aria-current', 'page')
  await row.click()
  await expect(page.getByRole('heading', { name: main.title })).toBeVisible()
})

async function explorerCardOpen(page: import('@playwright/test').Page) {
  await page
    .getByRole('group', { name: 'Explorer: Map the mobile app' })
    .getByRole('button', { name: 'Open subagent session' })
    .click()
  await expect(page.getByRole('navigation', { name: 'Session hierarchy' })).toBeVisible()
}

test('the calling Session’s row says how many subagents are running', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  await pickSession(page, /Plan the release/)
  await expect(page.getByRole('button', { name: '2 subagents, 1 running' })).toBeVisible()
  const list = await openDrawer(page)
  await expect(
    list.getByRole('button', { name: /Plan the release/ }).getByRole('status'),
  ).toContainText('1 subagent running')
})
