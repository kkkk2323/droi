// What the composer held when the user left a Session. The text survives a
// reload (localStorage, per browser); pasted images are kept only in memory,
// as their base64 would not fit in localStorage.
import type { ImageAttachment } from './attachments'

export interface Draft {
  text: string
  images: ImageAttachment[]
}

const PREFIX = 'droi.draft.'
const images = new Map<string, ImageAttachment[]>()

export function loadDraft(key: string): Draft {
  let text = ''
  try {
    text = localStorage.getItem(PREFIX + key) ?? ''
  } catch {
    // Private mode: nothing stored.
  }
  return { text, images: images.get(key) ?? [] }
}

export function saveDraft(key: string, draft: Draft): void {
  if (draft.images.length > 0) images.set(key, draft.images)
  else images.delete(key)
  try {
    if (draft.text) localStorage.setItem(PREFIX + key, draft.text)
    else localStorage.removeItem(PREFIX + key)
  } catch {
    // Private mode: the draft still holds while the page lives.
  }
}
