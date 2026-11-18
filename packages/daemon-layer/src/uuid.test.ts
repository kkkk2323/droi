import { afterEach, describe, expect, test } from 'vitest'
import { uuid } from './uuid'

const V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/
const randomUUID = crypto.randomUUID

describe('uuid', () => {
  afterEach(() => {
    Object.defineProperty(crypto, 'randomUUID', { value: randomUUID, configurable: true })
  })

  test('uses crypto.randomUUID when available', () => {
    expect(uuid()).toMatch(V4)
  })

  test('falls back to getRandomValues where randomUUID is missing (insecure contexts)', () => {
    Object.defineProperty(crypto, 'randomUUID', { value: undefined, configurable: true })
    const a = uuid()
    expect(a).toMatch(V4)
    expect(a).not.toBe(uuid())
  })
})
