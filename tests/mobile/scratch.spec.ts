// Sessions without a Workspace on the phone: Workspace None starts one in a fresh Scratch Workspace the
// Gateway makes, listed under Recents (ADR 0008).
import { existsSync, readdirSync, writeFileSync } from 'node:fs'
import { join } from 'node:path'
import { userEntry } from './fake-kernel/history'
import type { FakeKernel } from './fake-kernel/fake-kernel'
import { expect, openNewSession, openSessionList, pairPhone, pickSession, test } from './fixtures'

/** The Scratch folders the Gateway has made and not thrown away. */
function scratchFolders(fakeKernel: FakeKernel): string[] {
  return readdirSync(fakeKernel.scratch).filter((name) => name !== '.trash')
}

function trashed(fakeKernel: FakeKernel, name: string): boolean {
  return existsSync(join(fakeKernel.scratch, '.trash', name))
}

test.describe('with no Workspace yet', () => {
  test('None is preselected and the Session lands under Recents', async ({
    page,
    fakeKernel,
    phoneWire,
  }) => {
    fakeKernel.model.push({ match: 'A pasta recipe for two', text: 'Sure.' })
    await pairPhone(page, fakeKernel)
    const form = await openNewSession(page)
    await expect(form.getByRole('heading', { name: 'What’s on your mind?' })).toBeVisible()
    await expect(form.getByRole('button', { name: 'Workspace' })).toHaveText(/Workspace: None/)
    await expect.poll(() => scratchFolders(fakeKernel)).toHaveLength(1)
    const folder = join(fakeKernel.scratch, scratchFolders(fakeKernel)[0]!)

    await form.getByRole('textbox', { name: 'Message' }).fill('A pasta recipe for two')
    await form.getByRole('button', { name: 'Start session' }).click()
    expect(await phoneWire.waitForCall('conversation.create')).toMatchObject({ kind: 'scratch' })
    await expect(page.getByRole('log', { name: 'Transcript' })).toContainText('Sure.')

    const list = await openSessionList(page)
    await expect(list.getByRole('group', { name: 'Recents' })).toBeVisible()
    // The Session keeps its folder.
    expect(existsSync(folder)).toBe(true)
    const rows = await fakeKernel.kernel.call<{
      items: Array<{ kind: string; workspace: string }>
    }>('conversation.list')
    expect(rows.items).toContainEqual(expect.objectContaining({ kind: 'scratch' }))
    expect(rows.items.find((r) => r.kind === 'scratch')!.workspace).toContain(
      fakeKernel.scratch.split('/').pop()!,
    )
  })
})

test.describe('next to Workspaces', () => {
  test.use({
    scenario: {
      sessions: [
        {
          title: 'Pasta recipe',
          kind: 'scratch',
          cwd: '2026-09-20-aaaaaa',
          entries: [userEntry('b')],
        },
        { title: 'Fix the build', cwd: 'acme-web', entries: [userEntry('a')] },
      ],
    },
  })

  test('Recents sits last; None makes a folder and leaving throws the empty one away', async ({
    page,
    fakeKernel,
  }) => {
    await pairPhone(page, fakeKernel)
    const form = await openNewSession(page)
    await expect(form.getByRole('button', { name: 'Workspace' })).toHaveText(/acme-web\?/)
    await form.getByRole('button', { name: 'Workspace' }).click()
    const sheet = page.getByRole('dialog', { name: 'Workspace' })
    await expect(sheet.getByRole('radio')).toHaveText(['None', 'acme-web'])
    await sheet.getByRole('radio', { name: 'None' }).click()
    await expect(form.getByRole('heading', { name: 'What’s on your mind?' })).toBeVisible()
    await expect.poll(() => scratchFolders(fakeKernel)).toHaveLength(2)
    const made = scratchFolders(fakeKernel).find((name) => name !== '2026-09-20-aaaaaa')!

    const list = await openSessionList(page)
    // As in the computer's sidebar: the Workspaces under their own label, Recents after them.
    await expect(list.getByRole('group')).toHaveCount(3)
    await expect(list.getByRole('group').first()).toHaveAccessibleName('Workspaces')
    await expect(
      list.getByRole('group', { name: 'Workspaces' }).getByRole('group', { name: 'acme-web' }),
    ).toBeVisible()
    await expect(list.getByRole('group').last()).toHaveAccessibleName('Recents')
    await pickSession(page, /Fix the build/)
    // Nothing was written in it, so the Gateway removes it rather than filling the Trash.
    await expect.poll(() => scratchFolders(fakeKernel)).toEqual(['2026-09-20-aaaaaa'])
    expect(trashed(fakeKernel, made)).toBe(false)
  })

  test('archiving a Scratch Session moves its folder to the Trash', async ({
    page,
    fakeKernel,
    phoneWire,
  }) => {
    writeFileSync(join(fakeKernel.scratch, '2026-09-20-aaaaaa', 'recipe.md'), 'Pasta\n')
    await pairPhone(page, fakeKernel)
    const list = await openSessionList(page)
    await list.getByRole('button', { name: /Pasta recipe/ }).click({ delay: 800 })
    await page
      .getByRole('dialog', { name: /^Actions for / })
      .getByRole('button', { name: 'Archive' })
      .click()
    expect(await phoneWire.waitForCall('index.update')).toMatchObject({ archived: true })
    await expect.poll(() => trashed(fakeKernel, '2026-09-20-aaaaaa')).toBe(true)
    expect(existsSync(join(fakeKernel.scratch, '.trash', '2026-09-20-aaaaaa', 'recipe.md'))).toBe(
      true,
    )
  })
})
