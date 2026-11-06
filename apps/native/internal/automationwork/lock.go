package automationwork

import "errors"

// ErrLocked is a folder of Automations another Scheduler runs.
var ErrLocked = errors.New("another Droi runs these automations")

// lockFile is the Automations folder's lock; its name starts with a dot, so
// the Store takes it for no Automation.
const lockFile = ".lock"
