// Images attached in the composer, held as base64 until the message goes out
// (the Daemon takes image blocks inline, not by path). Reading and shrinking
// a picked image is up to each Client.

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

export function attachmentUrl(attachment: Pick<ImageAttachment, 'mediaType' | 'data'>): string {
  return `data:${attachment.mediaType};base64,${attachment.data}`
}
