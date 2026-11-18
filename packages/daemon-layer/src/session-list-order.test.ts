import { describe, expect, test } from 'vitest'
import { createSessionListOrder, sortOrderOf, viewOf, withPin } from './session-list-order'

const config = { gatewayUrl: 'http://127.0.0.1:4000', pairingToken: 'tok' }
const ORDER = {
  pinnedSessions: ['s1'],
  pinnedWorkspaces: [],
  workspaceOrder: ['/w/b', '/w/a'],
  sessionSort: 'created',
  sessionView: '',
}

describe('createSessionListOrder', () => {
  test('reads the window’s order with the token', async () => {
    const urls: string[] = []
    const source = createSessionListOrder(config, async (input) => {
      urls.push(String(input))
      return Response.json(ORDER)
    })
    expect(await source.read()).toEqual(ORDER)
    expect(urls).toEqual(['http://127.0.0.1:4000/session-list?token=tok'])
  })

  test('a Gateway without a window, or none to reach, is null', async () => {
    expect(
      await createSessionListOrder(config, async () => new Response(null, { status: 404 })).read(),
    ).toBeNull()
    expect(
      await createSessionListOrder(config, async () => {
        throw new TypeError('Failed to fetch')
      }).read(),
    ).toBeNull()
  })

  test('pins on the computer, and says when it could not', async () => {
    const asked: Array<{ url: string; method: string | undefined }> = []
    const source = createSessionListOrder(config, async (input, init) => {
      asked.push({ url: String(input), method: init?.method })
      return new Response(null, { status: asked.length === 1 ? 204 : 500 })
    })
    await source.setPinned('workspace', '/w/a', true)
    expect(asked[0]).toEqual({
      url: 'http://127.0.0.1:4000/session-list/pin?token=tok&kind=workspace&key=%2Fw%2Fa&pinned=true',
      method: 'POST',
    })
    await expect(source.setPinned('session', 's1', false)).rejects.toThrow('HTTP 500')
  })
})

describe('sortOrderOf', () => {
  test('follows the window’s Workspace order and Session sort', () => {
    expect(sortOrderOf(ORDER, { s1: 5 })).toEqual({
      workspaces: 'manual',
      sessions: 'created',
      manual: ['/w/b', '/w/a'],
      firstSeen: { s1: 5 },
    })
  })

  test('falls back to the default order', () => {
    expect(sortOrderOf(null, {}).workspaces).toBe('sessions')
    expect(sortOrderOf({ ...ORDER, workspaceOrder: [], sessionSort: '' }, {})).toMatchObject({
      workspaces: 'sessions',
      sessions: 'recent',
    })
  })
})

test('viewOf follows the window’s view, by Workspace when it names none', () => {
  expect(viewOf({ ...ORDER, sessionView: 'time' })).toBe('time')
  expect(viewOf(ORDER)).toBe('workspaces')
  expect(viewOf({ ...ORDER, sessionView: 'tiles' })).toBe('workspaces')
  expect(viewOf(null)).toBe('workspaces')
})

test('withPin adds or takes out one key', () => {
  expect(withPin(ORDER, 'workspace', '/w/a', true).pinnedWorkspaces).toEqual(['/w/a'])
  expect(withPin(ORDER, 'session', 's1', false).pinnedSessions).toEqual([])
  expect(withPin(ORDER, 'session', 's1', true).pinnedSessions).toEqual(['s1'])
})
