//go:build windows

package fakekernel

import (
	"errors"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// processTree is everything a test's Kernel started, so a test leaves no process behind.
//
// This is a minimal copy of `apps/native/pkg/host`'s Job Object handling, and should be replaced by it when
// that lands here (PR #228): one implementation of "stop the tree", not two that can drift.
type processTree interface {
	kill()
	close()
}

// prepareProcess leaves creation alone: Windows has no process groups to join, and the job is assigned right
// after Start, before the Kernel can begin anything.
func prepareProcess(*exec.Cmd) {}

func newProcessTree(cmd *exec.Cmd) (processTree, error) {
	if cmd.Process == nil {
		return nil, errors.New("fakekernel: no process to own")
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
	// A kernel starts no process before its handshake, which is after this, so everything it starts joins
	// the job.
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

// jobTree is one Kernel's job.
type jobTree struct{ job windows.Handle }

func (j *jobTree) kill() {
	if j.job != 0 {
		_ = windows.TerminateJobObject(j.job, 1)
	}
}

// close releases the job. KILL_ON_JOB_CLOSE is also what ends a process that joined late, so closing is a
// last sweep rather than only a free.
func (j *jobTree) close() {
	if j.job != 0 {
		_ = windows.CloseHandle(j.job)
		j.job = 0
	}
}
