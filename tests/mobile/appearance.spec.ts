import type { Page } from '@playwright/test'
import { session, userMessage } from '../fake-daemon/scenario'
import { expect, openSessionList, pairPhone, pickSession, relaunch, test } from './fixtures'

const deploy = session('Deploy', '/Users/dev/acme-web', [userMessage('ship it')])

test.use({ scenario: { sessions: [deploy] } })

// The theme's foreground: #161616 in light, #eeeeee in dark.
const LIGHT_TEXT = 'rgb(22, 22, 22)'
const DARK_TEXT = 'rgb(238, 238, 238)'

async function openSettings(page: Page) {
  await openSessionList(page)
  await page.getByRole('button', { name: 'Settings' }).click()
}

test('picking light, dark and system changes the theme', async ({ page, fakeDaemon }) => {
  await page.emulateMedia({ colorScheme: 'dark' })
  await pairPhone(page, fakeDaemon)
  await openSettings(page)
  const theme = page.getByRole('radiogroup', { name: 'Theme' })
  const label = theme.getByText('System')
  await expect(theme.getByRole('radio', { name: 'System' })).toBeChecked()
  await expect(label).toHaveCSS('color', DARK_TEXT)

  await theme.getByRole('radio', { name: 'Light', exact: true }).click()
  await expect(label).toHaveCSS('color', LIGHT_TEXT)
  await theme.getByRole('radio', { name: 'Dark' }).click()
  await expect(label).toHaveCSS('color', DARK_TEXT)
  // A picked theme holds whatever the system does.
  await page.emulateMedia({ colorScheme: 'light' })
  await expect(label).toHaveCSS('color', DARK_TEXT)

  await theme.getByRole('radio', { name: 'System' }).click()
  await expect(label).toHaveCSS('color', LIGHT_TEXT)
  await page.emulateMedia({ colorScheme: 'dark' })
  await expect(label).toHaveCSS('color', DARK_TEXT)
})

test('Solarized Light+ draws the Settings in its colours', async ({ page, fakeDaemon }) => {
  await page.emulateMedia({ colorScheme: 'dark' })
  await pairPhone(page, fakeDaemon)
  await openSettings(page)
  const theme = page.getByRole('radiogroup', { name: 'Theme' })
  await theme.getByRole('radio', { name: 'Solarized Light+' }).click()
  await expect(theme.getByRole('radio', { name: 'Solarized Light+' })).toBeChecked()
  // #586e75, Solarized's base01, whatever the system's appearance.
  await expect(theme.getByText('System')).toHaveCSS('color', 'rgb(88, 110, 117)')
})

test('text size changes the transcript and persists', async ({ page, fakeDaemon }) => {
  await pairPhone(page, fakeDaemon)
  await pickSession(page, /Deploy/)
  const message = page.getByRole('article', { name: 'You' }).getByText('ship it')
  await expect(message).toHaveCSS('font-size', '15px')

  await openSettings(page)
  await page
    .getByRole('radiogroup', { name: 'Text size' })
    .getByRole('radio', { name: 'Largest' })
    .click()
  // The app's own chrome keeps its size.
  await expect(page.getByRole('radio', { name: 'Largest' }).getByText('Largest')).toHaveCSS(
    'font-size',
    '15px',
  )
  // Settings opens from the list; go back there and into the Session.
  await page.goBack()
  await pickSession(page, /Deploy/)
  await expect(message).toHaveCSS('font-size', '17.8125px')
  await expect(page.getByRole('textbox', { name: 'Message' })).toHaveCSS('font-size', '17.8125px')

  await relaunch(page)
  await expect(message).toHaveCSS('font-size', '17.8125px')
})
