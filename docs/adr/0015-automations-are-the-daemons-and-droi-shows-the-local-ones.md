---
status: accepted
---

# Automations are the Daemon's; Droi shows and edits the local ones

Users wanted the Factory App's Automations in Droi: a prompt that runs on a schedule without anyone starting it.

The Daemon already does all of it for local Automations. Each one is a folder in `~/.factory/automations/<id>/` (`HEARTBEAT.md` holds the prompt and, in its front matter, the schedule, model and Workspace). A Daemon whose machine type is `local` checks every 60 seconds for one that is due and starts its run as an ordinary Session tagged `automation` (metadata `automationId`, `automationName`, `triggerSource`, `type: run`), with `--skip-permissions-unsafe`, and closes it 15 seconds after it goes idle. `daemon.list_automations`, `create_automation`, `update_automation`, `pause_automation`, `resume_automation`, `delete_automation`, `get_automation_history` and `dispatch_automation_run` manage them. Droi is a thin client over these calls, as the Factory App is: the native app has an Automations page, reached from the sidebar under New session, that lists them, shows one with its runs, and runs, pauses, resumes, edits, creates and deletes them. Runs leave the sidebar for that page, since a daily Automation would otherwise add thirty rows a month.

What the Daemon keeps in UTC, Droi shows in local time. Every cron expression the Daemon takes is read in UTC (`daily` is 09:00 UTC), so the page offers presets (every hour, every day, weekdays, every week, at a local time) and turns them into a UTC expression at today's offset; anything else is written as a cron expression, or words the Daemon turns into one. `daemon.run_automation` only returns the prompt a Client could start a run with; `dispatch_automation_run`, newer than the SDK's 0.9.1 schemas, makes the Daemon start the run itself, so Droi calls it by name. `list_automations` leaves out the Workspace, so the page reads it from `HEARTBEAT.md`'s front matter, which the native app can since it only runs on the Daemon's computer; `create_automation` takes none, so a new Automation with a Workspace is created and then updated.

The Factory App's other kinds (Slack, webhook, chained and CI triggers, cloud Automations on a droid computer, sharing with the organization, templates) run in Factory's cloud behind feature flags. Droi leaves them out.

## Considered Options

- **Droi schedules the runs itself.** Rejected: the Daemon already schedules, catches up after it was down, and refuses a second run while one is under way; Droi would only duplicate that, and turning off the Daemon's own poller (`FACTORY_MACHINE_TYPE` other than `local`) also stops `/loop`.
- **Sharing one Daemon with the Factory App, so that only one polls.** Not possible: the Factory App always starts its own Daemon from its bundle, over IPC or a port it picks, and ends any Factory Daemon already on that port.
- **Leaving the runs in the sidebar.** Rejected for the noise; search still finds them.

## Consequences

- An Automation runs only while some local Daemon runs: Droi's (while Droi is open, or always for a Host on its own), the Factory App's, or a `droid daemon` started by hand. A Daemon that was down starts one catch-up run when it comes back, not one per missed time.
- Two Daemons on one computer both poll the same folder and do not coordinate: when their checks land within a fraction of a second, every run starts twice. The page says so while the Factory App's Daemon runs, which is all Droi can do about it.
- Runs bypass the Gateway, so they get the Runtime Overlay (Memory's tools) but no System Prompt Addition, and the Daemon ends them without the `SessionEnd` hook, so Memory does not extract entries from them.
- A schedule follows the local offset of the day it was saved; across a daylight-saving change it runs an hour off until saved again.
- The Phone App does not list Automations yet, and still lists their runs as Sessions.
