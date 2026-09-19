import test from 'node:test'
import assert from 'node:assert/strict'
import { promptSig, redactText, sha256Hex } from '../src/backend/diagnostics/redact.ts'

test('redactText masks authKey query params', () => {
  const input = 'http://127.0.0.1:3001/?authKey=abcdef123&x=1'
  const out = redactText(input)
  assert.match(out, /authKey=.*REDACTED/i)
  assert.doesNotMatch(out, /authKey=abcdef123/)
})

test('promptSig is stable and includes head/tail', () => {
  const text = 'hello world'
  const a = promptSig(text)
  const b = promptSig(text)
  assert.equal(a.promptLen, text.length)
  assert.equal(a.promptSha256, b.promptSha256)
  assert.equal(a.promptSha256, sha256Hex(text))
  assert.equal(a.promptHead, text)
  assert.equal(a.promptTail, text)
})
