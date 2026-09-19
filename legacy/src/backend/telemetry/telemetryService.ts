import { PostHog } from 'posthog-node'
import type { TelemetryEvent } from './events.ts'

const POSTHOG_API_KEY = 'phc_s4nLg9kc8MpS4aXy9LoyMOexCWC2wUwu1XXkCLha4hO'
const POSTHOG_HOST = 'https://us.i.posthog.com'

export class TelemetryService {
  private client: PostHog | null = null
  private enabled = true
  private machineId = ''

  init(opts: { machineId: string; appVersion: string; enabled?: boolean }) {
    this.machineId = opts.machineId
    if (typeof opts.enabled === 'boolean') this.enabled = opts.enabled
    if (!this.enabled) return

    this.client = new PostHog(POSTHOG_API_KEY, {
      host: POSTHOG_HOST,
      flushAt: 20,
      flushInterval: 30_000,
    })

    this.client.identify({
      distinctId: this.machineId,
      properties: { app_version: opts.appVersion },
    })
  }

  setEnabled(enabled: boolean) {
    this.enabled = Boolean(enabled)
    if (!this.enabled && this.client) {
      this.client.optOut()
    } else if (this.enabled && this.client) {
      this.client.optIn()
    }
  }

  isEnabled(): boolean {
    return this.enabled
  }

  capture(event: TelemetryEvent) {
    if (!this.enabled || !this.client || !this.machineId) return
    this.client.capture({
      distinctId: this.machineId,
      event: event.event,
      properties: event.properties,
    })
  }

  async shutdown(): Promise<void> {
    if (!this.client) return
    try {
      await this.client.shutdown()
    } catch {
      // ignore shutdown errors
    }
    this.client = null
  }
}
