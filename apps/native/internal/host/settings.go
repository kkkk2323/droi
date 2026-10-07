// Package host is the native app's Host (CONTEXT.md): it starts and watches
// the Daemon, supplies the Factory credential, keeps the Shell settings and
// makes Scratch Workspaces. The window talks to the Daemon directly, so there
// is no Gateway in between: what the Gateway added to frames (the credential,
// the System Prompt Addition) the Host hands to the Controller instead.
package host

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

// DefaultMemoryModel is the model Memory Sessions run on (ADR 0011).
const DefaultMemoryModel = "glm-5.3-flash"

// Settings are the Shell settings, in the layout of the Desktop Shell's
// settings.json (apps/desktop/src/main/shell-settings.ts).
type Settings struct {
	RemoteAccess       bool    `json:"remoteAccess"`
	DroidPath          *string `json:"droidPath"`
	FactoryAPIBaseURL  *string `json:"factoryApiBaseUrl"`
	AppendSystemPrompt *string `json:"appendSystemPrompt"`
	PairingHost        *string `json:"pairingHost"`
	ScratchFolder      *string `json:"scratchFolder"`
	MemoryEnabled      bool    `json:"memoryEnabled"`
	MemoryModel        *string `json:"memoryModel"`
	PairingToken       string  `json:"pairingToken"`
	ComputerID         string  `json:"computerId"`
	APIKey             *string `json:"apiKey"`
	Login              *string `json:"login"`
}

// SettingsStore keeps Settings in a JSON file created mode 0600; like the
// droid CLI's credentials, the user's home directory is the trust boundary.
type SettingsStore struct {
	mu   sync.Mutex
	file string
	env  func(string) string
	cur  Settings
}

// OpenSettings loads the file, creating it with a Pairing Token and a
// computer id when it is new or unreadable.
func OpenSettings(file string, env func(string) string) *SettingsStore {
	if env == nil {
		env = os.Getenv
	}
	s := &SettingsStore{file: file, env: env}
	b, err := os.ReadFile(file)
	loaded := err == nil && json.Unmarshal(b, &s.cur) == nil
	changed := false
	if s.cur.PairingToken == "" {
		s.cur.PairingToken = randomToken()
		changed = true
	}
	if s.cur.ComputerID == "" {
		s.cur.ComputerID = randomUUID()
		changed = true
	}
	if !loaded || changed {
		_ = s.saveLocked()
	}
	return s
}

func (s *SettingsStore) saveLocked() error {
	if err := os.MkdirAll(filepath.Dir(s.file), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.cur, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.file + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.file)
}

// Get returns a copy of the settings.
func (s *SettingsStore) Get() Settings {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur
}

// Update changes the settings with fn and saves them.
func (s *SettingsStore) Update(fn func(*Settings)) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.cur)
	return s.saveLocked()
}

// APIKey is FACTORY_API_KEY, else the stored key.
func (s *SettingsStore) APIKey() string {
	if k := s.env("FACTORY_API_KEY"); k != "" {
		return k
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return deref(s.cur.APIKey)
}

// MemoryModel is the model Memory Sessions run on.
func (s Settings) MemoryModelOrDefault() string {
	if m := deref(s.MemoryModel); m != "" {
		return m
	}
	return DefaultMemoryModel
}

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// OptString is nil for an empty or blank string.
func OptString(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	t := strings.TrimSpace(s)
	return &t
}

var hostName = regexp.MustCompile(`(?i)^(\[[0-9a-f:.]+\]|[a-z0-9.-]+)$`)

// PairingHostOf is the host name in what the user typed for the pairing
// address: "laptop.myhome", "laptop.myhome:41417" or a full URL.
func PairingHostOf(text string) *string {
	t := strings.TrimSpace(text)
	if t == "" {
		return nil
	}
	if !regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://`).MatchString(t) {
		t = "http://" + t
	}
	u, err := url.Parse(t)
	if err != nil || !hostName.MatchString(u.Hostname()) {
		return nil
	}
	h := u.Hostname()
	return &h
}

// ScratchFolderOf is what the user typed for the Scratch folder as an
// absolute path, ~ being the home directory; nil for empty or relative.
func ScratchFolderOf(text, home string) *string {
	t := strings.TrimSpace(text)
	if t == "" {
		return nil
	}
	switch {
	case t == "~":
		t = home
	case strings.HasPrefix(t, "~/"):
		t = filepath.Join(home, t[2:])
	}
	if !filepath.IsAbs(t) {
		return nil
	}
	t = filepath.Clean(t)
	return &t
}

func randomToken() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func randomUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	const hex = "0123456789abcdef"
	out := make([]byte, 0, 36)
	for i, c := range b {
		if i == 4 || i == 6 || i == 8 || i == 10 {
			out = append(out, '-')
		}
		out = append(out, hex[c>>4], hex[c&15])
	}
	return string(out)
}
