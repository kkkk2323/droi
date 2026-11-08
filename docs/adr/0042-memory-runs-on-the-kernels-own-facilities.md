---
status: accepted
---

# Memory runs on the Kernel's own facilities, without hidden Sessions

ADR 0031 moved Memory to Codex's memory v2: a background pass summarizes each finished Session into a Session Summary, and a second pass folds those into a Memory Summary that every Session starts with. The flow, its file layout, its selection rules and its prompts are settled and working: about 1,600 lines of Go in `apps/native/internal/memorywork`, 2,480 with its tests. What is not settled is the machinery underneath it, and all of that machinery was forced by the droid Daemon:

- **The model had to run in a hidden Session.** There was no other legitimate way to call a model: `droid exec` starts a whole agent and leaves a record outside Droi's control, and an HTTP call to Factory means reproducing the client signature a proxy exists to hide (ADR 0011). So each Memory task opened a Session tagged `droi.memory`, with a structured-output format, no tools but the answer, permissions auto-rejected, archived and closed afterwards. That in turn needed Clients to filter those Sessions, a `droi.memory` tag to filter them by, a queue concern ("the Daemon runs only so many Sessions"), and a retry path for "the Daemon went away" (`ErrDaemonGone`).
- **The transcript had to be read off droid's files.** The Session's JSONL under `~/.factory/sessions` was parsed with heuristics, and "has this Session changed since it was summarized" was the file's mtime.
- **Which Sessions to skip had to be guessed from tags.** Subagents, Automation runs, mission workers and `droid exec` runs were recognized by `droi.*` and droid tags in that file.
- **Memory needed a child process and an overlay.** Memory's four tools were a stdio MCP Server, pre-approved through the Runtime Overlay's `persistentPermissions`, that learned its calling Session from `_meta.assemblySessionId` and then read the Session's tags off disk. Injection needed a `SessionStart` hook, which could only record a Session because the Daemon gave it nowhere else to write (`started/<id>.json`), and reads were logged to `reads.jsonl` for the same reason.

The Kernel has none of those constraints. It can call a model without a Conversation, read its own entries, answer which of them changed and what kind a Conversation is, and evaluate a prompt section per Conversation. So the machinery goes, and the flow stays.

We decided:

- **One protocol method for a model call of the Host's own: `model.complete`.** Params are a model, optional system text, an input, an optional JSON schema, a thinking level, a token cap and a timeout; the result is the resolved model, the text, the parsed JSON when a schema was given, and the usage. No Conversation is created, no entry is written, no tool runs, nothing streams. A build that does not implement it says so in `harness.info.capabilities.models`.
- **The Memory work stays in Go, driven by the Host's own timer.** `memorywork` keeps its passes (at start, every 15 minutes, soon after a new Memory Note, when the Kernel comes back), its 30-day / 6-hour window, its 16-per-pass limit and 4-at-a-time, its 80,000-character input budget, its batching, its size cutting and its retry ladder. The only two things that change are where the transcript comes from and how the model is called: `memorywork.Request{Title, ModelID, Prompt, Input, Schema}` becomes a `model.complete` call, and `Reply{JSON, SessionID, Model}` loses its Session id.
- **A transcript comes from `conversation.entries`.** The user's prompts, the agent's replies, the compaction summaries and one line per tool call, result and notice, within the same 80,000-character budget and in the same priority order. Redaction by pattern still happens before anything is sent. The droid JSONL parser and its heuristics are deleted.
- **Change detection uses the index, not a file's mtime.** A Conversation's last activity is a column of `droi_conversations` that commits with its entry (ADR 0038), so "summarized at" is compared with that. What used to be a tag lookup is a `kind` column: Automation runs, Workstream runs, subagent Conversations and Memory's own work are excluded by kind, not by scraping tags out of a file.
- **Memory's tools run in the Kernel's process.** The Kernel reads the whole Memory folder — the Memory Summary, every Session Summary, every Memory Note — and writes only `notes/`. Reads are counted in the Kernel and reported to the Host, which folds them into `state.json`; there is no `reads.jsonl` and no `started/`. The stdio MCP Server, the overlay's pre-approval, the `_meta` Session guessing and the `SessionStart` hook all go.
- **Injection is a snapshot, taken when a Conversation is created.** The Memory Summary and the Memory Notes it has not absorbed yet are read once, at `conversation.create`, and injected as one section for the life of that Conversation. They are never re-read per request: the section sits in the prompt's prefix, and a section that changed mid-Conversation would break the provider's prompt cache from that turn on, which costs more than the freshness is worth. A note written during a Conversation therefore reaches the next Conversation, not the current one — which is already how ADR 0031 behaves, since a note is injected beside the summary, not into it.
- **A new tool, `history_search`, searches the raw past conversations.** The Memory Summary is a lossy index by design; when it does not answer a question, the model can go and read what was actually said. It is a Kernel extension tool, not a protocol method, because only a model calls it.
- **`model.complete` is not Memory's alone.** A Conversation's title is written by Droi through the same method, in the language the user writes in, instead of being taken from the first message or asked of the Conversation itself: a title is a small structured answer, and this is the call that gets one without opening anything.
- **Each file has exactly one writer.** The Kernel writes `notes/`. The Host writes `session_summaries/`, `memory_summary.md` and `state.json`. Nothing writes another's files, so no lock is needed and a Kernel update cannot corrupt a summary.

## Why not move the flow into a durable task

A durable task is the Kernel's own way to do work beside a run: it survives a restart, it is reported through `task.changed`, and `resume()` settles it. It is the right shape for a compaction or a subagent. It is the wrong shape for this, for four reasons:

1. **It belongs to a Conversation.** A task is attached to the Conversation that started it (`byTaskId`), and a Memory pass belongs to none: it reads many Conversations and writes files in the Host's folder.
2. **It does not survive what it needs to survive.** `resume()` settles tasks of the process that died, but a pass that dies mid-batch must not be re-run blindly: ADR 0031's rule is that a batch's answer is saved before the next one goes out, and a lost batch is retried at the next pass, not resumed from a half-written state.
3. **The retry ladder, the 15-minute timer, "Update now" and `state.json` are the Host's**, and they are already written, tested and matched against real data (ADR 0031's model comparison took three days). Moving them into TypeScript buys nothing and risks the behaviour that was measured.
4. **The flow calls a model and writes files.** A task with a model call and file writes is a Conversation with no user, which is exactly the hidden Session this ADR is deleting.

So the split is: the Kernel provides the facilities (`model.complete`, `conversation.entries`, the index, the section), and the Host keeps the policy (which Session, how big a batch, when to retry, how to cut a summary that is too long).

## `history_search`, and its edges

- **Read-only.** It searches; it never writes, never edits, never opens a Conversation, and never triggers a run. The approval policy treats it as a read tool, like `read`, so it never asks.
- **Capped.** At most 20 entries, at most one page, each hit a conversation id, an entry id, its kind, when it was written and a trimmed snippet of about 200 characters with the matching words marked. No cursor: a model that wants more asks a narrower question. The tool says how many Conversations the default exclusions left out, so "nothing else" is a fact rather than a guess.
- **Excluded by default: Automation runs, Workstream runs and subagent Conversations**, because a thousand scheduled runs would drown the user's own work. An `include` argument adds them back. Nothing is ever excluded for being old.
- **The description tells the model when not to use it.** The tool's own text says to answer from the Memory Summary and the Session Summaries first, and to search only when the summary does not settle the question, because a search over months of transcripts costs tokens and attention that the summary was written to save.
- **One index, not two.** It queries the same FTS index the Kernel builds for `conversation.list`'s search and `conversation.entries` (ADR 0038's `droi_entries_fts`), with the same words-to-SQL rules, so there is one writer (the index's flush), one reindex on upgrade, and one answer to "does this entry hold these words".
- **Chinese is indexed as bigrams.** SQLite's FTS5 `unicode61` tokenizer treats a run of Chinese as one token, so a two-character word would never match; the `trigram` tokenizer cannot answer a query shorter than three characters, and Chinese words are usually two. An FTS5 table has one tokenizer, so the bigrams sit in a companion table, `droi_entries_bigrams` (`unicode61`, rowid the entry ID), written in the same flush as `droi_entries_fts`: for every run of Chinese, Japanese or Korean characters, its overlapping pairs (`内核迁移` → `内核 核迁 迁移`). A query word of exactly two such characters is answered from it; three or more go through the trigram index; Latin words go through the trigram index as before. A word that is a single CJK character, or two Latin letters, keeps the bounded scan of the newest 50,000 entries that `conversation.list` already uses: a prefix match on the bigrams would miss a character at the end of a run, and an exact answer over recent history is better than a partial one over all of it. A dictionary segmenter (jieba or ICU) would be better and is a later upgrade: it costs a native dependency per platform, and the bigram index answers the two-character case correctly today.
- **It does not search the Memory folder.** The Memory Summary, the Session Summaries and the notes have their own tools, and their ranking is the file order the Host keeps. Two search tools with different rankings answering the same question would be worse than one of each.

## What this deletes

`memorywork.NewRunner` and the hidden-Session machinery around it (the tag, the archive, the close, the queue worry, `ErrDaemonGone` and the retries that existed only for it); `memory.ReadTranscript` and the droid JSONL heuristics; the tag-scanning skip list (`skippedTags`) and the `droid.*` tag reads behind it; `pkg/memoryserver` as a process and its `memory___*` tool names; the Runtime Overlay's Memory entries; the `SessionStart` hook, `started/` and `reads.jsonl`; and the mtime comparison, replaced by the index's last activity.

## Considered options

- **Keep hidden Conversations and just port them.** Rejected: it would keep every cost the Kernel makes unnecessary — a tag, a filter in every Client, an archive, a close, a queue concern and a restart retry — to avoid writing one method.
- **Call the provider over HTTP directly from Go, as `droid-proxy` does.** Rejected: the request shape and the headers are the LLM Proxy's business (ADR 0039); reproducing its client signature in the Memory code is the second copy of a shape that already exists once.
- **Let the Kernel own the whole pipeline as a Kernel extension.** Rejected: file layout, prompts, `state.json`, the retry ladder and Settings → Memory's status are the Host's, they are written and measured, and moving them would rewrite 1,600 tested lines in another language for no gain.
- **Re-read the Memory Summary on every request, so a note is never stale.** Rejected: the section is in the prompt's prefix, so changing it mid-Conversation invalidates the provider's prompt cache for every later turn of that Conversation; the freshness it buys is a note nobody has asked for yet.
- **One `memory_search` that searches both the folder and the transcripts.** Rejected: two rankings, two tokenizations and two answers to "did you find it"; keeping them separate lets each be simple and lets the tool description say which one to reach for.
- **A word segmenter for Chinese.** Deferred, not rejected: it is the right long-term answer, and the bigram index is correct for the two-character words that dominate without a native dependency.

## Consequences

- **One more protocol method, and a build that does not have it cannot run Memory.** `harness.info.capabilities.models` is the flag; until `model.complete` exists, Settings → Memory says so rather than opening a Session that would fail.
- **Memory's cost is visible per call.** Each `model.complete` returns its usage; the Host records what it needs for Settings → Memory, and the Kernel stores nothing. A Memory pass no longer shows up in any Session's usage, which also means it no longer shows up in a Client's token totals.
- **Memory Sessions stop being a thing**: no settings page that opens one, no "the Daemon went away" retry, no queue worry, and `droi.memory` disappears from the tags a Client must understand (CONTEXT.md retires it).
- **A note written mid-Conversation reached that Conversation before and now does not.** In practice it already did not: ADR 0031 injects a note beside the summary at Session start. What is lost is a future in which injection became live, and it was given up knowingly for the cache.
- **The index gains a table and needs a reindex**, once, on upgrade: the bigram table holds each CJK pair once per entry, which is at most about the CJK text again, a few hundred MB on a large archive at worst, and is measured in M4.
- **`history_search` can be slow on a large archive the first time**, because SQLite fills its cache; it is bounded by the 20-hit cap and by the FTS query, not by a scan.

## What this ADR does not decide

- **Which model `model.complete` defaults to when a caller names none.** Today the Host names one (Settings → Memory's two models), so nothing depends on it.
- **Whether a dictionary segmenter replaces the bigram index**, and with which dependency (M4 measures the bigram index on the real 149-Session archive first).
- **What Settings → Memory shows** once Memory Sessions are gone: the same counts and the same last failure, minus the list of runs.
