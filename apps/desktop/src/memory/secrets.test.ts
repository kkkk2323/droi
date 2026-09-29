import { describe, expect, it } from 'vitest'
import { findSecret } from './secrets'

describe('findSecret', () => {
  it.each([
    ['an OpenAI key', 'use sk-proj-abcdefghijklmnopqrstuvwx1234 for the tests'],
    ['a Factory key', 'FACTORY_API_KEY is fk-AbCdEfGhIjKlMnOpQrStUv123456'],
    ['an AWS access key', 'the key AKIAIOSFODNN7EXAMPLE works'],
    ['a GitHub token', 'push with ghp_abcdefghijklmnopqrstuvwxyz0123456789'],
    ['a Slack token', 'xoxb-1234567890-abcdefghijkl'],
    ['a bearer token', 'Authorization: Bearer abcdefghijklmnopqrstuvwxyz.123456'],
    ['a JWT', 'token eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N'],
    ['a private key', '-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAA'],
    ['a password assignment', 'the staging db uses password=hunter2'],
    ['an api_key assignment', 'api_key: "0123456789abcdef0123"'],
  ])('refuses %s', (_label, text) => {
    expect(findSecret(text)).not.toBeNull()
  })

  it.each([
    'Run pnpm check && pnpm test before committing.',
    'The user prefers answers in Chinese.',
    'Passwords are hashed with argon2 in src/auth.',
    'Do not use the sk- prefix check in tests.',
    'The token budget is 200k.',
  ])('lets ordinary text through: %s', (text) => {
    expect(findSecret(text)).toBeNull()
  })

  it('names what it found without repeating it', () => {
    const reason = findSecret('password=hunter2')
    expect(reason).toMatch(/password/i)
    expect(reason).not.toContain('hunter2')
  })
})
