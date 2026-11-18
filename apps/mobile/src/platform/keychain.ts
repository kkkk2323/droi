// Secrets (Pairing Tokens) in the iPhone keychain, readable only on this
// device and only while it is unlocked.
import * as SecureStore from 'expo-secure-store'

const OPTIONS: SecureStore.SecureStoreOptions = {
  keychainAccessible: SecureStore.WHEN_UNLOCKED_THIS_DEVICE_ONLY,
}

export const keychain = {
  get: (key: string): Promise<string | null> => SecureStore.getItemAsync(key, OPTIONS),
  set: (key: string, value: string): Promise<void> => SecureStore.setItemAsync(key, value, OPTIONS),
  delete: (key: string): Promise<void> => SecureStore.deleteItemAsync(key, OPTIONS),
}
