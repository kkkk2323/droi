import { assistantMessage, session, userMessage } from '../e2e/fake-daemon/scenario'
import { streamedReply } from '../e2e/fake-daemon/turns'
import { expect, pairPhone, pickSession, presetStandIns, standIns, test } from './fixtures'

const chat = session('Chat', '/Users/dev/acme-web', [
  userMessage('hello'),
  assistantMessage('Hi! How can I help?'),
])

// A 1x1 PNG.
const PNG =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=='

test.use({
  scenario: {
    sessions: [chat],
    handlers: {
      'daemon.add_user_message': streamedReply({ deltas: ['Nice photo.'], delayMs: 50 }),
    },
  },
})

test('a picked image is shown, can be removed, and goes out with the message', async ({
  page,
  fakeDaemon,
}) => {
  await presetStandIns(page, {
    nextImages: [
      { name: 'first.png', mediaType: 'image/png', data: PNG },
      { name: 'second.png', mediaType: 'image/png', data: PNG },
    ],
  })
  await pairPhone(page, fakeDaemon)
  await pickSession(page, /Chat/)
  await page.getByRole('button', { name: 'Add image' }).click()
  await page.getByRole('menuitem', { name: 'Photo library' }).click()
  const attachments = page.getByRole('list', { name: 'Attachments' })
  await expect(attachments.getByRole('listitem')).toHaveCount(2)
  await page.getByRole('button', { name: 'Remove first.png' }).click()
  await expect(attachments.getByRole('listitem')).toHaveCount(1)

  // An image alone is enough to send.
  const send = page.getByRole('button', { name: 'Send' })
  await expect(send).toBeEnabled()
  await page.getByRole('textbox', { name: 'Message' }).fill('What is this?')
  await send.click()
  const sent = await fakeDaemon.waitForRequest('daemon.add_user_message')
  expect(sent.params).toMatchObject({
    text: 'What is this?',
    images: [{ type: 'base64', mediaType: 'image/png', data: PNG }],
  })
  await expect(attachments).toHaveCount(0)
  const you = page.getByRole('log', { name: 'Transcript' }).getByRole('article', { name: 'You' })
  await expect(you.last().getByRole('img', { name: 'Attached image' })).toBeVisible()
  expect((await standIns(page)).picked).toEqual(['library'])
})

test('the camera and the clipboard attach the same way; an empty clipboard says so', async ({
  page,
  fakeDaemon,
}) => {
  await pairPhone(page, fakeDaemon)
  await pickSession(page, /Chat/)
  await page.getByRole('button', { name: 'Add image' }).click()
  await page.getByRole('menuitem', { name: 'Paste image' }).click()
  await expect(page.getByRole('alert')).toHaveText('There is no image on the clipboard.')

  await page.evaluate((data) => {
    const scope = (window as unknown as { droiStandIns: { nextImages: unknown[] } }).droiStandIns
    scope.nextImages.push({ name: 'Photo 1', mediaType: 'image/png', data })
  }, PNG)
  await page.getByRole('button', { name: 'Add image' }).click()
  await page.getByRole('menuitem', { name: 'Take photo' }).click()
  await expect(page.getByRole('list', { name: 'Attachments' }).getByRole('listitem')).toHaveCount(1)
  await expect(page.getByRole('alert')).toHaveCount(0)
})
