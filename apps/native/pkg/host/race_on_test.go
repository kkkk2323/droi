//go:build race

package host

// raceDetector reports that this test binary runs under the race detector. It
// makes a signal reach a child and the child exit about a thousand times
// slower, so time budgets measured around a signal widen when it is on. It is
// the test binary that is slow, not the Supervisor.
const raceDetector = true
