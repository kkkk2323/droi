#!/usr/bin/env python3
"""Measure the installed Droi's CPU on a Mac: `scripts/measure-cpu.py [seconds] [label] [log]`.

Reads the CPU time of the running /Applications/Droi.app and of its Daemon over the
seconds (60 by default), with a sample every 5 seconds, and prints them as JSON with the
app's version and memory footprint. With a log path, it also appends the JSON as a line
there, to compare one version with the next. Keep the window as it was for each run:
CPU depends on which page shows and whether a Session works.
"""
import json, os, subprocess, sys, time

secs = int(sys.argv[1]) if len(sys.argv) > 1 else 60
label = sys.argv[2] if len(sys.argv) > 2 else "run"
log = sys.argv[3] if len(sys.argv) > 3 else ""


def run(*cmd):
    return subprocess.run(cmd, capture_output=True, text=True).stdout


def processes():
    rows = []
    for line in run("ps", "-Ao", "pid=,ppid=,rss=,time=,comm=").splitlines():
        p = line.split(None, 4)
        if len(p) == 5:
            rows.append(dict(pid=int(p[0]), ppid=int(p[1]), rss=int(p[2]), time=p[3], comm=p[4]))
    return rows


def seconds(t):
    total = 0.0
    for part in t.replace("-", ":").split(":"):
        total = total * 60 + float(part)
    return total


def usage(pid):
    for r in processes():
        if r["pid"] == pid:
            return seconds(r["time"]), r["rss"]
    sys.exit(f"process {pid} ended")


rows = processes()
app = next((r for r in rows if r["comm"].endswith("/Droi.app/Contents/MacOS/Droi") and r["ppid"] == 1), None)
if app is None:
    sys.exit("Droi is not running")
daemon = next((r for r in rows if r["ppid"] == app["pid"] and r["comm"].endswith("/droid")), None)

a0, _ = usage(app["pid"])
d0, _ = usage(daemon["pid"]) if daemon else (0, 0)
t0 = time.time()
samples, last_c, last_t = [], a0, t0
while time.time() - t0 < secs - 0.5:
    time.sleep(5)
    c, _ = usage(app["pid"])
    t = time.time()
    samples.append(round((c - last_c) / (t - last_t) * 100, 1))
    last_c, last_t = c, t
a1, app_rss = usage(app["pid"])
d1, daemon_rss = usage(daemon["pid"]) if daemon else (0, 0)
elapsed = time.time() - t0

footprint = next((l.strip() for l in run("footprint", str(app["pid"])).splitlines() if "Footprint:" in l), "")
result = dict(
    label=label,
    version=run("defaults", "read", "/Applications/Droi.app/Contents/Info.plist", "CFBundleShortVersionString").strip(),
    at=time.strftime("%Y-%m-%d %H:%M:%S"),
    seconds=round(elapsed),
    screen_locked='"CGSSessionScreenIsLocked" = Yes' in run("ioreg", "-n", "Root", "-d1"),
    app_pid=app["pid"],
    app_cpu_pct=round((a1 - a0) / elapsed * 100, 1),
    app_samples_5s=samples,
    app_rss_mb=round(app_rss / 1024),
    app_footprint=footprint,
    daemon_pid=daemon["pid"] if daemon else None,
    daemon_cpu_pct=round((d1 - d0) / elapsed * 100, 1) if daemon else None,
    daemon_rss_mb=round(daemon_rss / 1024) if daemon else None,
)
print(json.dumps(result, ensure_ascii=False, indent=1))
if log:
    log = os.path.expanduser(log)
    os.makedirs(os.path.dirname(log) or ".", exist_ok=True)
    with open(log, "a") as f:
        f.write(json.dumps(result, ensure_ascii=False) + "\n")
