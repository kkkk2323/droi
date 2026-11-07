//go:build !race

package host

// raceDetector reports that this test binary runs under the race detector. It
// widens the budgets of tests that time a signal.
const raceDetector = false
