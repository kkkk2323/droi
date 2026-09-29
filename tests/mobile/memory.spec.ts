import { session, userMessage } from '../fake-daemon/scenario'
import { expect, openDrawer, pairPhone, test } from './fixtures'

const work = session('Invoice export', '/Users/dev/billing-service', [userMessage('a')])
// The Desktop Shell's own model work (ADR 0011); no Client lists it.
const memorySession = session(
  'Memory: extract from 0427515a',
  '/Users/dev/billing-service',
  [userMessage('{}')],
  {
    tags: [{ name: 'droi.memory' }],
  },
)

test.use({ scenario: { sessions: [work, memorySession] } })

test('Memory Sessions are not listed', async ({ page, fakeDaemon }) => {
  await pairPhone(page, fakeDaemon)
  const list = await openDrawer(page)
  await expect(list.getByRole('button', { name: /Invoice export/ })).toBeVisible()
  await expect(list.getByRole('button', { name: /Memory: extract/ })).toHaveCount(0)
})
