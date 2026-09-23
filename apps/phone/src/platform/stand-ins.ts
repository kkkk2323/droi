// What the web stand-ins for the phone's hardware record, on
// `window.droiStandIns`, for the Playwright suite to read and drive. Only the
// `*.web.ts` platform modules use this; the web build exists for tests only.

export interface StandIns {
  haptics: string[]
  sounds: string[]
  /** What the provisioning profile would say; null as on a simulator. */
  signatureExpiry: string | null
}

export function standIns(): StandIns {
  const scope = globalThis as unknown as { droiStandIns?: Partial<StandIns> }
  const current = scope.droiStandIns ?? {}
  const filled: StandIns = {
    haptics: current.haptics ?? [],
    sounds: current.sounds ?? [],
    signatureExpiry: current.signatureExpiry ?? null,
  }
  scope.droiStandIns = filled
  return filled
}
