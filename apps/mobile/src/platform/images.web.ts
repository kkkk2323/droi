// Web stand-in for the photo library, camera and clipboard: hands out the
// images a test put in window.droiStandIns.nextImages and records the source.
import type { ImageAttachment } from '@droi/daemon-layer/attachments'
import { uuid } from '@droi/daemon-layer/uuid'
import { standIns } from './stand-ins'

export type ImageSource = 'library' | 'camera' | 'clipboard'

export class ImageSourceError extends Error {}

export async function pickImages(source: ImageSource): Promise<ImageAttachment[]> {
  const scope = standIns()
  scope.picked.push(source)
  const images = scope.nextImages.splice(0)
  if (source === 'clipboard' && images.length === 0) {
    throw new ImageSourceError('There is no image on the clipboard.')
  }
  return images.map((image) => ({ ...image, id: uuid() }))
}
