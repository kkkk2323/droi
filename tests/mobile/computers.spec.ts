import type { Page } from '@playwright/test'
import { FakeDaemon } from '../fake-daemon/fake-daemon'
import { session, userMessage } from '../fake-daemon/scenario'
import {
  expect,
  openDrawer,
  pairingLink,
  pairPhone,
  pasteLink,
  pickSession,
  test,
} from './fixtures'

const deploy = session('Deploy', '/Users/dev/acme-web', [userMessage('ship it')])
const budget = session('Budget', '/Users/dev/finance', [userMessage('sum it')])

test.use({ scenario: { sessions: [deploy] } })

async function withOffice(run: (office: FakeDaemon) => Promise<void>) {
  const office = await FakeDaemon.start({ sessions: [budget] })
  office.meta = { ...office.meta, name: 'Office Mac' }
  try {
    await run(office)
  } finally {
    await office.stop()
  }
}

async function addComputer(page: Page, link: string) {
  const list = await openDrawer(page)
  await list.getByRole('button', { name: /Switch computer/ }).click()
  await list.getByRole('menuitem', { name: 'Add a computer' }).click()
  await pasteLink(page, link)
  await expect(page.getByRole('heading', { name: 'New session' })).toBeVisible()
}

async function switchTo(page: Page, name: string) {
  const list = await openDrawer(page)
  await list.getByRole('button', { name: /Switch computer/ }).click()
  await list.getByRole('menuitem', { name }).click()
  // The switch rebuilds the connection and the main screen, drawer closed.
  const next = await openDrawer(page)
  await expect(next.getByRole('button', { name: `Switch computer, ${name}` })).toBeVisible()
}

async function openComputers(page: Page) {
  const list = await openDrawer(page)
  await list.getByRole('button', { name: 'Settings' }).click()
  await page.getByRole('button', { name: /Paired computers/ }).click()
}

test('scanning the pairing QR code pairs like the link', async ({ page, fakeDaemon }) => {
  await page.goto('/')
  await page.getByRole('button', { name: 'Scan QR code' }).click()
  await expect(page.getByText('Camera stand-in')).toBeVisible()
  await page.evaluate(
    (link) =>
      (window as unknown as { droiStandIns: { scan: (t: string) => void } }).droiStandIns.scan(
        link,
      ),
    pairingLink(fakeDaemon),
  )
  await expect(page.getByRole('heading', { name: 'New session' })).toBeVisible()
  const list = await openDrawer(page)
  await expect(list.getByRole('button', { name: /Deploy/ })).toBeVisible()
})

test('pairing the same computer again updates it instead of adding one', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  const token = fakeDaemon.resetToken()
  const moved = `${fakeDaemon.url.replace('127.0.0.1', 'localhost')}/#pair=${token}`
  await addComputer(page, moved)
  const list = await openDrawer(page)
  await expect(list.getByRole('status', { name: 'Connection' })).toHaveText('Connected')
  await openComputers(page)
  const rows = page.getByRole('button', { name: /Test Mac/ })
  await expect(rows).toHaveCount(1)
  await expect(rows).toHaveAccessibleName(/localhost:\d+/)
})

test('two computers switch, and the other one shows its last known list first', async ({
  page,
  fakeDaemon,
}) => {
  await withOffice(async (office) => {
    await pairPhone(page, fakeDaemon)
    await addComputer(page, pairingLink(office))
    let list = await openDrawer(page)
    await expect(list.getByRole('button', { name: /Budget/ })).toBeVisible()

    await switchTo(page, 'Test Mac')
    list = await openDrawer(page)
    await expect(list.getByRole('button', { name: /Deploy/ })).toBeVisible()
    await expect(list.getByRole('button', { name: /Budget/ })).toHaveCount(0)

    office.goDown()
    await switchTo(page, 'Office Mac')
    list = await openDrawer(page)
    await expect(list.getByRole('button', { name: /Budget/ })).toBeVisible()
    await expect(list.getByRole('status', { name: 'Connection' })).not.toHaveText('Connected')
  })
})

test('a Paired Computer can be renamed, re-addressed and removed', async ({ page, fakeDaemon }) => {
  await pairPhone(page, fakeDaemon)
  await openComputers(page)
  await page.getByRole('button', { name: /Test Mac/ }).click()
  await page.getByRole('textbox', { name: 'Name' }).fill('Desk')
  const port = new URL(fakeDaemon.url).port
  await page.getByRole('textbox', { name: 'Address' }).fill('not a url at all')
  await page.getByRole('button', { name: 'Save' }).click()
  await expect(page.getByRole('alert')).toHaveText(/Enter an address/)
  await page.getByRole('textbox', { name: 'Address' }).fill(`localhost:${port}`)
  await page.getByRole('button', { name: 'Save' }).click()
  await expect(page.getByRole('button', { name: `Desk: localhost:${port}` })).toBeVisible()

  await page.getByRole('button', { name: /Desk/ }).click()
  await page.getByRole('button', { name: 'Remove computer' }).click()
  await page.getByRole('button', { name: 'Remove', exact: true }).click()
  await expect(page.getByRole('button', { name: /Desk/ })).toHaveCount(0)
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Pair with a computer' })).toBeVisible()
})

test('relaunching reopens the last computer and Session', async ({ page, fakeDaemon }) => {
  await withOffice(async (office) => {
    await pairPhone(page, fakeDaemon)
    await addComputer(page, pairingLink(office))
    await switchTo(page, 'Test Mac')
    await pickSession(page, /Deploy/)
    await expect(page.getByRole('heading', { name: 'Deploy' })).toBeVisible()

    await page.reload()
    await expect(page.getByRole('heading', { name: 'Deploy' })).toBeVisible()
    const list = await openDrawer(page)
    await expect(list.getByRole('button', { name: 'Switch computer, Test Mac' })).toBeVisible()
  })
})
