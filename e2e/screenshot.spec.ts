// Not a test: renders the README screenshot from the Fake Daemon.
//   DROI_SCREENSHOT=1 pnpm test:e2e --project=desktop e2e/screenshot.spec.ts
import { mkdirSync } from 'node:fs'
import { expect, test } from './fixtures'
import {
  assistantMessage,
  session,
  thinkingBlock,
  userMessage,
  type MessageFixture,
} from './fake-daemon/scenario'

const OUT = process.env['DROI_SCREENSHOT_DIR'] ?? 'screenshot'

function history(): MessageFixture[] {
  // Fixture clocks advance per call, so build them in transcript order.
  const question = userMessage(
    'Login sometimes fails right after the token expires. Can you find out why?',
  )
  const read: MessageFixture = {
    ...assistantMessage(''),
    content: [
      thinkingBlock('The token refresh is not awaited; I should confirm before editing.', 1400),
      { type: 'tool_use', id: 'call_1', name: 'Read', input: { file_path: 'src/auth/login.ts' } },
    ],
  }
  const result: MessageFixture = {
    ...assistantMessage(''),
    role: 'tool',
    content: [
      {
        type: 'tool_result',
        toolUseId: 'call_1',
        content:
          'export async function login(user) {\n  refreshToken(user)\n  return session(user)\n}',
      },
    ],
  }
  return [
    question,
    read,
    result,
    assistantMessage(
      [
        '`login()` calls `refreshToken()` without awaiting it, so `session()` can run with the stale token.',
        '',
        'I will add the missing `await` and a regression test:',
        '',
        '```ts',
        'export async function login(user) {',
        '  await refreshToken(user)',
        '  return session(user)',
        '}',
        '```',
        '',
        '- `src/auth/login.ts`: await the refresh',
        '- `src/auth/login.test.ts`: cover the expired-token path',
      ].join('\n'),
    ),
  ]
}

test.use({
  scenario: {
    sessions: [
      session('Refactor invoice generator', '/Users/dev/billing-service', [
        userMessage('Refactor the invoice generator'),
      ]),
      session('Add dark mode toggle', '/Users/dev/acme-web', [userMessage('Add dark mode')]),
      session('Fix the login race', '/Users/dev/acme-web', history()),
    ],
  },
})

test('render screenshots', async ({ page, openClient, pickSession }) => {
  test.skip(!process.env['DROI_SCREENSHOT'], 'set DROI_SCREENSHOT=1 to render')
  mkdirSync(OUT, { recursive: true })
  await page.setViewportSize({ width: 1024, height: 640 })
  await openClient()
  await pickSession(/Fix the login race/)
  await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('regression test')
  await page.getByRole('textbox', { name: 'Message' }).blur()
  await page.waitForTimeout(300)
  await page.screenshot({ path: `${OUT}/page.png` })
})
