package proxy

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
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
