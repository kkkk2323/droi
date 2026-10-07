// Package collate orders names the way the web Client does with
// String.localeCompare, for the lists that sort by name.
package collate

import (
	"cmp"
	"strings"
)

// Compare approximates localeCompare: case folded first, then as is.
func Compare(a, b string) int {
	return cmp.Or(strings.Compare(strings.ToLower(a), strings.ToLower(b)), strings.Compare(a, b))
}
