package proxy

import (
	"io"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/contentenc"
)

// Requests are forwarded with the client's own Accept-Encoding (droid, on Bun,
// asks for gzip, deflate, br and zstd), so an upstream reply may arrive in any
// of them. decodeUpstreamBody hands the rest of the pipeline plaintext, which
// the prem and feature-flag rewrites, the SSE filter and the usage observer all
// read; without it they would silently see compressed bytes and stop matching.
//
// An encoding this proxy cannot decode leaves the body untouched and reports
// false, so the caller forwards it as the upstream sent it and keeps
// Content-Encoding on the reply.
func decodeUpstreamBody(contentEncoding string, body io.Reader) (io.Reader, io.Closer, bool) {
	return contentenc.Decode(contentEncoding, body)
}
