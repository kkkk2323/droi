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
| `memory.read`                              | Every connection                                                           |
| `harness.changed`                          | Every connection                                                           |
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

| `subscribe`        | A `conversation.view` carries                                                | Who uses it                  |
| ------------------ | ---------------------------------------------------------------------------- | ---------------------------- |
| `"view"` (default) | `{id, kind: "value", value, ops, retired}` — the window over entries, whole  | A client that keeps no state |
| `"delta"`          | `{id, kind: "value", value: null, ops, retired}` — Pi's incremental ops only | The phone and the window     |

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

- **SIGTERM**, then SIGKILL after 12 s (macOS, Linux). It closes the harness and then the storage.
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
| `limits.maxImportEntriesPerCall` | 1000                   | `conversation.import`   |
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
a restart need, and the kernel rebuilds every one of them from the transcripts when it has to, except two that
record what a person or a live process decided and that a reset never drops: `droi_disabled_skills` and
`droi_open_questions` (the non-blocking questions still open; a start drops a row whose question the
transcript no longer shows as pending)
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

`conversation.create` takes `kind` and `instructions`; both are stored. **`kind` is read only at the top
level** — it is a property of the conversation, not of its agent, so a `kind` inside `agent` is ignored rather
than stored or applied. It is Droi's own label (`chat`, `automation`, `section`, …): it goes into the
conversation's `droi.meta` entry and the index row, `conversation.list` filters on it, and Pi never sees it.
`instructions` is the agent's, and it is read from the top level and from inside `agent`.
A top-level field beats the config file's agent defaults, and `agent` beats the top level, so a Host can set a
default and a caller can still override one field of it.

`requestId` on `conversation.create` and `conversation.submit` is the client's
own deduplication key, scoped to the conversation. Repeating it returns the same
conversation or submission; reusing it with different params is `-32013`.

`cursor` is opaque. A client sends back the `cursor` it received, verbatim, with
the same query and limit. `conversation.list`'s cursor is the kernel's own
(`{"before": <conversationId>}`); `conversation.entries`'s is Pi's.

`conversation.entries`' `query` is `{order?, minEntryId?, maxEntryId?, kinds?}`. `kinds` (a non-empty list of
entry kinds, such as `["pi.user"]`) answers only entries of those kinds, filtered in the kernel so a client that
wants a long conversation's questions does not carry its answers and tool output. Such an answer has no cursor
(Pi's points after the last entry scanned, not the last one returned) and a `cursor` beside `kinds` is
`-32602`: the next page narrows `maxEntryId` (descending) or `minEntryId` (ascending) past the last item, and a
page shorter than `limit` is the last.

## Methods

32 methods. "Capability" names the `harness.info.capabilities` flag that says
whether this build implements the method; `false` means `-32009 unsupported`.

| Method                 | Params                                                                        | Result                                                                            | Capability      | M1  |
| ---------------------- | ----------------------------------------------------------------------------- | --------------------------------------------------------------------------------- | --------------- | --- |
| `harness.info`         | —                                                                             | `{version, kernel, capabilities, providers, models, extensions, policy?, agent?}` | —               | yes |
| `harness.configure`    | `{providers?, memoryDir?, section?, agent?, codemode?, compaction?}`          | `{}`                                                                              | —               | no  |
| `conversation.create`  | `{agent?, cwd?, instructions?, kind?, requestId?}`                            | `{conversationId, view}`                                                          | `conversations` | yes |
| `conversation.list`    | `{kind?, archived?, workspace?, repoRoot?, parent?, query?, cursor?, limit?}` | `{items[], cursor?}`                                                              | `conversations` | yes |
| `conversation.open`    | `{id, window?, subscribe?, seen?}`                                            | `{view, policy?, pending[]?}`                                                     | `conversations` | yes |
| `conversation.unwatch` | `{id}`                                                                        | `{}`                                                                              | `conversations` | yes |
| `conversation.submit`  | `{id, input?, continue?, whenBusy?, whenIdle?, requestId?}`                   | `{submissionId}`                                                                  | `conversations` | yes |
| `conversation.abort`   | `{id, background?}`                                                           | `{}`                                                                              | `conversations` | yes |
| `submission.get`       | `{id, submissionId}`                                                          | `{submissionId, status, entry?, answer?, reason?, detail?}`                       | `conversations` | no  |

The Kernel reports only what Pi keeps: `status`, `entry` and `answer`, plus `reason` for one that ended without
an answer and, when Pi kept it, `detail`: why, in the model's or the harness's words, for a client that ran the
turn unattended and has nowhere else to read it. It does not report where such a turn stopped, because it cannot: Pi 1.5.0 stores no link from a
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
| `conversation.compact` | `{id, instructions?, wait?}` | `{taskId, outcome?, error?}` | `compaction` | yes |
| `conversation.fork` | `{id, at}` | `{conversationId, view}` | `fork` | yes |
| `conversation.import` | `{id?, key?, entries, agent?, cwd?, kind?, title?, createdAt?, archived?, instructions?}` | `{conversationId, entryIds, replayed?}` | `import` | yes |
| `conversation.entries` | `{id, query?, limit?, cursor?}` | `{items[], cursor?}` | `conversations` | yes |
| `conversation.usage` | `{id}` | `{models, tools, context}` | `usage` | yes |
| `task.abort` | `{taskId}` | `{status}` | `tasks` | yes |
| `index.update` | `{id, title?, titleSource?, archived?, kind?, viewedAt?, ownerId?}` | `{}` | `index` | yes |
| `process.list` | `{id}` | `{processes[]}` | `processes` | no |
| `process.stop` | `{id, processId}` | `{status}` | `processes` | no |
| `policy.set` | `{id?, policy: {autonomy?, rules?}}` | `{policy}` | `approvals` | no |
| `approval.respond` | `{requestId, outcome, scope?}` | `{status}` | `approvals` | no |
| `ask.respond` | `{requestId, answers?, cancelled?}` | `{status}` | `asks` | no |
| `mcp.list` | `{repoRoot?}` | `{servers[]}` | `mcp` | no |
| `mcp.set` | `{name, server?, enabled?, remove?, repoRoot?, trust?, fingerprint?}` | `{servers[]}` | `mcp` | no |
| `mcp.authorize` | `{name, repoRoot?}` | `{authorizationUrl}` or `{authorized: true}` | `mcp` | no |
| `mcp.authorize.cancel` | `{name, repoRoot?}` | `{}` | `mcp` | no |
| `skills.list` | — | `{skills[]}` | `skills` | no |
| `skills.setDisabled` | `{name, disabled, id?}` | `{skills[]}` | `skills` | no |
| `resources.list` | `{id?}` | `{skills[], prompts[], contextFiles[]}` | `resources` | no |
| `droids.list` | — | `{droids[], diagnostics[]}` | `subagents` | no |
| `droids.set` | `{name, droid?, remove?}` | `{droids[], diagnostics[]}` | `subagents` | no |
| `model.complete` | `{model, system?, input, schema?, thinkingLevel?, maxTokens?, timeoutMs?}` | `{model, text, json?, usage}` | `models` | yes |

Every M3 method is built (`processes`, `mcp`, `skills`, `resources` and `codemode` are true). `-32009` stays the
answer for a method a build leaves out, with the capability that would bring it. `model.complete` is built: it reports
`capabilities.models: true` and is the call a Host makes for a name or a Memory
summary of its own (ADR 0042).

`conversation.compact {wait: true}` answers once the compaction ended: `outcome` is `compacted` (the summary is
written, or placed at the next boundary of a run under way), `nothing` (the whole context is within the newest part
a summary keeps verbatim, about 20,000 tokens, so there was nothing to summarize), `failed` with `error`, or `aborted`.
Without `wait` it answers `{taskId}` at once.

### Background processes: `bash_background`, `process.list`, `process.stop`

The `bash_background {command, cwd?}` tool starts a command that keeps running after the call, and answers at
once with `{processId, pid, logPath}` (also its `structuredOutput`). It runs in the shell `bash` uses
(PowerShell on a Windows without Git Bash), in the conversation's working directory or `cwd` relative to
it, with stdout and stderr in `<data>/logs/<conversation>/<processId>.log`. Its start and its end are
`droi.process` entries (`running`, then `done`, `failed` or `stopped`), each announced by
`process.changed`; the end carries `exitCode` and the last 4 KiB of the log as `tail`. The approval policy
judges it as it judges `bash`.

`process.list {id}` answers every process the conversation's transcript names, newest state each:
`{id, pid, command, logPath, startedAt, status, exitCode?, endedAt?, callId?}` with `status` in the
`process.changed` words (`exited` for an entry's `done`); `callId` is the `bash_background` call that started
it, so a client can show the process beside that call.

`process.stop` ends a background process and everything it started, and answers `{status}`: `stopped`, or
`gone` when there was nothing left to stop; a `processId` the conversation never had is `-32011`. On macOS
and Linux the kernel signals the process group (SIGTERM, then SIGKILL after 5 s), and only while the group
is still the one it started, by the rule a shutdown uses (below): never a bare PID. On Windows there is no
group to signal, so it runs `taskkill /T /F` — but only while this run of the kernel still holds the child's
handle, which keeps the PID from naming another program. A process the kernel no longer holds on Windows
(it was started before a restart) answers `gone`: the Host's Job Object already ended it when the old
kernel went.

**Background processes end with the kernel**, like every tool process: a kernel that stops ends them and
records `stopped`. A kernel killed with `SIGKILL` cannot; on macOS and Linux the next start finds the
processes it left (`droi_processes`, a table of the running ones), keeps listing those whose group still
holds a member as old as the process, and can still stop them; the rest read `stopped`.

### The list, and what it leaves out

`conversation.list` answers the conversations a person started: rows whose `ownerId` is absent. A
conversation that something spawned — a subagent's or a Workstream agent's own — is reached with
`parent: <id>`, which lists what that conversation owns instead. Nothing is filtered by `kind`.

**`parentId` and `ownerId` are two different questions.** `parentId` is lineage: the conversation a fork was
taken from, which a client shows as "forked from". `ownerId` is ownership: the conversation whose agent
started this one, and the reason it stays out of the list (ADR 0043). A fork is a conversation a person
asked for, so it is a row of its own; a subagent's conversation is not.

Three fields a sidebar groups and sorts by:

- `workspace` is the conversation's `cwd`, exactly as it was resolved: the directory its tools run in.
- `repoRoot` is the repository that `cwd` sits in: the parent of
  `git rev-parse --path-format=absolute --git-common-dir`, or `--show-toplevel` for a submodule or a bare
  repository, and the directory itself when git says it is not inside one. It is **not** `--show-toplevel`
  of the `cwd`: that gives every worktree a group of its own, where the common directory gives a worktree
  its main checkout's `repoRoot`, which is what makes one repository one group. It is worked out when the
  conversation is created, forked or its `cwd` changes, and stored in the index, so listing never launches
  git. Absent when git could not be asked (no git, a directory not there yet, a timeout); the next start
  asks again. Needs git 2.31 or newer (`--path-format`). A fork has its source's `workspace`, `repoRoot`
  and `kind`, and its source's title as `auto`. A `cwd` set to `null` clears `workspace` and `repoRoot`.
- `titleSource` says where `title` came from: `auto` is the kernel's own guess (the first line of
  the first user message), `chosen` is a title a person set through `index.update` or a generator that said
  so. Absent while a conversation has no title at all. The text alone cannot be compared against the kernel's
  truncation rule — a person may type exactly what the kernel would have guessed. `index.update` with
  `title: null` gives the conversation back its automatic name, the first line of its first user message,
  as `auto` (or no title, until there is a message). An empty string is a title a person chose.
- `busy: true` marks a conversation whose run is under way, so a client that connects mid-run can show it
  before the run's next `conversation.busy`; absent otherwise.
- `preview` is the first line of the newest message in the transcript — the answer once there is one, the
  question while it is still being written — cut to 200 characters with an ellipsis. It is plain text: no
  search marks, no entry kinds.

A `viewedAt` in `index.update` is the unread marker: a millisecond time says "a person has seen everything
up to here", `null` asks for the dot back, and an absent field leaves it as it was. `conversation.open`
writes a time by itself (unless it carries `seen: false`, which a client sends when it opens a conversation again on
a new connection without a person looking at it), so a client only needs this call when a conversation is on screen while an answer
arrives, or when it wants a cleared one to look unread again. Either way every client hears it: `index.changed`
carries `viewedAt`, so a dot one client clears goes away on the others too. A conversation is unread while its
`lastActivityAt` is later than its `viewedAt`, or while `viewedAt` is `null`. A new conversation starts seen up to
its creation, and one imported or rebuilt from storage up to its last activity: only what happens after is news.
`null` is a dot a client asked for.

`ownerId` sets which conversation owns this one, and `null` makes it a list row again. The kernel writes
this itself when a subagent or a Workstream spawns a conversation (M4); the method is how anything imported
gets the same relation. An owner that does not exist is `-32000`; one that is the conversation itself, or
that would make the two own each other, is `-32602`.

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

### The documents a client reads

`docs` is Pi's document store, passed through unchanged: the kernel reads it, never rewrites it. Four of
the kinds are the ones a client renders from, and their fields are Pi 1.5.0's own — the kernel adds and
removes nothing, so what is missing here is missing in the store rather than in the wire.

**`pi.agent`** — `{model?: {provider, modelId}, thinkingLevel?, instructions?, cwd?, maxTurns?, tools?, ...}`:
the agent **as it is stored**, not the resolved view — it holds what the conversation was configured with,
while `conversation.configure` answers that configuration resolved against the registry (extension names
expanded to tools, defaults filled in). A field absent here is one that was never set.

**`pi.inbox`** — the messages waiting for the running turn, in submission-ID order:

```json
{
  "items": [
    { "id": 17, "mode": "followUp", "content": "queued one" },
    { "id": 18, "mode": "steer", "content": "steer one" },
    { "id": 19, "mode": "write", "entry": { "kind": "droi.note", "data": { "text": "…" } } }
  ]
}
```

`mode` is `steer` or `followUp` for a user input and `write` for an entry the client asked to append;
`content` is the item's `conversation.submit` input, and `entry` is the draft it will be placed as. An
item leaves the list when a boundary places it, so the list is exactly "not yet in the transcript". The
submission `id` is the one `conversation.submit` answered, which is how a client pairs a queued message
with `submission.abort`.

**`pi.live`** — the running state. Everything a streaming view needs is here:

| Field               | Shape                                                                                                                  | When it holds                                     |
| ------------------- | ---------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------- |
| `run`               | `{taskId, inputs[], turns, first?}`                                                                                    | exactly while busy                                |
| `runFailure`        | `{taskId, inputs[], reason, detail?, first?}`                                                                          | the newest run that ended without an answer       |
| `generation`        | `{attempt, message?, retry?, deferred?}`                                                                               | while a model request is being made               |
| `generation.retry`  | `{at, error}`                                                                                                          | a failed attempt backing off, `at` in epoch ms    |
| `tools`             | `[{callId, name, status, output?, droppedBytes?, droppedLines?, details?, diagnostics?, taskId?, entry?, unoffered?}]` | the current tool round, in call order             |
| `nestedTools`       | `[{parentCallId, parentTaskId, taskId, callId, name, arguments, status, output?, summary?}]`                           | calls a running tool made through `executeTool()` |
| `compactions`       | `[{taskId, trigger, blocking, attempt, retry?}]`                                                                       | live compaction tasks                             |
| `compactionFailure` | `{taskId, trigger, error}`                                                                                             | the newest compaction that failed                 |

`tools[i].output` is the running output of a call that has not finished (`droppedBytes`/`droppedLines`
say what the bounds dropped), and it is cleared once the call settles: the result entry replaces it.
`nestedTools` is in creation order, so a parent precedes its children, and a call's slots disappear when
it settles.

**`pi.usage`** — `{models: {"provider/model": {input, output, cacheRead, cacheWrite, totalTokens, cost}},
tools: {...}}`, the ledger `conversation.usage` reads.

What `apps/kernel/test/documents.test.ts` pins, and what it does not:

| Claim                                                              | Pinned                                                                               |
| ------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| `pi.inbox` items: `id`, `mode`, `content` of a queued input        | yes                                                                                  |
| `pi.inbox` item with `mode: "write"` and its `entry` draft         | no — the kernel never submits a write; the shape is Pi's (`harness/inbox.d.ts:5-20`) |
| `pi.live.tools[i].output` while the call runs                      | yes                                                                                  |
| a `done` slot has no output, seen while its sibling still runs     | yes                                                                                  |
| `nestedTools` from a call a **tool** makes through `executeTool()` | yes, with a test extension                                                           |
| `nestedTools` from a **shipped** tool (`codemode`)                 | the result's `details.calls` with each call's `output` (`test/codemode.test.ts`)     |
| `pi.live.compactions` fields while a compaction runs               | yes                                                                                  |
| `pi.live` has no `compactions` key while idle                      | yes                                                                                  |
| `generation.retry` as `{at, error}` across attempts                | yes                                                                                  |
| `pi.agent` holds the **stored** configuration                      | yes, with `conversation.create`/`configure`                                          |

### The `droi.` entries

The kernel's own kinds are model-invisible bookkeeping that a client renders as cards. These are the
shapes the kernel guarantees — the first three exist today, the rest are fixed now and written in M3/M4,
so a client can build the card before the feature lands:

| kind                 | `data`                                                                                 | Written                     | Exists |
| -------------------- | -------------------------------------------------------------------------------------- | --------------------------- | ------ |
| `droi.meta`          | `{createdAt, requestId?, requestFingerprint?, kind?, workspace?, parentId?, ownerId?}` | the kernel                  | yes    |
| `droi.request`       | `{requestId, fingerprint, submissionId}`                                               | the kernel                  | yes    |
| `droi.note`          | `{text}`                                                                               | the kernel                  | yes    |
| `droi.approval`      | `{requestId, tool, args, status, outcome?, scope?, at}`                                | the approval layer          | M3     |
| `droi.ask`           | `{requestId, questions[], context, blocking, status, answers?, cancelled?, at}`        | the ask tool                | M3     |
| `droi.subagent.done` | `{conversationId, subagentType, description, status, background, callId, output}`      | a background subagent's end | yes    |
| `droi.process`       | `{processId, pid, command, logPath, callId?, status, exitCode?, at}`                   | `bash_background`           | M3     |

`status` is the entry's current state, one of `pending` / `allowed` / `denied` / `abandoned` for an
approval, `pending` / `answered` / `abandoned` for an ask, `running` / `done` / `failed` / `stopped` for a
process and for a subagent. `at` is epoch milliseconds. An entry that has not settled yet is the newest
record of that `requestId`: the kernel appends one entry per change rather than rewriting.

### Subagents

The `subagents` extension gives a conversation Factory Droid's subagent tools: `task {subagent_type, description,
prompt, image_paths?, complexity?, run_in_background?, resume?}`, `task_output {task_id, block?, timeout_seconds?}` and
`task_stop {task_id}`. A subagent runs in a conversation of its own, a child of the call (ADR 0043): its index row has
`kind: "subagent"` and `ownerId` the parent, so `conversation.list` leaves it out and `{parent}` finds it. `task_id` is
that conversation's id. The child has neither `task` nor `ask_user`.

A foreground call's `details` is `{conversationId, subagentType, description, status}` from the moment the child
exists, `status` `running` until the call answers `completed`, `failed`, or `stopped` when the child's run was
aborted (`conversation.abort` on `conversationId`, which is how a client stops one); a client opens `conversationId`
to show the subagent's work under the card. A background call answers at once with `background: true`; its end writes
`droi.subagent.done` into the parent, as a background script's end writes `droi.codemode.done`.

The droids are `worker` and `explorer`, built in, and a person's own in `<userDir>/droids/<name>.md` (Markdown with
YAML frontmatter `name`, `description`, `model?`, `reasoningEffort?`, `tools?`; the body is the prompt). One named as a
built-in takes its place. `droids.list` answers `{droids: [{name, description, prompt, model?, reasoningEffort?, tools?,
source, path?}], diagnostics: [{path, message}]}`, `source` `builtin` or `personal`; `droids.set` writes or removes
a person's own and answers the same.

### `conversation.import`

Write a transcript that already exists — M2's history constructor, and M5's importer for old droid sessions.
Without `id` it creates the conversation; with `id` it appends one batch to an existing one.

| Param                                                       | Create                | Append                                                    | Meaning                                                   |
| ----------------------------------------------------------- | --------------------- | --------------------------------------------------------- | --------------------------------------------------------- |
| `entries`                                                   | required, may be `[]` | required                                                  | Pi entry drafts, at most `limits.maxImportEntriesPerCall` |
| `id`                                                        | absent                | required                                                  | The conversation to append to; unknown is `-32000`        |
| `key`                                                       | optional              | optional                                                  | Idempotency key, see below                                |
| `agent`, `cwd`, `kind`, `title`, `instructions`, `archived` | optional              | **forbidden** (`-32602`, `issues[].path` names the field) | The same fields `conversation.create` takes               |
| `createdAt`                                                 | optional, epoch ms    | forbidden                                                 | The conversation's creation time, not the import's        |

It answers `{conversationId, entryIds, replayed?}`, where `entryIds` are the IDs the batch's entries got, in
order. The result carries no view: a forty-entry history's view is only the entries just written, and a caller
that wants one calls `conversation.open`.

**One batch, one commit.** The entries of one call are written together, so a client never sees half of them,
and the IDs are consecutive. A batch starts no run, creates no submission and does not touch `pi.live` or
`pi.inbox`: an import is history, not a turn.

**The timestamps are the messages' own.** `entry.model[j].timestamp` decides when a message happened, and the
conversation's `lastActivityAt` is the newest of them, so a list sorted by activity sorts the history as it
unfolded. A message without a timestamp is refused (`-32602`), because inheriting the import's time is exactly
what an import must not do.

**Usage is recorded.** A `pi.assistant` message that carries `usage` adds it to the conversation's `pi.usage`
the way a real answer does, so `conversation.usage` works on an imported history too. A missing counter or
cost counts as zero.

**Only structure is checked.** An imported history is somebody else's past: a tool result with no call before
it, or an answer that follows no user message, is preserved rather than refused. What is refused is what the
kernel owns — `droi.meta`, `droi.request` and `droi.import` (`issues[].path` is `entries[i].kind`) — because
writing one would forge an identity or a memo.

**The key travels in the same commit as the conversation, and means one of two things.** A `droi.import` memo
holds `{key, fingerprint, entryIds, scope, at}`, written with Pi's own conversation, its meta and its entries, so
a crash cannot separate them. `scope` is `"create"` or `"append"`:

- a **create** key is global: a retry of the same call answers the conversation it already made. The kernel
  caches those identities in `droi_import_keys`, written in the same index flush as the watermark covering the
  memo and rebuilt by the catch-up after an `INDEX_VERSION` reset. The cache is complete once the index is, so a
  miss means the key never created a conversation — there is no fallback search over the transcripts.
- an **append** key is local: it replays only inside the conversation that received it. Using one for a create,
  or a create's key for an append, is `-32013` rather than a guess.

**The fingerprint covers the entries.** The same key with different entries is a different request and answers
`-32013`, on both paths: a create that would otherwise silently keep the first history, and an append that would
otherwise answer with the old ids and drop what was asked for.

**An append into a busy conversation is `-32001`.** The check runs inside the commit that writes, against
`pi.live.run`: a check before it would leave the window a submission can slip through, and an imported entry
between a run's question and its answer is a transcript that never happened. The conversation's own clock is
unaffected, and the batch writes nothing.

**One key is admitted once at a time.** Two concurrent calls with the same key converge on one conversation
rather than race into two.

**Validation is structural, for every field Pi reads, and a `head` must point at something real.** `kind` must be
a non-empty string; `model` must be Pi's messages with a numeric `timestamp`; `edits` must be
`{target, action: "omit" | "replace", messages?}` with each replacement message checked in turn; `head` must be
`"self"` or a **safe** entry ID (at most 2^53−1 — a value outside it reaches SQLite's INTEGER column and poisons
the Session) that the conversation actually holds. One batch cannot point at itself: on a create there are no
entries yet, so only `"self"` is a valid head there. Every refusal is `-32602` with the field's path, and nothing
is stored. An assistant message without `usage` is stored with Pi's zero usage, so the next turn measures it by its
content instead of faulting on it.

**Activity follows messages only.** A batch with no message time (a `droi.note`, an empty append) leaves
`lastActivityAt` where it was, and a history imported without `createdAt` takes its clock from its first message,
not from the time of the import. The workspace is the resolved `cwd` — `agent.cwd` over the top-level one — the
same value `conversation.create` records. Every usage counter Pi keeps survives (`cacheWrite1h`, `reasoning`
included), so `conversation.usage` on an imported history matches a real one.

**Documents are not imported.** `pi.agent` comes from `agent`, `pi.usage` from the messages' usage, and
`pi.live` / `pi.inbox` are run state that only a real run produces — a test that needs those states drives the
model instead of importing them.

`capabilities.import` says whether this build has it.

### `model.complete`

One model call with no conversation and no tools. It exists for the work a Host
does with a model of its own — Memory's Session Summary and Memory Summary
(ADR 0042) — so that work needs no hidden conversation, no archive, and no
record a Client can open. The kernel sends the request, validates the answer
when a `schema` is given, and answers once; nothing is stored, and no entry,
conversation or task is created.

| Param           | Meaning                                                                                                                                                                                                                                                                              |
| --------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `model`         | `{provider, modelId}`, the same shape `agent.model` takes.                                                                                                                                                                                                                           |
| `system`        | Optional system text. Absent sends the kernel's own minimal instruction, never a bare user turn.                                                                                                                                                                                     |
| `input`         | A string, or a list of content blocks: `{"type":"text","text":...}`.                                                                                                                                                                                                                 |
| `schema`        | Optional JSON Schema. When given, the answer is parsed as JSON (inside a Markdown code fence, when the model put it in one) and must match it.                                                                                                                                       |
| `thinkingLevel` | Optional: `off`, `minimal`, `low`, `medium`, `high`, `xhigh` (a model's own levels, as Settings picks them for Memory). `off` asks for no thinking and sends nothing, which is the provider's own default: a model that reasons unless told otherwise still may. Absent is the same. |
| `maxTokens`     | Optional cap on the answer: a positive whole number, at most what the model itself takes (`-32017`).                                                                                                                                                                                 |
| `timeoutMs`     | Optional deadline for the whole call, at most `600000`. Absent means `120000`.                                                                                                                                                                                                       |

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
- `usage` is `{input, output, totalTokens, cost?}`, per call: the token counts the provider
  reported, and `cost` as dollars for this call when the model's catalog states a price (absent when it
  is free or unpriced, so a caller can tell "nothing" from "nothing to report").

A model that declines to answer is `-32018 model refused`, with `data.reason`
where the provider gave one. That is distinct from `-32014`: nothing can be
salvaged from a refusal by parsing it again.

The request carries `X-Droi-Call: model.complete`. That header is not part of the contract: it exists so a
scripted model in a test can answer this call differently from a conversation's, because pi-ai sends every
request as a stream and there is no other way to tell them apart. A client never sets it and never reads it.
The Host's LLM proxy is the place an `X-Droi-*` header must be removed before a request leaves the machine.

What this method does not do, and deliberately: it does not stream (a Host waits
for the whole answer and the answer is small), it keeps no history (a caller
that wants one passes the earlier turns in `input`), it runs no tool, and it
writes no usage row of its own — the `usage` in the result is the record.

### What `conversation.open` also answers: `pending`

A request for an answer reaches every connection **once**, as `approval.requested` or `ask.requested`. A
connection that was not there when it went out — the window reconnected, the kernel restarted after the
computer slept — would never learn about it, and the card that is waiting for a person would be a card
nobody sees.

So `conversation.open` answers the requests still waiting on that conversation:

```json
"pending": [
  {"kind": "approval", "requestId": "req-x3rtg4", "id": 7, "tool": "bash",
   "args": {"command": "rm -rf build"}, "options": [...], "scopeHint": "ask"},
  {"kind": "ask", "requestId": "req-ask-77", "id": 7,
   "questions": [...], "context": "Asked while running the test command.", "blocking": true}
]
```

Each entry carries exactly the fields its notification carries, plus `kind` to say which one it is. An empty
list is the normal answer. It is a snapshot of the kernel's own table of open requests, so it is right at the
moment of the call and no older: a client rebuilds its cards from every `conversation.open` (the first one,
and the one after a reconnect), and keeps them current with the two notifications as before.

Answering is unchanged: `approval.respond` / `ask.respond` with the `requestId`, first answer wins, a later
one is `-32007`.

Three fields a card needs:

- `scopeHint` on an approval is the autonomy level that made the call ask (`ask`, `edits` or `safe`), so a
  card can say why it is asking. What an "always" answer remembers is each option's own `scope`.
- `blocking` on an ask says the run waits for the answer: the card takes the composer's place. A
  non-blocking ask sits above the composer and its answer arrives as the next turn.
- `by` on `approval.resolved` / `ask.resolved` is the `X-Droi-Client` header of the connection that
  answered (`window`, `phone`, `host`), `unknown` for a connection that sent none, and `kernel` for a
  request the kernel settled itself (its ten-minute timeout, or a restart).

### The `ask_user` tool

The model asks with `{context, blocking?, questions: [{questionId?, question, options?: [{id, label}], multiSelect?}]}`.
`context` is required and non-empty; `questionId` defaults to `q1`, `q2`, … in order. Every connection gets
`ask.requested`, and `ask.respond {requestId, answers}` answers it, or `{requestId, cancelled: true}` dismisses
it. Each answer is `{questionId, selected?, text?}`: option ids the question offered (one unless
`multiSelect`), a text of the person's own, or both. Every question must have one; anything else is
`-32602`, and a later response to an answered question is `-32007`.

- **Blocking** (the default): the run waits. The answer is the tool's result, as `Q: … / A: …` lines with the
  options' labels; a dismissal, the ten-minute timeout, an abort or a stop give an error result instead.
- **Not blocking** (`blocking: false`): the tool returns at once and the run goes on. The answer becomes the
  conversation's next user message, which the kernel submits itself (`followUp` while a run is under way), in
  the same commit as the `answered` record. Such a question outlives the run and the process: it is kept until
  it is answered, and `conversation.open` lists it after a restart.

### Memory

With Memory on (`DROI_MEMORY_DIR` names the Memory folder, ADR 0042), every conversation has four tools and its
Memory in the prompt; with it off, neither. `memory_search`, `memory_read` and `memory_list` read the Memory
Summary, the Session Summaries and the Memory Notes, and `memory_note` writes a Memory Note under `notes/`, the only
place the kernel writes; all four run without asking. The `memory` section is the read policy, the Memory Summary
and the notes it has not taken in yet. It is a snapshot: a conversation keeps the text it was first shown, so a note
or a new summary changes no prompt the provider has cached, and a conversation sees Memory as it is now only after a
compaction. Each read of a Session Summary sends every connection `memory.read {file, at}`; the Host counts it, so
the summary does not age out.

### Settings that change while the kernel runs

`harness.configure {providers?, memoryDir?, section?, agent?, codemode?, compaction?}` is how the Host's settings reach a running kernel, so none of them
restarts it or stops a run (ADR 0038). `providers` (the shape of `DROI_KERNEL_PROVIDERS`) takes the place of the
catalog at once: each conversation's next request resolves its model in the new one, and a conversation whose model
is gone ends its next run without an answer. `memoryDir`, an absolute path or `null`, turns Memory on or off: the `memory`
extension is installed always and selected per conversation before each turn, as the MCP servers are, so the change
reaches every conversation at its next turn; a turn under way keeps what it had. Keys never cross the wire: a
provider names the environment variable its key is read from. `section`, a string or `null`, takes the place of
the app's prompt section (below) from each conversation's next turn. `agent`, `{model: {provider, modelId},
thinkingLevel?}` or `null`, is what a new conversation starts with when its create names no model: the Session
defaults' model, which every client then starts with, a phone included. The model must be one the kernel offers;
`harness.info` reports it as `agent` while one is set. `codemode`, `{mode: 'on' | 'only'}`, is how codemode presents
tools (below), from every conversation's next request; `harness.info` reports it as `codemode`. `compaction`,
`{model: {provider, modelId} | null, thinkingLevel?}`, is the model that writes every compaction's summary from the next compaction on
(below); `harness.info` reports it as `compaction`. Every connection hears `harness.changed {}`. A read-only kernel
refuses it.

### The compaction model

Pi summarizes with the conversation's own model. The kernel's compaction model, `DROI_KERNEL_COMPACTION_MODEL`
(`provider/modelId`) or the config file's `compaction.model` at launch and `harness.configure {compaction: {model}}`
while it runs, writes the summary instead: the kernel answers Pi's `beforeCompact` hook with Pi's own summary request
sent to that model (`src/extensions/compaction-model.ts`). A model the kernel does not have, or a request that fails,
leaves the summary to the conversation's model, with Pi's retries. Its thinking level (`off`, `minimal`, `low`, `medium`,
`high`, `xhigh`; `DROI_KERNEL_COMPACTION_THINKING` or `compaction.thinkingLevel`) goes with the model: a change that
names none is the model's own default. Its tokens count in the Host's LLM Proxy, not in
`conversation.usage`'s `models`. The summary is kept in the compaction task's memo, so a compaction resumed after a
restart does not pay for it twice.

`conversation.usage`'s `context.used` counts as Pi does when it decides to compact: the tokens the provider reported
for the last answer since the last compaction, plus an estimate of each message after it. `context.compactionAt` is
where Pi starts a compaction in the background, the window less 16,384 tokens kept for the answer and 32,768 more;
at the window less 16,384 it compacts before the next request.

### The brand's app (ADR 0041)

An app built on Droi gives every conversation three things, which the Host passes at launch in
`DROI_KERNEL_AGENT`, a JSON object whose fields are all optional:

- `mcpServers`: by name, entries in `mcp.json`'s shape, each with an optional `blockOnLoad: true`. They run beside
  the user's servers; one of them takes the place of a user's or a repository's server of the same name. `mcp.list`
  reports them with `app: true`; `mcp.set` does not change them. A turn waits for each `blockOnLoad` server to end
  its first connection (connected, or failed and reported), so the first turn after a start already has its tools.
- `alwaysAllow`: tool names (`<server>___<tool>`) the approval policy lets run without asking, judged after the
  user's own rules. They are in no `policy.changed` snapshot: they are the app's, not the user's to store.
- `section`: text added to every conversation's prompt, the Host's prompt addition and the brand's. `harness.configure
{section}` changes it.

### `codemode`: scripts that call the conversation's tools

Every conversation is offered the `codemode` tool unless its extensions say otherwise: its argument is
`{code}`, a JavaScript script that calls the conversation's other tools as `tools.<name>(args)` (an MCP tool as
`tools.<server>___<tool>`, grouped under its server in the prompt). It runs in QuickJS, in a worker thread of
its own, so a script that computes never holds up the kernel; Pi journals each result, so a script cut off by a
restart starts again from where it was. `conversation.configure {id, change: {extensions: {remove:
['codemode']}}}` takes it away from one conversation, for good.

Its mode, as pi-coding-agent's `codemode.mode` setting, is the kernel's, not a conversation's: `DROI_KERNEL_CODEMODE`
(or the config file's `codemode.mode`) at launch, and `harness.configure {codemode: {mode}}` while it runs. `on`, the
default, offers the model every tool and `codemode` beside them. `only` offers it `codemode`, `read` and the tools a
script cannot call (`callers: ['model']`: `ask_user`, the Memory tools); every other tool, an MCP server's included, is
reached from a script. A model that calls one of those itself gets Pi's `tool_unavailable` with a line saying how to
call it from a script. A conversation that took `codemode` away keeps all its tools in either mode.

Scripts have three tools the model is never offered (`callers: ['tools']`), each answering an array:
`grep {pattern, path?, glob?, ignoreCase?, literal?, limit?}` → `{matches: [{path, line, text}], truncated}`,
`find {pattern, path?, limit?}` → `{paths, truncated}` and `ls {path?, limit?}` →
`{entries: [{name, type, size}], truncated}`. Inside a git work tree `grep` and `find` see git's files (tracked and
untracked, without the ignored ones; a `path` git ignores all of, such as `node_modules/x`, is walked as it is). A script's `bash` or `powershell` call resolves to up to 1 MiB of output
(the first and last 512 KiB around a marker when longer), where the model sees the last 2000 lines or 50 KB. A
conversation with `codemode` has a `codemode-guidelines` prompt section on how to write a script: one nested call per
command, independent ones together, filtering in JavaScript rather than in a shell pipeline, `read` and `edit` rather
than `sed`. The `codemode` prompt section lists every tool outside an MCP server's namespace whole; Pi's budget (3000
tokens) is left to the servers' tools, which `searchTools()` finds when they do not fit.

Each call the script makes is a tool call of its own: the approval policy judges it as it would the model's
(an `ask` level asks for the script, then for each call it makes), and `pi.live.nestedTools` shows it while it
runs. The result keeps them as `details.calls[]`, in the order the script made them: Pi's
`{id, name, args, status, durationMs?, error?}` (`status` is `ok`, `error`, `cancelled` or `running`; `args` is
the arguments' JSON cut to 200 characters, so not always JSON), and three fields the kernel adds —
`arguments`, the object the call ran with, each string in it cut to 4 KiB; `output`, the call's answer as text,
at most 4 KiB of it; and `truncated: true` when the output was cut. An image is `[image]`. A call that failed
answers its error there. A script the user stops keeps the calls it made: those that ended as they ended, the
others `cancelled`.

A script runs until it ends, the user stops it, or its `// @options: {"timeout_ms": …}` line's time is up; with
no such line, for 30 minutes.

**Background scripts.** A script whose options line has `"background": true` runs as a job: `codemode` answers at
once (`Started in the background as job <id>`, `details: {calls: [], background: {jobId}}`) and the turn goes on.
The job is a `background` task of the conversation (`droi.codemode.job`), so Stop and the turn's end do not wait on
it and a restart resumes it, replaying what its script was handed. Its calls are tool tasks of its own: the approval
policy judges each, and while the conversation is idle `pi.live.nestedTools` shows them under the card of the call
that started it (a new round deletes those slots; the job keeps its own list). When it ends it writes a
`droi.codemode.done` entry, `{jobId, callId, status: completed | failed | stopped, output, details: {calls}}`, whose
user message carries the result to the model: placed as a follow-up input while a run is in progress, or appended
to the context while the conversation is idle (no run starts). The model's `codemode_job {id, wait_seconds?,
stop?}` reports a job, waits up to 600 seconds for it, or stops it (`Harness.abortTask`): calls still running are
cancelled, those that ended keep their answers.

A call a restart cut off is not run twice when its tool is not safe to repeat (`bash`): it ends as `error`, and
the script goes on with that answer.

### MCP servers, and the ones a repository brings

`mcp.list` answers every configured server with `{name, transport, status, enabled, tools[], error}`.
`status` is `connected`, `connecting`, `failed` or `disabled`, plus `untrusted` (below). `mcp.set` adds,
changes, enables, disables or removes one, and takes effect at once: a change reconnects, a removal
disconnects and closes the server's process. A `mcp.changed` notification carries the whole list after any
change, so a client never has to ask twice.

Two files feed the list, the global `<userDir>/mcp.json` and the repository's `<cwd>/.factory/mcp.json`, in
the `{"mcpServers": {…}}` shape every MCP client uses. A repository entry overrides the global one of the
same name field by field (an entry with its own `command` or `url` replaces the global one's connection
whole), and may carry only `enabled` to switch a global server off in that repository. What results is the
repository's server, and needs the repository's trust like any other.

**A repository's servers are code the repository chose.** A `command` with `args` is a process on this
machine, so a server that comes from a repository's own file is not started until the user allows it:

- `mcp.list` reports it `status: "untrusted"` and includes the `command`/`args` or `url` it would run, which
  is what a client shows in the confirmation, and the config's `fingerprint` (SHA-256).
- `mcp.set {name, repoRoot, trust: true, fingerprint}` allows it: `fingerprint` is the one the client showed,
  and a file that changed since answers `-32602` (list again and ask again) instead of allowing a config
  nobody saw. The kernel records the repository's real path and that SHA-256, connects at once, and
  broadcasts. `trust: false` revokes, and disconnects it.
- The record is kept across restarts and across an index reset: it is a decision, not a cache. Any change to
  the entry's own config changes its hash, so it is `untrusted` again — the same repository asking a
  different question gets asked again.
- A server in the global file is the user's own, and connects without this step.

`mcp.list` takes an optional `repoRoot` and answers each repository's servers with their own `repoRoot` and
`trusted`; without it, every repository this kernel has seen is included. The kernel does not guess which
repository a client is looking at. `repoRoot` must be a repository's root as `conversation.list` reports it;
anything else is `-32602`. A file that cannot be read is reported, not silently empty: the answer then carries
`diagnostics: [{path, message}]`.

**Which conversation sees which tools.** Each server is an extension of its own, and its tools are named
`<server>___<tool>` (other characters become `_`; a name past 64 characters ends in a hash). Before each
`conversation.submit` the kernel reads the conversation's repository file (the root of its `cwd`) and selects
the global servers that file does not switch off or replace, and that repository's own: a conversation is
never offered another repository's tools. The selection is stored as an edit of the default extensions
(`{add}`); a conversation whose extensions a client set outright is left as it is. A tool its server marks
`readOnlyHint` is judged as a read by the approval policy, as long as every server that registers that tool
name says so; every other MCP tool as a command.

**A schema with a composition at its root.** A tool whose `inputSchema` has `anyOf`, `oneOf` or `allOf` at its
root, and no `properties` there, is offered as `{type: "object", properties: {arguments: <its schema>}, required: ["arguments"]}`, its
`#` references moved under `arguments`, and a call's `arguments` is what the server gets. As it came, the
Anthropic route would offer it with no parameters at all (pi-ai sends only the root's `properties`) and OpenAI's
would refuse it. A call's recorded arguments, its approval card and a codemode script use the wrapped shape. A
recursive schema (`$recursiveAnchor` without `$id`) is offered as it came.

**Signing in.** An HTTP server that answers 401 is `failed` with an `error` that starts `authorization
required`, and is not retried on a timer. `mcp.authorize {name, repoRoot?}` answers the page to open as
`authorizationUrl`: for the global server of that name, or with `repoRoot` for that repository's, which must
be trusted first (`-32602` otherwise; signing in already talks to its URL). The page asks for the scope the
server's challenge named. The kernel listens on `127.0.0.1` for the redirect for five minutes, then connects
the server and broadcasts `mcp.changed`. A grant that needed no page answers `{authorized: true}`.
`mcp.authorize.cancel {name, repoRoot?}` stops listening. The tokens are kept in `<data>/mcp-auth.json`,
readable by its owner only, per server name and URL, and refreshed when they expire.

### The approval policy

A call a person has to allow waits for that answer. What decides is a **policy**: an autonomy level, and a
list of rules.

| Autonomy | Runs without asking                                             | Asks                      |
| -------- | --------------------------------------------------------------- | ------------------------- |
| `ask`    | only the tools that ask the user themselves (`ask_user`)        | everything else           |
| `edits`  | the read-only tools, `edit` and `write`                         | commands                  |
| `safe`   | the same, and commands that are **not** on the destructive list | the commands on that list |
| `all`    | everything                                                      | nothing                   |

The **destructive list** is matched by command position, never by substring: a line is split on `;`, `&&`,
`||`, `|`, `|&`, `&`, `$(`, a backtick and a newline, the leading `VAR=value`, `env`, `command`, `nohup`,
`time`, `timeout` (and its duration), `nice`, `stdbuf`, `exec` and `xargs` are skipped with their options,
and the first word decides (`sudo` itself is on the list). For `git push`, `git clean`, `git reset`,
`npm publish` and `pip install` the verb decides, found past the program's own options: `git -C repo push`
asks. `npm run format` does not ask; `sh -c "rm -rf x"`, `bash -lc "…"`, `cmd /c del x`, `find … -delete`,
`find … -exec rm {} +`, an encoded PowerShell command and a pipe into `sh` do. **A command the list does not
name does not ask under `safe`**; a user who wants to be asked about everything chooses `ask`.

The policy lives in the Host. The kernel keeps a snapshot of it, and answers nothing from an empty one:

- `policy.set` fills the snapshot: the default when `id` is absent, that conversation's own when it is.
  Each field is a change: `autonomy` left out keeps the level; `rules` left out keeps the list, `rules: []`
  clears it, and any other list replaces it whole. The answer, and every `policy.changed`, is the whole
  snapshot, with both fields always present.
- A rule is `{"kind":"tool","tool":"bash"}` or `{"kind":"file","path":"/abs/path"}`; a matching rule allows
  without asking. A `file` rule matches a tool whose arguments name that file, or a file under that folder
  (`read`, `edit`, `write`);
  a command names no file the kernel can trust, so an approval for `bash` or `powershell` never offers
  `proceed_always_file`, only once, always this tool, and cancel.
- Every change to the snapshot — this call, or a "always allow this file" answer — broadcasts one
  `policy.changed` carrying the **whole** snapshot for that scope. That notification is the only record: a
  Host stores what it hears and never pushes it back. A conversation's snapshot carries the default's
  `autonomy` while it has none of its own, so a Host stores a conversation's `rules` from it, and its level
  only from a `policy.set` of its own: stored back, the default's level would stay when the default changes.
- An "always" answer adds its rule to that conversation's own list, never to the default.
- Before the first `policy.set` arrives, nothing above `ask` runs without asking. It matters after a
  restart: Pi resumes a run on its own, and that can happen before the Host has sent the policy. Thirty
  seconds without one settles the level at `ask`.

`conversation.open` answers the policy in effect for its conversation as `policy`, and `harness.info` answers the default one, so a client does not have to ask for either.
A read-only kernel (`--read-only`) refuses `policy.set` with `-32008` and reports no policy at all.

### `resources.list`

What the kernel would load for a conversation, for a client that shows a slash menu or explains where the
instructions came from. `id` names the conversation whose `cwd` decides the workspace half; without one
only the personal half is read.

```json
{
  "skills": [
    {
      "name": "code-review",
      "scope": "personal",
      "path": "…/SKILL.md",
      "description": "…",
      "enabled": true
    }
  ],
  "prompts": [
    { "name": "fix-bug", "scope": "workspace", "path": "…/fix-bug.md", "description": "…" }
  ],
  "contextFiles": [{ "path": "/Users/me/AGENTS.md", "bytes": 812 }],
  "diagnostics": [
    {
      "path": "/Users/me/.droi/skills/Notes/SKILL.md",
      "message": "the name \"Notes\" is not a skill name"
    }
  ]
}
```

The capabilities are `skills` for `skills.list` and `skills.setDisabled`, and `resources` for this method;
a build without the resource loader leaves both false. `scope` is `personal` (the user's own directory) or
`workspace` (the conversation's `cwd` and its parents). `contextFiles` is the `AGENTS.md` chain in the order
it is applied: personal first, then from the outermost directory in. **Paths are advisory** — a client shows
them, and never reads them itself. `diagnostics` names each file that looks like a skill or a template but is
not used, and why: a name the Agent Skills rule refuses, a missing description, frontmatter that is not YAML,
or one that lost to another of the same name. It is empty when every file is used, and a client shows it so a
person can tell why a skill they wrote is not offered.

The slash commands the kernel expands are exactly `prompts[].name` and `skills[].name`. A client's own
commands (a new conversation, a compaction, a model change) never reach the kernel.

`conversation.submit` expands an input whose first text block is `/name args`: a prompt template's body with
`$1`…`$n`, `$@`/`$ARGUMENTS`, `${N:-default}` and `${@:N[:L]}` filled from the arguments (split like a shell),
or, for a skill, the skill's instructions in a `<skill name=… location=…>` block followed by the arguments.
Templates are looked up first; a disabled skill, and anything that names no command (`/usr/bin …`), goes as
typed. A `requestId` is matched against the input as typed, so a retry is the same request after a template
changed.

Every request carries two system prompt sections built from the same files: `skills` lists the enabled skills
the model may load (not those whose frontmatter says `disable-model-invocation: true`), each with its
`SKILL.md` path, and `project-context` carries the `AGENTS.md` chain, broadest first, up to 32 KiB; a file past
the limit is cut and the rest named. Both are read again before each request.

`skills.setDisabled {name, disabled, id?}` turns a skill off by name, wherever it is found, and answers
`skills.list` for the same `id`, so a list that showed a conversation's workspace skills gets them back. It is
a setting the kernel keeps in its database (not a cache: an index reset keeps it); a read-only kernel refuses
it with `-32008`. `skills.list` takes the same optional `id` as `resources.list`.

A conversation started with a skill command is named, and previewed, as it was typed (`/name args`), not by
the `<skill>` block the model is sent.

## Notifications

| Notification         | Payload                                                                                                                                                                                                              |
| -------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `conversation.view`  | `{id, kind: "value"\|"full"\|"failed", value, ops, retired}` — Pi's `WatchCall`, or `{id, kind:"failed", error}`; `value` is `null` for a `"delta"` subscriber's `"value"` push, and whole for `"view"` and `"full"` |
| `conversation.busy`  | `{id, run, runFailure?}` — `run` is Pi's `LiveRun` or `null`                                                                                                                                                         |
| `approval.requested` | `{requestId, id, tool, args, options[], scopeHint?}`                                                                                                                                                                 |
| `approval.resolved`  | `{requestId, id, outcome, by}`                                                                                                                                                                                       |
| `ask.requested`      | `{requestId, id, questions[], context, blocking?}` — `context` is required and non-empty: the sentence that says what the answer is for                                                                              |
| `ask.resolved`       | `{requestId, id, answers?, cancelled, by}`                                                                                                                                                                           |
| `task.changed`       | `{taskId, kind, status, conversationId}`                                                                                                                                                                             |
| `mcp.changed`        | `{servers[]}`                                                                                                                                                                                                        |
| `process.changed`    | `{id, processId, status, exitCode?, tail?}` — `status` is `running`, `exited`, `stopped` or `failed`; a `droi.process` entry says `done` where a notice says `exited`                                                |
| `index.changed`      | `{id, title?, titleSource?, archived?, lastActivityAt?, viewedAt}` — `viewedAt` is `null` while a client has asked for the dot                                                                                       |
| `policy.changed`     | `{id, policy}` — the whole snapshot of that scope; `id` is `null` for the default policy                                                                                                                             |
| `memory.read`        | `{file, at}` — a Session Summary the model read with `memory_read`, as its path in the Memory folder, and when                                                                                                       |
| `harness.changed`    | `{}` — `harness.configure` changed the providers, the models, Memory or the default agent: read `harness.info` again                                                                                                 |
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
