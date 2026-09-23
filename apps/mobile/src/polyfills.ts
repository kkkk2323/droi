// Web Crypto's random functions, which Hermes lacks and the shared daemon
// layer's uuid() needs. Expo's runtime already provides URL, TextDecoder and
// structuredClone.
import * as Crypto from 'expo-crypto'

const scope = globalThis as { crypto?: { getRandomValues?: unknown; randomUUID?: unknown } }

if (typeof scope.crypto?.getRandomValues !== 'function') {
  scope.crypto = {
    ...scope.crypto,
    getRandomValues: Crypto.getRandomValues,
    randomUUID: Crypto.randomUUID,
  }
}
