package host

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"github.com/kkkk2323/droi/apps/native/pkg/sessions"
)

// The Daemon lists only the Sessions of its login's organization: droid
// hides the others' and will not load one. Each Factory account is
// usually an organization of its own, so after a switch the Sessions of the
// account before seem gone, though they are on disk. The Host keeps, for
// each organization, the Sessions its Daemon listed last, so the window can
// show them beside the live list as Sessions of their account.

type sessionIndex struct {
	Orgs map[string][]sessions.Summary `json:"orgs"`
}

// ElsewhereSession is a Session another signed-in account's Daemon listed.
type ElsewhereSession struct {
	sessions.Summary
	Account AccountInfo
}

func (h *Host) sessionIndexPath() string { return filepath.Join(h.cfg.UserData, "session-index.json") }

// loadIndex reads the index once. Call with indexMu held.
func (h *Host) loadIndex() *sessionIndex {
	if h.index == nil {
		h.index = &sessionIndex{}
		if b, err := os.ReadFile(h.sessionIndexPath()); err == nil {
			_ = json.Unmarshal(b, h.index)
		}
		if h.index.Orgs == nil {
			h.index.Orgs = map[string][]sessions.Summary{}
		}
	}
	return h.index
}

// RecordSessions keeps list, every page of it, as the Sessions the Daemon
// lists for organization org; those of other organizations in it are left
// out, as the Daemon lists them only when droid shows other organizations'.
func (h *Host) RecordSessions(org string, list []sessions.Summary) error {
	if org == "" {
		return nil
	}
	own := make([]sessions.Summary, 0, len(list))
	for _, s := range list {
		if s.OrganizationID == org {
			own = append(own, s)
		}
	}
	h.indexMu.Lock()
	defer h.indexMu.Unlock()
	idx := h.loadIndex()
	if cur, ok := idx.Orgs[org]; ok && sameSummaries(cur, own) {
		return nil
	}
	idx.Orgs[org] = own
	h.indexRev++
	return h.saveIndex()
}

// forgetOrganization drops what the index keeps for org.
func (h *Host) forgetOrganization(org string) error {
	h.indexMu.Lock()
	defer h.indexMu.Unlock()
	idx := h.loadIndex()
	if _, ok := idx.Orgs[org]; !ok {
		return nil
	}
	delete(idx.Orgs, org)
	h.indexRev++
	return h.saveIndex()
}

// saveIndex writes the index. Call with indexMu held.
func (h *Host) saveIndex() error {
	b, err := json.Marshal(h.index)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(h.cfg.UserData, 0o700); err != nil {
		return err
	}
	return writeFileAtomic(h.sessionIndexPath(), b)
}

// SessionIndexRev changes whenever the kept Sessions do.
func (h *Host) SessionIndexRev() uint64 {
	h.indexMu.Lock()
	defer h.indexMu.Unlock()
	return h.indexRev
}

// SessionsElsewhere are the Sessions kept for the organizations of the
// signed-in accounts the Daemon does not run as, each with its account;
// none of the active account's organization, which the Daemon lists.
func (h *Host) SessionsElsewhere() []ElsewhereSession {
	accounts := h.Accounts()
	active := ""
	for _, acc := range accounts {
		if acc.Active && acc.Login != nil {
			active = deref(acc.Login.OrgID)
		}
	}
	h.indexMu.Lock()
	defer h.indexMu.Unlock()
	idx := h.loadIndex()
	var out []ElsewhereSession
	seen := map[string]bool{active: true}
	for _, acc := range accounts {
		if acc.Active || acc.Login == nil {
			continue
		}
		org := deref(acc.Login.OrgID)
		if org == "" || seen[org] {
			continue
		}
		seen[org] = true
		for _, s := range idx.Orgs[org] {
			out = append(out, ElsewhereSession{Summary: s, Account: acc})
		}
	}
	return out
}

func sameSummaries(a, b []sessions.Summary) bool {
	x, err1 := json.Marshal(a)
	y, err2 := json.Marshal(b)
	return err1 == nil && err2 == nil && bytes.Equal(x, y)
}

// visibleOrgsKey is the user setting droid reads, as general's
// sessionVisibleOrganizationIds, for the organizations whose Sessions stay
// listed beside the active one's. It counts only once Factory turns on
// droid's show_cross_org_sessions flag, and changes nothing before.
const visibleOrgsKey = "sessionVisibleOrganizationIds"

// shareSessionVisibility names in droid's user settings the organizations
// of every signed-in account, when there are two or more, so that droid
// lists and opens their Sessions under any of them once it may. Ids the
// user put there stay; only those Droi added go when their account does.
func (h *Host) shareSessionVisibility() error {
	var want []string
	for _, acc := range h.Accounts() {
		if acc.Login != nil {
			if org := deref(acc.Login.OrgID); org != "" && !slices.Contains(want, org) {
				want = append(want, org)
			}
		}
	}
	if len(want) < 2 {
		want = nil
	}
	added := h.Settings.Get().VisibleOrgsAdded
	path := filepath.Join(h.SharedFactoryHome(), "settings.json")
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		if len(want) == 0 {
			return nil
		}
		b, err = []byte("{}\n"), nil
	}
	if err != nil {
		return err
	}
	var cur []string
	if raw, ok, err := jsonKey(b, visibleOrgsKey); err != nil {
		return err
	} else if ok {
		_ = json.Unmarshal(raw, &cur)
	}
	next := slices.DeleteFunc(slices.Clone(cur), func(id string) bool { return slices.Contains(added, id) && !slices.Contains(want, id) })
	var nowAdded []string
	for _, id := range want {
		switch {
		case !slices.Contains(cur, id):
			next = append(next, id)
			nowAdded = append(nowAdded, id)
		case slices.Contains(added, id):
			nowAdded = append(nowAdded, id)
		}
	}
	if !slices.Equal(next, cur) {
		var value any = next
		if len(next) == 0 {
			value = nil
		}
		out, err := setJSONKey(b, visibleOrgsKey, value)
		if err != nil {
			return err
		}
		mode := fs.FileMode(0o600)
		if st, err := os.Stat(path); err == nil {
			mode = st.Mode().Perm()
		}
		if err := writeFileAtomic(path, out); err != nil {
			return err
		}
		_ = os.Chmod(path, mode)
	}
	if slices.Equal(nowAdded, added) {
		return nil
	}
	return h.Settings.Update(func(s *Settings) { s.VisibleOrgsAdded = nowAdded })
}

// jsonField is a top-level member of a JSON object, its value as written.
type jsonField struct {
	key   string
	value json.RawMessage
}

func jsonFields(b []byte) ([]jsonField, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	if t, err := d.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("settings.json is not an object")
	}
	var out []jsonField
	for d.More() {
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		key, _ := t.(string)
		var v json.RawMessage
		if err := d.Decode(&v); err != nil {
			return nil, err
		}
		out = append(out, jsonField{key, v})
	}
	return out, nil
}

func jsonKey(b []byte, key string) (json.RawMessage, bool, error) {
	fields, err := jsonFields(b)
	if err != nil {
		return nil, false, err
	}
	for _, f := range fields {
		if f.key == key {
			return f.value, true, nil
		}
	}
	return nil, false, nil
}

// setJSONKey sets one top-level member of a JSON object, or takes it out
// for a nil value, and keeps the others in their order, indented by two
// spaces as droid writes its settings.
func setJSONKey(b []byte, key string, value any) ([]byte, error) {
	fields, err := jsonFields(b)
	if err != nil {
		return nil, err
	}
	i := slices.IndexFunc(fields, func(f jsonField) bool { return f.key == key })
	if value == nil {
		if i >= 0 {
			fields = slices.Delete(fields, i, i+1)
		}
	} else {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		if i >= 0 {
			fields[i].value = raw
		} else {
			fields = append(fields, jsonField{key, raw})
		}
	}
	var out bytes.Buffer
	out.WriteString("{")
	for i, f := range fields {
		if i > 0 {
			out.WriteString(",")
		}
		k, _ := json.Marshal(f.key)
		out.WriteString("\n  ")
		out.Write(k)
		out.WriteString(": ")
		if err := json.Indent(&out, f.value, "  ", "  "); err != nil {
			return nil, err
		}
	}
	if len(fields) > 0 {
		out.WriteString("\n")
	}
	out.WriteString("}\n")
	return out.Bytes(), nil
}
