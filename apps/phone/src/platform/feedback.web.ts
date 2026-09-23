// Web stand-in for alert sounds and haptics: records what would have played.
import { standIns } from './stand-ins'

export type AlertSound = 'finished' | 'needs-input'

export async function playSound(sound: AlertSound): Promise<void> {
  standIns().sounds.push(sound)
}

export function haptic(sound: AlertSound): void {
  standIns().haptics.push(sound)
}
