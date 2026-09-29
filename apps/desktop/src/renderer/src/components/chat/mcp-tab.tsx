// The Session's MCP Servers: each with its connection, its switch, its sign-in
// and its tools. Adding one is a view of its own: Factory's catalogue to
// search and add from in one click, and a form for a server by hand or for a
// catalogue entry with a value to fill in first.
// A sign-in opens in the browser and finishes at the Daemon's own callback on
// the computer, so from a Remote Client the page has to be opened there.
import { useState } from 'react'
import { Collapsible } from '@base-ui/react/collapsible'
import { ChevronLeft, ChevronRight, ExternalLink, Plus, Search } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Select } from '@/components/ui/select'
import { Spinner } from '@/components/ui/spinner'
import { Switch, settingInputClass } from '@/components/ui/setting-row'
import {
  EMPTY_SERVER_FORM,
  STATUS_LABELS,
  canRemove,
  filterRegistry,
  formFromRegistry,
  isDroiMemory,
  isReadOnly,
  needsSetup,
  needsSignIn,
  parseServerForm,
  sortServers,
  type AddMcpServerInput,
  type McpRegistryEntry,
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
import { cn } from '@/lib/utils'
import { Badge, Failure, Loading } from './skills-tab'

type View =
  | { kind: 'list' }
  | { kind: 'catalogue' }
  /** `from` is the catalogue entry being set up; null for a server by hand. */
  | { kind: 'form'; from: McpRegistryEntry | null }

export function McpTab({ sessionId }: { sessionId: string }) {
  const view = useMcpServers(sessionId)
  const { tools } = useMcpTools(sessionId)
  const actions = useMcpActions(sessionId)
  const [showing, setShowing] = useState<View>({ kind: 'list' })
  if (view.isLoading) return <Loading what="MCP servers" />
  if (view.error) return <Failure message={view.error} />
  const servers = sortServers(view.servers)
  const taken = servers.map((s) => s.name)
  const add = async (params: AddMcpServerInput) => {
    const added = await actions.add(params)
    if (added) setShowing({ kind: 'list' })
    return added
  }

  if (showing.kind === 'catalogue') {
    return (
      <Catalogue
        sessionId={sessionId}
        taken={taken}
        busy={actions.busy}
        error={actions.error}
        onAdd={add}
        onSetUp={(entry) => setShowing({ kind: 'form', from: entry })}
        onByHand={() => setShowing({ kind: 'form', from: null })}
        onBack={() => setShowing({ kind: 'list' })}
      />
    )
  }
  if (showing.kind === 'form') {
    return (
      <AddServer
        from={showing.from}
        taken={taken}
        error={actions.error}
        onAdd={add}
        onBack={() => setShowing({ kind: 'catalogue' })}
      />
    )
  }
  return (
    <div className="flex flex-col gap-3">
      {view.summary?.configError ? (
        <Failure
          message={`${view.summary.configError.path}: ${view.summary.configError.message}`}
        />
      ) : null}
      {actions.error ? <Failure message={actions.error} /> : null}
      {servers.length === 0 ? (
        <p className="p-2 text-sm text-muted-foreground">No MCP servers are configured.</p>
      ) : (
        <ul aria-label="MCP servers" className="flex flex-col gap-0.5">
          {servers.map((server) => (
            <ServerRow
              key={server.name}
              server={server}
              tools={tools.filter((tool) => tool.serverName === server.name)}
              actions={actions}
            />
          ))}
        </ul>
      )}
      <div className="px-2">
        <Button size="sm" variant="outline" onClick={() => setShowing({ kind: 'catalogue' })}>
          <Plus aria-hidden data-icon="inline-start" />
          Add server
        </Button>
      </div>
    </div>
  )
}

function SubHeader({ title, onBack }: { title: string; onBack: () => void }) {
  return (
    <div className="flex items-center gap-1">
      <Button
        size="icon-xs"
        variant="ghost"
        aria-label="Back"
        className="text-muted-foreground"
        onClick={onBack}
      >
        <ChevronLeft aria-hidden />
      </Button>
      <h3 className="text-sm font-medium">{title}</h3>
    </div>
  )
}

const DOT: Record<string, string> = {
  connected: 'bg-success',
  connecting: 'bg-attention animate-pulse',
  failed: 'bg-destructive-foreground',
  disconnected: 'bg-muted-foreground/40',
  disabled: 'bg-muted-foreground/40',
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
  const [toolsOpen, setToolsOpen] = useState(false)
  const [confirmRemove, setConfirmRemove] = useState(false)
  const status = String(server.status)
  const readOnly = isReadOnly(server)
  const droi = isDroiMemory(server)
  const busy = actions.busy === server.name
  const on = status !== 'disabled'
  // The Daemon does not always count the tools in the server list.
  const toolCount = server.toolCount ?? tools.length
  return (
    <li aria-label={server.name} className="rounded-lg px-2 py-1.5">
      <div className="flex items-start gap-3">
        <span
          role="img"
          aria-label={STATUS_LABELS[status] ?? status}
          title={STATUS_LABELS[status] ?? status}
          className={cn(
            'mt-[7px] size-2 shrink-0 rounded-full',
            DOT[status] ?? DOT['disconnected'],
          )}
        />
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="text-sm font-medium">{server.name}</span>
            <Badge>{String(server.serverType)}</Badge>
            {String(server.source) === 'org' ? <Badge>Organization</Badge> : null}
            {String(server.source) === 'project' ? <Badge>Project</Badge> : null}
            {droi ? <Badge>Droi</Badge> : null}
          </div>
          <p className="mt-0.5 text-[13px] leading-5 text-muted-foreground">
            {STATUS_LABELS[status] ?? status}
            {toolCount && on ? ` · ${plural(toolCount, 'tool')}` : ''}
            {server.error ? ` · ${server.error}` : ''}
          </p>
          {droi ? (
            <p className="text-[13px] leading-5 text-muted-foreground">
              Droi’s Memory. Turn it on or off on the computer in Settings → Memory.
            </p>
          ) : null}
          {server.pendingAuthUrl ? (
            <SignInNotice
              serverName={server.name}
              url={server.pendingAuthUrl}
              message={server.pendingAuthMessage ?? 'Sign in to continue'}
              onCancel={() => void actions.cancelSignIn(server.name)}
            />
          ) : null}
          <Collapsible.Root open={toolsOpen} onOpenChange={setToolsOpen}>
            <div className="mt-1 flex flex-wrap items-center gap-1">
              {on && toolCount ? (
                <Collapsible.Trigger
                  render={<Button size="xs" variant="ghost" />}
                  className="text-muted-foreground"
                >
                  <ChevronRight
                    aria-hidden
                    data-icon="inline-start"
                    className={cn('transition-transform', toolsOpen && 'rotate-90')}
                  />
                  {plural(toolCount, 'tool')}
                </Collapsible.Trigger>
              ) : null}
              {!readOnly && on && needsSignIn(server) && !server.pendingAuthUrl ? (
                <Button
                  size="xs"
                  variant="outline"
                  disabled={busy}
                  onClick={() => void actions.signIn(server.name)}
                >
                  Sign in
                </Button>
              ) : null}
              {!readOnly && server.hasAuthTokens ? (
                <Button
                  size="xs"
                  variant="ghost"
                  disabled={busy}
                  onClick={() => void actions.signOut(server.name)}
                >
                  Sign out
                </Button>
              ) : null}
              {canRemove(server) ? (
                confirmRemove ? (
                  <>
                    <Button
                      size="xs"
                      variant="destructive"
                      disabled={busy}
                      onClick={() => void actions.remove(server.name)}
                    >
                      Remove {server.name}
                    </Button>
                    <Button size="xs" variant="ghost" onClick={() => setConfirmRemove(false)}>
                      Keep
                    </Button>
                  </>
                ) : (
                  <Button
                    size="xs"
                    variant="ghost"
                    className="text-muted-foreground"
                    disabled={busy}
                    onClick={() => setConfirmRemove(true)}
                  >
                    Remove
                  </Button>
                )
              ) : null}
            </div>
            {on && toolCount ? (
              <Collapsible.Panel>
                <ToolList
                  server={server}
                  tools={tools}
                  readOnly={readOnly}
                  onToggle={(tool, enabled) =>
                    void actions.setToolEnabled(server.name, tool.name, enabled)
                  }
                />
              </Collapsible.Panel>
            ) : null}
          </Collapsible.Root>
        </div>
        {droi ? null : (
          <Switch
            aria-label={`${server.name} enabled`}
            checked={on}
            disabled={readOnly || busy}
            onCheckedChange={(checked) => void actions.setEnabled(server.name, checked)}
          />
        )}
      </div>
    </li>
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
  onToggle: (tool: McpTool, enabled: boolean) => void
}) {
  if (tools.length === 0) return <Loading what="tools" />
  return (
    <ul aria-label={`${server.name} tools`} className="mt-1 flex flex-col gap-0.5 pl-1">
      {tools.map((tool) => (
        <li key={tool.name} className="flex items-center gap-3 py-0.5">
          <div className="min-w-0 flex-1">
            <span className="font-mono text-xs">{tool.name}</span>
            {tool.description ? (
              <p className="line-clamp-1 text-xs text-muted-foreground">{tool.description}</p>
            ) : null}
          </div>
          <Switch
            aria-label={`${tool.name} enabled`}
            checked={tool.isEnabled}
            disabled={readOnly}
            onCheckedChange={(checked) => onToggle(tool, checked)}
            className="scale-90"
          />
        </li>
      ))}
    </ul>
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
  const local = Boolean(window.droiShell)
  return (
    <div
      role="status"
      aria-label={`Sign in to ${serverName}`}
      className="mt-1 flex flex-wrap items-center gap-2 rounded-md bg-attention/10 px-2 py-1.5 text-[13px]"
    >
      <span className="min-w-0 flex-1">
        {message}
        {local ? null : (
          <span className="block text-muted-foreground">
            The sign-in finishes on the computer running Droi; open the page there.
          </span>
        )}
      </span>
      <a
        href={url}
        target="_blank"
        rel="noreferrer"
        className="inline-flex items-center gap-1 font-medium underline underline-offset-2"
      >
        Open sign-in page
        <ExternalLink aria-hidden className="size-3" />
      </a>
      <Button size="xs" variant="ghost" onClick={onCancel}>
        Cancel
      </Button>
    </div>
  )
}

const TYPE_OPTIONS = [
  { value: 'stdio', label: 'Command (stdio)' },
  { value: 'http', label: 'HTTP' },
  { value: 'sse', label: 'SSE' },
]

function Catalogue({
  sessionId,
  taken,
  busy,
  error,
  onAdd,
  onSetUp,
  onByHand,
  onBack,
}: {
  sessionId: string
  taken: string[]
  busy: string | null
  error: string | null
  onAdd: (params: AddMcpServerInput) => Promise<boolean>
  onSetUp: (entry: McpRegistryEntry) => void
  onByHand: () => void
  onBack: () => void
}) {
  const [query, setQuery] = useState('')
  const registry = useMcpRegistry(sessionId)
  const entries = filterRegistry(registry.entries, query, taken)
  const addNow = (entry: McpRegistryEntry) => {
    const parsed = parseServerForm(formFromRegistry(entry))
    if (parsed.ok) void onAdd(parsed.params)
    else onSetUp(entry)
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="sticky -top-3 z-10 -mx-3 -mt-3 flex flex-col gap-2 bg-popover px-3 pt-3 pb-1">
        <SubHeader title="Add MCP server" onBack={onBack} />
        <label className="relative flex items-center">
          <Search
            aria-hidden
            className="pointer-events-none absolute left-2.5 size-3.5 text-muted-foreground"
          />
          <input
            type="search"
            aria-label="Search the catalogue"
            placeholder="Search Factory's catalogue"
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            // oxlint-disable-next-line jsx-a11y/no-autofocus -- the view opens to search
            autoFocus
            className={cn(settingInputClass, 'w-full pl-8')}
          />
        </label>
        {error ? <Failure message={error} /> : null}
      </div>
      {registry.isLoading ? (
        <Loading what="the catalogue" />
      ) : registry.error ? (
        <p className="px-2 text-sm text-muted-foreground">The catalogue is not available.</p>
      ) : entries.length === 0 ? (
        <p className="px-2 text-sm text-muted-foreground">
          {query.trim()
            ? `Nothing in the catalogue matches “${query.trim()}”.`
            : 'Everything in the catalogue is added.'}
        </p>
      ) : (
        <ul aria-label="Catalogue" className="flex flex-col gap-0.5">
          {entries.map((entry) => {
            const setup = needsSetup(entry)
            const adding = busy === entry.name
            return (
              <li
                key={entry.name}
                aria-label={entry.name}
                className="flex items-start gap-3 rounded-lg px-2 py-1.5 hover:bg-muted/50"
              >
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-1.5">
                    <span className="text-sm font-medium">{entry.name}</span>
                    <Badge>{entry.type}</Badge>
                  </div>
                  <p className="line-clamp-2 text-[13px] leading-5 text-muted-foreground">
                    {entry.description}
                  </p>
                  {entry.note ? (
                    <p className="mt-0.5 text-xs text-muted-foreground/80">{entry.note}</p>
                  ) : null}
                </div>
                <Button
                  size="xs"
                  variant="outline"
                  className="mt-0.5 shrink-0"
                  disabled={busy !== null}
                  aria-label={`${setup ? 'Set up' : 'Add'} ${entry.name}`}
                  onClick={() => (setup ? onSetUp(entry) : addNow(entry))}
                >
                  {adding ? <Spinner className="size-3" /> : null}
                  {setup ? 'Set up' : 'Add'}
                </Button>
              </li>
            )
          })}
        </ul>
      )}
      <div className="flex items-center gap-1 border-t px-2 pt-2 text-[13px] text-muted-foreground">
        Not in the catalogue?
        <Button size="xs" variant="ghost" onClick={onByHand}>
          Add a server by hand
        </Button>
      </div>
    </div>
  )
}

function AddServer({
  from,
  taken,
  error,
  onAdd,
  onBack,
}: {
  from: McpRegistryEntry | null
  taken: string[]
  error: string | null
  onAdd: (params: AddMcpServerInput) => Promise<boolean>
  onBack: () => void
}) {
  const [form, setForm] = useState<ServerForm>(() =>
    from ? formFromRegistry(from) : EMPTY_SERVER_FORM,
  )
  const [problem, setProblem] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const set = (patch: Partial<ServerForm>) => setForm((f) => ({ ...f, ...patch }))
  const remote = form.type !== 'stdio'

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
    <form
      aria-label="Add MCP server"
      className="flex flex-col gap-2"
      onSubmit={(event) => {
        event.preventDefault()
        void submit()
      }}
    >
      <SubHeader title={from ? `Set up ${from.name}` : 'Add a server by hand'} onBack={onBack} />
      {from?.note ? (
        <p className="rounded-md bg-attention/10 px-2.5 py-1.5 text-[13px]">{from.note}</p>
      ) : null}
      <div className="flex flex-wrap gap-2">
        <input
          aria-label="Server name"
          placeholder="Name"
          value={form.name}
          onChange={(e) => set({ name: e.target.value })}
          className={settingInputClass}
        />
        <Select
          label="Server type"
          value={form.type}
          onChange={(value) => set({ type: value as ServerForm['type'] })}
          options={TYPE_OPTIONS}
        />
      </div>
      {remote ? (
        <>
          <input
            aria-label="Server URL"
            placeholder="https://…"
            value={form.url}
            onChange={(e) => set({ url: e.target.value })}
            className={cn(settingInputClass, 'flex-none')}
          />
          <textarea
            aria-label="Headers"
            placeholder={'Headers, one per line: Authorization: Bearer …'}
            value={form.headers}
            onChange={(e) => set({ headers: e.target.value })}
            rows={2}
            className={cn(settingInputClass, 'h-auto flex-none py-2 font-mono text-xs')}
          />
        </>
      ) : (
        <>
          <input
            aria-label="Command"
            placeholder="Command, e.g. npx"
            value={form.command}
            onChange={(e) => set({ command: e.target.value })}
            className={cn(settingInputClass, 'flex-none font-mono')}
          />
          <input
            aria-label="Arguments"
            placeholder="Arguments, separated by spaces"
            value={form.args}
            onChange={(e) => set({ args: e.target.value })}
            className={cn(settingInputClass, 'flex-none font-mono')}
          />
        </>
      )}
      {(problem ?? error) ? <Failure message={(problem ?? error)!} /> : null}
      <div className="flex items-center justify-end gap-1">
        <Button size="sm" variant="ghost" type="button" onClick={onBack}>
          Cancel
        </Button>
        <Button size="sm" type="submit" disabled={saving}>
          Add server
        </Button>
      </div>
    </form>
  )
}

function plural(count: number, word: string): string {
  return `${count} ${count === 1 ? word : `${word}s`}`
}
