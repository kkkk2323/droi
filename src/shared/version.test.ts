import { expect, test } from 'vitest'
import { APP_NAME } from './version'

test('app name', () => {
  expect(APP_NAME).toBe('Droi')
})
