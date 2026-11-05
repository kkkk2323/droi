---
status: accepted
---

# Droi runs its own Automations from the Host, out of Factory's sync

_Supersedes ADR 0016: the Automations page, its local times and keeping runs out of the session list stand; the Daemon's Automations and the calls that manage them go._

ADR 0016 left Automations to the Daemon: a folder in `~/.factory/automations/<id>/` that the Daemon runs on a schedule. droid 1.x syncs every local Automation to Factory every five minutes, with no switch (`/api/automations/sync`), and Factory's backend decides on its own whether a scheduled run was missed. It mails the user, and after ten days of missed runs it pauses the Automation; the next sync writes `paused: true` into the local `HEARTBEAT.md` (`applyPlatformPauses`), and the Daemon stops running it. The user's "Droid二进制逆向" showed this: the Daemon's log has a run every day from October 31, each one reported to Factory, yet Factory counted six missed runs and would pause it about November 10. The cause is in Factory's backend, out of Droi's reach. ADR 0016's other costs remain: a second Daemon on the computer can start the same run twice, and runs bypass the Gateway, so they start without the System Prompt Addition.

We decided:

- **An Automation is Droi's.** Each one is a folder in Droi's own data, `<Droi data>/automations/<id>/`, with a `HEARTBEAT.md` in the Daemon's layout (name, schedule, model, Workspace, autonomy and paused in the front matter; the prompt below). The Daemon never reads this folder: nothing syncs, and the Factory App's Daemon does not run it.
- **The Host schedules.** It checks once a minute and starts a run of each Automation that is due, never a second one while a run is under way. After the Host was down or the computer asleep, it starts one catch-up run, not one for each missed time. A schedule is read in the computer's own time zone, so it stays put across a daylight-saving change.
- **A run is a Session the Host starts on its Daemon**, as it starts Memory Sessions (ADR 0011): tagged `droi.automation` with the Automation's id and what triggered the run, in the Automation's Workspace, with its model and autonomy, and with the System Prompt Addition the Gateway adds to a Client's Session. The Host closes the Session once it is idle and records the run (Session, outcome, time) in the folder. While a run is under way, the Host keeps the computer from sleeping when idle.
- **A run has a trigger and a target.** The triggers are the schedule and Run now; a webhook through the Relay (ADR 0034) comes later. The target is a new Session; sending the prompt to an existing Session, such as a Workstream's Orchestrator (ADR 0036), comes later.
- **Clients reach Automations through the Host.** The Gateway answers listing, editing, pausing and running them, so the window of another computer and, later, the Phone App manage them as this computer's window does. Runs stay off the session list, on the Automations page.
- **Droid manages them as well.** An MCP Server Droi carries, attached to its Daemon through the Runtime Overlay as the Memory Server is, offers tools to create, list, edit, pause and run an Automation; its calls go to the Host. An Automation the tools create runs at an autonomy level (High unless asked for lower), never without asking, and the tools cannot change the prompt, Workspace or model of one the user set to run without asking, so a Session cannot widen its own permissions through them. Runs do not get these tools. The Sessions Droi's Clients start leave out Factory's own Automation tools (`CreateAutomation`, `ListAutomations`, `RunAutomation` and the rest, as `disabledToolIds`, which the Gateway adds where it adds the System Prompt Addition), so Droid does not make an Automation that syncs.
- **One Droi runs a data folder's Automations.** A lock in the folder lets one Host at a time schedule them; another Droi on the same data says that one does, and takes over within a minute after it quits.
- **Moving over.** The Automations page lists the Factory Automations still in `~/.factory/automations` and, on request, copies one into Droi's folder with all its files (not the Daemon's bookkeeping), turning its UTC schedule into local time, and deletes it from the Daemon. The copy remembers the Factory Automation's id, so a second move does not copy it twice, and the Factory Automation's earlier runs stay on its page. One that posts into an existing Session, or whose schedule the Daemon never ran, is not moved.

## Considered Options

- **Keep ADR 0016.** Rejected: the sync, the mails and the pauses come with the Daemon's Automations, and nothing turns them off.
- **Let the LLM Proxy swallow `/api/automations/sync`.** Rejected: only Droi's droid goes through the LLM Proxy; the Factory App's Daemon syncs the same folder; any droid update may move the call.
- **Keep the clock on the Relay, which is always on.** Rejected: a run happens on the computer, which a Relay cannot wake, so a sleeping computer gains nothing; and the Relay would keep state it does not keep now.
- **Loops (ADR 0023).** Rejected for Automations: a Loop sends its prompt to one Session, which grows with every run, and fires only while that Session has a worker. Loops stay right for a recurring prompt to one Session, such as a Workstream's checks (ADR 0036).

## Consequences

- Factory's web app and the Factory App no longer see these Automations or their runs' `VISUAL.html`. Factory's templates and its Slack, GitHub and cloud triggers are not Droi's.
- Droi now does what the Daemon did (catch-up, one run at a time): the price of leaving the sync.
- An Automation runs only while its Host runs, as before. A sleeping computer runs it once when it wakes; for runs on time, keep it on a Host that does not sleep (`droi host`, or a desktop that stays on).
- Memory skips the runs, as it skips the Daemon's.
- After Droi lost the Daemon during a run, the Host asks the Daemon how the run's turn ended (`daemon.get_session_turn_outcome`, by the prompt's message id) instead of guessing.
- `CONTEXT.md` names Automation, Factory Automation and Automation Run.

## Resolved when it landed

- droid 1.4.1's Automation tools are `CreateAutomation`, `ListAutomations`, `ReadAutomation`, `EditAutomation`, `DeleteAutomation`, `RunAutomation` and `ListAutomationTemplates`. `disabledToolIds` given to `initialize_session` is saved in the Session's settings and applied again on `load_session`.
- The Daemon's `delete_automation` removes the folder and then marks Factory's record deleted, so a moved Automation does not stay on Factory as missed.
- The computer stays awake without cgo: `caffeinate -i -w <pid>` on macOS, `SetThreadExecutionState` from a thread of its own on Windows.
- A live test (`go test ./apps/native/internal/hostrun -run Live`, droid 1.4.1) runs one now, a scheduled run, a pause and a move.
