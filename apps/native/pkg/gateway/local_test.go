package gateway

import "testing"

func TestLocalTokensAreFreshAndLongEnoughToGuessNoneOfThem(t *testing.T) {
	seen := map[string]bool{}
	for range 20 {
		tok := NewLocalToken()
		if len(tok) < 32 || seen[tok] {
			t.Fatalf("token %q is short or repeated", tok)
		}
		seen[tok] = true
	}
}

func TestALocalClientReachesTheDaemonSocketWithItsToken(t *testing.T) {
	g := startGateway(t, baseOptions(startStandInDaemon(t)))

	conn := connect(t, g.DaemonSocketURL(localToken))
	send(t, conn, "hello")
	if got := nextMessage(t, conn); got != "echo:hello" {
		t.Fatal(got)
	}
}

func TestTheLocalClientsPlaceholderIsTheGatewaysOwn(t *testing.T) {
	cred, err := PlaceholderCredential(t.Context())
	if err != nil || cred == nil || cred.APIKey != APIKeyPlaceholder || cred.Token != "" {
		t.Fatalf("%+v, %v", cred, err)
	}
}
