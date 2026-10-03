import { describe, expect, it } from 'vitest'
import { setPreferenceStorage } from './local-preference'
import { defaultToolMode, isToolMode, newSessionToolMode } from './tool-mode'

describe('newSessionToolMode', () => {
  it('asks for nothing and shows the draft when droid decides', () => {
    expect(newSessionToolMode(undefined, null, 'script_only')).toEqual({
      shown: 'script_only',
      requested: null,
    })
  })

  it('asks for the device default and shows it before the draft answers', () => {
    expect(newSessionToolMode(undefined, 'script_only', null)).toEqual({
      shown: 'script_only',
      requested: 'script_only',
    })
  })

  it('lets the page choice win over the default and the draft', () => {
    expect(newSessionToolMode('direct_only', 'script_only', 'script_only')).toEqual({
      shown: 'direct_only',
      requested: 'direct_only',
    })
  })

  it('shows nothing while droid decides and the draft has not answered', () => {
    expect(newSessionToolMode(undefined, null, null)).toEqual({ shown: null, requested: null })
  })
})

describe('the stored default', () => {
  it('reads only the modes droid knows and keeps the choice', () => {
    const values = new Map([['droi.toolExecutionMode', 'script_and_direct']])
    setPreferenceStorage({
      getItem: (key) => values.get(key) ?? null,
      setItem: (key, value) => void values.set(key, value),
      removeItem: (key) => void values.delete(key),
    })
    expect(isToolMode('direct_and_script')).toBe(true)
    expect(isToolMode('script_and_direct')).toBe(false)
    expect(defaultToolMode.get()).toBeNull()
    defaultToolMode.set('script_only')
    expect(values.get('droi.toolExecutionMode')).toBe('script_only')
    defaultToolMode.set(null)
    expect(values.get('droi.toolExecutionMode')).toBe('')
  })
})
