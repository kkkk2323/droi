import type { Page } from '@playwright/test'
import { FakeKernel } from './fake-kernel/fake-kernel'
import { userEntry } from './fake-kernel/history'
import {
  expect,
  openSessionList,
  pairingLink,
  pairPhone,
  pasteLink,
  pickSession,
  relaunch,
  sessionList,
  test,
} from './fixtures'

test.use({
  scenario: { sessions: [{ title: 'Deploy', cwd: 'acme-web', entries: [userEntry('ship it')] }] },
})

async function withOffice(run: (office: FakeKernel) => Promise<void>) {
  const office = await FakeKernel.start({
    name: 'Office Mac',
    computerId: '0ff1ce00-0000-4000-8000-000000000000',
    sessions: [{ title: 'Budget', cwd: 'finance', entries: [userEntry('sum it')] }],
  })
  try {
    await run(office)
  } finally {
    await office.stop()
  }
}

async function addComputer(page: Page, link: string) {
  await openSessionList(page)
  await page.getByRole('button', { name: /Switch computer/ }).click()
  await page.getByRole('menuitem', { name: 'Add a computer' }).click()
  // A computer that lost its pairing shows a pairing form of its own on the list.
  await expect(page.getByRole('heading', { name: 'Pair with a computer' })).toBeVisible()
  await pasteLink(page, link)
  await expect(sessionList(page)).toBeVisible()
}

async function switchTo(page: Page, name: string) {
  await openSessionList(page)
  await page.getByRole('button', { name: /Switch computer/ }).click()
  await page.getByRole('menuitem', { name }).click()
  // The switch rebuilds the connection and starts again from that computer's list.
  await openSessionList(page)
  await expect(page.getByRole('button', { name: `Switch computer, ${name}` })).toBeVisible()
}

async function openComputers(page: Page) {
  await openSessionList(page)
  await page.getByRole('button', { name: 'Settings' }).click()
  await page.getByRole('button', { name: /Paired computers/ }).click()
}

test('scanning the pairing QR code pairs like the link', async ({ page, fakeKernel }) => {
  await page.goto('/')
  await page.getByRole('button', { name: 'Scan QR code' }).click()
  await expect(page.getByText('Camera stand-in')).toBeVisible()
  await page.evaluate(
    (link) =>
      (window as unknown as { droiStandIns: { scan: (t: string) => void } }).droiStandIns.scan(
        link,
      ),
    pairingLink(fakeKernel),
  )
  await expect(sessionList(page)).toBeVisible()
  const list = await openSessionList(page)
  await expect(list.getByRole('button', { name: /Deploy/ })).toBeVisible()
})

test('pairing the same computer again updates it instead of adding one', async ({
  page,
  fakeKernel,
}) => {
  await pairPhone(page, fakeKernel)
  // The computer at another address, as after its network changed.
  const moved = `${fakeKernel.url.replace('127.0.0.1', 'localhost')}/#pair=${fakeKernel.token}`
  await addComputer(page, moved)
  await openSessionList(page)
  await expect(page.getByRole('status', { name: 'Connection' })).toHaveText('Connected')
  await openComputers(page)
  const rows = page.getByRole('button', { name: /Test Mac/ })
  await expect(rows).toHaveCount(1)
  await expect(rows).toHaveAccessibleName(/localhost:\d+/)
})

test('two computers switch, and the other one shows its last known list first', async ({
  page,
  fakeKernel,
}) => {
  await withOffice(async (office) => {
    await pairPhone(page, fakeKernel)
    await addComputer(page, pairingLink(office))
    let list = await openSessionList(page)
    await expect(list.getByRole('button', { name: /Budget/ })).toBeVisible()

    await switchTo(page, 'Test Mac')
    list = await openSessionList(page)
    await expect(list.getByRole('button', { name: /Deploy/ })).toBeVisible()
    await expect(list.getByRole('button', { name: /Budget/ })).toHaveCount(0)

    await office.goDown()
    await switchTo(page, 'Office Mac')
    list = await openSessionList(page)
    await expect(list.getByRole('button', { name: /Budget/ })).toBeVisible()
    await expect(page.getByRole('status', { name: 'Connection' })).not.toHaveText('Connected')
  })
})

test('a Paired Computer can be renamed, re-addressed and removed', async ({ page, fakeKernel }) => {
  await pairPhone(page, fakeKernel)
  await openComputers(page)
  await page.getByRole('button', { name: /Test Mac/ }).click()
  await page.getByRole('textbox', { name: 'Name' }).fill('Desk')
  const port = new URL(fakeKernel.url).port
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

test('relaunching reopens the last computer and Session', async ({ page, fakeKernel }) => {
  await withOffice(async (office) => {
    await pairPhone(page, fakeKernel)
    await addComputer(page, pairingLink(office))
    await switchTo(page, 'Test Mac')
    await pickSession(page, /Deploy/)
    await expect(page.getByRole('heading', { name: 'Deploy' })).toBeVisible()

    await relaunch(page)
    await expect(page.getByRole('heading', { name: 'Deploy' })).toBeVisible()
    await openSessionList(page)
    await expect(page.getByRole('button', { name: 'Switch computer, Test Mac' })).toBeVisible()
  })
})
