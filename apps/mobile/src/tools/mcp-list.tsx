// The computer's MCP Servers on the phone, as a Session sees them: the user's own,
// and those its repository's .factory/mcp.json brings, which run only once the
// user allows them. A server of the user's is switched, added by hand or
// removed here. A sign-in finishes at the Kernel's own callback on the computer,
// which this phone's browser cannot reach, so the page's link is offered to copy
// and open there.
import {
  EMPTY_SERVER_FORM,
  STATUS_LABELS,
  canRemove,
  isReadOnly,
  needsSignIn,
  needsTrust,
  parseServerForm,
  serverCommand,
  serverKey,
  sortServers,
  toolLabel,
  type AddMcpServerInput,
  type McpServer,
  type ServerForm,
} from '@droi/daemon-layer/mcp'
import {
  useMcpActions,
  useMcpServers,
  useSignInPages,
  type McpActions,
} from '@droi/daemon-layer/use-mcp'
import { ChevronLeft, ChevronRight } from 'lucide-react-native'
import { useState } from 'react'
import { Pressable, StyleSheet, Switch, TextInput, View } from 'react-native'
import { copyText } from '../platform/clipboard'
import { Text } from '../ui/primitives'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'
import { Badge, Failure, Loading } from './skills-list'

export function McpList({ repoRoot }: { repoRoot: string | null }) {
  const view = useMcpServers(repoRoot)
  const actions = useMcpActions()
  const pages = useSignInPages()
  const [adding, setAdding] = useState(false)
  if (view.isLoading) return <Loading what="MCP servers" />
  if (view.error) return <Failure message={view.error} />
  const servers = sortServers(view.servers)
  const taken = servers.filter((s) => !s.repoRoot).map((s) => s.name)

  if (adding) {
    return (
      <AddServer
        taken={taken}
        error={actions.error}
        onAdd={async (params) => {
          if (await actions.add(params)) setAdding(false)
        }}
        onBack={() => setAdding(false)}
      />
    )
  }
  return (
    <>
      {actions.error ? <Failure message={actions.error} /> : null}
      {servers.length === 0 ? (
        <Text tone="muted">No MCP servers are configured.</Text>
      ) : (
        <View role="list" aria-label="MCP servers" style={styles.list}>
          {servers.map((server) => (
            <ServerRow
              key={serverKey(server)}
              server={server}
              signInPage={pages.get(serverKey(server)) ?? null}
              actions={actions}
            />
          ))}
        </View>
      )}
      <View style={styles.actions}>
        <SmallButton label="Add server" onPress={() => setAdding(true)} />
      </View>
    </>
  )
}

function SubHeader({ title, onBack }: { title: string; onBack: () => void }) {
  const colors = useColors()
  return (
    <View style={styles.subHeader}>
      <Pressable role="button" aria-label="Back" onPress={onBack} hitSlop={8} style={styles.back}>
        <ChevronLeft size={18} color={colors.mutedForeground} />
      </Pressable>
      <Text weight="medium">{title}</Text>
    </View>
  )
}

function ServerRow({
  server,
  signInPage,
  actions,
}: {
  server: McpServer
  signInPage: string | null
  actions: McpActions
}) {
  const colors = useColors()
  const [toolsOpen, setToolsOpen] = useState(false)
  const [confirmRemove, setConfirmRemove] = useState(false)
  const status = server.status
  const readOnly = isReadOnly(server)
  const busy = actions.busy === server.name
  const on = server.enabled && status !== 'disabled'
  const toolCount = server.tools.length
  const dot: Record<string, string> = {
    connected: colors.success,
    connecting: colors.attention,
    untrusted: colors.attention,
    failed: colors.destructiveForeground,
  }
  return (
    <View role="listitem" aria-label={server.name} style={styles.row}>
      <View style={styles.rowHead}>
        <View
          role="img"
          aria-label={STATUS_LABELS[status] ?? status}
          style={[styles.dot, { backgroundColor: dot[status] ?? colors.border }]}
        />
        <View style={styles.rowBody}>
          <View style={styles.titleRow}>
            <Text weight="medium">{server.name}</Text>
            <Badge>{server.transport === 'http' ? 'HTTP' : 'Command'}</Badge>
            {server.repoRoot ? <Badge>Project</Badge> : null}
            {server.app ? <Badge>App</Badge> : null}
          </View>
          <Text size="sm" tone="muted">
            {STATUS_LABELS[status] ?? status}
            {toolCount && on ? ` · ${plural(toolCount, 'tool')}` : ''}
            {server.error ? ` · ${server.error}` : ''}
          </Text>
        </View>
        {readOnly ? null : (
          <Switch
            aria-label={`${server.name} enabled`}
            value={on}
            disabled={busy}
            onValueChange={(checked) => void actions.setEnabled(server, checked)}
            trackColor={{ true: colors.primary }}
          />
        )}
      </View>
      {needsTrust(server) ? (
        <TrustNotice server={server} busy={busy} onAllow={() => void actions.trust(server)} />
      ) : null}
      {signInPage ? (
        <SignInNotice
          serverName={server.name}
          url={signInPage}
          onCancel={() => void actions.cancelSignIn(server)}
        />
      ) : null}
      <View style={styles.actions}>
        {on && toolCount ? (
          <Pressable
            role="button"
            aria-expanded={toolsOpen}
            onPress={() => setToolsOpen(!toolsOpen)}
            style={styles.toolsToggle}
          >
            <ChevronRight
              size={12}
              color={colors.mutedForeground}
              style={{ transform: [{ rotate: toolsOpen ? '90deg' : '0deg' }] }}
            />
            <Text size="xs" tone="muted" weight="medium">
              {plural(toolCount, 'tool')}
            </Text>
          </Pressable>
        ) : null}
        {needsSignIn(server) && !signInPage ? (
          <SmallButton
            label="Sign in"
            disabled={busy}
            onPress={() => void actions.signIn(server)}
          />
        ) : null}
        {canRemove(server) ? (
          confirmRemove ? (
            <>
              <SmallButton
                label={`Remove ${server.name}`}
                destructive
                disabled={busy}
                onPress={() => void actions.remove(server)}
              />
              <SmallButton label="Keep" onPress={() => setConfirmRemove(false)} />
            </>
          ) : (
            <SmallButton label="Remove" disabled={busy} onPress={() => setConfirmRemove(true)} />
          )
        ) : null}
      </View>
      {toolsOpen && on ? (
        <View role="list" aria-label={`${server.name} tools`} style={styles.tools}>
          {server.tools.map((tool) => (
            <View key={tool} role="listitem" style={styles.tool}>
              <Text size="xs" mono>
                {toolLabel(server, tool)}
              </Text>
            </View>
          ))}
        </View>
      ) : null}
    </View>
  )
}

function TrustNotice({
  server,
  busy,
  onAllow,
}: {
  server: McpServer
  busy: boolean
  onAllow: () => void
}) {
  const colors = useColors()
  return (
    <View
      role="status"
      aria-label={`Allow ${server.name}?`}
      style={[styles.notice, { backgroundColor: colors.card }]}
    >
      <Text size="sm">This repository’s .factory/mcp.json wants to run:</Text>
      <Text size="xs" mono selectable>
        {serverCommand(server)}
      </Text>
      <View style={styles.actions}>
        <SmallButton
          label="Allow"
          accessibilityLabel={`Allow ${server.name}`}
          disabled={busy}
          onPress={onAllow}
        />
      </View>
    </View>
  )
}

function SignInNotice({
  serverName,
  url,
  onCancel,
}: {
  serverName: string
  url: string
  onCancel: () => void
}) {
  const colors = useColors()
  const [copied, setCopied] = useState(false)
  return (
    <View
      role="status"
      aria-label={`Sign in to ${serverName}`}
      style={[styles.notice, { backgroundColor: colors.card }]}
    >
      <Text size="sm">Sign in to continue</Text>
      <Text size="xs" tone="muted">
        Copy the sign-in link and open it on the computer running Droi to finish signing in.
      </Text>
      <View style={styles.actions}>
        <SmallButton
          label={copied ? 'Link copied' : 'Copy sign-in link'}
          onPress={() => {
            void copyText(url).then(() => setCopied(true))
          }}
        />
        <SmallButton label="Cancel" onPress={onCancel} />
      </View>
    </View>
  )
}

const TRANSPORTS: Array<{ value: ServerForm['transport']; label: string }> = [
  { value: 'stdio', label: 'Command' },
  { value: 'http', label: 'HTTP' },
]

function AddServer({
  taken,
  error,
  onAdd,
  onBack,
}: {
  taken: string[]
  error: string | null
  onAdd: (params: AddMcpServerInput) => Promise<void>
  onBack: () => void
}) {
  const colors = useColors()
  const [form, setForm] = useState<ServerForm>(EMPTY_SERVER_FORM)
  const [problem, setProblem] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const set = (patch: Partial<ServerForm>) => setForm((f) => ({ ...f, ...patch }))
  const remote = form.transport === 'http'
  const inputStyle = [
    styles.input,
    { borderColor: colors.input, color: colors.foreground, backgroundColor: colors.background },
  ]

  const submit = async () => {
    const parsed = parseServerForm(form)
    if (!parsed.ok) {
      setProblem(parsed.error)
      return
    }
    if (taken.includes(parsed.params.name)) {
      setProblem(`There is already a server named "${parsed.params.name}".`)
      return
    }
    setProblem(null)
    setSaving(true)
    await onAdd(parsed.params)
    setSaving(false)
  }

  return (
    <View role="form" aria-label="Add MCP server" style={styles.form}>
      <SubHeader title="Add MCP server" onBack={onBack} />
      <View role="radiogroup" aria-label="Server type" style={styles.actions}>
        {TRANSPORTS.map((t) => (
          <Pressable
            key={t.value}
            role="radio"
            aria-checked={form.transport === t.value}
            aria-label={t.label}
            onPress={() => set({ transport: t.value })}
            style={[
              styles.typeButton,
              {
                borderColor: colors.border,
                backgroundColor: form.transport === t.value ? colors.accent : colors.background,
              },
            ]}
          >
            <Text size="xs" weight={form.transport === t.value ? 'medium' : 'regular'}>
              {t.label}
            </Text>
          </Pressable>
        ))}
      </View>
      <TextInput
        aria-label="Server name"
        placeholder="Name"
        placeholderTextColor={colors.mutedForeground}
        value={form.name}
        onChangeText={(name) => set({ name })}
        autoCapitalize="none"
        autoCorrect={false}
        style={inputStyle}
      />
      {remote ? (
        <>
          <TextInput
            aria-label="Server URL"
            placeholder="https://…"
            placeholderTextColor={colors.mutedForeground}
            value={form.url}
            onChangeText={(url) => set({ url })}
            autoCapitalize="none"
            autoCorrect={false}
            keyboardType="url"
            style={inputStyle}
          />
          <TextInput
            aria-label="Headers"
            placeholder="Headers, one per line: Authorization: Bearer …"
            placeholderTextColor={colors.mutedForeground}
            value={form.headers}
            onChangeText={(headers) => set({ headers })}
            autoCapitalize="none"
            autoCorrect={false}
            multiline
            style={[inputStyle, styles.multiline]}
          />
        </>
      ) : (
        <>
          <TextInput
            aria-label="Command"
            placeholder="Command, e.g. npx"
            placeholderTextColor={colors.mutedForeground}
            value={form.command}
            onChangeText={(command) => set({ command })}
            autoCapitalize="none"
            autoCorrect={false}
            style={inputStyle}
          />
          <TextInput
            aria-label="Arguments"
            placeholder="Arguments, separated by spaces"
            placeholderTextColor={colors.mutedForeground}
            value={form.args}
            onChangeText={(args) => set({ args })}
            autoCapitalize="none"
            autoCorrect={false}
            style={inputStyle}
          />
        </>
      )}
      {(problem ?? error) ? <Failure message={(problem ?? error)!} /> : null}
      <View style={[styles.actions, styles.formActions]}>
        <SmallButton label="Cancel" onPress={onBack} />
        <SmallButton label="Add server" primary disabled={saving} onPress={() => void submit()} />
      </View>
    </View>
  )
}

function SmallButton({
  label,
  onPress,
  accessibilityLabel,
  disabled = false,
  destructive = false,
  primary = false,
}: {
  label: string
  onPress: () => void
  accessibilityLabel?: string
  disabled?: boolean
  destructive?: boolean
  primary?: boolean
}) {
  const colors = useColors()
  return (
    <Pressable
      role="button"
      aria-label={accessibilityLabel}
      disabled={disabled}
      onPress={onPress}
      style={({ pressed }) => [
        styles.smallButton,
        {
          borderColor: primary ? colors.primary : colors.border,
          backgroundColor: primary ? colors.primary : pressed ? colors.accent : colors.background,
          opacity: disabled ? 0.5 : 1,
        },
      ]}
    >
      <Text
        size="xs"
        weight="medium"
        style={{
          color: primary
            ? colors.primaryForeground
            : destructive
              ? colors.destructiveForeground
              : colors.foreground,
        }}
      >
        {label}
      </Text>
    </Pressable>
  )
}

function plural(count: number, word: string): string {
  return `${count} ${count === 1 ? word : `${word}s`}`
}

const styles = StyleSheet.create({
  list: { gap: space.md },
  row: { gap: space.sm, paddingVertical: space.sm },
  rowHead: { flexDirection: 'row', alignItems: 'center', gap: space.sm },
  rowBody: { flex: 1, minWidth: 0, gap: 1 },
  titleRow: { flexDirection: 'row', alignItems: 'center', gap: space.xs, flexWrap: 'wrap' },
  dot: { width: 8, height: 8, borderRadius: 4 },
  actions: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: space.xs },
  formActions: { justifyContent: 'flex-end' },
  toolsToggle: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.xs,
    minHeight: 44,
    paddingRight: space.xs,
  },
  tools: { gap: space.xs, paddingLeft: space.lg },
  tool: { flexDirection: 'row', alignItems: 'center', gap: space.sm, minHeight: 32 },
  notice: { borderRadius: radius.md, padding: space.sm, gap: space.xs },
  smallButton: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: space.xs,
    borderWidth: 1,
    borderRadius: radius.md,
    paddingHorizontal: space.sm,
    height: 44,
    justifyContent: 'center',
  },
  subHeader: { flexDirection: 'row', alignItems: 'center', gap: space.xs },
  back: { width: 44, height: 44, alignItems: 'center', justifyContent: 'center' },
  typeButton: {
    borderWidth: 1,
    borderRadius: radius.md,
    paddingHorizontal: space.md,
    height: 44,
    justifyContent: 'center',
  },
  form: { gap: space.sm },
  input: {
    borderWidth: 1,
    borderRadius: radius.md,
    paddingHorizontal: space.md,
    height: 44,
    fontFamily: fonts.mono,
    fontSize: fontSize.sm,
  },
  multiline: { height: 64, paddingTop: space.sm },
})
