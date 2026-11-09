package host

// loginPath is current on Windows: an app started from the Start menu has the PATH a terminal has.
func loginPath(_, current string) string { return current }
