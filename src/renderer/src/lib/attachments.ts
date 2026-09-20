// Images pasted or dropped into the composer, held as base64 until the
// message goes out (the Daemon takes image blocks inline, not by path).

export const IMAGE_MEDIA_TYPES = ['image/jpeg', 'image/png', 'image/gif', 'image/webp'] as const
export type ImageMediaType = (typeof IMAGE_MEDIA_TYPES)[number]

export interface ImageAttachment {
  id: string
  name: string
  mediaType: ImageMediaType
  /** Base64 without the data-URL prefix, as the Daemon wants it. */
  data: string
}

export function isImageMediaType(type: string): type is ImageMediaType {
  return (IMAGE_MEDIA_TYPES as readonly string[]).includes(type)
}

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

/** Longest edge the model can use; Anthropic downsamples anything larger anyway. */
export const MAX_IMAGE_EDGE = 1568
/**
 * Above this the image is re-encoded. The Daemon estimates context as
 * characters / 4 and counts base64 image data at full length, so a 2 MB
 * screenshot reads as ~700k tokens and trips compaction; kept small it does not.
 */
export const MAX_IMAGE_BYTES = 300_000

/** Scale (w, h) down to fit `max` on the longest edge, never up. */
export function fitWithin(width: number, height: number, max: number): [number, number] {
  const longest = Math.max(width, height)
  if (longest <= max) return [width, height]
  const scale = max / longest
  return [Math.max(1, Math.round(width * scale)), Math.max(1, Math.round(height * scale))]
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

export function attachmentUrl(attachment: Pick<ImageAttachment, 'mediaType' | 'data'>): string {
  return `data:${attachment.mediaType};base64,${attachment.data}`
}
