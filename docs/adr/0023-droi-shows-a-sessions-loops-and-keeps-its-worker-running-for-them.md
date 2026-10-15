---
status: accepted
---

# Droi shows a Session's Loops and keeps its worker running for them

A Loop (droid's `/loop`, or the Loop tool Droid calls) is a cron the Daemon keeps in `~/.factory/crons`: a prompt sent to the same Session every interval, a cron expression read in UTC. Droi did not show them, so a Loop Droid made in a Droi Session ran unseen, and stopped unseen.

It stopped for two reasons. The Daemon fires a Loop only while its Session has a worker, and closes an idle worker after 30 minutes unless the load asked it not to (`disableInactivityTimeout`, which the droid CLI's own Sessions set); a Loop of 30 minutes or more then waits, held `session-inactive`. And a Daemon that starts holds every Loop it finds (`daemon-start`) until its Session is loaded again, which Droi did only when the Session was opened. Loading a Session resumes its held Loops; the Daemon does not catch up the runs missed.

So the native app reads every Loop once connected (`daemon.list_crons`) and follows `daemon.cron.state_changed`. It loads each Session with a Loop that runs or waits to run without the inactivity timeout, again when one of its Loops is held, and once none is left to run loads it with the timeout again, so its worker can close. The shelf above the composer lists the Session's Loops, how often each runs and when next, with buttons that pause, resume and delete one. `/loop <interval> <prompt>` (or `<prompt> every <interval>`) makes one as droid's does: an interval of 1 minute to 24 hours that a cron repeats exactly, anchored to the time it was made, run once at once; `/loop` with only an interval sends the `loop.md` droid would find, and `/loop stop` deletes the Session's Loops.

## Considered Options

- **Show the Loops and leave the worker alone, as the Factory App does.** Rejected: a Loop of half an hour or more would wait after its first idle half hour, and a Loop would wait after every restart of Droi until its Session was opened.
- **Load every Session without the inactivity timeout, as the droid CLI does.** Rejected: the CLI has one Session open; Droi would keep a worker, a process each, for every Session ever opened.
- **`/loop` without an interval asks Droid to make the Loop, as droid's does.** Left out: droid sends instructions for its Loop tool, which it first turns on for the Session; Droi asks for an interval instead.

## Consequences

- A Loop runs only while the Daemon runs: Droi has to be open, or the Host running on its own. Quitting Droi stops them; they resume once it is back, without the missed runs.
- Each Session with a Loop to run keeps a worker process, even when nobody looks at it.
- The Phone App does not show Loops yet.
