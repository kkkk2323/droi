# Kernel protocol

The contract between the Go Host (`apps/native/pkg/kernel`) and the Kernel sidecar
(`apps/kernel`). It is the wire form of `architecture-draft.md` §2; when the two
disagree, this directory and the shared change record win.

Both sides test against `fixtures/` — the same JSON files, unmarshalled on the Go
side and parsed on the TypeScript side.

## Transport

The Host starts the sidecar and reads its handshake from the first line of stdout:

```text
DROI_KERNEL_TOKEN=<random> kernel --port 0 --data <UserData>/kernel --host 127.0.0.1
{"port":51234,"pid":4121,"version":"kernel-1.5.0-1"}
```

`--port 0` asks the operating system for a free port. The token is a per-launch
secret the Host generates, and it arrives in the environment, never on the
command line: on macOS `ps` shows the arguments of every user's processes, so a
token there is readable by any local user, while a process's environment is not.
A kernel started without `DROI_KERNEL_TOKEN` writes a line to stderr and exits
with status `1`: no retry supplies a token.

| Endpoint      | Purpose                                                                                                                                                              |
| ------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `GET /health` | Liveness. `200 {"ok":true,"version":"kernel-1.5.0-1","pi":"1.5.0"}`. Needs no token: it answers one question.                                                        |
| `GET /rpc`    | WebSocket upgrade. Requires `Authorization: Bearer <token>`. A wrong or missing token is HTTP `401`, or WebSocket close code `4401` if the upgrade already happened. |

One WebSocket carries every method and notification. Text frames only, one
JSON-RPC 2.0 message per frame.

### Connections and subscriptions

A client may open several connections — the window and the phone each open
their own — and they see the same state. Which notification reaches which
connection is part of the contract:

| Notification                               | Reaches                                                                    |
| ------------------------------------------ | -------------------------------------------------------------------------- |
| `conversation.view`                        | Only the connections that called `conversation.open` for that conversation |
| `conversation.busy`                        | Every connection                                                           |
| `approval.requested` / `approval.resolved` | Every connection                                                           |
| `ask.requested` / `ask.resolved`           | Every connection                                                           |
| `task.changed`                             | Every connection                                                           |
| `index.changed`                            | Every connection                                                           |
| `mcp.changed`                              | Every connection                                                           |
| `process.changed`                          | Every connection                                                           |
| `warning`                                  | Every connection                                                           |

Views are per connection because a view is a window over a transcript, and each
client asks for the window it can draw. Everything else is state one client
changes and every client must see.

- `conversation.open` subscribes the calling connection and answers with the
  view. Calling it twice on one connection replaces that connection's window.
- `conversation.unwatch` unsubscribes the calling connection: no further
  `conversation.view` for that conversation reaches it. It changes nothing
  else — the conversation, its runs and other connections' subscriptions stay
  as they are. A connection that closes is unsubscribed from every conversation
  it opened.
- The kernel releases a conversation's watch when its last subscriber goes, and
  answers the next `conversation.open` with a fresh read.
- Approvals and asks reach every connection precisely so the window and the
  phone both show the question. The first `approval.respond` / `ask.respond`
  wins: that connection gets the result and every connection, the others
  included, then gets `approval.resolved` / `ask.resolved`. A later response
  for the same request is `-32007 request not found`.

### Subscriptions and how often a view is pushed

A client says what it wants when it subscribes, because the two clients are not
alike: the window sits on the machine that runs the kernel, and the phone
reaches it over the Relay, on someone else's network.

| `subscribe`        | A `conversation.view` carries                                                | Who uses it                 |
| ------------------ | ---------------------------------------------------------------------------- | --------------------------- |
| `"view"` (default) | `{id, kind: "value", value, ops, retired}` — the window over entries, whole  | The window on this computer |
| `"delta"`          | `{id, kind: "value", value: null, ops, retired}` — Pi's incremental ops only | The phone over the Relay    |

A `"delta"` subscription, as a client asks for it:

```json
{
  "jsonrpc": "2.0",
  "id": "4",
  "method": "conversation.open",
  "params": { "id": 7, "subscribe": "delta" }
}
```

`kind: "full"` carries the whole `value` and `ops: []`. The kernel sends it
whenever it cannot prove the ops are continuous with what a `"delta"` subscriber
holds: the ops of one coalescing window could not be merged (a retire and a
splice that would cross), or the client subscribed while a commit was in flight.
The answer to `conversation.open` is already a whole view, so a fresh subscriber
starts from one. A client that receives `full` replaces its state with it; it
never has to guess whether the state it kept is whole.

**The kernel pushes at most `harness.info.limits.viewPushesPerSecond` (5) times
per second per conversation to each connection.** Commits that fall inside one
interval are coalesced: their ops are merged into one push, or, when they cannot
be merged, into one `full`. **A push is never dropped**: what is coalesced is an
intermediate state, and the last state always arrives. This is a frequency cap,
not a throttle of the data — a client that changes nothing still sees the final
state within 200 ms of the commit that produced it.

The reason is the Relay: the same bytes cross a third party's network for a
phone, and a hundred-entry window re-sent on every streaming chunk is an order of
magnitude more traffic than the ops that changed. Locally the difference is not
worth the client state machine, so the default stays whole views.

## Envelope

JSON-RPC 2.0.

```json
{"jsonrpc":"2.0","id":"17","method":"conversation.open","params":{"id":7}}
{"jsonrpc":"2.0","id":"17","result":{"view":{}}}
{"jsonrpc":"2.0","id":"17","error":{"code":-32000,"message":"conversation not found","data":{"conversationId":7}}}
{"jsonrpc":"2.0","method":"conversation.busy","params":{"id":7,"run":null}}
```

- `id` is a string or a number, chosen by the client. A request without `id` is a
  notification: the kernel runs it and sends no response, not even an error.
- `params` may be omitted instead of `{}`.
- `result` is always an object.
- Every error carries `message` (a sentence for a log, English) and, where the
  table below names one, `data` with machine-readable fields. Clients branch on
  `code`, never on `message`.

### Errors

| Code     | Name                      | `data`                              | When                                                                     |
| -------- | ------------------------- | ----------------------------------- | ------------------------------------------------------------------------ |
| `-32700` | parse error               | —                                   | The frame is not JSON.                                                   |
| `-32600` | invalid request           | —                                   | Not a JSON-RPC 2.0 request: no `method`, wrong `jsonrpc`.                |
| `-32601` | method not found          | `{"method"}`                        | No such method.                                                          |
| `-32602` | invalid params            | `{"issues":[{"path","message"}]}`   | The params do not match the method's shape.                              |
| `-32603` | internal error            | —                                   | A bug in the kernel. The kernel stays up.                                |
| `-32000` | conversation not found    | `{"conversationId"}`                |                                                                          |
| `-32001` | conversation busy         | `{"conversationId"}`                | `submit` with `whenBusy: "reject"`, or `continue` while busy.            |
| `-32002` | conversation idle         | `{"conversationId"}`                | `submit` with `whenIdle: "reject"` while idle.                           |
| `-32003` | submission not found      | `{"conversationId","submissionId"}` |                                                                          |
| `-32004` | submission settled        | `{"submissionId","status"}`         | `submission.abort` on a done or unanswered submission.                   |
| `-32005` | submission already placed | `{"submissionId"}`                  | `submission.abort` after the input entered the transcript.               |
| `-32006` | task not found            | `{"taskId"}`                        |                                                                          |
| `-32007` | request not found         | `{"requestId"}`                     | An approval or an ask that is unknown or already answered.               |
| `-32008` | read-only                 | —                                   | The kernel was opened with `--read-only`.                                |
| `-32009` | unsupported               | `{"capability"}`                    | A method this build does not implement, see `harness.info.capabilities`. |
| `-32010` | kernel failed             | `{"reason"}`                        | The Session failed (storage). The kernel exits; the Host restarts it.    |
| `-32011` | process not found         | `{"processId"}`                     |                                                                          |
| `-32012` | timeout                   | `{"method","timeoutMs"}`            | The kernel's own deadline for the call.                                  |
| `-32013` | conflict                  | `{"requestId"}`                     | The same `requestId` was used for a different request.                   |
| `-32014` | model output invalid      | `{"issues":[...],"text"}`           | `model.complete`'s answer does not match the `schema` it was given.      |
| `-32015` | model call failed         | `{"status"?,"provider"?}`           | The provider answered with an error. `message` carries its own words.    |
| `-32016` | model not found           | `{"model"}`                         | `model.complete` with a provider or model id this build does not know.   |
| `-32017` | budget exceeded           | `{"maxTokens"}`                     | `maxTokens` or the call's own budget was above what the build allows.    |
| `-32018` | model refused             | `{"reason"?}`                       | The model declined to answer. Nothing can be salvaged by parsing again.  |

### Stopping the kernel

The Host stops a kernel two ways:

- **SIGTERM**, then SIGKILL after 12 s (macOS, Linux). This is how `process.stop` would do it, and it
  closes the harness and then the storage.
- **`kernel.shutdown`**, over the WebSocket that already carried the token. Windows has no SIGTERM a
  parent can send: a supervisor can only `TerminateProcess`, which would leave `storage.close()` unrun
  and the SQLite file to be recovered on the next start. So the Host asks instead.

The method answers `{"status":"closing"}` first and starts closing on the next tick, so a client waiting
for its answer is not cut off by the process going away. It then refuses new work (`-32008 read-only`,
message "shutting down"), closes the harness, closes the storage, and exits `0`; a kernel that cannot
close exits `1`. A second call during closing answers `{"status":"closing"}` again rather than
failing.

```json
{"jsonrpc":"2.0","id":"26","method":"kernel.shutdown"}
{"jsonrpc":"2.0","id":"26","result":{"status":"closing"}}
```

**A kernel ends the tools it started, and only those.** Pi runs each tool shell `detached`, in a process group
of its own, so a tool's background process (`sleep 100 &`, a server it started) is not in the kernel's group and
survives a plain `SIGKILL` of the kernel. The kernel therefore records each tool shell's group id and, when the
shell exits, the time it did — and at shutdown takes **one** `ps` snapshot and ends the groups whose identity
still checks out:

| The record                 | The snapshot                                               | What happens                                                                              |
| -------------------------- | ---------------------------------------------------------- | ----------------------------------------------------------------------------------------- |
| the shell is still running | —                                                          | killed: its pid is still its own, so the group cannot have been reused                    |
| the shell exited           | the group is empty                                         | dropped, nothing signalled                                                                |
| the shell exited           | a member started at or before the exit time                | killed: a reused group id can only hold processes that started after the number came free |
| the shell exited           | every member started after the exit time                   | dropped, nothing signalled — not enough evidence                                          |
| either                     | `ps` missing, failing, unreadable, or taking over a second | nothing is signalled, and a warning is logged                                             |
| the shell exited           | the wall clock moved more than a second since it exited    | nothing is signalled, and a warning is logged                                             |

A pid is not an identity, and neither is a group id: a record outlives the group it names, and the system may
hand that number to somebody else's tree. Killing a stranger is worse than missing one of ours, so an unclear
case does nothing.

Two things make the comparison itself safe:

- **The reference time is read after `ps`**, not before. A row's elapsed time describes the process at the
  moment the table was sampled, so taking the clock beforehand would add the snapshot's own duration to every
  start time's error; and a table that takes longer than a second is refused outright.
- **Start times and exit times are only compared when the wall clock did not move.** `ps` reports wall-clock
  uptime and the exit records are wall-clock readings, while `performance.now()` cannot be stepped: shutdown
  compares the wall-minus-monotonic drift with the drift recorded at the exit, and if they differ by more than
  a second the clock jumped and those groups are left alone.

Three consequences, recorded here rather than discovered later:

- **Missed:** our background process started a child and then exited, so only that grandchild remains. It
  started after the shell exited, the evidence says "not ours", and it survives the kernel.
- **Accepted risk:** a group id reused **inside the same second** as the exit reads as ours — elapsed times are
  printed to the second, so that is the resolution of the evidence itself.
- **Missed after a reboot or a clock step:** a jump leaves nothing to compare against.

Records are capped: past a thousand of them, one snapshot drops the ones whose groups have emptied. Nothing is
scanned while a tool runs — the `ps` cost is one snapshot at shutdown, not one call per tool.

On Windows there are no process groups: a tool's tree belongs to the Host's process tree — the Job Object the
Host gives the kernel — whose handle ends its members when the kernel exits, and the kernel signals nothing by
pid there.

A kernel killed with `SIGKILL` cannot run any of this; the Host's own process tree is then the only thing that
ends a tool that outlived it.

### Starting the kernel for a test

A test, a CI job or the Fake Daemon points the kernel at a local model without writing a config file:

```text
DROI_KERNEL_TOKEN=<random> \
DROI_KERNEL_PROVIDERS='[{"id":"fake","baseUrl":"http://127.0.0.1:9999/v1","api":"openai-completions","apiKeyEnv":"FAKE_PROVIDER_KEY","models":[{"id":"fake-1","contextWindow":200000,"maxTokens":8000,"input":["text","image"]}]}]' \
kernel --data <tmp dir> --port 0
```

`DROI_KERNEL_PROVIDERS` replaces the providers the config file names and can change nothing else: the
port, the data directory, the read-only flag and the token still come from the command line and the
environment. **It is not a way in.** Every connection needs `Authorization: Bearer <DROI_KERNEL_TOKEN>`,
and a token in the query string is refused like a wrong one — the header is the only place it is read.
An empty or malformed value is a start-up failure with status `1` rather than a silently unconfigured
kernel.

The settings a test needs beyond that:

| Flag or variable        | Meaning                                                                      |
| ----------------------- | ---------------------------------------------------------------------------- |
| `--data <dir>`          | Where `kernel.sqlite`, the lock and (in M3) process logs live. Required.     |
| `--port 0`              | Ask the OS for a free port; the handshake line reports it.                   |
| `--host 127.0.0.1`      | The interface to bind. The default.                                          |
| `--config <file>`       | A JSON file with `providers`, `agent`, `progress` and `viewPushesPerSecond`. |
| `--read-only`           | Answer reads for a viewer; every write is `-32008`.                          |
| `DROI_KERNEL_TOKEN`     | The launch token. Required; never on the command line.                       |
| `DROI_KERNEL_LOG=debug` | Log every frame and every commit to stderr.                                  |

One kernel per data directory, enforced by a lock **the operating system owns**. `<data>/kernel.lock.db` is a
SQLite database whose only job is to be locked: the first starter opens it, keeps a write transaction open
for the life of the process, and every other starter's first write fails with `SQLITE_BUSY` — the standard
`fcntl` / `LockFileEx` file lock, enforced across processes and released by the kernel when the process ends,
however it ends. A start that cannot take it is **refused with status `2`**, which the Host may retry; it is
not a crash.

There is nothing to clean up and nothing to judge stale: a lock left by a `SIGKILL`, a `taskkill /F` or a
segfault is gone the moment the process is, and all a killed kernel leaves behind is a hot journal that SQLite
rolls back for the next starter. `<data>/kernel.pid` is **diagnosis only** — the pid a refusal names — and no
version reads it to decide anything. A `<data>/kernel.lock` file of text from a pre-release build is ignored.

**Never delete or replace `kernel.lock.db` while a kernel is running.** The lock belongs to the file, so
removing it lets the next starter create a fresh one and take it — two kernels, one data directory, both
holding "the" lock. Nothing in the kernel removes or renames it, and a cleanup tool must not either; the only
file a kernel deletes is `kernel.pid`, which is advice. Replacing it _while no kernel runs_ is harmless.

One kernel per data directory is not a performance choice: a second one would open a second connection to the
same SQLite file, and a synchronous `busy_timeout` wait there blocks the whole event loop.

### Exit statuses

| Status | Meaning                                                                                         |
| ------ | ----------------------------------------------------------------------------------------------- |
| `0`    | Asked to stop (`kernel.shutdown`, SIGTERM, SIGINT) and closed cleanly.                          |
| `1`    | Could not start, or could not close: bad arguments, an unusable data directory, a bug, a crash. |
| `2`    | Refused to start because **another kernel holds the data directory**. A retry may succeed.      |
| `3`    | The storage failed. The Host must restart the kernel, not reuse it.                             |

`2` is the only status that means "try again later": another kernel owns the data directory, and it may exit.
Everything a retry cannot fix is `1` — missing token, bad arguments, a `kernel.lock.db` that is not a database
or a data directory that cannot be written — and the stderr line says which. `3` is a restart with the same
data directory.

### Page sizes

Two caps keep one answer from carrying a whole history, and both are published so a client never has to
guess:

| Limit                            | Value                  | Where it applies        |
| -------------------------------- | ---------------------- | ----------------------- |
| `limits.maxEntriesPerPage`       | 200                    | `conversation.entries`  |
| `limits.maxConversationsPerPage` | 200                    | `conversation.list`     |
| `limits.viewWindowDefault`       | `{min: 100, max: 200}` | the window a view holds |

`harness.info.extensions[]` names what this build installs: `{name, tools[], sections[]}`. The sections are
the prompt sections the extension renders (`environment`, `cwd` for the coding tools).

A `limit` above the cap is not an error and not a lie: the page comes back at the cap **with a cursor**,
and the client asks again with it. A client that ignores the cursor sees a prefix of the history
(ascending) or a suffix (descending) and cannot tell — which is exactly how a long run once looked
deadlocked at its 49th round: 200 entries in, a test asked for 5000, got the ascending first 200, and
waited forever for its own round. `harness.info.limits` exists so that is not a guess.

### Search

`conversation.list`'s `query` searches entry text and titles. The index is FTS5 with the **trigram**
tokenizer, which matters for Chinese: `unicode61` treats a whole run of characters as one token, so 内存
in 这是内存里的摘要 would never match. Trigram indexes every three-character sequence, so:

| Query                    | How it is answered                                              |
| ------------------------ | --------------------------------------------------------------- |
| three characters or more | the index: `text LIKE '%…%'` with the trigram `L0` optimization |
| one or two characters    | a table scan, bounded to the most recent 50,000 entries         |
| two words                | each narrowed by AND, in either language                        |

The short-term bound is the one place a search can miss something: a two-character Chinese word in an
entry older than the last 50,000 is not found. Measured, the scan over that window takes about 5 ms.
A title is matched with the whole query as one phrase, so a renamed conversation is found even when its
transcript holds nothing.

### The index

Pi's SQLite file holds the transcripts; Droi's own tables (`droi_` prefix) hold what a list, a search and
a restart need, and the kernel rebuilds every one of them from the transcripts when it has to
(`shared/design/kernel-index-ownership.md` has the rules and their reasons):

| Rule                                           | What it means                                                                                                                                                                                                                                                                                                                                                                           |
| ---------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| An entry belongs to `entry.conversationId`     | Pi's scan of a conversation **includes its ancestors' transcript**, and those entries keep their original `conversationId`. Indexing them again under the fork would give it its parent's `requestId` (a unique-index failure on the next start) and move the parent's search rows, because an FTS rowid is the entry ID and IDs are global.                                            |
| A fork writes its own `droi.meta`              | `{parentId, createdAt}`, and no `requestId`: its identity is its own, from its first entry.                                                                                                                                                                                                                                                                                             |
| Watermarks are per conversation                | A single number would have to pass every conversation at once, so a flush in the middle of a catch-up would skip the ones not read yet. The startup repair reads only above the watermark, which is why a failed flush puts its batch **back in the queue** instead of dropping it.                                                                                                     |
| `lastActivityAt` comes from entries            | Each persisted entry advances it with the entry's own message timestamp, so a long run's tool output keeps the conversation at the top while it works.                                                                                                                                                                                                                                  |
| `droi.meta` and `droi.request` are bookkeeping | They contribute no search text and no preview, and their timestamps are the conversation's `createdAt`, not its activity: a conversation holding only a `droi.meta` must not report 1970.                                                                                                                                                                                               |
| `index.version` (`INDEX_VERSION`)              | Kept in `droi_meta`, with `index.tokenizer`. A build whose value differs drops the caches (`droi_entries_fts`, `droi_index_watermarks`, `droi_requests`, the fingerprint columns) and rebuilds them from the transcripts on the next start. M1's databases are not a compatibility promise: the kernel has not shipped, and a migration for a shape nobody has is worse than a rebuild. |

### Read-only

`--read-only` makes the kernel a viewer of a database another process drives (design §9: a synced copy
on a device that only shows it). It is enforced three ways, not one:

1. Pi itself refuses every commit with writes (`HarnessOptions.readOnly`), so no new path can slip one in.
2. The kernel does not call `resume()`, so an unfinished run is not executed a second time here.
3. The kernel does not reconcile pending questions at start: a viewer writes nothing.
4. Every write method answers `-32008` before it reaches Pi.

### When the kernel fails

A Storage error is final in Pi: the Session closes and nothing works afterwards. The kernel then

- marks itself failed, so every later request (including one already in flight) answers `-32010` with the
  reason rather than `-32603`,
- announces `warning {kind:"kernel_failed"}` on every connection,
- closes the harness, the storage and the database, and **exits with status 3** (`kernel.shutdown` cannot
  turn that into `0`: in a failed state it answers `-32010` like every other method).

The exit is the point. A kernel that stays up but cannot write is invisible to the Host's Supervisor,
which only restarts a process that died.

## Identifiers

`conversationId`, `entryId`, `taskId`, `submissionId` and `documentId` are JSON
numbers (Pi's durable IDs, which are int64 and read as `bigint` in SQLite; the
kernel serializes them as numbers, so they are exact below 2^53, which is far
beyond any realistic count).

`requestId` on `conversation.create` and `conversation.submit` is the client's
own deduplication key, scoped to the conversation. Repeating it returns the same
conversation or submission; reusing it with different params is `-32013`.

`cursor` is opaque. A client sends back the `cursor` it received, verbatim, with
the same query and limit. `conversation.list`'s cursor is the kernel's own
(`{"before": <conversationId>}`); `conversation.entries`'s is Pi's.

## Methods

27 methods. "Capability" names the `harness.info.capabilities` flag that says
whether this build implements the method; `false` means `-32009 unsupported`.

| Method                 | Params                                                      | Result                                                           | Capability      | M1  |
| ---------------------- | ----------------------------------------------------------- | ---------------------------------------------------------------- | --------------- | --- |
| `harness.info`         | —                                                           | `{version, kernel, capabilities, providers, models, extensions}` | —               | yes |
| `conversation.create`  | `{agent?, cwd?, instructions?, kind?, requestId?}`          | `{conversationId, view}`                                         | `conversations` | yes |
| `conversation.list`    | `{kind?, archived?, workspace?, query?, cursor?, limit?}`   | `{items[], cursor?}`                                             | `conversations` | yes |
| `conversation.open`    | `{id, window?, subscribe?}`                                 | `{view}`                                                         | `conversations` | yes |
| `conversation.unwatch` | `{id}`                                                      | `{}`                                                             | `conversations` | yes |
| `conversation.submit`  | `{id, input?, continue?, whenBusy?, whenIdle?, requestId?}` | `{submissionId}`                                                 | `conversations` | yes |
| `conversation.abort`   | `{id, background?}`                                         | `{}`                                                             | `conversations` | yes |
| `submission.get`       | `{id, submissionId}`                                        | `{submissionId, status, entry?, answer?, reason?}`               | `conversations` | no  |

The Kernel reports only what Pi keeps: `status`, `entry` and `answer`, plus `reason` for one that ended without
an answer. It does not report where such a turn stopped, because it cannot: Pi 1.5.0 stores no link from a
submission to the run task that carried it, and a turn's entries carry a task rather than a submission, so any
end the Kernel derived would belong to a different turn after one skipped round (shared/design §7). **A client
bounds a turn by the status**: `[entry, answer]` for `done`, `[entry, the conversation's head]` while `placed`
— one conversation runs one turn at a time, and later inputs either queue or steer into the same run, which Pi
treats as that same turn and answers with the same `done` record — nothing for `queued`, and for `unanswered` a
client reports the reason rather than inventing a range.

Three qualifications a client that reads a range needs:

- **A queued submission writes no model-visible entry**, but it is not silent: `conversation.submit` with a
  `requestId` also appends the Kernel's own `droi.request` memo, in the same commit, for a submission that has
  not been placed. Keep `droi.*` entries out of a turn, or attribute them by their payload — a memo carries
  `{requestId, fingerprint, submissionId}`, so it belongs to the submission it names and to no other.
- **Read the status again after the transcript.** `submission.get` and `conversation.entries` are separate
  snapshots. Between them the run may end and a queued follow-up may already be answered, so a range read from
  a `placed` record can cover the next turn. Re-read and bound by what it says now: `unanswered` means the
  range is gone, `done` means it ends at `answer`.
- **A `continue` places no entry**, even when it answers, so `[entry, …]` has no lower bound for it. Its
  `answer` still names the reply; nothing else about its turn is bounded.

| `submission.abort` | `{id, submissionId}` | `{status}` | `conversations` | yes |
| `conversation.configure` | `{id, change}` | `{agent}` | `conversations` | yes |
| `conversation.compact` | `{id, instructions?}` | `{taskId}` | `compaction` | yes |
| `conversation.fork` | `{id, at}` | `{conversationId, view}` | `fork` | yes |
| `conversation.entries` | `{id, query?, limit?, cursor?}` | `{items[], cursor?}` | `conversations` | yes |
| `conversation.usage` | `{id}` | `{models, tools, context}` | `usage` | yes |
| `task.abort` | `{taskId}` | `{status}` | `tasks` | yes |
| `index.update` | `{id, title?, archived?, kind?}` | `{}` | `index` | yes |
| `process.list` | `{id}` | `{processes[]}` | `processes` | no |
| `process.stop` | `{id, processId}` | `{status}` | `processes` | no |
| `approval.respond` | `{requestId, outcome, scope?}` | `{status}` | `approvals` | no |
| `ask.respond` | `{requestId, answers?, cancelled?}` | `{status}` | `asks` | no |
| `mcp.list` | — | `{servers[]}` | `mcp` | no |
| `mcp.set` | `{name, server?, enabled?, remove?}` | `{servers[]}` | `mcp` | no |
| `mcp.authorize` | `{name}` | `{authorizationUrl}` | `mcp` | no |
| `mcp.authorize.cancel` | `{name}` | `{}` | `mcp` | no |
| `skills.list` | — | `{skills[]}` | `skills` | no |
| `skills.setDisabled` | `{name, disabled}` | `{skills[]}` | `skills` | no |
| `model.complete` | `{model, system?, input, schema?, thinkingLevel?, maxTokens?, timeoutMs?}` | `{model, text, json?, usage}` | `models` | no |

M1 answers the M3 methods still: `process.list` returns `{"processes":[]}`,
`mcp.list` and `skills.list` return an empty list, and the write methods and the
two `*.respond` methods are `-32009`. `model.complete` reports
`capabilities.models: false` until it is built; a Host that needs it for Memory
(ADR 0042) must read the flag rather than call it (M4).

### Values

`view` is Pi's `ConversationView`: `{conversation, entries, before?, docs}`.
`entries` is a window over the transcript; `docs` holds the built-in conversation
documents by kind (`pi.agent`, `pi.live`, `pi.usage`, `pi.inbox`, ...).
`conversation.view` sends the same object, so a client parses one shape.

`entry` is Pi's `EntryRecord`: `{id, conversationId, kind, model?, data?, head?,
edits?, byTaskId?}`. The six `pi.` kinds are fixed; the `droi.` kinds are the
kernel's own (`droi.approval`, `droi.ask`, `droi.subagent`, `droi.process`,
`droi.note`).

`agent` in requests is a change: a given field replaces the stored one, `null`
clears it, an absent field changes nothing. Extensions and tools cross the wire
as names, not objects:

```json
{
  "model": { "provider": "factory", "modelId": "claude-sonnet-4-5" },
  "thinkingLevel": "off",
  "extensions": { "add": ["coding"], "remove": [] },
  "modelTools": { "remove": ["bash"], "add": [] },
  "instructions": null,
  "cwd": "/Users/clive/dev/droi",
  "maxTurns": 40
}
```

`conversation.configure` answers with the resolved agent:
`{model?, thinkingLevel, extensions: [names], tools: [names], modelTools?,
instructions?, cwd?, maxTurns?}`.

`input` in `conversation.submit` is a string or a list of content blocks:
`{"type":"text","text":...}` and `{"type":"image","data":<base64>,"mimeType":...}`.
`continue: true` asks for a run on the context as it is, with no new user message.

### `model.complete`

One model call with no conversation and no tools. It exists for the work a Host
does with a model of its own — Memory's Session Summary and Memory Summary
(ADR 0042) — so that work needs no hidden conversation, no archive, and no
record a Client can open. The kernel sends the request, validates the answer
when a `schema` is given, and answers once; nothing is stored, and no entry,
conversation or task is created.

| Param           | Meaning                                                                                                  |
| --------------- | -------------------------------------------------------------------------------------------------------- |
| `model`         | `{provider, modelId}`, the same shape `agent.model` takes.                                               |
| `system`        | Optional system text. Absent means the kernel's own, minimal one.                                        |
| `input`         | A string, or a list of content blocks: `{"type":"text","text":...}`.                                     |
| `schema`        | Optional JSON Schema. When given, the answer is parsed as JSON and must match it.                        |
| `thinkingLevel` | Optional: `off`, `low`, `medium`, `high`. Absent means the model's default, which may think for minutes. |
| `maxTokens`     | Optional cap on the answer.                                                                              |
| `timeoutMs`     | Optional deadline for the whole call, at most `600000`. Absent means `120000`.                           |

The result is `{model, text, json?, usage}`:

- `model` is the model that answered, resolved: a `modelId` the build does not
  know is `-32016 model not found`, never a silent substitute, because Memory
  records which model wrote a summary.
- `text` is the answer as text, always present.
- `json` is present when `schema` was given: the parsed answer, which has already
  been checked against the schema. A schema that the answer does not satisfy is
  `-32014 model output invalid`, with the issues and the text the model did
  answer (`data.issues`, `data.text`), so the caller can retry or report; it is
  not a parse error and the call is not retried by the kernel.
- `usage` is `{input, output, totalTokens, cost?}`, per call.

A model that declines to answer is `-32018 model refused`, with `data.reason`
where the provider gave one. That is distinct from `-32014`: nothing can be
salvaged from a refusal by parsing it again.

What this method does not do, and deliberately: it does not stream (a Host waits
for the whole answer and the answer is small), it keeps no history (a caller
that wants one passes the earlier turns in `input`), it runs no tool, and it
writes no usage row of its own — the `usage` in the result is the record.

## Notifications

| Notification         | Payload                                                                                                                                                                                                              |
| -------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `conversation.view`  | `{id, kind: "value"\|"full"\|"failed", value, ops, retired}` — Pi's `WatchCall`, or `{id, kind:"failed", error}`; `value` is `null` for a `"delta"` subscriber's `"value"` push, and whole for `"view"` and `"full"` |
| `conversation.busy`  | `{id, run, runFailure?}` — `run` is Pi's `LiveRun` or `null`                                                                                                                                                         |
| `approval.requested` | `{requestId, id, tool, args, options[], scopeHint?}`                                                                                                                                                                 |
| `approval.resolved`  | `{requestId, id, outcome, by}`                                                                                                                                                                                       |
| `ask.requested`      | `{requestId, id, questions[], context}` — `context` is required and non-empty: the sentence that says what the answer is for                                                                                         |
| `ask.resolved`       | `{requestId, id, answers?, cancelled}`                                                                                                                                                                               |
| `task.changed`       | `{taskId, kind, status, conversationId}`                                                                                                                                                                             |
| `mcp.changed`        | `{servers[]}`                                                                                                                                                                                                        |
| `process.changed`    | `{id, processId, status, exitCode?, tail?}`                                                                                                                                                                          |
| `index.changed`      | `{id, title?, archived?, lastActivityAt?}`                                                                                                                                                                           |
| `warning`            | `{kind, message}` — Pi `onReport` kinds: `storage_failed`, `task_faulted`, `task_interrupted`, `migration_failed`, `task_outdated`, `task_error`, `extension_error`, `host_error`, `listener_error`                  |

Who receives each one is in _Connections and subscriptions_ above: only
`conversation.view` is per connection, everything else reaches every connection.

A client that subscribes with `"view"` consumes `conversation.view.value` and ignores
`ops`: redrawing a window of at most 100 entries is cheap, and it needs no state
of its own. A client that subscribes with `"delta"` applies `ops` to the state it
kept and replaces that state whenever a `"full"` arrives — the window over the
Relay pays for the bytes it does not send, and gets the guarantee that it always
has a way back to a whole view.

## Fixtures

```
fixtures/
  index.json            every fixture file, in the order a client meets them
  methods/<method>.json one request and its response
  notifications/<name>.json one notification
  errors/<case>.json    a request and its error response
```

A method fixture:

```json
{
  "kind": "method",
  "method": "conversation.open",
  "request": { "jsonrpc": "2.0", "id": "4", "method": "conversation.open", "params": { "id": 7 } },
  "response": { "jsonrpc": "2.0", "id": "4", "result": { "view": {} } }
}
```

A notification fixture:

```json
{
  "kind": "notification",
  "method": "conversation.busy",
  "notification": {
    "jsonrpc": "2.0",
    "method": "conversation.busy",
    "params": { "id": 7, "run": null }
  }
}
```

An error fixture adds `"error": {"code": -32000, "name": "conversation not found"}`
beside `request` and `response`.

Ids in the fixtures are illustrative. The numbers inside (`conversationId`,
`entryId`, timestamps) are sample values that are internally consistent, so a
fixture set reads like one session.
