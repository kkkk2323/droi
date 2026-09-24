import { describe, expect, test } from 'vitest'
import { attachmentUrl } from '@droi/daemon-layer/attachments'
import { pathsAsText, readImageAttachment, transferFiles } from './attachments'

describe('attachments', () => {
  test('transferFiles splits images from other files and tolerates no transfer', () => {
    const png = new File([new Uint8Array([1, 2, 3])], 'a.png', { type: 'image/png' })
    const txt = new File(['hi'], 'a.txt', { type: 'text/plain' })
    const html = new File(['<p>'], 'a.html', { type: 'text/html' })
    const transfer = { files: [png, txt, html] } as unknown as DataTransfer
    expect(transferFiles(transfer)).toEqual({ images: [png], others: [txt, html] })
    expect(transferFiles(null)).toEqual({ images: [], others: [] })
  })

  test('pathsAsText joins full paths, quotes ones with spaces and skips pathless files', () => {
    const files = ['a.html', 'b c.txt', 'pasted'].map((name) => new File([''], name))
    const paths: Record<string, string> = {
      'a.html': '/Users/dev/site/a.html',
      'b c.txt': '/Users/dev/My Docs/b c.txt',
      pasted: '',
    }
    expect(pathsAsText(files, (file) => paths[file.name] ?? '')).toBe(
      '/Users/dev/site/a.html "/Users/dev/My Docs/b c.txt"',
    )
  })

  test('readImageAttachment encodes the bytes as base64 and names unnamed pastes', async () => {
    const file = new File([new Uint8Array([0, 255, 16])], '', { type: 'image/jpeg' })
    const attachment = await readImageAttachment(file, 'id-1')
    expect(attachment).toEqual({
      id: 'id-1',
      name: 'Pasted image',
      mediaType: 'image/jpeg',
      data: Buffer.from([0, 255, 16]).toString('base64'),
    })
    expect(attachmentUrl(attachment)).toBe(`data:image/jpeg;base64,${attachment.data}`)
  })
})
