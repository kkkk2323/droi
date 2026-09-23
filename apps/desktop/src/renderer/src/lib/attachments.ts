// Images pasted or dropped into the web Client's composer, read from the
// browser's files into the shared ImageAttachment shape.
import {
  MAX_IMAGE_BYTES,
  MAX_IMAGE_EDGE,
  fitWithin,
  isImageMediaType,
  type ImageAttachment,
  type ImageMediaType,
} from '@droi/daemon-layer/attachments'

/**
 * Image files in a paste or drop; other kinds are ignored. Some clipboards
 * (an image copied out of a browser, say) list the bitmap only under `items`,
 * with `files` empty, so both are read.
 */
export function imageFiles(transfer: DataTransfer | null): File[] {
  if (!transfer) return []
  const files = Array.from(transfer.files ?? [])
  if (files.length === 0 && transfer.items) {
    for (const item of Array.from(transfer.items)) {
      const file = item.kind === 'file' ? item.getAsFile() : null
      if (file) files.push(file)
    }
  }
  return files.filter((file) => isImageMediaType(file.type))
}

/** Re-encode an oversized image as a JPEG that fits MAX_IMAGE_EDGE; small ones pass through. */
export async function shrinkImage(file: File | Blob): Promise<Blob> {
  if (typeof createImageBitmap !== 'function' || typeof OffscreenCanvas !== 'function') return file
  let bitmap: ImageBitmap
  try {
    bitmap = await createImageBitmap(file)
  } catch {
    return file
  }
  try {
    const [width, height] = fitWithin(bitmap.width, bitmap.height, MAX_IMAGE_EDGE)
    const fits = width === bitmap.width && height === bitmap.height
    if (fits && file.size <= MAX_IMAGE_BYTES) return file
    const canvas = new OffscreenCanvas(width, height)
    const ctx = canvas.getContext('2d')
    if (!ctx) return file
    ctx.fillStyle = '#fff'
    ctx.fillRect(0, 0, width, height)
    ctx.drawImage(bitmap, 0, 0, width, height)
    return await canvas.convertToBlob({ type: 'image/jpeg', quality: 0.85 })
  } finally {
    bitmap.close()
  }
}

export async function readImageAttachment(file: File, id: string): Promise<ImageAttachment> {
  const shrunk = await shrinkImage(file)
  const bytes = new Uint8Array(await shrunk.arrayBuffer())
  let binary = ''
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }
  return {
    id,
    name: file.name || 'Pasted image',
    mediaType: (isImageMediaType(shrunk.type) ? shrunk.type : file.type) as ImageMediaType,
    data: btoa(binary),
  }
}
