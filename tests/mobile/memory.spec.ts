import { userEntry } from './fake-kernel/history'
import { expect, openSessionList, pairPhone, test } from './fixtures'

test.use({
  scenario: {
    sessions: [
      { title: 'Invoice export', cwd: 'billing-service', entries: [userEntry('a')] },
      // The Host's own Memory work (ADR 0011); no Client lists it.
      {
        title: 'Memory: extract from 0427515a',
        cwd: 'billing-service',
        kind: 'memory',
        entries: [userEntry('{}')],
      },
    ],
  },
})

test('Memory Sessions are not listed', async ({ page, fakeKernel }) => {
  await pairPhone(page, fakeKernel)
  const list = await openSessionList(page)
  await expect(list.getByRole('button', { name: /Invoice export/ })).toBeVisible()
  await expect(list.getByRole('button', { name: /Memory: extract/ })).toHaveCount(0)
})
