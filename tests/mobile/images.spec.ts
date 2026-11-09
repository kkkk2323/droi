import { exchange } from './fake-kernel/history'
import { expect, pairPhone, pickSession, presetStandIns, standIns, test } from './fixtures'

// A 1x1 PNG.
const PNG =
  'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=='

test.use({
  scenario: {
    sessions: [
      { title: 'Chat', cwd: 'acme-web', entries: exchange('hello', 'Hi! How can I help?') },
    ],
  },
})

test('a picked image is shown, can be removed, and goes out with the message', async ({
  page,
  fakeKernel,
  phoneWire,
}) => {
  fakeKernel.model.push({ match: 'What is this?', text: 'Nice photo.' })
  await presetStandIns(page, {
    nextImages: [
      { name: 'first.png', mediaType: 'image/png', data: PNG },
      { name: 'second.png', mediaType: 'image/png', data: PNG },
    ],
  })
  await pairPhone(page, fakeKernel)
  await pickSession(page, /Chat/)
  await page.getByRole('button', { name: 'Add image' }).click()
  await page.getByRole('menuitem', { name: 'Photo library' }).click()
  const attachments = page.getByRole('list', { name: 'Attachments' })
  await expect(attachments.getByRole('listitem')).toHaveCount(2)
  // The remove button sits wholly inside the scrolling row, not cut off by it.
  const list = (await attachments.boundingBox())!
  const remove = (await page.getByRole('button', { name: 'Remove first.png' }).boundingBox())!
  expect(remove.y).toBeGreaterThanOrEqual(list.y)
  await page.getByRole('button', { name: 'Remove first.png' }).click()
  await expect(attachments.getByRole('listitem')).toHaveCount(1)

  // An image alone is enough to send.
  const send = page.getByRole('button', { name: 'Send' })
  await expect(send).toBeEnabled()
  await page.getByRole('textbox', { name: 'Message' }).fill('What is this?')
  await send.click()
  const submitted = await phoneWire.waitForCall('conversation.submit')
  expect(JSON.stringify(submitted)).toContain(PNG)
  await expect(attachments).toHaveCount(0)
  const you = page.getByRole('log', { name: 'Transcript' }).getByRole('article', { name: 'You' })
  await expect(
    you.filter({ hasText: 'What is this?' }).getByRole('img', { name: 'Attached image' }),
  ).toBeVisible()
  // The model was shown the picture.
  const turn = await fakeKernel.model.waitForTurn('What is this?')
  const asked = turn.messages.filter((m) => m.role === 'user').at(-1)!
  expect(JSON.stringify(asked.content)).toContain(`data:image/png;base64,${PNG}`)
  await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('Nice photo.')
  expect((await standIns(page)).picked).toEqual(['library'])
})

test('the camera and the clipboard attach the same way; an empty clipboard says so', async ({
  page,
  fakeKernel,
}) => {
  await pairPhone(page, fakeKernel)
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
