// Session alerts, after Factory App: a sound when Droid finishes or needs an
// answer, a desktop notification when that happens out of sight, and an
// unread mark in the sidebar. Sounds and notifications need the Desktop
// Shell, so only the Local Client plays them.
import type { AlertsBridge, BuiltinSound } from '@shared/alerts'
import { createPreference } from './local-preference'

export type AlertEvent = 'completion' | 'awaiting-input'

export type SoundChoice = 'off' | 'bell' | BuiltinSound | 'custom'
export const SOUND_CHOICES: SoundChoice[] = ['off', 'bell', 'fx-ok01', 'fx-ack01', 'custom']

/** When sounds play, relative to whether the Droi window has focus. */
export type FocusMode = 'always' | 'focused' | 'unfocused'
export const FOCUS_MODES: FocusMode[] = ['always', 'focused', 'unfocused']

export interface AlertPreferences {
  completionSound: SoundChoice
  awaitingInputSound: SoundChoice
  /** The files picked for "Custom…", per event. */
  customCompletionSound: string | null
  customAwaitingInputSound: string | null
  focusMode: FocusMode
  notifyOnComplete: boolean
  notifyOnWaitingForInput: boolean
}

/** Factory App's defaults. */
export const DEFAULT_ALERT_PREFERENCES: AlertPreferences = {
  completionSound: 'fx-ok01',
  awaitingInputSound: 'fx-ack01',
  customCompletionSound: null,
  customAwaitingInputSound: null,
  focusMode: 'always',
  notifyOnComplete: true,
  notifyOnWaitingForInput: true,
}

export function parseAlertPreferences(value: unknown): AlertPreferences {
  const v = (value && typeof value === 'object' ? value : {}) as Record<string, unknown>
  const pick = <T>(key: string, allowed: readonly T[], fallback: T): T =>
    allowed.includes(v[key] as T) ? (v[key] as T) : fallback
  const path = (key: string) => (typeof v[key] === 'string' && v[key] ? (v[key] as string) : null)
  const flag = (key: string, fallback: boolean) =>
    typeof v[key] === 'boolean' ? (v[key] as boolean) : fallback
  const d = DEFAULT_ALERT_PREFERENCES
  return {
    completionSound: pick('completionSound', SOUND_CHOICES, d.completionSound),
    awaitingInputSound: pick('awaitingInputSound', SOUND_CHOICES, d.awaitingInputSound),
    customCompletionSound: path('customCompletionSound'),
    customAwaitingInputSound: path('customAwaitingInputSound'),
    focusMode: pick('focusMode', FOCUS_MODES, d.focusMode),
    notifyOnComplete: flag('notifyOnComplete', d.notifyOnComplete),
    notifyOnWaitingForInput: flag('notifyOnWaitingForInput', d.notifyOnWaitingForInput),
  }
}

export const alertPreferences = createPreference<AlertPreferences>(
  'droi.alerts',
  DEFAULT_ALERT_PREFERENCES,
  { parse: (raw) => parseAlertPreferences(JSON.parse(raw)), serialize: JSON.stringify },
)

/**
 * Follows each Session's working state and says when it calls for an alert,
 * the way Factory App does: once when it starts waiting for an answer, and
 * once when it goes idle after doing something.
 */
export class AlertTracker {
  #active = new Set<string>()
  #awaiting = new Set<string>()

  update(sessionId: string, workingState: string): AlertEvent | null {
    if (workingState === 'idle') {
      this.#awaiting.delete(sessionId)
      return this.#active.delete(sessionId) ? 'completion' : null
    }
    this.#active.add(sessionId)
    if (workingState !== 'waiting_for_tool_confirmation') return null
    if (this.#awaiting.has(sessionId)) return null
    this.#awaiting.add(sessionId)
    return 'awaiting-input'
  }

  /** The Session dropped out of this Client (a reconnect, a close); its next state is no news. */
  forget(sessionId: string): void {
    this.#active.delete(sessionId)
    this.#awaiting.delete(sessionId)
  }
}

export function soundGateAllows(mode: FocusMode, focused: boolean): boolean {
  return mode === 'always' || (mode === 'focused') === focused
}

const cachedSounds = new Map<string, Promise<string | null>>()

function soundUrl(
  bridge: AlertsBridge,
  choice: BuiltinSound | 'custom',
  customPath: string | null,
): Promise<string | null> {
  if (choice === 'custom') {
    if (!customPath) return Promise.resolve(null)
    // Not cached: the file may be replaced under the same name.
    return bridge.readSoundFile(customPath)
  }
  let url = cachedSounds.get(choice)
  if (!url) {
    url = bridge.builtinSound(choice).catch(() => null)
    cachedSounds.set(choice, url)
  }
  return url
}

/**
 * Plays the sound chosen for the event. `test` skips the focus gate, for the
 * Settings page's Test button. A built-in or custom sound that cannot be read
 * falls back to the bell.
 */
export async function playAlertSound(
  bridge: AlertsBridge,
  preferences: AlertPreferences,
  event: AlertEvent,
  { test = false }: { test?: boolean } = {},
): Promise<void> {
  if (!test && !soundGateAllows(preferences.focusMode, document.hasFocus())) return
  const choice =
    event === 'completion' ? preferences.completionSound : preferences.awaitingInputSound
  if (choice === 'off') return
  if (choice === 'bell') return playBell()
  const custom =
    event === 'completion'
      ? preferences.customCompletionSound
      : preferences.customAwaitingInputSound
  const url = await soundUrl(bridge, choice, custom)
  if (!url) return playBell()
  try {
    await new Audio(url).play()
  } catch (cause) {
    console.warn('Alert sound did not play', cause)
  }
}

let audioContext: AudioContext | null = null

/** Factory App's bell: a short 880 Hz sine. */
async function playBell(): Promise<void> {
  try {
    audioContext ??= new AudioContext()
    if (audioContext.state !== 'running') await audioContext.resume()
    const oscillator = audioContext.createOscillator()
    const gain = audioContext.createGain()
    oscillator.type = 'sine'
    oscillator.frequency.value = 880
    gain.gain.value = 0.15
    oscillator.connect(gain)
    gain.connect(audioContext.destination)
    const start = audioContext.currentTime
    oscillator.start(start)
    oscillator.stop(start + 0.18)
  } catch (cause) {
    console.warn('Alert bell did not play', cause)
  }
}
