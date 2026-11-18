// A Session alert's sound and haptic. Sounds play through the ambient audio
// session, so the silent switch mutes them.
import { createAudioPlayer, setAudioModeAsync, type AudioPlayer } from 'expo-audio'
import * as Haptics from 'expo-haptics'

export type AlertSound = 'finished' | 'needs-input'

const SOURCES: Record<AlertSound, number> = {
  finished: require('../../assets/sounds/finished.wav'),
  'needs-input': require('../../assets/sounds/needs-input.wav'),
}

const players = new Map<AlertSound, AudioPlayer>()
let modeSet: Promise<void> | null = null

export async function playSound(sound: AlertSound): Promise<void> {
  modeSet ??= setAudioModeAsync({ playsInSilentMode: false, interruptionMode: 'mixWithOthers' })
  await modeSet
  let player = players.get(sound)
  if (!player) {
    player = createAudioPlayer(SOURCES[sound])
    players.set(sound, player)
  }
  await player.seekTo(0)
  player.play()
}

export function haptic(sound: AlertSound): void {
  void Haptics.notificationAsync(
    sound === 'finished'
      ? Haptics.NotificationFeedbackType.Success
      : Haptics.NotificationFeedbackType.Warning,
  )
}
