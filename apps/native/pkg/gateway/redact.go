package gateway

import "regexp"

var tokenParam = regexp.MustCompile(TokenQuery + `=[^&"'\s]+`)

// RedactToken hides the token in a Daemon socket URL inside s, such as the
// dial errors the SDK reports, so an error shown in the window never reveals
// a Pairing Token.
func RedactToken(s string) string {
	return tokenParam.ReplaceAllString(s, TokenQuery+"=…")
}
