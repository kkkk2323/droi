package proxy

import (
	"bufio"
	"bytes"
	"io"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/kkkk2323/droi/apps/native/pkg/llmproxy/diag"
)

// Factory's whoami identity may carry premBaseHostV2. When it is set, droid
// sends LLM and session-sync traffic straight to that host and ignores
// FACTORY_API_BASE_URL, so those requests would bypass the proxy. The proxy
// rewrites the field in every non-LLM response to its own premPathPrefix and
// forwards requests under that prefix to the real Prem host.
const (
	premPathPrefix      = "/_prem"
	defaultPremUpstream = "https://prem.factory.ai"
	// premRewriteMaxLine bounds how much of one response line is held to look
	// for the field; longer lines pass through unchanged.
	premRewriteMaxLine = 1 << 20
)

var premFieldName = []byte("premBaseHostV2")

// premHostField matches the field both as plain JSON and as JSON embedded in a
// JSON string (escaped quotes), e.g. a channel frame whose data is a JSON text.
var premHostField = regexp.MustCompile(`(\\?"premBaseHostV2\\?"\s*:\s*\\?")([^"\\]*)(\\?")`)

// stripPremPrefix maps a /_prem/... proxy path to the upstream Prem path.
func stripPremPrefix(path string) (string, bool) {
	if path == premPathPrefix {
		return "/", true
	}
	if strings.HasPrefix(path, premPathPrefix+"/") {
		return path[len(premPathPrefix):], true
	}
	return "", false
}

func (p *Proxy) localPremBaseURL() string {
	return p.BaseURL() + premPathPrefix
}

// PremUpstream reports the Prem host requests under premPathPrefix go to: the
// last value seen in a whoami identity, or the production default.
func (p *Proxy) PremUpstream() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.premUpstream != "" {
		return p.premUpstream
	}
	return defaultPremUpstream
}

// rewritePremHosts replaces every premBaseHostV2 value in line with the
// proxy's Prem prefix and remembers the original as the Prem upstream.
func (p *Proxy) rewritePremHosts(line []byte) []byte {
	local := p.localPremBaseURL()
	return premHostField.ReplaceAllFunc(line, func(m []byte) []byte {
		sub := premHostField.FindSubmatch(m)
		original := string(sub[2])
		if original == "" || original == local {
			return m
		}
		p.mu.Lock()
		changed := p.premUpstream != original
		p.premUpstream = original
		p.mu.Unlock()
		if changed {
			p.recorder.RecordEvent(diag.Event{Time: time.Now(), Kind: "prem_host_rewritten", Detail: original + " -> " + local})
			log.Printf("prem_host_rewritten upstream=%s local=%s", original, local)
		}
		out := make([]byte, 0, len(sub[1])+len(local)+len(sub[3]))
		out = append(out, sub[1]...)
		out = append(out, local...)
		return append(out, sub[3]...)
	})
}

// copyRewritingPremHosts copies src to dst line by line, rewriting lines that
// carry premBaseHostV2. Each line is forwarded as soon as it is complete and
// flush runs whenever no more input is buffered, so streamed responses (NDJSON,
// SSE) keep their latency.
func (p *Proxy) copyRewritingPremHosts(dst io.Writer, src io.Reader, flush func()) (int64, error) {
	br := bufio.NewReaderSize(src, 64<<10)
	var written int64
	var pending []byte
	for {
		chunk, err := br.ReadSlice('\n')
		pending = append(pending, chunk...)
		lineDone := err != bufio.ErrBufferFull
		if lineDone || len(pending) >= premRewriteMaxLine {
			out := pending
			if lineDone && bytes.Contains(out, premFieldName) {
				out = p.rewritePremHosts(out)
			}
			n, werr := dst.Write(out)
			written += int64(n)
			pending = pending[:0]
			if werr != nil {
				return written, werr
			}
			if br.Buffered() == 0 {
				flush()
			}
		}
		if lineDone && err != nil {
			if err == io.EOF {
				return written, nil
			}
			return written, err
		}
	}
}
