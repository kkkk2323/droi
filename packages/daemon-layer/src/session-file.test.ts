import { describe, expect, test } from 'vitest'
import { createSessionFileLookup, sessionDetails } from './session-file'

const SESSION = {
  sessionId: 'a1b2c3',
  title: 'Fix the login bug',
  cwd: '/Users/dev/acme-web',
}

describe('sessionDetails', () => {
  test('lists the title, id, Workspace and transcript', () => {
    expect(sessionDetails(SESSION, '/Users/dev/.factory/sessions/-Users-dev-acme-web/a1b2c3.jsonl'))
      .toBe(`Title: Fix the login bug
Session ID: a1b2c3
Workspace: /Users/dev/acme-web
Transcript: /Users/dev/.factory/sessions/-Users-dev-acme-web/a1b2c3.jsonl`)
  })

  test('leaves out what is unknown', () => {
    expect(sessionDetails({ ...SESSION, cwd: null }, null)).toBe(
      'Title: Fix the login bug\nSession ID: a1b2c3',
    )
  })
})

describe('createSessionFileLookup', () => {
  const config = { gatewayUrl: 'http://127.0.0.1:4000', pairingToken: 'tok' }

  test('asks the Gateway with the token and the Session id', async () => {
    const urls: string[] = []
    const lookup = createSessionFileLookup(config, async (input) => {
      urls.push(String(input))
      return Response.json({ path: '/x/a1b2c3.jsonl' })
    })
    expect(await lookup('a1b2c3')).toBe('/x/a1b2c3.jsonl')
    expect(urls).toEqual(['http://127.0.0.1:4000/session-file?token=tok&sessionId=a1b2c3'])
  })

  test('a missing file or an unreachable Gateway is null', async () => {
    expect(
      await createSessionFileLookup(config, async () => new Response(null, { status: 404 }))('a'),
    ).toBeNull()
    expect(
      await createSessionFileLookup(config, async () => {
        throw new TypeError('Failed to fetch')
      })('a'),
    ).toBeNull()
  })
})
