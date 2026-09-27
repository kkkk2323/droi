import { describe, expect, it } from 'vitest'
import { localImagePath } from './local-image'

describe('localImagePath', () => {
  it('names the file behind an absolute path, undoing the percent-encoding', () => {
    expect(localImagePath('/Users/dev/shot.png')).toBe('/Users/dev/shot.png')
    expect(localImagePath('/Users/dev/a%20b/%E6%88%AA%E5%9B%BE.png')).toBe(
      '/Users/dev/a b/截图.png',
    )
    expect(localImagePath(' /tmp/x.png ')).toBe('/tmp/x.png')
    expect(localImagePath('/tmp/100%.png')).toBe('/tmp/100%.png')
  })

  it('leaves what the browser can load alone', () => {
    expect(localImagePath('https://example.com/a.png')).toBeNull()
    expect(localImagePath('data:image/png;base64,AAAA')).toBeNull()
    expect(localImagePath('blob:http://localhost/abc')).toBeNull()
    expect(localImagePath('//cdn.example.com/a.png')).toBeNull()
    expect(localImagePath('docs/a.png')).toBeNull()
    expect(localImagePath('')).toBeNull()
  })
})
