import { useEffect, useState, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import QRCode from 'qrcode'
import {
  ArrowLeft,
  Check,
  Copy,
  ExternalLink,
  LogOut,
  UserRound,
  RefreshCw,
  Server,
  Settings2,
  Smartphone,
  type LucideIcon,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
import { SettingRow, Switch, settingInputClass } from '@/components/ui/setting-row'
import { showArchivedSessions, usePreference } from '@/lib/local-preference'
import { useTheme } from '@/lib/theme'
import { TEXT_SIZES, TEXT_SIZE_LABELS, applyTextSize, textSize } from '@/lib/text-size'
import { cn } from '@/lib/utils'
import type {
  PairingInfo,
  ShellSettingsBridge,
  ShellSettingsSnapshot,
} from '@shared/shell-settings'

/**
 * Settings. General holds Client-side preferences and works everywhere; the
 * Desktop Shell tabs need the preload bridge, so a Remote Client sees General only.
 */
const SETTINGS_KEY = ['shell-settings'] as const
const PAIRING_KEY = ['shell-pairing'] as const

type Tab = 'account' | 'general' | 'daemon' | 'remote'

const TABS: Array<{ id: Tab; label: string; icon: LucideIcon; needsShell: boolean }> = [
  { id: 'account', label: 'Account', icon: UserRound, needsShell: true },
  { id: 'general', label: 'General', icon: Settings2, needsShell: false },
  { id: 'daemon', label: 'Daemon', icon: Server, needsShell: true },
  { id: 'remote', label: 'Remote Access', icon: Smartphone, needsShell: true },
]

export function SettingsPage({
  bridge,
  onBack,
}: {
  bridge: ShellSettingsBridge | null
  onBack: () => void
}) {
  const queryClient = useQueryClient()
  const tabs = TABS.filter((t) => bridge || !t.needsShell)
  const [tab, setTab] = useState<Tab>(bridge ? 'account' : 'general')
  const settingsQuery = useQuery({
    queryKey: SETTINGS_KEY,
    queryFn: () => bridge!.get(),
    enabled: bridge !== null,
  })
  const pairingQuery = useQuery({
    queryKey: PAIRING_KEY,
    queryFn: () => bridge!.getPairing(),
    enabled: bridge !== null,
  })

  useEffect(
    () =>
      bridge?.onChange(() => {
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
  const current = tabs.find((t) => t.id === tab) ?? tabs[0]!

  return (
    <section
      aria-label="Settings"
      className="flex h-dvh overflow-hidden bg-sidebar text-foreground"
    >
      <nav
        aria-label="Settings sections"
        className="app-drag flex w-56 shrink-0 flex-col gap-1 border-r px-3 pb-3 pt-[calc(env(safe-area-inset-top)+2.75rem)]"
      >
        <button
          type="button"
          onClick={onBack}
          className="mb-2 flex h-8 items-center gap-2 rounded-lg px-2 text-[13px] text-muted-foreground transition-colors hover:bg-sidebar-accent/60 hover:text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
        >
          <ArrowLeft aria-hidden className="size-4" />
          Back
        </button>
        {tabs.map(({ id, label, icon: Icon }) => (
          <button
            key={id}
            type="button"
            aria-current={tab === id ? 'page' : undefined}
            onClick={() => setTab(id)}
            className={cn(
              'flex h-8 items-center gap-2 rounded-lg px-2 text-left text-[13px] text-sidebar-foreground transition-colors hover:bg-sidebar-accent/60 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring/50',
              tab === id && 'bg-sidebar-accent text-sidebar-accent-foreground',
            )}
          >
            <Icon aria-hidden className="size-4 text-muted-foreground" />
            {label}
          </button>
        ))}
      </nav>

      <div className="min-w-0 flex-1 overflow-y-auto bg-background">
        <div className="app-drag h-[calc(env(safe-area-inset-top)+2.75rem)]" />
        <div className="mx-auto flex w-full max-w-2xl flex-col gap-3 px-8 pb-12">
          <h2 className="mb-2 text-lg font-semibold tracking-tight">{current.label}</h2>
          {error ? (
            <p role="alert" className="text-sm text-destructive-foreground">
              {error}
            </p>
          ) : null}
          {tab === 'general' ? (
            <GeneralTab version={snapshot?.version ?? null} />
          ) : !bridge || !snapshot || !pairing ? (
            <p className="text-sm text-muted-foreground">Loading settings…</p>
          ) : tab === 'account' ? (
            <AccountTab snapshot={snapshot} bridge={bridge} onSaved={setSnapshot} />
          ) : tab === 'daemon' ? (
            <>
              <ApiKeyRow snapshot={snapshot} bridge={bridge} onSaved={setSnapshot} />
              <BaseUrlRow
                key={snapshot.factoryApiBaseUrl ?? ''}
                snapshot={snapshot}
                bridge={bridge}
                onSaved={setSnapshot}
              />
              <DroidPathRow
                key={snapshot.droidPath ?? ''}
                snapshot={snapshot}
                bridge={bridge}
                onSaved={setSnapshot}
              />
            </>
          ) : (
            <>
              <RemoteAccessRow
                key={String(snapshot.remoteAccess)}
                snapshot={snapshot}
                bridge={bridge}
                onSaved={setSnapshot}
              />
              <PairingRow
                enabled={snapshot.remoteAccess}
                pairing={pairing}
                bridge={bridge}
                onReset={setPairing}
              />
            </>
          )}
        </div>
      </div>
    </section>
  )
}

function GeneralTab({ version }: { version: string | null }) {
  const [theme, setTheme] = useTheme()
  const [size, setSize] = usePreference(textSize)
  const [showArchived, setShowArchived] = usePreference(showArchivedSessions)
  return (
    <>
      <SettingRow
        title="Theme"
        description="Light is the default. The choice is stored per browser."
        control={
          <Select
            label="Theme"
            value={theme}
            onChange={(next) => setTheme(next === 'dark' ? 'dark' : 'light')}
            options={[
              { value: 'light', label: 'Light' },
              { value: 'dark', label: 'Dark' },
            ]}
          />
        }
      />
      <SettingRow
        title="Text size"
        description="Scales every label, message and code block together. ⌘= and ⌘- zoom on top of this."
        control={
          <Select
            label="Text size"
            value={size}
            onChange={(next) => {
              const picked = TEXT_SIZES.find((s) => s === next) ?? 'default'
              setSize(picked)
              applyTextSize(picked)
            }}
            options={TEXT_SIZES.map((value) => ({ value, label: TEXT_SIZE_LABELS[value] }))}
          />
        }
      />
      <SettingRow
        title="Show archived sessions"
        description="List archived sessions in the sidebar alongside the active ones."
        control={
          <Switch
            aria-label="Show archived sessions"
            checked={showArchived}
            onCheckedChange={setShowArchived}
          />
        }
      />
      {version ? (
        <>
          <SettingRow
            title="Everything stays on this computer"
            description="Sessions, settings and the Factory API key live here. Phones connect to this computer through the Gateway; nothing is sent elsewhere."
          />
          <SettingRow title="About" description={`Droi ${version}`} />
        </>
      ) : null}
    </>
  )
}

function AccountTab({ snapshot, bridge, onSaved }: RowProps) {
  const [busy, setBusy] = useState(false)
  const login = snapshot.login
  const run = async (action: () => Promise<ShellSettingsSnapshot>) => {
    setBusy(true)
    try {
      onSaved(await action())
    } finally {
      setBusy(false)
    }
  }
  const mismatch =
    login.status === 'signed-in' &&
    snapshot.daemonIdentity !== null &&
    snapshot.daemonIdentity.userId !== login.account.userId

  return (
    <>
      {login.status === 'signed-in' ? (
        <SettingRow
          title="Signed in with Factory"
          description={
            <span className="font-mono text-xs">
              {login.account.email ?? login.account.userId}
              {login.account.orgId ? ` · ${login.account.orgId}` : ''}
            </span>
          }
          control={
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={busy}
              onClick={() => void run(() => bridge.signOut())}
            >
              <LogOut aria-hidden />
              Sign out
            </Button>
          }
        />
      ) : login.status === 'pending' ? (
        <SettingRow
          title="Finish signing in"
          description="Your browser opened Factory. Enter this code there if it asks for one."
          control={
            <Button
              type="button"
              variant="outline"
              size="sm"
              disabled={busy}
              onClick={() => void run(() => bridge.cancelSignIn())}
            >
              Cancel
            </Button>
          }
        >
          <div className="flex flex-wrap items-center gap-3">
            <code
              aria-label="Sign-in code"
              className="rounded-lg border bg-background px-3 py-1.5 font-mono text-base tracking-[0.2em]"
            >
              {login.pending.userCode}
            </code>
            <a
              href={login.pending.verificationUriComplete}
              target="_blank"
              rel="noreferrer"
              className="inline-flex items-center gap-1 text-sm underline underline-offset-2"
            >
              Open the sign-in page again
              <ExternalLink aria-hidden className="size-3.5" />
            </a>
          </div>
        </SettingRow>
      ) : (
        <SettingRow
          title="Sign in with Factory"
          description="Uses the same login as the droid CLI. Droi then needs no API key; sessions and settings stay on this computer."
          control={
            <Button
              type="button"
              size="sm"
              disabled={busy}
              onClick={() => void run(() => bridge.signIn())}
            >
              Sign in
            </Button>
          }
        >
          {login.error ? (
            <p role="alert" className="text-sm text-destructive-foreground">
              {login.error}
            </p>
          ) : null}
        </SettingRow>
      )}
      {mismatch ? (
        <SettingRow
          className="border border-amber-500/40"
          title="The droid CLI is logged in as someone else"
          description={`The Daemon runs as ${snapshot.daemonIdentity!.userId} (from droid login) while Droi is signed in as ${login.status === 'signed-in' ? login.account.userId : ''}. Sessions belong to the CLI's user, so run \`droid login\` with the same account or sign in here with that one.`}
        />
      ) : null}
      {snapshot.daemonIdentity === null ? (
        <SettingRow
          title="The droid CLI is not logged in"
          description="The Daemon signs in with the droid CLI's login on this computer. Run `droid login` in a terminal, or add a Factory API key under Daemon."
        />
      ) : null}
    </>
  )
}

type RowProps = {
  snapshot: ShellSettingsSnapshot
  bridge: ShellSettingsBridge
  onSaved: (s: ShellSettingsSnapshot) => void
}

function ApiKeyRow({ snapshot, bridge, onSaved }: RowProps) {
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
    <SettingRow
      title="Factory API key"
      description={
        snapshot.apiKeyFromEnvironment
          ? 'Supplied by FACTORY_API_KEY in the environment; the stored key is ignored.'
          : 'Stored encrypted on this computer and used by the Gateway. Phones never see it.'
      }
      control={
        <StatusPill ok={snapshot.hasApiKey} label={snapshot.hasApiKey ? 'Set' : 'Missing'} />
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
          className={settingInputClass}
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
      <p role="status" aria-label="API key status" className="mt-2 text-xs text-muted-foreground">
        {snapshot.hasApiKey
          ? 'A key is set.'
          : 'No key set. The Daemon cannot authenticate without one.'}
      </p>
    </SettingRow>
  )
}

function DroidPathRow({ snapshot, bridge, onSaved }: RowProps) {
  const [value, setValue] = useState(snapshot.droidPath ?? '')
  return (
    <SettingRow
      title="droid executable"
      description={
        snapshot.droidFound
          ? `Using ${snapshot.droidFound}`
          : 'droid was not found on PATH or in ~/.local/bin. Enter its full path.'
      }
      control={
        <StatusPill
          ok={Boolean(snapshot.droidFound)}
          label={snapshot.droidFound ? 'Found' : 'Not found'}
        />
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
          className={`${settingInputClass} font-mono`}
        />
        <Button type="submit" variant="outline">
          Save path
        </Button>
      </form>
    </SettingRow>
  )
}

function BaseUrlRow({ snapshot, bridge, onSaved }: RowProps) {
  const [value, setValue] = useState(snapshot.factoryApiBaseUrl ?? '')
  const inherited = snapshot.factoryApiBaseUrlFromEnvironment
  return (
    <SettingRow
      title="Factory API base URL"
      description={
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
          className={`${settingInputClass} font-mono`}
        />
        <Button type="submit" variant="outline">
          Save URL
        </Button>
      </form>
    </SettingRow>
  )
}

function RemoteAccessRow({ snapshot, bridge, onSaved }: RowProps) {
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
    <SettingRow
      title="Remote Access"
      description={
        checked
          ? 'On: the Gateway also listens on this computer’s network addresses so a paired phone can connect.'
          : 'Off: only this window can connect. Turn it on to pair a phone on the same network.'
      }
      control={
        <Switch
          aria-label="Remote Access"
          checked={checked}
          onCheckedChange={(next) => void toggle(next)}
        />
      }
    />
  )
}

function PairingRow({
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
    <SettingRow
      title="Pair a phone"
      description={
        enabled
          ? 'Scan the code or open the link on a phone connected to the same network. To keep Droi on an iPhone home screen, add the page to the home screen first, then paste the link once inside the app. Resetting revokes every paired phone.'
          : 'Turn on Remote Access to pair a phone.'
      }
    >
      {enabled && pairing.link ? (
        <div className="flex flex-col gap-4 sm:flex-row sm:items-start">
          <div
            role="img"
            aria-label="Pairing QR code"
            className="size-44 shrink-0 rounded-lg border bg-white p-2 [&_svg]:size-full"
            dangerouslySetInnerHTML={{ __html: svg }}
          />
          <div className="flex min-w-0 flex-1 flex-col gap-2">
            <code
              aria-label="Pairing link"
              className="break-all rounded-lg border bg-background px-3 py-2 font-mono text-xs leading-5"
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
    </SettingRow>
  )
}

function StatusPill({ ok, label }: { ok: boolean; label: ReactNode }) {
  return (
    <span
      className={cn(
        'inline-flex h-6 items-center gap-1.5 rounded-full border px-2 text-xs',
        ok ? 'text-foreground' : 'text-muted-foreground',
      )}
    >
      <span
        aria-hidden
        className={cn('size-1.5 rounded-full', ok ? 'bg-emerald-500' : 'bg-muted-foreground')}
      />
      {label}
    </span>
  )
}
