// Remote Access through a Relay (ADR 0034): the phone tries the computer's network address
// first and goes through the Relay when that does not answer.
import { userEntry } from './fake-kernel/history'
import {
  expect,
  openSessionList,
  pairingLink,
  pasteLink,
  pickSession,
  sessionList,
  test,
} from './fixtures'
import { startRelayStandIn, type RelayStandIn } from './relay-stand-in'

test.use({
  scenario: { sessions: [{ title: 'Deploy', cwd: 'acme-web', entries: [userEntry('ship it')] }] },
})

const kernelSockets = (relay: RelayStandIn) =>
  relay.requests.filter((path) => path.startsWith('/kernel')).length

/** A network address nothing answers at, as the computer's is when the phone is away. */
const AWAY = 'http://127.0.0.1:9'

test('away from the computer’s network, the phone pairs and works through the relay', async ({
  page,
  fakeKernel,
}) => {
  const relay = await startRelayStandIn(fakeKernel.url)
  try {
    await page.goto('/')
    await pasteLink(
      page,
      `${AWAY}/#pair=${fakeKernel.token}&relay=${encodeURIComponent(relay.base)}`,
    )
    await expect(sessionList(page)).toBeVisible()
    await pickSession(page, /Deploy/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript.getByRole('article', { name: 'You' })).toHaveText('ship it')
    await fakeKernel.turn('Deploy', 'status?', { chunks: ['All ', 'green.'] })
    await expect(transcript).toContainText('All green.')
    expect(relay.requests).toContain('/meta')
    // The socket's upgrade can reach the relay after the page shows the list.
    await expect.poll(() => kernelSockets(relay)).toBeGreaterThan(0)
  } finally {
    await relay.close()
  }
})

test('on the computer’s network, the phone connects directly and leaves the relay alone', async ({
  page,
  fakeKernel,
}) => {
  const relay = await startRelayStandIn(fakeKernel.url)
  try {
    await page.goto('/')
    await pasteLink(page, `${pairingLink(fakeKernel)}&relay=${encodeURIComponent(relay.base)}`)
    await expect(sessionList(page)).toBeVisible()
    await openSessionList(page)
    await expect(page.getByRole('status', { name: 'Connection' })).toHaveText('Connected')
    expect(relay.requests).toEqual([])
  } finally {
    await relay.close()
  }
})

test('a link to the relay alone pairs the phone', async ({ page, fakeKernel }) => {
  const relay = await startRelayStandIn(fakeKernel.url)
  try {
    await page.goto('/')
    await pasteLink(page, `${relay.base}/#pair=${fakeKernel.token}`)
    await expect(sessionList(page)).toBeVisible()
    await expect.poll(() => kernelSockets(relay)).toBeGreaterThan(0)
  } finally {
    await relay.close()
  }
})

test('leaving the computer’s network moves the connection to the relay', async ({
  page,
  fakeKernel,
}) => {
  const relay = await startRelayStandIn(fakeKernel.url)
  // A computer with a Relay names it in /meta.
  const network = await startRelayStandIn(fakeKernel.url, {
    prefix: '',
    relayInMeta: () => relay.base,
  })
  try {
    await page.goto('/')
    await pasteLink(
      page,
      `${network.base}/#pair=${fakeKernel.token}&relay=${encodeURIComponent(relay.base)}`,
    )
    await pickSession(page, /Deploy/)
    expect(kernelSockets(relay)).toBe(0)

    await network.close()
    await expect.poll(() => kernelSockets(relay), { timeout: 20_000 }).toBeGreaterThan(0)
    await expect(page.getByRole('status', { name: 'Reconnecting' })).toHaveCount(0, {
      timeout: 15_000,
    })
    await expect(page.getByRole('heading', { name: 'Deploy' })).toBeVisible()
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await fakeKernel.turn('Deploy', 'still there?', { text: 'Yes.' })
    await expect(transcript).toContainText('Yes.')
  } finally {
    await relay.close()
  }
})

test('a phone paired before the relay was set follows the relay the computer names', async ({
  page,
  fakeKernel,
}) => {
  const relay = await startRelayStandIn(fakeKernel.url)
  const network = await startRelayStandIn(fakeKernel.url, {
    prefix: '',
    relayInMeta: () => relay.base,
  })
  try {
    await page.goto('/')
    // A link from before the relay: the network address alone.
    await pasteLink(page, `${network.base}/#pair=${fakeKernel.token}`)
    await pickSession(page, /Deploy/)
    expect(kernelSockets(relay)).toBe(0)

    await network.close()
    await expect.poll(() => kernelSockets(relay), { timeout: 20_000 }).toBeGreaterThan(0)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await fakeKernel.turn('Deploy', 'away?', { text: 'Through the relay.' })
    await expect(transcript).toContainText('Through the relay.')
  } finally {
    await relay.close()
  }
})

test('an unreachable computer with a relay names both ways it tried', async ({
  page,
  fakeKernel,
}) => {
  const relay = await startRelayStandIn(fakeKernel.url)
  await page.goto('/')
  await pasteLink(page, `${AWAY}/#pair=${fakeKernel.token}&relay=${encodeURIComponent(relay.base)}`)
  await expect(sessionList(page)).toBeVisible()
  await relay.close()
  await fakeKernel.goDown()
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Can’t reach Test Mac' })).toBeVisible({
    timeout: 15_000,
  })
  await expect(page.getByText(AWAY, { exact: true })).toBeVisible()
  await expect(page.getByText(relay.base, { exact: true })).toBeVisible()
  await expect(
    page.getByRole('list', { name: 'What to check' }).getByRole('listitem').nth(1),
  ).toContainText('relay as connected')
})
