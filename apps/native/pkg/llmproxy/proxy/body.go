package proxy

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"strings"
)

const (
	messageCallTypeHeader = "X-Message-Call-Type"
	sessionTitleCallType  = "session-title-generation"
	// maxDecodedRequestBody bounds a gzip request body once decoded.
	maxDecodedRequestBody = 64 << 20
)

// OpenedSession is one Session a Daemon has open, as
// daemon.list_opened_sessions reports it.
type OpenedSession struct {
	SessionID    string `json:"sessionId"`
	UpdatedAt    int64  `json:"updatedAt"`
	WorkingState string `json:"workingState"`
	Cwd          string `json:"cwd,omitempty"`
}

func gunzipBody(raw []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	decoded, err := io.ReadAll(io.LimitReader(zr, maxDecodedRequestBody+1))
	if err != nil {
		return nil, err
	}
	if len(decoded) > maxDecodedRequestBody {
		return nil, fmt.Errorf("decoded request body exceeds %d bytes", maxDecodedRequestBody)
	}
	return decoded, nil
}

// encodeWireBody is plain as the request body a client sent in encoding, so
// that a body the proxy changed goes out as the client encoded it.
func encodeWireBody(encoding string, plain []byte) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "", "identity":
		return plain, nil
	case "gzip":
		return gzipBody(plain)
	}
	return nil, fmt.Errorf("unsupported content encoding %q", encoding)
}

func gzipBody(plain []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(plain); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
