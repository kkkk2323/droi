import type { AlertEvent } from '@droi/daemon-layer/alerts'
import { usePreference } from '@droi/daemon-layer/local-preference'
import type { AlertsBridge } from '@shared/alerts'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
import { SettingRow, Switch } from '@/components/ui/setting-row'
import {
  alertPreferences,
  FOCUS_MODES,
  playAlertSound,
  SOUND_CHOICES,
  type AlertPreferences,
  type FocusMode,
  type SoundChoice,
} from '@/lib/alerts'

const SOUND_LABELS: Record<SoundChoice, string> = {
  off: 'Off',
  bell: 'Bell',
  'fx-ok01': 'Soft chime',
  'fx-ack01': 'Acknowledge tone',
  custom: 'Custom…',
}

const FOCUS_LABELS: Record<FocusMode, string> = {
  always: 'Always',
  focused: 'Only when Droi is focused',
  unfocused: 'Only when Droi is in the background',
}

const SLOTS: Record<
  AlertEvent,
  {
    title: string
    description: string
    sound: 'completionSound' | 'awaitingInputSound'
    custom: 'customCompletionSound' | 'customAwaitingInputSound'
    default: SoundChoice
  }
> = {
  completion: {
    title: 'Completion sound',
    description: 'Plays when Droid finishes and the Session stops.',
    sound: 'completionSound',
    custom: 'customCompletionSound',
    default: 'fx-ok01',
  },
  'awaiting-input': {
    title: 'Needs-input sound',
    description: 'Plays when Droid stops to ask for permission or an answer.',
    sound: 'awaitingInputSound',
    custom: 'customAwaitingInputSound',
    default: 'fx-ack01',
  },
}

/** Sounds and desktop notifications, after Factory App's settings. Local Client only. */
export function NotificationsTab({ alerts }: { alerts: AlertsBridge }) {
  const [preferences, setPreferences] = usePreference(alertPreferences)
  const update = (patch: Partial<AlertPreferences>) => setPreferences({ ...preferences, ...patch })
  return (
    <>
      <SoundRow event="completion" alerts={alerts} preferences={preferences} update={update} />
      <SoundRow event="awaiting-input" alerts={alerts} preferences={preferences} update={update} />
      <SettingRow
        title="When to play sounds"
        control={
          <Select
            label="When to play sounds"
            value={preferences.focusMode}
            onChange={(next) =>
              update({ focusMode: FOCUS_MODES.find((mode) => mode === next) ?? 'always' })
            }
            options={FOCUS_MODES.map((value) => ({ value, label: FOCUS_LABELS[value] }))}
          />
        }
      />
      <SettingRow
        title="Notify when Droid finishes"
        description="A desktop notification, unless you are looking at that Session."
        control={
          <Switch
            aria-label="Notify when Droid finishes"
            checked={preferences.notifyOnComplete}
            onCheckedChange={(checked) => update({ notifyOnComplete: checked })}
          />
        }
      />
      <SettingRow
        title="Notify when Droid needs input"
        description="A desktop notification when a Session waits for a permission or an answer."
        control={
          <Switch
            aria-label="Notify when Droid needs input"
            checked={preferences.notifyOnWaitingForInput}
            onCheckedChange={(checked) => update({ notifyOnWaitingForInput: checked })}
          />
        }
      />
    </>
  )
}

function SoundRow({
  event,
  alerts,
  preferences,
  update,
}: {
  event: AlertEvent
  alerts: AlertsBridge
  preferences: AlertPreferences
  update: (patch: Partial<AlertPreferences>) => void
}) {
  const slot = SLOTS[event]
  const customPath = preferences[slot.custom]
  const pickFile = async () => {
    const path = await alerts.pickSoundFile()
    if (path) update({ [slot.sound]: 'custom', [slot.custom]: path })
  }
  return (
    <SettingRow
      title={slot.title}
      description={slot.description}
      control={
        <div className="flex items-center gap-2">
          <Select
            label={slot.title}
            value={preferences[slot.sound]}
            onChange={(next) => {
              const choice = SOUND_CHOICES.find((c) => c === next) ?? slot.default
              // "Custom…" needs a file first; without one the old choice stays.
              if (choice === 'custom' && !customPath) void pickFile()
              else update({ [slot.sound]: choice })
            }}
            options={SOUND_CHOICES.map((value) => ({
              value,
              label:
                value === slot.default ? `${SOUND_LABELS[value]} (default)` : SOUND_LABELS[value],
            }))}
          />
          <Button
            size="sm"
            variant="outline"
            aria-label={`Test ${slot.title.toLowerCase()}`}
            onClick={() => void playAlertSound(alerts, preferences, event, { test: true })}
          >
            Test
          </Button>
        </div>
      }
    >
      {preferences[slot.sound] === 'custom' && customPath ? (
        <div className="flex items-center gap-2 text-[13px] text-muted-foreground">
          <span className="min-w-0 flex-1 truncate" title={customPath}>
            Custom file: {customPath.split(/[\\/]/).pop()}
          </span>
          <Button size="sm" variant="outline" onClick={() => void pickFile()}>
            Choose…
          </Button>
        </div>
      ) : null}
    </SettingRow>
  )
}
