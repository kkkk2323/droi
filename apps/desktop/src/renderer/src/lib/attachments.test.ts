import { describe, expect, test } from 'vitest'
import { attachmentUrl } from '@droi/daemon-layer/attachments'
import { imageFiles, readImageAttachment } from './attachments'

describe('attachments', () => {
  test('imageFiles keeps only image files and tolerates no transfer', () => {
    const png = new File([new Uint8Array([1, 2, 3])], 'a.png', { type: 'image/png' })
    const txt = new File(['hi'], 'a.txt', { type: 'text/plain' })
    const transfer = { files: [png, txt] } as unknown as DataTransfer
    expect(imageFiles(transfer)).toEqual([png])
    expect(imageFiles(null)).toEqual([])
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
