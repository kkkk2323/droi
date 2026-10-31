// What Hermes lacks and the Phone App needs: the newer half of AbortSignal,
// which @factory/droid-sdk uses as it connects (platform/abort-signal.ts), and
// Web Crypto's random functions, which the shared daemon layer's uuid() needs.
// Expo's runtime already provides URL, TextDecoder and structuredClone.
import * as Crypto from 'expo-crypto'
import { installAbortSignalFixes } from './platform/abort-signal'

installAbortSignalFixes()

const scope = globalThis as { crypto?: { getRandomValues?: unknown; randomUUID?: unknown } }

if (typeof scope.crypto?.getRandomValues !== 'function') {
  scope.crypto = {
    ...scope.crypto,
    getRandomValues: Crypto.getRandomValues,
    randomUUID: Crypto.randomUUID,
  }
}
