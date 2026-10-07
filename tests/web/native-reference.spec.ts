// Not a test: records the Local Client's look as the target for the native
// app (apps/native). Each screen is a 2x screenshot plus the computed style
// of every visible element, written to apps/native/testdata/reference.
//   DROI_NATIVE_REF=1 pnpm test:e2e --project=desktop native-reference.spec.ts
import { mkdirSync, writeFileSync } from 'node:fs'
import { resolve } from 'node:path'
import type { Page } from '@playwright/test'
import { expect, openLocalClient, pickSession, test } from './fixtures'
import {
  assistantMessage,
  session,
  thinkingBlock,
  userMessage,
  type MessageFixture,
} from '../fake-daemon/scenario'
import { askUserTurn, permissionTurn } from '../fake-daemon/turns'

const OUT = resolve(import.meta.dirname, '../../apps/native/testdata/reference')
const WIDTH = 1024
const HEIGHT = 640

function history(): MessageFixture[] {
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

const sessions = [
  session('Refactor invoice generator', '/Users/dev/billing-service', [
    userMessage('Refactor the invoice generator'),
  ]),
  session('Add dark mode toggle', '/Users/dev/acme-web', [userMessage('Add dark mode')]),
  session('Fix the login race', '/Users/dev/acme-web', history()),
]

async function capture(page: Page, name: string): Promise<void> {
  // Nothing focused, no caret blinking, no hover left over.
  await page.evaluate(() => (document.activeElement as HTMLElement | null)?.blur())
  await page.mouse.move(WIDTH - 1, HEIGHT - 1)
  await page.waitForTimeout(400)
  await page.screenshot({ path: `${OUT}/${name}.png` })
  const dump = await page.evaluate(
    ({ height }) => {
      const round = (n: number) => Math.round(n * 10) / 10
      const out: unknown[] = []
      for (const el of Array.from(document.querySelectorAll('body *'))) {
        const r = el.getBoundingClientRect()
        if (r.width === 0 || r.height === 0 || r.bottom < 0 || r.top > height) continue
        const s = getComputedStyle(el)
        if (s.visibility === 'hidden' || s.display === 'none') continue
        if (
          ['path', 'circle', 'rect', 'line', 'polyline', 'polygon', 'ellipse'].includes(el.tagName)
        )
          continue
        const text = Array.from(el.childNodes)
          .filter((n) => n.nodeType === 3)
          .map((n) => n.textContent)
          .join('')
          .trim()
          .slice(0, 60)
        out.push({
          tag: el.tagName.toLowerCase(),
          role: el.getAttribute('role') ?? undefined,
          label: el.getAttribute('aria-label') ?? undefined,
          text: text || undefined,
          icon:
            el.tagName === 'svg'
              ? (el.getAttribute('class') ?? '')
                  .split(' ')
                  .find((c) => c.startsWith('lucide-') && c !== 'lucide-icon')
              : undefined,
          box: [round(r.x), round(r.y), round(r.width), round(r.height)],
          font: `${s.fontWeight} ${s.fontSize}/${s.lineHeight} ${s.fontFamily.split(',')[0]}`,
          ls: s.letterSpacing === 'normal' ? undefined : s.letterSpacing,
          color: s.color,
          bg: s.backgroundColor === 'rgba(0, 0, 0, 0)' ? undefined : s.backgroundColor,
          radius: s.borderRadius === '0px' ? undefined : s.borderRadius,
          border:
            s.borderTopWidth === '0px' &&
            s.borderRightWidth === '0px' &&
            s.borderBottomWidth === '0px' &&
            s.borderLeftWidth === '0px'
              ? undefined
              : `${s.borderTopWidth} ${s.borderRightWidth} ${s.borderBottomWidth} ${s.borderLeftWidth} ${s.borderTopColor}`,
          pad: s.padding === '0px' ? undefined : s.padding,
          shadow: s.boxShadow === 'none' ? undefined : s.boxShadow,
          opacity: s.opacity === '1' ? undefined : s.opacity,
        })
      }
      return out
    },
    { height: HEIGHT },
  )
  writeFileSync(`${OUT}/${name}.json`, `${JSON.stringify(dump, null, 1)}\n`)
}

async function open(page: Page, fakeDaemon: Parameters<typeof openLocalClient>[1], theme?: string) {
  if (theme) {
    await page.addInitScript((t) => localStorage.setItem('droi.theme', t), theme)
  }
  await page.setViewportSize({ width: WIDTH, height: HEIGHT })
  await openLocalClient(page, fakeDaemon)
}

test.use({ deviceScaleFactor: 2 })
test.skip(!process.env['DROI_NATIVE_REF'], 'set DROI_NATIVE_REF=1 to record')
test.beforeAll(() => mkdirSync(OUT, { recursive: true }))

test.describe('a Session', () => {
  test.use({ scenario: { sessions } })

  for (const theme of ['light', 'dark', 'solarized-light']) {
    test(`session ${theme}`, async ({ page, fakeDaemon }) => {
      await open(page, fakeDaemon, theme)
      await pickSession(page, /Fix the login race/)
      await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('regression test')
      await capture(page, `session-${theme}`)
    })
  }

  test('tool call open', async ({ page, fakeDaemon }) => {
    await open(page, fakeDaemon)
    await pickSession(page, /Fix the login race/)
    await page.getByRole('button', { name: /Reasoned for/ }).click()
    await page.getByRole('button', { name: /^Read/ }).first().click()
    await capture(page, 'session-expanded')
  })

  test('new session', async ({ page, fakeDaemon }) => {
    await open(page, fakeDaemon)
    await page.getByRole('button', { name: 'New session' }).first().click()
    await expect(page.getByRole('button', { name: 'Start session' })).toBeVisible()
    await capture(page, 'new-session')
  })

  test('settings', async ({ page, fakeDaemon }) => {
    await open(page, fakeDaemon)
    await page.getByRole('button', { name: 'Settings' }).click()
    await expect(page.getByRole('heading', { name: 'Account' })).toBeVisible()
    await capture(page, 'settings')
    for (const section of ['General', 'Session defaults', 'Notifications', 'Advanced', 'About']) {
      await page
        .getByRole('navigation', { name: 'Settings sections' })
        .getByRole('button', { name: section })
        .click()
      await expect(page.getByRole('heading', { name: section, level: 2 })).toBeVisible()
      await capture(page, `settings-${section.toLowerCase().replace(/ /g, '-')}`)
    }
  })

  test('model picker', async ({ page, fakeDaemon }) => {
    await open(page, fakeDaemon)
    await pickSession(page, /Fix the login race/)
    await page.getByRole('button', { name: 'Model and reasoning effort' }).click()
    await expect(page.getByRole('dialog', { name: 'Choose a model' })).toBeVisible()
    await page.mouse.move(WIDTH - 1, HEIGHT - 1)
    await page.waitForTimeout(400)
    await page.screenshot({ path: `${OUT}/model-picker.png` })
  })

  test('sidebar hidden', async ({ page, fakeDaemon }) => {
    await open(page, fakeDaemon)
    await pickSession(page, /Fix the login race/)
    await page.getByRole('button', { name: 'Hide sidebar' }).click()
    await capture(page, 'sidebar-hidden')
  })
})

test.describe('a permission request', () => {
  test.use({
    scenario: {
      sessions: [session('Deploy', '/Users/dev/acme-web', [userMessage('hi')])],
      handlers: {
        'daemon.add_user_message': permissionTurn({
          command: 'rm -rf build',
          reply: 'Build directory removed.',
        }),
      },
    },
  })
  test('permission', async ({ page, fakeDaemon }) => {
    await open(page, fakeDaemon)
    await pickSession(page, /Deploy/)
    await page.getByRole('textbox', { name: 'Message' }).fill('Clean the build dir')
    await page.getByRole('button', { name: 'Send' }).click()
    await expect(page.getByRole('group', { name: 'Permission request: Execute' })).toBeVisible()
    await capture(page, 'permission')
  })
})

test.describe('an ask-user question', () => {
  test.use({
    scenario: {
      sessions: [session('Deploy', '/Users/dev/acme-web', [userMessage('hi')])],
      handlers: {
        'daemon.add_user_message': askUserTurn({
          question: 'Which environment?',
          options: ['staging', 'production'],
        }),
      },
    },
  })
  test('ask user', async ({ page, fakeDaemon }) => {
    await open(page, fakeDaemon)
    await pickSession(page, /Deploy/)
    await page.getByRole('textbox', { name: 'Message' }).fill('Ship it')
    await page.getByRole('button', { name: 'Send' }).click()
    await expect(page.getByText('Which environment?')).toBeVisible()
    await capture(page, 'ask-user')
  })
})
