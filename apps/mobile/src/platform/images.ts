// Images for the composer: picked from the library, taken with the camera or
// pasted. Each is re-encoded as a JPEG that fits the model's longest edge
// (an iPhone photo is HEIC and several MB, which the Daemon would count as
// hundreds of thousands of tokens).
import { MAX_IMAGE_EDGE, fitWithin, type ImageAttachment } from '@droi/daemon-layer/attachments'
import { uuid } from '@droi/daemon-layer/uuid'
import * as Clipboard from 'expo-clipboard'
import { ImageManipulator, SaveFormat } from 'expo-image-manipulator'
import * as ImagePicker from 'expo-image-picker'

export type ImageSource = 'library' | 'camera' | 'clipboard'

export class ImageSourceError extends Error {}

export async function pickImages(source: ImageSource): Promise<ImageAttachment[]> {
  if (source === 'clipboard') {
    const image = await Clipboard.getImageAsync({ format: 'jpeg' })
    if (!image) throw new ImageSourceError('There is no image on the clipboard.')
    return [await encode(image.data, image.size.width, image.size.height, 'Pasted image')]
  }
  if (source === 'camera') {
    const permission = await ImagePicker.requestCameraPermissionsAsync()
    if (!permission.granted)
      throw new ImageSourceError('Droi may not use the camera. Allow it in Settings.')
  }
  const result =
    source === 'camera'
      ? await ImagePicker.launchCameraAsync({ mediaTypes: ['images'], quality: 1 })
      : await ImagePicker.launchImageLibraryAsync({
          mediaTypes: ['images'],
          allowsMultipleSelection: true,
          selectionLimit: 5,
          quality: 1,
        })
  if (result.canceled) return []
  return Promise.all(
    result.assets.map((asset, index) =>
      encode(asset.uri, asset.width, asset.height, asset.fileName ?? `Photo ${index + 1}`),
    ),
  )
}

async function encode(
  uri: string,
  width: number,
  height: number,
  name: string,
): Promise<ImageAttachment> {
  const context = ImageManipulator.manipulate(uri)
  const [fitWidth, fitHeight] = fitWithin(width, height, MAX_IMAGE_EDGE)
  if (width > 0 && height > 0 && (fitWidth !== width || fitHeight !== height)) {
    context.resize({ width: fitWidth, height: fitHeight })
  }
  const image = await context.renderAsync()
  const saved = await image.saveAsync({ format: SaveFormat.JPEG, compress: 0.85, base64: true })
  return { id: uuid(), name, mediaType: 'image/jpeg', data: saved.base64 ?? '' }
}
