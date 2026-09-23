import { expect, test } from './fixtures'
import { assistantMessage, session, userMessage } from '../fake-daemon/scenario'
import { streamedReply } from '../fake-daemon/turns'

const chat = session('Phone chat', '/Users/dev/acme-web', [
  userMessage('hello from the desk'),
  assistantMessage('Hi! I am here.'),
])

test.describe('phone form factor', () => {
  test.use({
    viewport: { width: 390, height: 844 },
    hasTouch: true,
    isMobile: true,
    scenario: {
      sessions: [chat],
      handlers: { 'daemon.add_user_message': streamedReply({ deltas: ['On ', 'the phone.'] }) },
    },
  })

  test('the sidebar is a drawer that opens, picks a Session and closes', async ({
    page,
    openClient,
  }) => {
    await openClient()
    await expect(page.getByRole('navigation', { name: 'Sessions' })).toHaveCount(0)
    const open = page.getByRole('button', { name: 'Open sessions' })
    await open.tap()
    const drawer = page.getByRole('dialog', { name: 'Sessions' })
    await expect(drawer).toBeVisible()
    await drawer.getByRole('button', { name: /Phone chat/ }).tap()
    await expect(drawer).toHaveCount(0)
    await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('I am here.')

    await open.tap()
    await expect(drawer).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(drawer).toHaveCount(0)
  })

  test('the composer is usable and the conversation fills the screen', async ({
    page,
    openClient,
  }) => {
    await openClient()
    await page.getByRole('button', { name: 'Open sessions' }).tap()
    await page
      .getByRole('dialog', { name: 'Sessions' })
      .getByRole('button', { name: /Phone chat/ })
      .tap()
    const input = page.getByRole('textbox', { name: 'Message' })
    await expect(input).toBeInViewport()
    await input.fill('typing on glass')
    await page.getByRole('button', { name: 'Send' }).tap()
    await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('On the phone.')
    await expect(input).toBeInViewport()
  })

  test('nothing scrolls horizontally at 360px', async ({ page, openClient }) => {
    await page.setViewportSize({ width: 360, height: 740 })
    await openClient()
    await page.getByRole('button', { name: 'Open sessions' }).tap()
    await page
      .getByRole('dialog', { name: 'Sessions' })
      .getByRole('button', { name: /Phone chat/ })
      .tap()
    await expect(page.getByRole('log', { name: 'Transcript' })).toBeVisible()
    const overflow = await page.evaluate(() => ({
      scroll: document.documentElement.scrollWidth,
      inner: window.innerWidth,
    }))
    expect(overflow.scroll).toBeLessThanOrEqual(overflow.inner)
  })

  test('the PWA manifest and icons are served', async ({ page, openClient }) => {
    await openClient()
    const href = await page.locator('link[rel="manifest"]').getAttribute('href')
    expect(href).toBe('/manifest.webmanifest')
    const manifest = await page.request.get(href!)
    expect(manifest.status()).toBe(200)
    const body = await manifest.json()
    expect(body).toMatchObject({ name: 'Droi', display: 'standalone', start_url: '/' })
    for (const icon of body.icons as Array<{ src: string }>) {
      expect((await page.request.get(icon.src)).status()).toBe(200)
    }
  })

  test('reduced motion turns animations off', async ({ browser, fakeDaemon }) => {
    const context = await browser.newContext({
      reducedMotion: 'reduce',
      viewport: { width: 390, height: 844 },
    })
    const page = await context.newPage()
    const fragment = new URLSearchParams({ pair: fakeDaemon.token, gateway: fakeDaemon.url })
    await page.goto(`/#${fragment.toString()}`)
    await expect(page.getByRole('status', { name: 'Connection' })).toHaveText(/Connected/)
    // The reconnecting banner's dot pulses; the status dot is static when connected.
    fakeDaemon.goDown()
    const dot = page.getByRole('alert').locator('span[aria-hidden]').first()
    await expect(dot).toBeAttached()
    const { name, duration } = await dot.evaluate((el) => ({
      name: getComputedStyle(el).animationName,
      duration: getComputedStyle(el).animationDuration,
    }))
    expect(name).not.toBe('none')
    expect(parseFloat(duration)).toBeLessThan(0.001)
    await context.close()
  })
})
