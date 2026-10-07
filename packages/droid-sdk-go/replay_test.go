package droid_test

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

// TestReplayRecordings decodes every frame recorded from a real Daemon
// (testdata/live, written by the live tests) with the generated types: each
// result into its method's result type, each session notification into a
// known shape. A failure means the protocol moved; rerun the generator
// against a newer @factory/droid-sdk. Fields the types lack are only logged:
// the Daemon is usually newer than the SDK the types come from.
func TestReplayRecordings(t *testing.T) {
	files, _ := filepath.Glob("testdata/live/*.jsonl")
	if len(files) == 0 {
		t.Skip("no recordings; run the live tests with DROID_LIVE_RECORD=testdata/live")
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) { replay(t, file) })
	}
}

type recorded struct {
	Dir   string          `json:"dir"`
	Frame json.RawMessage `json:"frame"`
}

type frame struct {
	Type   string          `json:"type"`
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
}

func replay(t *testing.T, file string) {
	f, err := os.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 64<<20)
	methodOf := map[string]string{}
	unknownFields := map[string]int{}
	counts := map[string]int{}
	for sc.Scan() {
		var r recorded
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		var m frame
		if err := json.Unmarshal(r.Frame, &m); err != nil {
			t.Fatal(err)
		}
		if r.Dir == "sent" {
			if m.Method != "" {
				methodOf[m.ID] = m.Method
				if p := protocol.NewParams(m.Method); p != nil {
					decode(t, "params of "+m.Method, m.Params, p, unknownFields)
				}
			}
			continue
		}
		switch {
		case m.Type == "response":
			method := methodOf[m.ID]
			if m.Error != nil {
				var e struct {
					Code int `json:"code"`
				}
				if err := json.Unmarshal(m.Error, &e); err != nil || e.Code == 0 {
					t.Errorf("%s: malformed error %s", method, m.Error)
				}
				counts[fmt.Sprintf("error %s %d", method, e.Code)]++
				continue
			}
			if res := protocol.NewResult(method); res != nil {
				decode(t, "result of "+method, m.Result, res, unknownFields)
				counts["result "+method]++
			}
		case m.Method == protocol.NotificationSessionNotification:
			var p protocol.SessionNotificationParams
			decode(t, m.Method, m.Params, &p, unknownFields)
			v, err := p.Notification.Value()
			if err != nil {
				t.Errorf("session notification %s: %v", p.Notification.Type, err)
			}
			if v == nil {
				t.Errorf("unknown session notification type %q", p.Notification.Type)
				continue
			}
			b, _ := json.Marshal(p.Notification)
			decode(t, "notification "+p.Notification.Type, b, v, unknownFields)
			counts["notification "+p.Notification.Type]++
		case m.Type == "request" && m.Method == protocol.ServerRequestRequestPermission:
			decode(t, m.Method, m.Params, new(protocol.RequestPermissionParams), unknownFields)
			counts["request "+m.Method]++
		case m.Type == "request" && m.Method == protocol.ServerRequestAskUser:
			decode(t, m.Method, m.Params, new(protocol.AskUserParams), unknownFields)
			counts["request "+m.Method]++
		default:
			counts["other "+m.Method]++
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	logSorted(t, "decoded", counts)
	logSorted(t, "fields the generated types lack", unknownFields)
}

// decode decodes raw into v, failing on a type mismatch; it reports fields
// v lacks into unknown.
func decode(t *testing.T, what string, raw json.RawMessage, v any, unknown map[string]int) {
	t.Helper()
	if len(raw) == 0 {
		return
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Errorf("%s: %v", what, err)
		return
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		unknown[what+": "+err.Error()]++
	}
}

func logSorted(t *testing.T, title string, m map[string]int) {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	t.Logf("%s (%d):", title, len(keys))
	for _, k := range keys {
		t.Logf("  %4d  %s", m[k], k)
	}
}
