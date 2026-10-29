---
status: accepted
---

# Droi keeps the Session Times and orders every Client's list by them

The Session list dated each Session by the Daemon's `updatedAt`, which is the modified time of the Session's transcript file. Much more than the user writes to that file: every hook run, opening an old Session (the SessionStart hook's "resume"), and every step of a turn. Each fix patched the date in a Client: 0feb4b8 moved a Session up when its turn began and held the places of working Sessions in memory, f5249d7 kept an older read of the list from overwriting a newer one, 101fd64 kept the Memory hook from writing at "resume". The window and the Phone App each kept such a patch of their own (`touch` and `holdTurns` in the window, `activeAt` on the phone). A patch was gone at a restart, it was different on each Client, and the next writer of the file broke it again. Unread marks were each Client's own as well: a Session read on the computer stayed unread on the phone.

The report that started this work was a Session that did not rise after the user chatted in it. Its date was right. It sat in a folded Workspace, and with "Most sessions" a recently used Workspace does not move up. So a correct date alone does not help: the list grouped by Workspace cannot put the Session just used first.

[Synara](https://github.com/Emanuele-web04/synara) orders its list by the time of the user's last message (`latestUserMessageAt`), puts the Sessions that need attention above the rest, and holds the order while the pointer is over the list. We do the same, and Droi keeps that time itself:

- **The Host keeps the Session Times**, one set per computer, in `<user data>/session-times.json`. For each Session: `sentAt`, when the user last sent it a message from a Client (a `daemon.add_user_message` with the role `user` and no visibility of its own); `endedAt`, when its last turn ended (an idle report after the Session was seen working or sent a message; every Client's socket carries the same report, and only the first counts); `viewedAt`, when a Client last showed it. All three are in Unix milliseconds, by the computer's clock.
- **The Gateway writes them** from the frames it forwards, and serves them: `GET /session-times` answers every Session's, and with `after=<revision>` the Sessions changed after that revision, as soon as one changes or after 25 seconds (a long poll). `POST /session-times/viewed?sessionId=` notes a Client showing a Session. A launch starts its revisions at its start time in milliseconds, so a Client that holds the revision of an earlier launch gets a full answer.
- **Sessions sent messages before the Session Times existed are read from their transcripts once.** In the background at the Host's first start, the time of the last message the user typed in each transcript (`~/.factory/sessions/*/*.jsonl`) becomes its `sentAt`. Hook records, tool results and messages with a visibility do not count. About 5,000 transcripts take about 6 seconds.
- **One order for every Client**, in `pkg/sessions` (Go) and `packages/daemon-layer/src/sessions.ts`: pinned Sessions first, then a rank (a Session whose turn runs, waiting for an answer included, then an unread one, then the rest), then the time it was last used. That time is `sentAt`, or the Daemon's date for a Session sent no message through a Gateway (one used only in the droid CLI). The Created order has no ranks.
- **Unread is shared**: a Session is unread when its last turn ended after any Client last showed it. A Client marks a Session read as soon as it shows it, ahead of the computer's answer. The phone's mark is a guess by the phone's clock, which can differ from the computer's, so the computer's record replaces it once the computer has noted the view.
- **A second view.** The Sort menu offers By workspace (the default, as before) and By time: one list of Pinned, Right now (working or unread), Today, Yesterday and Earlier (30 at a time), each row naming its Workspace. The window shares its view with the phone through `/session-list` (`sessionView`), and the phone follows it as it follows the order.
- **The order holds under the pointer.** While the pointer is over the sidebar's list, each listed Session keeps its rank and time from when the pointer came in, so no row moves between aiming and clicking. A Session new to the list still takes its place.

This narrows ADR 0001, where the Daemon's session store is the only source of truth: it still is for every conversation, and the Host keeps only when the user used each Session and when a Client showed it, which the Daemon does not record.

A Gateway of an older Droi answers 404 for the Session Times; a Client connected to it dates Sessions by the Daemon and keeps its own unread marks, as before.

## Considered Options

- **Keep patching the Daemon's date in each Client.** Rejected: three fixes in a month did not hold, every Client had its own, and each new writer of the transcript (hooks, Memory, Loops) would break it again.
- **Read each transcript for its last user message at every refresh of the list.** Rejected: 5,000 transcripts take seconds. They are read once, for the backfill.
- **A Droi index of the Sessions in place of the Daemon's list**, as Synara keeps one. Deferred: titles, Workspaces, worktrees and tags belong to the Daemon, and the Session Times are enough to fix the order and the unread marks. It can come later, on top of them.
- **Count every message as use: Loops, Automations, the droid CLI.** Rejected: a Loop that fires every five minutes would stay at the top. Use is what the user sends from a Client.
- **Keep the unread marks per Client.** Rejected: the user reads a Session on one Client and expects the others to agree.
- **Make the list by time the default.** Rejected by the user: the Workspace groups stay the default, and By time is a choice in the Sort menu.

## Consequences

- A Session used in the droid CLI after the backfill moves only by the Daemon's date, and only when it has no `sentAt`; one sent a message from a Client before keeps that time.
- Loops and Automation Runs do not move a Session up. A Loop's turn that ends still marks it unread.
- The Gateway sees a turn end only through a connected Client's socket: a turn that ends while no Client is connected (a `droi host` with the phone away) is not noted, and its Session does not turn unread.
- The unread marks and the order are one computer's: two Paired Computers keep their own.
- The window and the phone share the unread marks only when both run 1.77.0 or later against a Host of 1.77.0 or later.
- `session-times.json` keeps one entry per Session ever dated, about 100 bytes each, and nothing removes them yet.
