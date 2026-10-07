package memorywork

// Row is one Memory as Settings → Memory shows it: a Project Memory, or
// the Global Memory when Workspace is "".
type Row struct {
	Workspace string
	Entries   int
	Chars     int
	// SoftLimit is the size past which Settings suggests consolidating.
	SoftLimit     int
	OverSoftLimit bool
	// LastConsolidated is an ISO time; "" before the first consolidation.
	LastConsolidated string
	Consolidating    bool
	// Searches of this Memory since calls were first logged, how many found
	// nothing, and the entries no search has returned (corrections left out).
	Searches, EmptySearches, NeverFound int
}

// Overview is every Memory on this computer.
type Overview struct {
	Rows []Row
	// LoggedSince is the first logged Memory Server call; "" before any.
	LoggedSince string
}

// Result is what a consolidation did: the category slices whose answer was
// applied, and those left unchanged.
type Result struct{ Applied, Rejected int }
