# Native app performance

How the MyGo native app (`apps/native`) compares with the Electron Desktop
Shell, measured on 2026-10-07. Both were version 1.33.5 (Electron 44.4.3) and
ran on one MacBook Pro (Apple M4 Pro, 48 GB, built-in 120 Hz display), each
with a fresh profile (`DROI_USER_DATA_DIR`) and a Daemon of its own, opening
the same Session of 1166 messages (the latest 400 load).

## Results

|                                               | Native     | Electron     |
| --------------------------------------------- | ---------- | ------------ |
| Launch to the Session list shown              | **0.95 s** | 2.9 s        |
| Launch to the long Session's transcript shown | **2.0 s**  | 4.1 s        |
| Processes, the Daemon left out                | **1**      | 4            |
| Memory 10 s after launch                      | **252 MB** | 640 MB       |
| Memory after 40 s idle                        | **103 MB** | 526 MB       |
| CPU while idle                                | 1.1%       | 1.2%         |
| CPU time for the same scroll                  | **1.2 s**  | 5.7 s        |
| App size                                      | **19 MB**  | 394 MB       |
| DMG                                           | **6.6 MB** | about 139 MB |

The Daemon's memory is the same on both sides (530 to 600 MB): it is the same
`droid` binary.

Scrolling the transcript (14 times up, 8 times down, 5 wheel notches each):

- Native: a frame takes 2.3 ms at the median, 4.7 ms at p99 and 8.1 ms at
  most; none missed the display's 8.3 ms.
- Electron: most frames keep the 120 Hz pace, but 3 gaps were longer than
  25 ms, the longest 158 ms (a long animation frame of 151 ms).

The two scrolling figures are measured differently: MyGo reports how long it
took to make each frame (`MYGO_FRAME_STATS`), Chromium only the time between
frames (`requestAnimationFrame`, `long-animation-frame`). Both show dropped
frames; the numbers are not otherwise comparable.

## How it was measured

- **Launch**: from `open -n` to the first frame that lists the Sessions, and
  to the first frame with the Session's transcript after the app is asked to
  open it. The native app logs both under `DROI_BENCH=1`; the Electron one is
  read through the DevTools protocol (`--remote-debugging-port`), waiting two
  animation frames after the DOM has them.
- **Memory**: the summed physical footprint (`footprint`) of the app's
  process tree without the Daemon and what it runs, 10 s after launch and
  again 40 s later.
- **CPU**: the CPU time (`ps -o time`) of the same processes over 30 s of
  idle, and before and after the scroll.
- Each launch figure is the median of two runs; they differed by less than
  5%.

## Why the native app scrolls smoothly

Two changes made a long transcript scroll without dropped frames (before
them, frames took 10 to 16 ms):

- The transcript's list builds only the rows in view, and a reply is cut
  into rows of its own: one per block, per tool call and per call a Script
  made. A turn of dozens of tool calls was one row before, built and laid out
  whole on every frame it showed.
- A tool call's result is decoded from its JSON once, not on every frame.

To look at frames yourself, run the app with `MYGO_FRAME_STATS=all`, which
logs how long each frame's build, layout, paint and present took.

## Finding what costs CPU

A frame should cost what is on screen, not what the history holds. Work done in `View`
runs on every frame it builds. Animations move in `Draw`, so their frames only paint: the
pulses (`kit.Pulse`) 30 times a second (`Painter.AnimationFrameRate`; on macOS the display
then refreshes as often while nothing else moves), and MyGo's loading
indicators (`kit.Spinner`, the transcript's activity row, the waits for the Daemon, a
followed computer and the browser) once each display refresh. In October 2026 such a
frame of a Session at work painted in about 0.16 ms in a `ui.Tester`, about 1% of a core
at 60 Hz. Anything built from the
Session list is read once per change of the list (`listRev`, `listMemo`), not decoded or
copied per frame.

- **Frame cost in a test**: `TestAFrameOverALongHistoryStaysCheap` (in
  `internal/app`, with `-v`) draws the window over 4379 Sessions and 372 subagents
  and logs each frame's time and allocations; it fails over `frameBudget`. Add
  `-cpuprofile` or `-memprofile` (with `-memprofilerate 4096`) and read them with
  `go tool pprof`. The Fake Daemon does not start with more than about 5000 Sessions.
- **The running app**: `MYGO_FRAME_STATS=all` logs each frame's build, layout, paint and
  present; `DROI_PPROF=127.0.0.1:6061` serves Go's profiles
  (`/debug/pprof/profile?seconds=30`). Both need a restart, which ends the Sessions an
  agent runs through that Droi: try a build on another computer instead, with a throwaway
  `DROI_USER_DATA_DIR` that follows this computer's Gateway.
- **The installed app, without a restart**: `sample <pid> 5`, then name the Droi frames
  with `lipo Droi -thin arm64` and `go tool addr2line` on each offset plus `0x100000000`.
- **Its CPU over time**: `scripts/measure-cpu.py 60 <label> <log.jsonl>` reads the
  installed app's and its Daemon's CPU for 60 seconds; run it three times per version,
  with the window the same.
- **Another Mac**: an app started there over SSH may not reach this computer's Gateway on
  the local network ("no route to host": macOS's Local Network privacy), so forward it with
  `ssh -N -R 41418:127.0.0.1:41417 <host>` and add the computer there as `127.0.0.1:41418`.
