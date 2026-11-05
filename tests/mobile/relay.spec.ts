// Remote Access through a Relay (ADR 0034): the phone tries the computer's network address
// first and goes through the Relay when that does not answer.
import { session, userMessage } from '@droi/fake-daemon/scenario'
import {
  expect,
  openSessionList,
  pairingLink,
  pasteLink,
  pickSession,
  playTurn,
  sessionList,
  test,
} from './fixtures'
import { startRelayStandIn, type RelayStandIn } from './relay-stand-in'

const deploy = session('Deploy', '/Users/dev/acme-web', [userMessage('ship it')])

test.use({ scenario: { sessions: [deploy] } })

const daemonSockets = (relay: RelayStandIn) =>
  relay.requests.filter((path) => path.startsWith('/daemon')).length

/** A network address nothing answers at, as the computer's is when the phone is away. */
const AWAY = 'http://127.0.0.1:9'

test('away from the computer’s network, the phone pairs and works through the relay', async ({
  page,
  fakeDaemon,
}) => {
  const relay = await startRelayStandIn(fakeDaemon.url)
  try {
    await page.goto('/')
    await pasteLink(
      page,
      `${AWAY}/#pair=${fakeDaemon.token}&relay=${encodeURIComponent(relay.base)}`,
    )
    await expect(sessionList(page)).toBeVisible()
    await pickSession(page, /Deploy/)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    await expect(transcript.getByRole('article', { name: 'You' })).toHaveText('ship it')
    playTurn(fakeDaemon, deploy.sessionId, 'status?', ['All ', 'green.'])
    await expect(transcript).toContainText('All green.')
    expect(relay.requests).toContain('/meta')
    expect(daemonSockets(relay)).toBeGreaterThan(0)
  } finally {
    await relay.close()
  }
})

test('on the computer’s network, the phone connects directly and leaves the relay alone', async ({
  page,
  fakeDaemon,
}) => {
  const relay = await startRelayStandIn(fakeDaemon.url)
  try {
    await page.goto('/')
    await pasteLink(page, `${pairingLink(fakeDaemon)}&relay=${encodeURIComponent(relay.base)}`)
    await expect(sessionList(page)).toBeVisible()
    await openSessionList(page)
    await expect(page.getByRole('status', { name: 'Connection' })).toHaveText('Connected')
    expect(relay.requests).toEqual([])
  } finally {
    await relay.close()
  }
})

test('a link to the relay alone pairs the phone', async ({ page, fakeDaemon }) => {
  const relay = await startRelayStandIn(fakeDaemon.url)
  try {
    await page.goto('/')
    await pasteLink(page, `${relay.base}/#pair=${fakeDaemon.token}`)
    await expect(sessionList(page)).toBeVisible()
    expect(daemonSockets(relay)).toBeGreaterThan(0)
  } finally {
    await relay.close()
  }
})

test('leaving the computer’s network moves the connection to the relay', async ({
  page,
  fakeDaemon,
}) => {
  const network = await startRelayStandIn(fakeDaemon.url, { prefix: '' })
  const relay = await startRelayStandIn(fakeDaemon.url)
  try {
    await page.goto('/')
    await pasteLink(
      page,
      `${network.base}/#pair=${fakeDaemon.token}&relay=${encodeURIComponent(relay.base)}`,
    )
    await pickSession(page, /Deploy/)
    expect(daemonSockets(relay)).toBe(0)

    await network.close()
    await expect.poll(() => daemonSockets(relay), { timeout: 20_000 }).toBeGreaterThan(0)
    await expect(page.getByRole('status', { name: 'Reconnecting' })).toHaveCount(0, {
      timeout: 15_000,
    })
    await expect(page.getByRole('heading', { name: 'Deploy' })).toBeVisible()
    const transcript = page.getByRole('log', { name: 'Transcript' })
    playTurn(fakeDaemon, deploy.sessionId, 'still there?', ['Yes.'])
    await expect(transcript).toContainText('Yes.')
  } finally {
    await relay.close()
  }
})

test('a phone paired before the relay was set follows the relay the computer names', async ({
  page,
  fakeDaemon,
}) => {
  const relay = await startRelayStandIn(fakeDaemon.url)
  const network = await startRelayStandIn(fakeDaemon.url, {
    prefix: '',
    relayInMeta: () => relay.base,
  })
  try {
    await page.goto('/')
    // A link from before the relay: the network address alone.
    await pasteLink(page, `${network.base}/#pair=${fakeDaemon.token}`)
    await pickSession(page, /Deploy/)
    expect(daemonSockets(relay)).toBe(0)

    await network.close()
    await expect.poll(() => daemonSockets(relay), { timeout: 20_000 }).toBeGreaterThan(0)
    const transcript = page.getByRole('log', { name: 'Transcript' })
    playTurn(fakeDaemon, deploy.sessionId, 'away?', ['Through the relay.'])
    await expect(transcript).toContainText('Through the relay.')
  } finally {
    await relay.close()
  }
})

test('an unreachable computer with a relay names both ways it tried', async ({
  page,
  fakeDaemon,
}) => {
  const relay = await startRelayStandIn(fakeDaemon.url)
  await page.goto('/')
  await pasteLink(page, `${AWAY}/#pair=${fakeDaemon.token}&relay=${encodeURIComponent(relay.base)}`)
  await expect(sessionList(page)).toBeVisible()
  await relay.close()
  fakeDaemon.goDown()
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
