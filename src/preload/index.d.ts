import type { DroidClientAPI } from '../shared/protocol'

declare global {
  interface Window {
    droid: DroidClientAPI
  }
}
