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

/** Image files in a paste or drop; other kinds are ignored. */
export function imageFiles(transfer: DataTransfer | null): File[] {
  if (!transfer) return []
  return Array.from(transfer.files).filter((file) => isImageMediaType(file.type))
}

export async function readImageAttachment(file: File, id: string): Promise<ImageAttachment> {
  const bytes = new Uint8Array(await file.arrayBuffer())
  let binary = ''
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }
  return {
    id,
    name: file.name || 'Pasted image',
    mediaType: file.type as ImageMediaType,
    data: btoa(binary),
  }
}

export function attachmentUrl(attachment: Pick<ImageAttachment, 'mediaType' | 'data'>): string {
  return `data:${attachment.mediaType};base64,${attachment.data}`
}
