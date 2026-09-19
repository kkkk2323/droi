import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import QRCode from 'qrcode'
import { Check, Copy, RefreshCw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import type {
  PairingInfo,
  ShellSettingsBridge,
  ShellSettingsSnapshot,
} from '@shared/shell-settings'

/** Desktop Shell settings. Rendered only in the Local Client, which has the preload bridge. */
const SETTINGS_KEY = ['shell-settings'] as const
const PAIRING_KEY = ['shell-pairing'] as const

export function SettingsPage({ bridge }: { bridge: ShellSettingsBridge }) {
  const queryClient = useQueryClient()
  const settingsQuery = useQuery({ queryKey: SETTINGS_KEY, queryFn: () => bridge.get() })
  const pairingQuery = useQuery({ queryKey: PAIRING_KEY, queryFn: () => bridge.getPairing() })

  useEffect(
    () =>
      bridge.onChange(() => {
        void queryClient.invalidateQueries({ queryKey: SETTINGS_KEY })
        void queryClient.invalidateQueries({ queryKey: PAIRING_KEY })
      }),
    [bridge, queryClient],
  )

  const setSnapshot = (next: ShellSettingsSnapshot) => queryClient.setQueryData(SETTINGS_KEY, next)
  const setPairing = (next: PairingInfo) => queryClient.setQueryData(PAIRING_KEY, next)
  const snapshot = settingsQuery.data
  const pairing = pairingQuery.data
  const error = settingsQuery.error?.message ?? pairingQuery.error?.message ?? null

  if (!snapshot || !pairing) {
    return (
      <section aria-label="Settings" className="p-6 text-sm text-muted-foreground">
        Loading settings…
      </section>
    )
  }

  return (
    <section
      aria-label="Settings"
      className="mx-auto flex h-full w-full max-w-xl flex-col gap-8 overflow-y-auto px-6 py-8"
    >
      <div>
        <h2 className="text-base font-semibold tracking-tight">Settings</h2>
        <p className="mt-1 text-xs text-muted-foreground">Droi {snapshot.version}</p>
      </div>
      {error ? (
        <p role="alert" className="text-sm text-destructive-foreground">
          {error}
        </p>
      ) : null}

      <ApiKeySection snapshot={snapshot} bridge={bridge} onSaved={setSnapshot} />
      <DroidPathSection
        key={snapshot.droidPath ?? ''}
        snapshot={snapshot}
        bridge={bridge}
        onSaved={setSnapshot}
      />
      <BaseUrlSection
        key={snapshot.factoryApiBaseUrl ?? ''}
        snapshot={snapshot}
        bridge={bridge}
        onSaved={setSnapshot}
      />
      <RemoteAccessSection
        key={String(snapshot.remoteAccess)}
        snapshot={snapshot}
        bridge={bridge}
        onSaved={setSnapshot}
      />
      <PairingSection
        enabled={snapshot.remoteAccess}
        pairing={pairing}
        bridge={bridge}
        onReset={setPairing}
      />
    </section>
  )
}

function Field({
  label,
  hint,
  children,
}: {
  label: string
  hint?: string
  children: React.ReactNode
}) {
  return (
    <div className="flex flex-col gap-2">
      <h3 className="text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
        {label}
      </h3>
      {children}
      {hint ? <p className="text-xs text-muted-foreground">{hint}</p> : null}
    </div>
  )
}

const inputClass =
  'h-9 min-w-0 flex-1 rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring/50'

function ApiKeySection({
  snapshot,
  bridge,
  onSaved,
}: {
  snapshot: ShellSettingsSnapshot
  bridge: ShellSettingsBridge
  onSaved: (s: ShellSettingsSnapshot) => void
}) {
  const [value, setValue] = useState('')
  const [saving, setSaving] = useState(false)
  const save = async (apiKey: string | null) => {
    setSaving(true)
    try {
      onSaved(await bridge.setApiKey(apiKey))
      setValue('')
    } finally {
      setSaving(false)
    }
  }
  return (
    <Field
      label="Factory API key"
      hint={
        snapshot.apiKeyFromEnvironment
          ? 'Supplied by FACTORY_API_KEY in the environment; the stored key is ignored.'
          : 'Stored encrypted on this computer and used by the Gateway. Phones never see it.'
      }
    >
      <form
        className="flex gap-2"
        onSubmit={(event) => {
          event.preventDefault()
          void save(value)
        }}
      >
        <input
          aria-label="Factory API key"
          type="password"
          autoComplete="off"
          placeholder={snapshot.hasApiKey ? '•••••••••••• (key stored)' : 'fk-…'}
          value={value}
          disabled={snapshot.apiKeyFromEnvironment}
          onChange={(event) => setValue(event.target.value)}
          className={inputClass}
        />
        <Button type="submit" disabled={saving || !value.trim() || snapshot.apiKeyFromEnvironment}>
          Save key
        </Button>
        {snapshot.hasApiKey && !snapshot.apiKeyFromEnvironment ? (
          <Button type="button" variant="outline" disabled={saving} onClick={() => void save(null)}>
            Remove
          </Button>
        ) : null}
      </form>
      <p role="status" aria-label="API key status" className="text-xs text-muted-foreground">
        {snapshot.hasApiKey
          ? 'A key is set.'
          : 'No key set. The Daemon cannot authenticate without one.'}
      </p>
    </Field>
  )
}

function DroidPathSection({
  snapshot,
  bridge,
  onSaved,
}: {
  snapshot: ShellSettingsSnapshot
  bridge: ShellSettingsBridge
  onSaved: (s: ShellSettingsSnapshot) => void
}) {
  const [value, setValue] = useState(snapshot.droidPath ?? '')
  return (
    <Field
      label="droid executable"
      hint={
        snapshot.droidFound
          ? `Using ${snapshot.droidFound}`
          : 'droid was not found on PATH or in ~/.local/bin. Enter its full path.'
      }
    >
      <form
        className="flex gap-2"
        onSubmit={(event) => {
          event.preventDefault()
          void bridge.update({ droidPath: value.trim() || null }).then(onSaved)
        }}
      >
        <input
          aria-label="droid path override"
          placeholder="Leave empty to auto-detect"
          value={value}
          spellCheck={false}
          onChange={(event) => setValue(event.target.value)}
          className={`${inputClass} font-mono`}
        />
        <Button type="submit" variant="outline">
          Save path
        </Button>
      </form>
    </Field>
  )
}

function BaseUrlSection({
  snapshot,
  bridge,
  onSaved,
}: {
  snapshot: ShellSettingsSnapshot
  bridge: ShellSettingsBridge
  onSaved: (s: ShellSettingsSnapshot) => void
}) {
  const [value, setValue] = useState(snapshot.factoryApiBaseUrl ?? '')
  const inherited = snapshot.factoryApiBaseUrlFromEnvironment
  return (
    <Field
      label="Factory API base URL"
      hint={
        snapshot.factoryApiBaseUrl
          ? 'The Daemon sends its Factory API traffic to this URL (FACTORY_API_BASE_URL). Use it for a local proxy such as droid-proxy.'
          : inherited
            ? `Inherited from the environment: ${inherited}. Set a value here to override it.`
            : 'Leave empty to talk to Factory directly. Set it to route the Daemon through a local proxy such as droid-proxy (for example http://127.0.0.1:37650).'
      }
    >
      <form
        className="flex gap-2"
        onSubmit={(event) => {
          event.preventDefault()
          void bridge.update({ factoryApiBaseUrl: value.trim() || null }).then(onSaved)
        }}
      >
        <input
          aria-label="Factory API base URL"
          placeholder={inherited ?? 'https://api.factory.ai'}
          value={value}
          spellCheck={false}
          autoCapitalize="off"
          onChange={(event) => setValue(event.target.value)}
          className={`${inputClass} font-mono`}
        />
        <Button type="submit" variant="outline">
          Save URL
        </Button>
      </form>
    </Field>
  )
}

function RemoteAccessSection({
  snapshot,
  bridge,
  onSaved,
}: {
  snapshot: ShellSettingsSnapshot
  bridge: ShellSettingsBridge
  onSaved: (s: ShellSettingsSnapshot) => void
}) {
  // Flips at once; the Shell confirms (or reverts) when the Gateway has rebound.
  const [checked, setChecked] = useState(snapshot.remoteAccess)
  const toggle = async (next: boolean) => {
    setChecked(next)
    try {
      onSaved(await bridge.update({ remoteAccess: next }))
    } catch {
      setChecked(!next)
    }
  }
  return (
    <Field
      label="Remote Access"
      hint="When on, the Gateway also listens on this computer's network addresses so a paired phone can connect. Off by default."
    >
      <label className="flex items-center gap-3 text-sm">
        <input
          type="checkbox"
          role="switch"
          aria-label="Remote Access"
          aria-checked={checked}
          checked={checked}
          onChange={(event) => void toggle(event.target.checked)}
          className="size-4 accent-primary"
        />
        {checked ? 'On: phones on this network can pair' : 'Off: only this window can connect'}
      </label>
    </Field>
  )
}

function PairingSection({
  enabled,
  pairing,
  bridge,
  onReset,
}: {
  enabled: boolean
  pairing: PairingInfo
  bridge: ShellSettingsBridge
  onReset: (p: PairingInfo) => void
}) {
  const [copied, setCopied] = useState(false)
  const qr = useQuery({
    queryKey: ['pairing-qr', pairing.link],
    queryFn: () => QRCode.toString(pairing.link ?? '', { type: 'svg', margin: 1, width: 192 }),
    enabled: Boolean(pairing.link),
    staleTime: Infinity,
  })
  const svg = qr.data ?? ''

  const copy = async () => {
    if (!pairing.link) return
    await navigator.clipboard.writeText(pairing.link)
    setCopied(true)
    setTimeout(() => setCopied(false), 1_500)
  }

  return (
    <Field
      label="Pair a phone"
      hint={
        enabled
          ? 'Scan the code or open the link on a phone connected to the same network. Resetting revokes every paired phone.'
          : 'Turn on Remote Access to pair a phone.'
      }
    >
      {enabled && pairing.link ? (
        <div className="flex flex-col gap-3 sm:flex-row sm:items-start">
          <div
            role="img"
            aria-label="Pairing QR code"
            className="size-48 shrink-0 rounded-md bg-white p-2 [&_svg]:size-full"
            dangerouslySetInnerHTML={{ __html: svg }}
          />
          <div className="flex min-w-0 flex-1 flex-col gap-2">
            <code
              aria-label="Pairing link"
              className="break-all rounded-md bg-muted px-2 py-1.5 font-mono text-xs"
            >
              {pairing.link}
            </code>
            <div className="flex gap-2">
              <Button type="button" size="sm" variant="outline" onClick={() => void copy()}>
                {copied ? <Check aria-hidden /> : <Copy aria-hidden />}
                {copied ? 'Copied' : 'Copy link'}
              </Button>
              <Button
                type="button"
                size="sm"
                variant="destructive"
                onClick={() => void bridge.resetPairingToken().then(onReset)}
              >
                <RefreshCw aria-hidden />
                Reset pairing token
              </Button>
            </div>
          </div>
        </div>
      ) : enabled ? (
        <p className="text-sm text-muted-foreground">
          No network address found. Connect this computer to a network and try again.
        </p>
      ) : null}
    </Field>
  )
}
