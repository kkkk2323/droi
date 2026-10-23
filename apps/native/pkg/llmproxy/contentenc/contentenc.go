// Package contentenc decodes HTTP bodies in the encodings droid (on Bun) asks
// for: gzip, deflate, br and zstd.
package contentenc

import (
	"bufio"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"io"
	"strings"

	"github.com/andybalholm/brotli"
	"github.com/klauspost/compress/zstd"
)

// AcceptEncoding is the Accept-Encoding droid sends.
const AcceptEncoding = "gzip, deflate, br, zstd"

// Decode returns the reader to use, a closer for the decoder (nil when there
// is none), and whether the reader yields plaintext. An encoding it cannot
// decode leaves the body untouched and reports false.
func Decode(contentEncoding string, body io.Reader) (io.Reader, io.Closer, bool) {
	switch strings.ToLower(strings.TrimSpace(contentEncoding)) {
	case "", "identity":
		return body, nil, true
	case "gzip", "x-gzip":
		zr, err := gzip.NewReader(body)
		if err != nil {
			return body, nil, false
		}
		return zr, zr, true
	case "deflate":
		// RFC 9110 means zlib here, but servers send raw DEFLATE too; the
		// zlib header tells them apart without consuming the stream.
		br := bufio.NewReader(body)
		if head, err := br.Peek(2); err == nil && isZlibHeader(head) {
			zr, err := zlib.NewReader(br)
			if err != nil {
				return br, nil, false
			}
			return zr, zr, true
		}
		fr := flate.NewReader(br)
		return fr, fr, true
	case "br":
		return brotli.NewReader(body), nil, true
	case "zstd":
		zr, err := zstd.NewReader(body)
		if err != nil {
			return body, nil, false
		}
		rc := zr.IOReadCloser()
		return rc, rc, true
	default:
		return body, nil, false
	}
}

func isZlibHeader(head []byte) bool {
	if len(head) < 2 {
		return false
	}
	return head[0]&0x0f == 8 && (uint16(head[0])<<8|uint16(head[1]))%31 == 0
}
