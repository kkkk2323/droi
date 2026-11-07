package host

import (
	"errors"
	"os/exec"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// prepareProcess leaves the sidecar's creation alone. Windows has no process
// groups to join; ownership of the tree is a Job Object, made and assigned
// right after Start (newProcessTree), before the sidecar can start anything.
func prepareProcess(*exec.Cmd) {}

// signalProcess tells the sidecar to stop.
//
// Windows has no SIGTERM. A stop asks kernel.shutdown first when the Host wires
// one to Terminate, which is the orderly way and runs the sidecar's own close;
// this is what is left when that is not wired or fails: TerminateProcess, which
// skips storage.close() and leaves SQLite to recover from its write-ahead log
// on the next start.
func signalProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// newProcessTree puts the sidecar in a Job Object of its own.
//
// Children join their parent's job unless they break away, and
// JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE means the job's handle is the ownership:
// while the Host holds it, every process in the job is there to be killed, and
// closing the handle ends them. That is what taskkill /T cannot promise — it
// names a process, and a sidecar that already exited is not there to name.
//
// The assumption is that Kernel starts no process before its handshake, which
// is after this call: a process that was already running when the job was made
// belongs to no job.
func newProcessTree(cmd *exec.Cmd) (processTree, error) {
	if cmd.Process == nil {
		return nil, errors.New("no process")
	}
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	process, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	defer windows.CloseHandle(process)
	if err := windows.AssignProcessToJobObject(job, process); err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	return &jobTree{job: job}, nil
}

// jobTree is one generation's job.
//
// The handle is guarded: the watchdog and the retire path both end a tree, and
// one of them may reach close while the other is on its way to kill.
type jobTree struct {
	mu  sync.Mutex
	job windows.Handle
}

// kill ends every process in the job. A job that is already empty, or already
// released, answers nothing the caller needs: the tree is gone either way.
func (j *jobTree) kill() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.job != 0 {
		_ = windows.TerminateJobObject(j.job, 1)
	}
}

// close releases the job. KILL_ON_JOB_CLOSE is also what ends a process that
// joined late, so closing is a last sweep rather than only a free.
func (j *jobTree) close() {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.job != 0 {
		_ = windows.CloseHandle(j.job)
		j.job = 0
	}
}
