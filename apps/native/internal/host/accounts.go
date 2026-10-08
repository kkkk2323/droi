package host

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// An account is a Factory login the Daemon can run as (ADR 0017). The
// first is the droid CLI's own, in ~/.factory; each one added in Droi has a
// folder of its own under the user data, which the Daemon gets as
// FACTORY_HOME_OVERRIDE: droid then keeps its login, logs and state in
// <folder>/.factory.

// sharedEntries are what every added account's .factory links to in the
// droid CLI's, so all accounts see one set of skills, droids, commands,
// hooks, plugins, MCP servers, settings and Sessions. AGENTS.md is copied
// instead: droid ignores a linked one.
var sharedEntries = []string{"skills", "droids", "commands", "hooks", "plugins", "mcp.json", "settings.json", "sessions"}

// AccountInfo is an account as Settings lists it.
type AccountInfo struct {
	// ID is "" for the droid CLI's own account.
	ID string
	// Login is nil while the account is signed out.
	Login  *Account
	Active bool
}

var (
	errNoAccount    = hostError("no such account")
	errAccountInUse = hostError("the account in use cannot be removed")
)

// SharedFactoryHome is the droid CLI's own ~/.factory, which the added
// accounts share their skills, settings and Sessions with.
func (h *Host) SharedFactoryHome() string {
	root := h.cfg.Home
	if d := h.cfg.Env("FACTORY_HOME_OVERRIDE"); d != "" {
		root = d
	}
	return filepath.Join(root, ".factory")
}

func (h *Host) accountRoot(id string) string {
	return filepath.Join(h.cfg.UserData, "accounts", id)
}

func (h *Host) homeOf(id string) string {
	if id == "" {
		return h.SharedFactoryHome()
	}
	return filepath.Join(h.accountRoot(id), ".factory")
}

// ActiveAccount is the id of the account the Daemon runs as, "" for the
// droid CLI's own.
func (h *Host) ActiveAccount() string {
	st := h.Settings.Get()
	if slices.Contains(st.Accounts, st.ActiveAccount) {
		return st.ActiveAccount
	}
	return ""
}

func (h *Host) cli(home string) *CliLoginReader {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := h.readers[home]
	if r == nil {
		r = &CliLoginReader{FactoryHome: home}
		h.readers[home] = r
	}
	return r
}

// Accounts are the droid CLI's account, then the added ones in the order
// they were added.
func (h *Host) Accounts() []AccountInfo {
	active := h.ActiveAccount()
	ids := append([]string{""}, h.Settings.Get().Accounts...)
	out := make([]AccountInfo, 0, len(ids))
	for _, id := range ids {
		info := AccountInfo{ID: id, Active: id == active}
		if l := h.cli(h.homeOf(id)).Read(); l != nil {
			acc := l.Account
			info.Login = &acc
		}
		out = append(out, info)
	}
	return out
}

// SwitchAccount makes the Daemon run as another account: it restarts, and
// the Sessions it was running stop.
func (h *Host) SwitchAccount(id string) error {
	if id == h.ActiveAccount() {
		return nil
	}
	if id != "" && !slices.Contains(h.Settings.Get().Accounts, id) {
		return errNoAccount
	}
	if err := h.Settings.Update(func(s *Settings) { s.ActiveAccount = id }); err != nil {
		return err
	}
	h.changed()
	go h.RestartDaemon()
	return nil
}

// AddAccount starts the device flow for another account; it returns once
// there is a code to enter. The account joins Accounts when the browser is
// done, without being switched to.
func (h *Host) AddAccount(ctx context.Context) (*LoginPending, error) {
	return h.adding.SignIn(ctx)
}

// AddingAccount is where adding an account stands: Pending with the code,
// else SignedOut, with Error when the last try failed.
func (h *Host) AddingAccount() LoginState { return h.adding.State() }

// CancelAddAccount stops adding an account.
func (h *Host) CancelAddAccount() { h.adding.CancelSignIn() }

// addAccount takes the login the device flow got. An account already in
// the list gets the new login; any other gets a folder of its own.
func (h *Host) addAccount(access, refresh string) error {
	claims := DecodeJWT(access)
	email, _ := claims["email"].(string)
	org, _ := claims["org_id"].(string)
	for _, acc := range h.Accounts() {
		if acc.Login == nil || acc.Login.Email == nil || email == "" || !strings.EqualFold(*acc.Login.Email, email) || deref(acc.Login.OrgID) != org {
			continue
		}
		if err := WriteCliLogin(h.homeOf(acc.ID), access, refresh); err != nil {
			return err
		}
		if acc.Active {
			go h.RestartDaemon()
		}
		return nil
	}
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		return err
	}
	id := hex.EncodeToString(b)
	if err := WriteCliLogin(h.homeOf(id), access, refresh); err != nil {
		return err
	}
	return h.Settings.Update(func(s *Settings) { s.Accounts = append(slices.Clone(s.Accounts), id) })
}

// RemoveAccount forgets an added account and deletes its folder; what it
// shared with the droid CLI's account stays.
func (h *Host) RemoveAccount(id string) error {
	if id == "" || id == h.ActiveAccount() {
		return errAccountInUse
	}
	if !slices.Contains(h.Settings.Get().Accounts, id) {
		return errNoAccount
	}
	if err := h.Settings.Update(func(s *Settings) {
		s.Accounts = slices.DeleteFunc(slices.Clone(s.Accounts), func(x string) bool { return x == id })
	}); err != nil {
		return err
	}
	h.mu.Lock()
	delete(h.readers, h.homeOf(id))
	h.mu.Unlock()
	h.changed()
	home := h.homeOf(id)
	// The links go first, so nothing below can reach into what they point at.
	for _, name := range sharedEntries {
		if st, err := os.Lstat(filepath.Join(home, name)); err == nil && st.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			if err := os.Remove(filepath.Join(home, name)); err != nil {
				return err
			}
		}
	}
	return os.RemoveAll(h.accountRoot(id))
}

// shareFactoryHome makes an added account's .factory share the droid CLI's:
// a link for each shared entry, and a copy of AGENTS.md, made again at every
// start so it follows edits. The shared folders are made in the CLI's home
// when missing, so the account never grows its own; an entry the account
// already has of its own is left alone, unless it is an empty folder.
func shareFactoryHome(home, shared string) error {
	if err := os.MkdirAll(home, 0o700); err != nil {
		return err
	}
	for _, name := range sharedEntries {
		target := filepath.Join(shared, name)
		dir := filepath.Ext(name) == ""
		if dir {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		} else if _, err := os.Stat(target); err != nil {
			continue
		}
		at := filepath.Join(home, name)
		if cur, err := os.Readlink(at); err == nil {
			if cur == target {
				continue
			}
			if err := os.Remove(at); err != nil {
				return err
			}
		} else if st, err := os.Lstat(at); err == nil {
			if !st.IsDir() || os.Remove(at) != nil {
				continue
			}
		}
		if err := link(target, at, dir); err != nil {
			return err
		}
	}
	return copyAgentsFile(filepath.Join(shared, "AGENTS.md"), filepath.Join(home, "AGENTS.md"))
}

func copyAgentsFile(from, to string) error {
	b, err := os.ReadFile(from)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.Remove(to); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}
	if cur, err := os.ReadFile(to); err == nil && bytes.Equal(cur, b) {
		return nil
	}
	return writeFileAtomic(to, b)
}
