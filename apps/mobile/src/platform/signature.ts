// When the app's signature lapses, from the provisioning profile Xcode
// embedded in the bundle. A simulator build has none.
import { File, Paths } from 'expo-file-system'
import { latin1, profileExpiry } from '../lib/signature'

export async function signatureExpiry(): Promise<Date | null> {
  try {
    const profile = new File(Paths.bundle, 'embedded.mobileprovision')
    if (!profile.exists) return null
    return profileExpiry(latin1(await profile.bytes()))
  } catch {
    return null
  }
}
