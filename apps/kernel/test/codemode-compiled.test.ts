import { expect, test } from 'bun:test'
import { compiledIn } from '../src/codemode/sandbox.ts'

// A compiled Kernel starts codemode's worker as an entrypoint of its own build, so it must know it is one: on
// Windows its URL spells the embedded folder `~BUN` as `%7EBUN`.
test('a compiled Kernel knows it is one on every platform, and a source checkout knows it is not', () => {
  expect(compiledIn('/$bunfs/root/main.js', 'file:///$bunfs/root/main.js')).toBe(true)
  expect(compiledIn('B:\\~BUN\\root\\main.js', 'file:///B:/%7EBUN/root/main.js')).toBe(true)
  expect(compiledIn('', 'file:///B:/%7EBUN/root/main.js')).toBe(true)
  expect(compiledIn('B:\\~BUN\\root\\main.js', '')).toBe(true)
  expect(
    compiledIn(
      '/Users/me/droi/apps/kernel/src/codemode/sandbox.ts',
      'file:///Users/me/droi/apps/kernel/src/codemode/sandbox.ts',
    ),
  ).toBe(false)
  expect(compiledIn(import.meta.path, import.meta.url)).toBe(false)
})
