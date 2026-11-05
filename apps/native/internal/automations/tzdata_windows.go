package automations

// Windows has no zone database of its own for time.LoadLocation; macOS and
// Linux have the system's.
import _ "time/tzdata"
