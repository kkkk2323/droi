package relay

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTheInstallCommandNamesTheDomain(t *testing.T) {
	for in, want := range map[string]string{
		" Relay.Example.org ": "--domain relay.example.org",
		"":                    "--domain relay.example.com",
		"x; rm -rf /":         "--domain relay.example.com",
		"localhost":           "--domain relay.example.com",
	} {
		got := InstallCommand(in)
		if !strings.HasPrefix(got, "curl -fsSL https://github.com/kkkk2323/droi/releases/latest/download/install-relay.sh | sudo sh -s -- ") || !strings.HasSuffix(got, want) {
			t.Errorf("%q: %q", in, got)
		}
	}
}

func TestTheRelayAddressCarriesTheKeyInItsFragment(t *testing.T) {
	if got := Address("https://relay.example.com/", "Zk3-x_9"); got != "https://relay.example.com/#key=Zk3-x_9" {
		t.Fatal(got)
	}
	if got := (config{Domain: "Relay.Example.com"}).public(); got != "https://relay.example.com" {
		t.Fatal(got)
	}
	if got := (config{Listen: "127.0.0.1:8787", PublicURL: "https://example.com/relay/"}).public(); got != "https://example.com/relay" {
		t.Fatal(got)
	}
}

func TestInstallNeedsADomainOrAProxyAddress(t *testing.T) {
	for _, c := range []config{
		{},
		{Listen: "127.0.0.1:8787"},
		{Domain: "relay.example.com", Listen: ":8787"},
		{Domain: "not a domain"},
		{Listen: "127.0.0.1:8787", PublicURL: "relay.example.com"},
		{Listen: "127.0.0.1:8787", PublicURL: "https://relay.example.com/#x"},
	} {
		if c.checkInstall() == nil {
			t.Errorf("%+v was accepted", c)
		}
	}
	for _, c := range []config{
		{Domain: "relay.example.com"},
		{Listen: "127.0.0.1:8787", PublicURL: "https://relay.example.com"},
	} {
		if err := c.checkInstall(); err != nil {
			t.Errorf("%+v: %v", c, err)
		}
	}
	if err := (config{Listen: ":8787"}).checkServe(); err != nil {
		t.Errorf("serve on a port alone: %v", err)
	}
}

func TestTheKeyIsMadeOnceAndKept(t *testing.T) {
	t.Setenv("DROI_RELAY_KEY", "")
	dir := filepath.Join(t.TempDir(), "relay")
	if _, err := loadKey(dir, false); err == nil || !strings.Contains(err.Error(), "sudo droi-relay install") {
		t.Fatalf("no relay yet: %v", err)
	}
	first, err := loadKey(dir, true)
	if err != nil || len(first) < 32 {
		t.Fatalf("%q %v", first, err)
	}
	if fi, err := os.Stat(filepath.Join(dir, keyName)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the key file: %v %v", fi.Mode(), err)
	}
	// Installing again, as an upgrade does, keeps the key computers have.
	if again, _ := loadKey(dir, true); again != first {
		t.Fatalf("the key changed: %q → %q", first, again)
	}
	reset, _ := newKey(dir)
	if again, _ := loadKey(dir, false); again != reset || reset == first {
		t.Fatal("reset-key did not make a new key")
	}
	t.Setenv("DROI_RELAY_KEY", "from-env")
	if k, _ := loadKey(dir, false); k != "from-env" {
		t.Fatal(k)
	}
}

func TestTheServiceRunsServeWithItsDirectory(t *testing.T) {
	u := unit("/usr/local/bin/droi-relay", "/var/lib/droi-relay")
	for _, want := range []string{"ExecStart=/usr/local/bin/droi-relay serve --dir /var/lib/droi-relay\n", "ReadWritePaths=/var/lib/droi-relay\n", "Restart=on-failure\n", "WantedBy=multi-user.target\n"} {
		if !strings.Contains(u, want) {
			t.Errorf("no %q in\n%s", want, u)
		}
	}
}

func TestMainNeedsACommand(t *testing.T) {
	for args, want := range map[string]int{"": 2, "nope": 2, "help": 0, "serve --tls-cert x": 2, "install --bogus": 2} {
		if got := Main(strings.Fields(args), "test"); got != want {
			t.Errorf("%q: %d, want %d", args, got, want)
		}
	}
}
