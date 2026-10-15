//go:build !windows

package host

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSecretToolKeyAsksTheKeyringForDroidsKey(t *testing.T) {
	var name string
	var args []string
	run := func(_ context.Context, n string, a ...string) ([]byte, error) {
		name, args = n, a
		return []byte("c2VjcmV0\n"), nil
	}
	if got := secretToolKey(run, keyringAccount); got != "c2VjcmV0\n" {
		t.Fatalf("got %q", got)
	}
	want := []string{"lookup", "service", "Factory CLI", "account", "auth-encryption-key"}
	if name != "secret-tool" || !reflect.DeepEqual(args, want) {
		t.Fatalf("ran %s %q, want secret-tool %q", name, args, want)
	}
}

func TestSecretToolKeyIsEmptyWithoutSecretToolOrKey(t *testing.T) {
	for name, err := range map[string]error{
		"secret-tool is not installed": errors.New("executable file not found in $PATH"),
		"no keyring or no such key":    errors.New("exit status 1"),
		"the keyring prompt timed out": context.DeadlineExceeded,
	} {
		run := func(context.Context, string, ...string) ([]byte, error) { return []byte("partial"), err }
		if got := secretToolKey(run, keyringAccount); got != "" {
			t.Errorf("%s: got %q", name, got)
		}
	}
}

func TestCliLoginReadsTheKeyringKeyWhenThereIsNoKeyfile(t *testing.T) {
	home := t.TempDir()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	creds := `{"access_token":"a.b.c","refresh_token":"r"}`
	enc, err := Encrypt(creds, key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "auth.v2.keyring"), []byte(enc), 0o600); err != nil {
		t.Fatal(err)
	}
	keyText := base64.StdEncoding.EncodeToString(key)
	run := func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Join(args, " ") != "lookup service Factory CLI account auth-encryption-key" {
			return nil, errors.New("exit status 1")
		}
		return []byte(keyText), nil
	}
	r := &CliLoginReader{FactoryHome: home, ReadSecureKey: func(account string) string { return secretToolKey(run, account) }}
	login := r.Read()
	if login == nil || login.AccessToken != "a.b.c" || login.RefreshToken != "r" {
		t.Fatalf("got %+v", login)
	}
}
