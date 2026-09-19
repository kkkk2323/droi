export type TelemetryEventName = 'app_launched' | 'startup_metrics'

export interface TelemetryEvent {
  event: TelemetryEventName
  properties?: Record<string, string | number | boolean | undefined>
}
