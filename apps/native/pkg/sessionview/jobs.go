package sessionview

import (
	"cmp"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
	"github.com/kkkk2323/droi/apps/native/pkg/transcript"
)

// JobOutputLines is how much of a background command's output its preview shows.
const JobOutputLines = 20

// jobsRead is how often a View reads its jobs again while a turn streams
// into the last message, and checks the processes of those running.
const jobsRead = 2 * time.Second

// jobsState is what a View knows of its Session's background jobs.
type jobsState struct {
	list []transcript.Job
	byID map[string]int
	// rev, count and at are the revision, message count and time list was
	// read at: a job starts and ends with a new message, so a turn
	// streaming into the last one reads them again only now and then.
	rev   int64
	count int
	at    time.Time
	// gone are the commands whose process is no longer on this computer:
	// the Daemon's record of the end may never come when it stopped first.
	gone    map[string]bool
	checked time.Time
	tails   map[string]*jobTail
	// stopping are the jobs StopJob waits on the Daemon to stop.
	stopping map[string]bool
}

type jobTail struct {
	text    string
	at      time.Time
	reading bool
}

func (v *View) local() bool { return v.env.Local != nil && v.env.Local() }

// Jobs are the Session's background jobs, oldest first: commands an Execute
// left running and Scripts still going after their first answer.
func (v *View) Jobs() []transcript.Job {
	ctl := v.env.Controller
	if ctl == nil {
		return nil
	}
	s := ctl.Store().Session(v.id)
	if s == nil {
		return nil
	}
	js := &v.jobs
	now := v.env.Now()
	rev, count := v.env.Revisions.Load(v.id), s.MessageCount()
	if js.byID == nil || rev != js.rev && (count != js.count || now.Sub(js.at) >= jobsRead) {
		js.rev, js.count, js.at = rev, count, now
		js.list = transcript.Jobs(s.Messages())
		js.byID = make(map[string]int, len(js.list))
		for i, j := range js.list {
			js.byID[j.ID] = i
		}
	}
	if v.local() && transcript.RunningJobs(js.list) > 0 && now.Sub(js.checked) >= jobsRead {
		js.checked = now
		for _, j := range js.list {
			if !j.Done && !j.Script && j.PID > 0 && !processAlive(j.PID) {
				if js.gone == nil {
					js.gone = map[string]bool{}
				}
				js.gone[j.ID] = true
			}
		}
	}
	for i := range js.list {
		if js.gone[js.list[i].ID] {
			js.list[i].Done = true
		}
	}
	return withProcesses(js.list, js.byID, js.gone, s.BackgroundProcesses())
}

// withProcesses are jobs with the Daemon's list of the Session's background
// processes: a job it lists can be stopped, and one whose start is not among
// the messages read (a load brings only the last ones) is added.
func withProcesses(jobs []transcript.Job, byID map[string]int, gone map[string]bool, procs []protocol.SessionBackgroundProcess) []transcript.Job {
	if len(procs) == 0 {
		return jobs
	}
	out := slices.Clone(jobs)
	for _, p := range procs {
		id := cmp.Or(p.ToolUseID, p.ID)
		if i, ok := byID[id]; ok {
			if !out[i].Done {
				out[i].ProcessID = p.ID
			}
			continue
		}
		if gone[id] {
			continue
		}
		out = append(out, transcript.ProcessJob(p))
	}
	return out
}

// StopJob asks the Daemon to stop a background command it lists. UI thread.
func (v *View) StopJob(j transcript.Job) {
	ctl := v.env.Controller
	js := &v.jobs
	if ctl == nil || j.ProcessID == "" || js.stopping[j.ID] {
		return
	}
	if js.stopping == nil {
		js.stopping = map[string]bool{}
	}
	js.stopping[j.ID] = true
	go func() {
		cl, err := ctl.Client()
		var res *protocol.KillBackgroundProcessResult
		if err == nil {
			res, err = cl.KillBackgroundProcess(v.env.Ctx, protocol.KillBackgroundProcessParams{SessionID: v.id, ProcessID: j.ProcessID})
		}
		if err == nil && !res.Killed {
			err = errors.New(l10n.L("it is not running, or droid could not signal it"))
		}
		v.env.Update(func() {
			delete(js.stopping, j.ID)
			if err != nil {
				v.err = l10n.L("Could not stop %s: %s", j.Summary, err.Error())
				return
			}
			// The Daemon tells the end as the process exits; until then
			// the job reads as ended, so it is not stopped twice.
			if js.gone == nil {
				js.gone = map[string]bool{}
			}
			js.gone[j.ID] = true
		})
	}()
}

// Stopping is whether StopJob waits on the Daemon for j.
func (v *View) Stopping(j transcript.Job) bool { return v.jobs.stopping[j.ID] }

// RunningCommands are the Session's background commands still running.
// A Script still going is not one: it is the run of a call Droid waits on.
func (v *View) RunningCommands() []transcript.Job {
	var out []transcript.Job
	for _, j := range v.Jobs() {
		if !j.Done && !j.Script {
			out = append(out, j)
		}
	}
	return out
}

// job is the background job a tool call started, if any; jobs must be
// read this frame.
func (v *View) job(id string) (transcript.Job, bool) {
	if i, ok := v.jobs.byID[id]; ok {
		return v.jobs.list[i], true
	}
	return transcript.Job{}, false
}

// JobState is how a job reads: running for how long, or how it ended.
func (v *View) JobState(j transcript.Job) string {
	if !j.Done {
		took := v.env.Now().Sub(time.UnixMilli(int64(j.StartedAt)))
		return l10n.L("Running in background · %s", transcript.FormatDuration(float64(max(took, time.Second).Milliseconds())))
	}
	var state string
	switch {
	case j.ExitCode != nil && *j.ExitCode != 0:
		state = l10n.L("Exited with code %d", *j.ExitCode)
	case j.Status == "failed":
		state = l10n.L("Failed")
	case j.Status == "cancelled":
		state = l10n.L("Cancelled")
	default:
		state = l10n.L("Finished")
	}
	if j.EndedAt > j.StartedAt && j.StartedAt > 0 {
		state += " · " + transcript.FormatDuration(j.EndedAt-j.StartedAt)
	}
	return state
}

// jobMark is a background job's mark at the end of its row: how it runs
// or ended, and for how long.
func (v *View) jobMark(c *ui.Context, j transcript.Job) {
	k, t := v.look()
	state := v.JobState(j)
	ui.Row(c).Role(ui.RoleStatus).Label(state).Gap(k.Px(6)).Shrink(0).Children(func() {
		k.Text(c, state, 11, 16.5).TextColor(t.MutedForeground).FontFeatures("tnum").SingleLine()
		switch {
		case !j.Done:
			c.After(time.Second)
			k.Spinner(c, 14, t.Info)
		case j.Failed():
			k.Icon(c, "circle-x", 14, t.DestructiveForeground)
		default:
			k.Icon(c, "check", 14, t.Success)
		}
	})
}

// ShowsOutput is whether JobOutput draws anything for j.
func (v *View) ShowsOutput(j transcript.Job) bool {
	return !j.Script && j.OutputFile != "" && v.local()
}

// JobOutput is the last lines a background command wrote, read from its
// output file while the Daemon runs on this computer; false draws nothing.
func (v *View) JobOutput(c *ui.Context, j transcript.Job) bool {
	if !v.ShowsOutput(j) {
		return false
	}
	k, t := v.look()
	text, read := v.jobTail(j)
	if !j.Done {
		c.After(jobsRead)
	}
	color := t.MutedForeground
	if text == "" {
		color = t.MutedForeground.Alpha(0.7)
		text = l10n.L("No output yet")
		if !read {
			text = l10n.L("Reading output…")
		}
	}
	ui.Column(c).Role(ui.RoleGroup).Label(l10n.L("Output")).Children(func() {
		ui.Scroll(c).MaxHeight(k.Px(240)).Children(func() {
			k.Text(c, text, 11.5, 20).Font(k.Mono).TextColor(color).Selectable().Padding(k.Px(6), k.Px(10))
		})
		k.Text(c, j.OutputFile, 10.5, 15.75).Font(k.Mono).TextColor(t.MutedForeground.Alpha(0.7)).Tooltip(j.OutputFile).
			SingleLine().Padding(0, k.Px(10), k.Px(6), k.Px(10))
	})
	return true
}

// jobTail is the command's last lines as last read, reading them again
// while it runs; read is false until the first read came back.
func (v *View) jobTail(j transcript.Job) (text string, read bool) {
	js := &v.jobs
	if js.tails == nil {
		js.tails = map[string]*jobTail{}
	}
	tl := js.tails[j.ID]
	if tl == nil {
		tl = &jobTail{}
		js.tails[j.ID] = tl
	}
	now := v.env.Now()
	if !tl.reading && (tl.at.IsZero() || !j.Done && now.Sub(tl.at) >= jobsRead) {
		tl.reading = true
		path := j.OutputFile
		go func() {
			text := lastLines(path, JobOutputLines)
			v.env.Update(func() { tl.text, tl.at, tl.reading = text, now, false })
		}()
	}
	return tl.text, !tl.at.IsZero()
}

// lastLines are a file's last n lines, from its last 32 KiB; "" when it
// cannot be read.
func lastLines(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	const most = 32 << 10
	if st, err := f.Stat(); err == nil && st.Size() > most {
		_, _ = f.Seek(st.Size()-most, io.SeekStart)
	}
	b, _ := io.ReadAll(io.LimitReader(f, most))
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}
