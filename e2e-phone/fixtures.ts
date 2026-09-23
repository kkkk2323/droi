// Fixtures for the Phone App's web build: the web Client's Fake Daemon and
// Scenarios, plus access to the stand-ins for the phone's hardware.
import type { Page } from '@playwright/test'
import { test, expect } from '../e2e/fixtures'

/** What the Phone App's stand-ins recorded (see apps/phone/src/platform/stand-ins.ts). */
export interface StandIns {
  haptics: string[]
  sounds: string[]
  signatureExpiry: string | null
}

export function standIns(page: Page): Promise<StandIns> {
  return page.evaluate(
    () =>
      (window as unknown as { droiStandIns?: StandIns }).droiStandIns ?? {
        haptics: [],
        sounds: [],
        signatureExpiry: null,
      },
  )
}

/** Set stand-in values before the app starts. */
export async function presetStandIns(page: Page, values: Partial<StandIns>): Promise<void> {
  await page.addInitScript((preset) => {
    Object.assign(window, { droiStandIns: preset })
  }, values)
}

export { test, expect }
