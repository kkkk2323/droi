// Contract between the Desktop Shell and the Local Client for Session alerts:
// the sounds played when Droid finishes or needs an answer, and the desktop
// notifications. Only the Local Client has it; a Remote Client stays silent.

/** The two sounds the `droid` CLI installs under ~/.factory/sounds. */
export const BUILTIN_SOUNDS = ['fx-ok01', 'fx-ack01'] as const
export type BuiltinSound = (typeof BUILTIN_SOUNDS)[number]

export interface AlertNotification {
  title: string
  body: string
  /** The Session a click on the notification opens. */
  sessionId: string
}

export interface AlertsBridge {
  /** A built-in sound as a data: URL, or null when this computer does not have it. */
  builtinSound(name: BuiltinSound): Promise<string | null>
  /** Asks for an audio file; resolves to its path, or null when cancelled. */
  pickSoundFile(): Promise<string | null>
  /** An audio file as a data: URL, or null when it is missing, too large or not audio. */
  readSoundFile(path: string): Promise<string | null>
  notify(notification: AlertNotification): Promise<void>
  /** Called with the Session id when a notification is clicked; returns the unsubscribe. */
  onNotificationClick(listener: (sessionId: string) => void): () => void
}

export const ALERTS_IPC = {
  builtinSound: 'droi:alerts:builtin-sound',
  pickSoundFile: 'droi:alerts:pick-sound-file',
  readSoundFile: 'droi:alerts:read-sound-file',
  notify: 'droi:alerts:notify',
  notificationClicked: 'droi:alerts:notification-clicked',
} as const
