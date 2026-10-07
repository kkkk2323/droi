package host

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Account is who a Factory login belongs to.
type Account struct {
	UserID string  `json:"userId"`
	OrgID  *string `json:"orgId"`
	Email  *string `json:"email"`
}

// CliLogin is the droid CLI's own login, read from its credential store
// under ~/.factory, so the Host needs no sign-in of its own: the Daemon runs
// as this login, so its access token is what daemon.authenticate wants.
// The token is never refreshed here: the CLI rotates refresh tokens, so
// doing it from a second process would log the CLI out.
type CliLogin struct {
	AccessToken string
	Account     Account
}

const (
	keychainService    = "Factory CLI"
	securityCliAccount = "auth-encryption-key-security-cli"
	keyringAccount     = "auth-encryption-key"
)

// CliLoginReader reads the CLI's login, again only when its files change.
type CliLoginReader struct {
	FactoryHome string
	// ReadSecureKey reads a key from the system's key store; nil uses
	// /usr/bin/security on macOS and the Credential Manager on Windows.
	ReadSecureKey func(account string) string

	mu       sync.Mutex
	keys     map[string][]byte
	identity string
	cached   *CliLogin
	read     bool
}

func (r *CliLoginReader) secureKey(account string) []byte {
	if k, ok := r.keys[account]; ok {
		return k
	}
	if r.keys == nil {
		r.keys = map[string][]byte{}
	}
	readKey := r.ReadSecureKey
	if readKey == nil {
		readKey = securityKey
	}
	var key []byte
	if enc := strings.TrimSpace(readKey(account)); enc != "" {
		key, _ = base64.StdEncoding.DecodeString(enc)
	}
	if len(key) != 32 {
		key = nil
	}
	// A missing key is remembered too: asking again would only spawn processes.
	r.keys[account] = key
	return key
}

// Read returns the CLI's current login, or nil when it is logged out or
// unreadable.
func (r *CliLoginReader) Read() *CliLogin {
	r.mu.Lock()
	defer r.mu.Unlock()
	type source struct {
		file string
		key  func() []byte
	}
	sources := []source{
		{"auth.v2.loginkeychain", func() []byte { return r.secureKey(securityCliAccount) }},
		{"auth.v2.keyring", func() []byte { return r.secureKey(keyringAccount) }},
		{authKeyfileFile, func() []byte { return readKeyFile(filepath.Join(r.FactoryHome, authKeyFile)) }},
	}
	var ids []string
	for _, s := range sources {
		ids = append(ids, fileIdentity(filepath.Join(r.FactoryHome, s.file)))
	}
	identity := strings.Join(ids, "|")
	if r.read && identity == r.identity {
		return r.cached
	}
	var login *CliLogin
	for _, s := range sources {
		b, err := os.ReadFile(filepath.Join(r.FactoryHome, s.file))
		if err != nil || len(b) == 0 {
			continue
		}
		key := s.key()
		if key == nil {
			continue
		}
		if login = parseCredentials(Decrypt(string(b), key), r.FactoryHome); login != nil {
			break
		}
	}
	r.identity, r.cached, r.read = identity, login, true
	return login
}

// The droid CLI's credential files; the keyfile backend's key sits in authKeyFile.
const (
	authKeyfileFile = "auth.v2.file"
	authKeyFile     = "auth.v2.key"
)

// authSecureFiles are the ciphertexts under a key in the system's key
// store, which droid prefers to the keyfile's.
var authSecureFiles = []string{"auth.v2.keyring", "auth.v2.loginkeychain"}

// WriteCliLogin gives a login to the droid CLI, as Factory's own desktop
// app does: the Daemon and the CLI then run as it and refresh it, and Droi
// reads it back like any CLI login. It goes into the keyfile backend, the
// one that needs no key store; the secure ciphertexts, an earlier login
// droid would read first, are removed.
func WriteCliLogin(factoryHome, accessToken, refreshToken string) error {
	if err := os.MkdirAll(factoryHome, 0o700); err != nil {
		return err
	}
	keyPath := filepath.Join(factoryHome, authKeyFile)
	key := readKeyFile(keyPath)
	if key == nil {
		key = make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return err
		}
		if err := writeFileAtomic(keyPath, []byte(base64.StdEncoding.EncodeToString(key))); err != nil {
			return err
		}
	}
	creds := map[string]any{"access_token": accessToken, "refresh_token": refreshToken}
	// The WorkOS organization, which droid refreshes the token in.
	if org, ok := DecodeJWT(accessToken)["org_id"].(string); ok && org != "" {
		creds["active_organization_id"] = org
	}
	plain, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	enc, err := Encrypt(string(plain), key)
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(factoryHome, authKeyfileFile), []byte(enc)); err != nil {
		return err
	}
	for _, f := range authSecureFiles {
		if err := os.Remove(filepath.Join(factoryHome, f)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// RemoveCliLogin signs the droid CLI out: its credential ciphertexts go,
// their keys stay, as `droid` itself leaves them.
func RemoveCliLogin(factoryHome string) error {
	for _, f := range append([]string{authKeyfileFile}, authSecureFiles...) {
		if err := os.Remove(filepath.Join(factoryHome, f)); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}

// writeFileAtomic replaces path with data, readable by the user only.
func writeFileAtomic(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Encrypt seals text in the CLI's `iv:tag:ciphertext` AES-256-GCM format.
func Encrypt(text string, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, 16)
	if err != nil {
		return "", err
	}
	iv := make([]byte, 16)
	if _, err := rand.Read(iv); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, iv, []byte(text), nil)
	ct, tag := sealed[:len(sealed)-gcm.Overhead()], sealed[len(sealed)-gcm.Overhead():]
	b := base64.StdEncoding.EncodeToString
	return b(iv) + ":" + b(tag) + ":" + b(ct), nil
}

// Decrypt opens the CLI's `iv:tag:ciphertext` AES-256-GCM format.
func Decrypt(encrypted string, key []byte) string {
	parts := strings.Split(strings.TrimSpace(encrypted), ":")
	if len(parts) != 3 {
		return ""
	}
	var raw [3][]byte
	for i, p := range parts {
		b, err := base64.StdEncoding.DecodeString(p)
		if err != nil {
			return ""
		}
		raw[i] = b
	}
	iv, tag, ct := raw[0], raw[1], raw[2]
	if len(iv) != 16 || len(tag) != 16 {
		return ""
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return ""
	}
	gcm, err := cipher.NewGCMWithNonceSize(block, 16)
	if err != nil {
		return ""
	}
	plain, err := gcm.Open(nil, iv, append(append([]byte{}, ct...), tag...), nil)
	if err != nil {
		return ""
	}
	return string(plain)
}

func parseCredentials(text, factoryHome string) *CliLogin {
	if text == "" {
		return nil
	}
	var v struct {
		AccessToken string  `json:"access_token"`
		ActiveOrgID *string `json:"active_organization_id"`
	}
	if json.Unmarshal([]byte(text), &v) != nil || v.AccessToken == "" {
		return nil
	}
	claims := DecodeJWT(v.AccessToken)
	acc := Account{UserID: "unknown", OrgID: v.ActiveOrgID}
	if sub, ok := claims["sub"].(string); ok {
		acc.UserID = sub
	}
	if email, ok := claims["email"].(string); ok {
		acc.Email = &email
	}
	// The Daemon compares Factory's own ids, which the CLI records in
	// host.json; the JWT only carries the WorkOS ids.
	if reg := ReadRegistration(factoryHome); reg != nil {
		acc.UserID = reg.UserID
		if acc.OrgID == nil {
			acc.OrgID = reg.OrgID
		}
	}
	return &CliLogin{AccessToken: v.AccessToken, Account: acc}
}

// Registration is who the CLI registered this computer as.
type Registration struct {
	UserID string  `json:"userId"`
	OrgID  *string `json:"orgId"`
}

// ReadRegistration reads ~/.factory/host.json.
func ReadRegistration(factoryHome string) *Registration {
	b, err := os.ReadFile(filepath.Join(factoryHome, "host.json"))
	if err != nil {
		return nil
	}
	var v struct {
		ComputerRegistration *struct {
			UserID         *string `json:"userId"`
			FirestoreOrgID *string `json:"firestoreOrgId"`
		} `json:"computerRegistration"`
	}
	if json.Unmarshal(b, &v) != nil || v.ComputerRegistration == nil || v.ComputerRegistration.UserID == nil {
		return nil
	}
	return &Registration{UserID: *v.ComputerRegistration.UserID, OrgID: v.ComputerRegistration.FirestoreOrgID}
}

func readKeyFile(path string) []byte {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(b)))
	if err != nil || len(key) != 32 {
		return nil
	}
	return key
}

func fileIdentity(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return "-"
	}
	return fmt.Sprintf("%d:%d:%d", inode(st), st.ModTime().UnixNano(), st.Size())
}

// DecodeJWT returns a JWT's claims, unverified.
func DecodeJWT(token string) map[string]any {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return map[string]any{}
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return map[string]any{}
	}
	var claims map[string]any
	if json.Unmarshal(b, &claims) != nil {
		return map[string]any{}
	}
	return claims
}

// JWTExpiry is when a JWT expires; zero when it says not.
func JWTExpiry(token string) time.Time {
	if exp, ok := DecodeJWT(token)["exp"].(float64); ok {
		return time.Unix(int64(exp), 0)
	}
	return time.Time{}
}
