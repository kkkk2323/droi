// Web stand-in for the keychain: sessionStorage, so it survives a reload
// (a relaunch in the tests) and stays apart from app storage (localStorage).
const PREFIX = 'droi.keychain.'

export const keychain = {
  get: async (key: string): Promise<string | null> => sessionStorage.getItem(PREFIX + key),
  set: async (key: string, value: string): Promise<void> =>
    sessionStorage.setItem(PREFIX + key, value),
  delete: async (key: string): Promise<void> => sessionStorage.removeItem(PREFIX + key),
}
