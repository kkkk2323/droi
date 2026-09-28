// The Session's MCP Servers on the phone: connection, switch, tools, and a
// form to add one. A sign-in finishes at the Daemon's own callback on the
// computer, which this phone's browser cannot reach, so the page's link is
// offered to copy and open there.
import {
  EMPTY_SERVER_FORM,
  STATUS_LABELS,
  canRemove,
  formFromRegistry,
  isReadOnly,
  needsSignIn,
  parseServerForm,
  sortServers,
  type AddMcpServerInput,
  type McpServer,
  type McpTool,
  type ServerForm,
} from '@droi/daemon-layer/mcp'
import {
  useMcpActions,
  useMcpRegistry,
  useMcpServers,
  useMcpTools,
  type McpActions,
} from '@droi/daemon-layer/use-mcp'
import { ChevronRight } from 'lucide-react-native'
import { useState } from 'react'
import { Pressable, StyleSheet, Switch, TextInput, View } from 'react-native'
import { copyText } from '../platform/clipboard'
import { Text } from '../ui/primitives'
import { fontSize, fonts, radius, space } from '../ui/theme'
import { useColors } from '../ui/use-colors'
import { Badge, Failure, Loading } from './skills-list'

export function McpList({ sessionId }: { sessionId: string }) {
  const view = useMcpServers(sessionId)
  const { tools } = useMcpTools(sessionId)
  const actions = useMcpActions(sessionId)
  const [adding, setAdding] = useState(false)
  if (view.isLoading) return <Loading what="MCP servers" />
  if (view.error) return <Failure message={view.error} />
  const servers = sortServers(view.servers)
  return (
    <>
      {view.summary?.configError ? (
        <Failure
          message={`${view.summary.configError.path}: ${view.summary.configError.message}`}
        />
      ) : null}
      {actions.error ? <Failure message={actions.error} /> : null}
      {servers.length === 0 ? (
        <Text tone="muted">No MCP servers are configured.</Text>
      ) : (
        <View role="list" aria-label="MCP servers" style={styles.list}>
          {servers.map((server) => (
            <ServerRow
              key={server.name}
              server={server}
              tools={tools.filter((tool) => tool.serverName === server.name)}
              actions={actions}
            />
          ))}
        </View>
      )}
      {adding ? (
        <AddServer
          sessionId={sessionId}
          taken={servers.map((s) => s.name)}
          onAdd={(params) => actions.add(params)}
          onDone={() => setAdding(false)}
        />
      ) : (
        <View style={styles.actions}>
          <SmallButton label="Add server" onPress={() => setAdding(true)} />
        </View>
      )}
    </>
  )
}

function ServerRow({
  server,
  tools,
  actions,
}: {
  server: McpServer
  tools: McpTool[]
  actions: McpActions
}) {
  const colors = useColors()
  const [toolsOpen, setToolsOpen] = useState(false)
  const [confirmRemove, setConfirmRemove] = useState(false)
  const status = String(server.status)
  const readOnly = isReadOnly(server)
  const busy = actions.busy === server.name
  const on = status !== 'disabled'
  // The Daemon does not always count the tools in the server list.
  const toolCount = server.toolCount ?? tools.length
  const dot: Record<string, string> = {
    connected: colors.success,
    connecting: colors.attention,
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
            <Badge>{String(server.serverType)}</Badge>
            {String(server.source) === 'org' ? <Badge>Organization</Badge> : null}
            {String(server.source) === 'project' ? <Badge>Project</Badge> : null}
          </View>
          <Text size="sm" tone="muted">
            {STATUS_LABELS[status] ?? status}
            {toolCount && on ? ` · ${plural(toolCount, 'tool')}` : ''}
            {server.error ? ` · ${server.error}` : ''}
          </Text>
        </View>
        <Switch
          aria-label={`${server.name} enabled`}
          value={on}
          disabled={readOnly || busy}
          onValueChange={(checked) => void actions.setEnabled(server.name, checked)}
          trackColor={{ true: colors.primary }}
        />
      </View>
      {server.pendingAuthUrl ? (
        <SignInNotice
          serverName={server.name}
          url={server.pendingAuthUrl}
          message={server.pendingAuthMessage ?? 'Sign in to continue'}
          onCancel={() => void actions.cancelSignIn(server.name)}
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
        {!readOnly && on && needsSignIn(server) && !server.pendingAuthUrl ? (
          <SmallButton
            label="Sign in"
            disabled={busy}
            onPress={() => void actions.signIn(server.name)}
          />
        ) : null}
        {!readOnly && server.hasAuthTokens ? (
          <SmallButton
            label="Sign out"
            disabled={busy}
            onPress={() => void actions.signOut(server.name)}
          />
        ) : null}
        {canRemove(server) ? (
          confirmRemove ? (
            <>
              <SmallButton
                label={`Remove ${server.name}`}
                destructive
                disabled={busy}
                onPress={() => void actions.remove(server.name)}
              />
              <SmallButton label="Keep" onPress={() => setConfirmRemove(false)} />
            </>
          ) : (
            <SmallButton label="Remove" disabled={busy} onPress={() => setConfirmRemove(true)} />
          )
        ) : null}
      </View>
      {toolsOpen && on ? (
        <ToolList
          server={server}
          tools={tools}
          readOnly={readOnly}
          onToggle={(tool, enabled) => void actions.setToolEnabled(server.name, tool, enabled)}
        />
      ) : null}
    </View>
  )
}

function ToolList({
  server,
  tools,
  readOnly,
  onToggle,
}: {
  server: McpServer
  tools: McpTool[]
  readOnly: boolean
  onToggle: (toolName: string, enabled: boolean) => void
}) {
  const colors = useColors()
  if (tools.length === 0) return <Loading what="tools" />
  return (
    <View role="list" aria-label={`${server.name} tools`} style={styles.tools}>
      {tools.map((tool) => (
        <View key={tool.name} role="listitem" style={styles.tool}>
          <View style={styles.rowBody}>
            <Text size="xs" mono>
              {tool.name}
            </Text>
            {tool.description ? (
              <Text size="xs" tone="muted" numberOfLines={1}>
                {tool.description}
              </Text>
            ) : null}
          </View>
          <Switch
            aria-label={`${tool.name} enabled`}
            value={tool.isEnabled}
            disabled={readOnly}
            onValueChange={(checked) => onToggle(tool.name, checked)}
            trackColor={{ true: colors.primary }}
            style={styles.smallSwitch}
          />
        </View>
      ))}
    </View>
  )
}

function SignInNotice({
  serverName,
  url,
  message,
  onCancel,
}: {
  serverName: string
  url: string
  message: string
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
      <Text size="sm">{message}</Text>
      <Text size="xs" tone="muted">
        The sign-in finishes on the computer running Droi: open the page there.
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

const TYPES: Array<{ value: ServerForm['type']; label: string }> = [
  { value: 'stdio', label: 'Command' },
  { value: 'http', label: 'HTTP' },
  { value: 'sse', label: 'SSE' },
]

function AddServer({
  sessionId,
  taken,
  onAdd,
  onDone,
}: {
  sessionId: string
  taken: string[]
  onAdd: (params: AddMcpServerInput) => Promise<boolean>
  onDone: () => void
}) {
  const colors = useColors()
  const [form, setForm] = useState<ServerForm>(EMPTY_SERVER_FORM)
  const [problem, setProblem] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const registry = useMcpRegistry(sessionId)
  const set = (patch: Partial<ServerForm>) => setForm((f) => ({ ...f, ...patch }))
  const remote = form.type !== 'stdio'
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
    const added = await onAdd(parsed.params)
    setSaving(false)
    if (added) onDone()
  }

  return (
    <View
      role="form"
      aria-label="Add MCP server"
      style={[styles.form, { backgroundColor: colors.card }]}
    >
      {registry.entries.filter((entry) => !taken.includes(entry.name)).length > 0 ? (
        <View role="list" aria-label="Catalogue" style={styles.list}>
          <Text size="xs" weight="medium" tone="muted">
            From Factory's catalogue
          </Text>
          {registry.entries
            .filter((entry) => !taken.includes(entry.name))
            .map((entry) => (
              <View key={entry.name} role="listitem" style={styles.rowHead}>
                <View style={styles.rowBody}>
                  <Text size="sm">{entry.name}</Text>
                  <Text size="xs" tone="muted" numberOfLines={1}>
                    {entry.description}
                  </Text>
                  {entry.note ? (
                    <Text size="xs" tone="muted" numberOfLines={2}>
                      {entry.note}
                    </Text>
                  ) : null}
                </View>
                <SmallButton label="Use" onPress={() => set(formFromRegistry(entry))} />
              </View>
            ))}
        </View>
      ) : null}
      <View role="radiogroup" aria-label="Server type" style={styles.actions}>
        {TYPES.map((t) => (
          <Pressable
            key={t.value}
            role="radio"
            aria-checked={form.type === t.value}
            aria-label={t.label}
            onPress={() => set({ type: t.value })}
            style={[
              styles.typeButton,
              {
                borderColor: colors.border,
                backgroundColor: form.type === t.value ? colors.accent : colors.background,
              },
            ]}
          >
            <Text size="xs" weight={form.type === t.value ? 'medium' : 'regular'}>
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
            placeholder="Arguments"
            placeholderTextColor={colors.mutedForeground}
            value={form.args}
            onChangeText={(args) => set({ args })}
            autoCapitalize="none"
            autoCorrect={false}
            style={inputStyle}
          />
        </>
      )}
      {problem ? <Failure message={problem} /> : null}
      <View style={[styles.actions, styles.formActions]}>
        <SmallButton label="Cancel" onPress={onDone} />
        <SmallButton label="Add server" primary disabled={saving} onPress={() => void submit()} />
      </View>
    </View>
  )
}

function SmallButton({
  label,
  onPress,
  disabled = false,
  destructive = false,
  primary = false,
}: {
  label: string
  onPress: () => void
  disabled?: boolean
  destructive?: boolean
  primary?: boolean
}) {
  const colors = useColors()
  return (
    <Pressable
      role="button"
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
  list: { gap: space.sm },
  row: { gap: space.xs, paddingVertical: space.xs },
  rowHead: { flexDirection: 'row', alignItems: 'center', gap: space.sm },
  rowBody: { flex: 1, minWidth: 0, gap: 1 },
  titleRow: { flexDirection: 'row', alignItems: 'center', gap: space.xs, flexWrap: 'wrap' },
  dot: { width: 8, height: 8, borderRadius: 4 },
  actions: { flexDirection: 'row', flexWrap: 'wrap', alignItems: 'center', gap: space.xs },
  formActions: { justifyContent: 'flex-end' },
  toolsToggle: {
    flexDirection: 'row',
    alignItems: 'center',
    gap: 2,
    height: 28,
    paddingRight: space.xs,
  },
  tools: { gap: space.xs, paddingLeft: space.lg },
  tool: { flexDirection: 'row', alignItems: 'center', gap: space.sm },
  smallSwitch: { transform: [{ scale: 0.8 }] },
  notice: { borderRadius: radius.lg, padding: space.sm, gap: space.xs },
  smallButton: {
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.md,
    paddingHorizontal: space.sm,
    height: 30,
    justifyContent: 'center',
  },
  typeButton: {
    borderWidth: StyleSheet.hairlineWidth,
    borderRadius: radius.md,
    paddingHorizontal: space.md,
    height: 30,
    justifyContent: 'center',
  },
  form: { borderRadius: radius.xl, padding: space.md, gap: space.sm },
  input: {
    borderWidth: 1,
    borderRadius: radius.lg,
    paddingHorizontal: space.md,
    height: 40,
    fontFamily: fonts.mono,
    fontSize: fontSize.sm,
  },
  multiline: { height: 64, paddingTop: space.sm },
})
