package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	droid "github.com/kkkk2323/droi/packages/droid-sdk-go"
)

const (
	testToken  = "correct-pairing-token"
	localToken = "local-window-token"
	apiKey     = "fk-secret-key"
)

// standInDaemon echoes every frame back as "echo:<frame>".
type standInDaemon struct {
	srv *httptest.Server

	mu       sync.Mutex
	received []string
	sockets  []*websocket.Conn
}

func startStandInDaemon(t *testing.T) *standInDaemon {
	t.Helper()
	d := &standInDaemon{}
	d.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		d.mu.Lock()
		d.sockets = append(d.sockets, c)
		d.mu.Unlock()
		for {
			_, data, err := c.Read(context.Background())
			if err != nil {
				return
			}
			d.mu.Lock()
			d.received = append(d.received, string(data))
			d.mu.Unlock()
			if c.Write(context.Background(), websocket.MessageText, []byte("echo:"+string(data))) != nil {
				return
			}
		}
	}))
	t.Cleanup(func() {
		d.mu.Lock()
		for _, s := range d.sockets {
			_ = s.CloseNow()
		}
		d.mu.Unlock()
		d.srv.Close()
	})
	return d
}

func (d *standInDaemon) url() string { return "ws" + strings.TrimPrefix(d.srv.URL, "http") }

func (d *standInDaemon) frames() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.received...)
}

func (d *standInDaemon) socketCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.sockets)
}

func baseOptions(d *standInDaemon) Options {
	daemonURL := func() string { return "" }
	if d != nil {
		daemonURL = d.url
	}
	return Options{
		DaemonURL:    daemonURL,
		PairingToken: func() string { return testToken },
		LocalToken:   func() string { return localToken },
		Credential: func(context.Context) (*droid.Credential, error) {
			return &droid.Credential{APIKey: apiKey}, nil
		},
		Version:      "1.2.3",
		ComputerName: "Studio Mac",
		ComputerID:   "c0ffee00-0000-4000-8000-000000000000",
	}
}

func startGateway(t *testing.T, opts Options) *Gateway {
	t.Helper()
	g, err := Start(opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = g.Close() })
	return g
}

// The Clients' URL helpers (packages/daemon-layer/src/gateway.ts).
func daemonURL(gatewayURL, token string) string {
	return "ws" + strings.TrimPrefix(gatewayURL, "http") + DaemonPath + "?" + TokenQuery + "=" + url.QueryEscape(token)
}

func pairingCheckURL(gatewayURL, token string) string {
	return gatewayURL + DaemonPath + "?" + TokenQuery + "=" + url.QueryEscape(token)
}

func scratchURL(gatewayURL, token, endpoint, path string) string {
	q := url.Values{TokenQuery: {token}}
	if path != "" {
		q.Set(ScratchPathQuery, path)
	}
	return gatewayURL + endpoint + "?" + q.Encode()
}

func sessionFileURL(gatewayURL, token, sessionID string) string {
	q := url.Values{TokenQuery: {token}, SessionIDQuery: {sessionID}}
	return gatewayURL + SessionFilePath + "?" + q.Encode()
}

// freshClient never reuses a connection, so a closed listener shows at once.
var freshClient = &http.Client{
	Timeout:   3 * time.Second,
	Transport: &http.Transport{DisableKeepAlives: true},
}

func connect(t *testing.T, u string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

func dialError(t *testing.T, u string) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, u, nil)
	if err == nil {
		_ = c.CloseNow()
		t.Fatal("the upgrade succeeded")
	}
	return err
}

func send(t *testing.T, c *websocket.Conn, frame string) {
	t.Helper()
	if err := c.Write(context.Background(), websocket.MessageText, []byte(frame)); err != nil {
		t.Fatal(err)
	}
}

func nextMessage(t *testing.T, c *websocket.Conn) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// waitClosed fails unless the socket closes within a few seconds.
func waitClosed(t *testing.T, c *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		if _, _, err := c.Read(ctx); err != nil {
			if ctx.Err() != nil {
				t.Fatal("the socket stayed open")
			}
			return
		}
	}
}

func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition never held")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func params(t *testing.T, frame string) map[string]any {
	t.Helper()
	var m struct {
		Params map[string]any `json:"params"`
	}
	if err := json.Unmarshal([]byte(frame), &m); err != nil {
		t.Fatalf("%q: %v", frame, err)
	}
	return m.Params
}

func frame(method, id string, p map[string]any) string {
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": p})
	return string(b)
}

func TestRejectsABadTokenAtUpgradeAndForwardsNothing(t *testing.T) {
	d := startStandInDaemon(t)
	g := startGateway(t, baseOptions(d))
	if err := dialError(t, daemonURL(g.URL(), "wrong-token")); !strings.Contains(err.Error(), "401") {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if d.socketCount() != 0 || len(d.frames()) != 0 {
		t.Fatal("the Daemon was reached")
	}
}

func TestRejectsAMissingToken(t *testing.T) {
	g := startGateway(t, baseOptions(startStandInDaemon(t)))
	if err := dialError(t, "ws"+strings.TrimPrefix(g.URL(), "http")+DaemonPath); !strings.Contains(err.Error(), "401") {
		t.Fatal(err)
	}
}

func TestSwapsTheAPIKeyIntoAuthenticate(t *testing.T) {
	d := startStandInDaemon(t)
	g := startGateway(t, baseOptions(d))
	c := connect(t, daemonURL(g.URL(), testToken))
	send(t, c, frame("daemon.authenticate", "1", map[string]any{"apiKey": APIKeyPlaceholder, "caller": "sdk"}))
	echoed := nextMessage(t, c)
	got := d.frames()
	if len(got) != 1 {
		t.Fatal(got)
	}
	var forwarded struct {
		ID     string            `json:"id"`
		Method string            `json:"method"`
		Params map[string]string `json:"params"`
	}
	_ = json.Unmarshal([]byte(got[0]), &forwarded)
	if forwarded.Params["apiKey"] != apiKey || forwarded.Method != "daemon.authenticate" || forwarded.ID != "1" {
		t.Fatal(got[0])
	}
	if echoed != "echo:"+got[0] {
		t.Fatal(echoed)
	}
}

func TestSwapsThePlaceholderSpawnCredential(t *testing.T) {
	d := startStandInDaemon(t)
	g := startGateway(t, baseOptions(d))
	c := connect(t, daemonURL(g.URL(), testToken))
	for _, method := range []string{"daemon.initialize_session", "daemon.load_session"} {
		send(t, c, frame(method, method, map[string]any{"cwd": "/x", "token": APIKeyPlaceholder}))
		nextMessage(t, c)
	}
	for _, f := range d.frames() {
		if params(t, f)["token"] != apiKey {
			t.Fatal(f)
		}
	}
	if n := len(d.frames()); n != 2 {
		t.Fatal(n)
	}
}

func TestAppendsTheSystemPromptAdditionToInitializeSessionOnly(t *testing.T) {
	d := startStandInDaemon(t)
	var mu sync.Mutex
	addition := "Answer in French."
	opts := baseOptions(d)
	opts.LocalToken = nil
	opts.AppendSystemPrompt = func() string {
		mu.Lock()
		defer mu.Unlock()
		return addition
	}
	g := startGateway(t, opts)
	c := connect(t, daemonURL(g.URL(), testToken))
	sendParams := func(method string, p map[string]any) map[string]any {
		send(t, c, frame(method, method, p))
		nextMessage(t, c)
		got := d.frames()
		return params(t, got[len(got)-1])
	}
	initialize := map[string]any{"cwd": "/x", "token": APIKeyPlaceholder}

	want := map[string]any{
		"cwd":          "/x",
		"token":        apiKey,
		"systemPrompt": map[string]any{"type": "preset", "preset": "droid", "append": "Answer in French."},
	}
	if got := sendParams("daemon.initialize_session", initialize); !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	if got := sendParams("daemon.load_session", initialize); got["systemPrompt"] != nil {
		t.Fatal(got)
	}
	// A Client's own choice stands.
	own := map[string]any{"cwd": "/x", "token": APIKeyPlaceholder, "systemPrompt": "Be terse."}
	if got := sendParams("daemon.initialize_session", own); got["systemPrompt"] != "Be terse." {
		t.Fatal(got)
	}
	mu.Lock()
	addition = "  "
	mu.Unlock()
	if got := sendParams("daemon.initialize_session", initialize); got["systemPrompt"] != nil {
		t.Fatal(got)
	}
}

func TestALoginTokenReplacesAPIKeyWithTokenAndKeepsFramesInOrder(t *testing.T) {
	d := startStandInDaemon(t)
	token := make(chan string, 1)
	opts := baseOptions(d)
	opts.LocalToken = nil
	opts.Credential = func(ctx context.Context) (*droid.Credential, error) {
		select {
		case tok := <-token:
			return &droid.Credential{Token: tok}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	g := startGateway(t, opts)
	c := connect(t, daemonURL(g.URL(), testToken))
	send(t, c, frame("daemon.authenticate", "1", map[string]any{"apiKey": APIKeyPlaceholder, "caller": "sdk"}))
	// Sent while the credential is still being fetched; must not overtake it.
	send(t, c, `{"jsonrpc":"2.0","id":"2","method":"daemon.list_available_sessions","params":{}}`)
	time.Sleep(50 * time.Millisecond)
	if n := len(d.frames()); n != 0 {
		t.Fatal(n)
	}
	token <- "eyJ.login.jwt"
	eventually(t, func() bool { return len(d.frames()) == 2 })
	got := d.frames()
	var first, second struct {
		ID     string         `json:"id"`
		Method string         `json:"method"`
		Params map[string]any `json:"params"`
	}
	_ = json.Unmarshal([]byte(got[0]), &first)
	_ = json.Unmarshal([]byte(got[1]), &second)
	if first.Method != "daemon.authenticate" || !reflect.DeepEqual(first.Params, map[string]any{"token": "eyJ.login.jwt", "caller": "sdk"}) {
		t.Fatal(got[0])
	}
	if second.ID != "2" {
		t.Fatal(got[1])
	}
}

func TestForwardsEveryOtherFrameByteIdenticalInBothDirections(t *testing.T) {
	d := startStandInDaemon(t)
	g := startGateway(t, baseOptions(d))
	c := connect(t, daemonURL(g.URL(), testToken))
	frames := []string{
		frame("daemon.list_available_sessions", "2", map[string]any{}),
		`{"jsonrpc":"2.0",  "id":"3","method":"x","params":{"apiKey":"not-an-auth-frame"}}`,
		`{"jsonrpc":"2.0","id":"4","method":"daemon.add_user_message","params":{"text":"say droi-gateway please"}}`,
		"not even json",
	}
	for _, f := range frames {
		send(t, c, f)
		if got := nextMessage(t, c); got != "echo:"+f {
			t.Fatal(got)
		}
	}
	if got := d.frames(); !reflect.DeepEqual(got, frames) {
		t.Fatal(got)
	}
}

func TestClosingTheDaemonSideClosesTheClientSide(t *testing.T) {
	d := startStandInDaemon(t)
	g := startGateway(t, baseOptions(d))
	c := connect(t, daemonURL(g.URL(), testToken))
	send(t, c, "ping")
	nextMessage(t, c)
	d.mu.Lock()
	daemonSide := d.sockets[0]
	d.mu.Unlock()
	go func() { _ = daemonSide.Close(websocket.StatusNormalClosure, "") }()
	waitClosed(t, c)
}

func TestMetaDescribesTheShellWithoutSecrets(t *testing.T) {
	g := startGateway(t, baseOptions(startStandInDaemon(t)))
	resp, err := http.Get(g.URL() + MetaPath)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		t.Fatal(resp.StatusCode)
	}
	var got map[string]any
	_ = json.Unmarshal(body, &got)
	want := map[string]any{
		"app":          "Droi",
		"version":      "1.2.3",
		"remoteAccess": false,
		"name":         "Studio Mac",
		"computerId":   "c0ffee00-0000-4000-8000-000000000000",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(string(body))
	}
	if strings.Contains(string(body), apiKey) || strings.Contains(string(body), testToken) {
		t.Fatal("secret in /meta")
	}
}

func TestPairingCheckAnswers204ForTheRightTokenAnd401Otherwise(t *testing.T) {
	g := startGateway(t, baseOptions(startStandInDaemon(t)))
	for token, want := range map[string]int{testToken: 204, "nope": 401} {
		resp, err := http.Get(pairingCheckURL(g.URL(), token))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want || resp.Header.Get("Access-Control-Allow-Origin") != "*" {
			t.Fatalf("%s: %d", token, resp.StatusCode)
		}
	}
}

func TestAcceptsTheLocalTokenAndRevokingPairingSparesTheLocalBridge(t *testing.T) {
	g := startGateway(t, baseOptions(startStandInDaemon(t)))
	local := connect(t, daemonURL(g.URL(), localToken))
	remote := connect(t, daemonURL(g.URL(), testToken))
	send(t, local, "a")
	send(t, remote, "b")
	nextMessage(t, local)
	nextMessage(t, remote)

	g.Revoke(TokenPairing)
	waitClosed(t, remote)
	send(t, local, "still here")
	if got := nextMessage(t, local); got != "echo:still here" {
		t.Fatal(got)
	}
}

func TestRemoteAccessBindsLANAddressesOnDemandAndNeverTouchesTheLocalBridge(t *testing.T) {
	g := startGateway(t, baseOptions(startStandInDaemon(t)))
	lan := LANInterfaceAddresses()
	local := connect(t, daemonURL(g.URL(), localToken))
	if g.RemoteAccess() {
		t.Fatal("Remote Access on from the start")
	}
	lanURL := ""
	if len(lan) > 0 {
		lanURL = "http://" + net.JoinHostPort(lan[0], strconv.Itoa(g.Port()))
		if resp, err := freshClient.Get(lanURL + MetaPath); err == nil {
			resp.Body.Close()
			t.Fatal("LAN address answered while Remote Access is off")
		}
	}

	g.SetRemoteAccess(true)
	if got := g.LANAddresses(); !reflect.DeepEqual(got, nonNil(lan)) {
		t.Fatalf("%v != %v", got, lan)
	}
	if lanURL != "" {
		resp, err := freshClient.Get(lanURL + MetaPath)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatal(resp.StatusCode)
		}
		phone := connect(t, daemonURL(lanURL, testToken))
		send(t, phone, "from phone")
		if got := nextMessage(t, phone); got != "echo:from phone" {
			t.Fatal(got)
		}
		g.SetRemoteAccess(false)
		waitClosed(t, phone)
		if resp, err := freshClient.Get(lanURL + MetaPath); err == nil {
			resp.Body.Close()
			t.Fatal("LAN address still answers")
		}
	} else {
		g.SetRemoteAccess(false)
	}
	if g.RemoteAccess() {
		t.Fatal("Remote Access still on")
	}
	send(t, local, "still local")
	if got := nextMessage(t, local); got != "echo:still local" {
		t.Fatal(got)
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func TestBindsLoopbackOnlyWhileRemoteAccessIsOff(t *testing.T) {
	g := startGateway(t, baseOptions(startStandInDaemon(t)))
	if g.RemoteAccess() || len(g.LANAddresses()) != 0 {
		t.Fatal(g.LANAddresses())
	}
}

func TestFailsTheUpgradeWith503WhenNoDaemonIsRunning(t *testing.T) {
	g := startGateway(t, baseOptions(nil))
	if err := dialError(t, daemonURL(g.URL(), testToken)); !strings.Contains(err.Error(), "503") {
		t.Fatal(err)
	}
}

func TestBindsEveryLANAddressFromTheStartWithRemoteAccessOn(t *testing.T) {
	opts := baseOptions(startStandInDaemon(t))
	opts.RemoteAccess = true
	g := startGateway(t, opts)
	if got := g.LANAddresses(); !reflect.DeepEqual(got, nonNil(LANInterfaceAddresses())) {
		t.Fatal(got)
	}
}

func TestServesNoWebClient(t *testing.T) {
	g := startGateway(t, baseOptions(startStandInDaemon(t)))
	resp, err := http.Get(g.URL() + "/")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatal(resp.StatusCode)
	}
	if err := dialError(t, "ws"+strings.TrimPrefix(g.URL(), "http")+"/other?token="+testToken); !strings.Contains(err.Error(), "404") {
		t.Fatal(err)
	}
}

func TestFallsBackToAnEphemeralPortWhenThePreferredOneIsTaken(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	opts := baseOptions(nil)
	opts.Port = taken.Addr().(*net.TCPAddr).Port
	g := startGateway(t, opts)
	if g.Port() == opts.Port || g.Port() == 0 {
		t.Fatal(g.Port())
	}
	if g.URL() != "http://127.0.0.1:"+strconv.Itoa(g.Port()) {
		t.Fatal(g.URL())
	}
}

// fakeScratch records calls; fail makes every call fail.
type fakeScratch struct {
	mu    sync.Mutex
	calls []string
	fail  error
}

const scratchFolder = "/Users/me/.droi/chats/2026-09-26-abcdef"

func (s *fakeScratch) record(call string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail != nil {
		return s.fail
	}
	s.calls = append(s.calls, call)
	return nil
}

func (s *fakeScratch) Create() (string, error) {
	if err := s.record("create"); err != nil {
		return "", err
	}
	return scratchFolder, nil
}
func (s *fakeScratch) Trash(path string) error   { return s.record("trash " + path) }
func (s *fakeScratch) Restore(path string) error { return s.record("restore " + path) }

func scratchGateway(t *testing.T) (*Gateway, *fakeScratch) {
	scratch := &fakeScratch{}
	// No Daemon: these requests never reach it.
	opts := baseOptions(nil)
	opts.Scratch = scratch
	return startGateway(t, opts), scratch
}

func post(t *testing.T, u string) (*http.Response, string) {
	t.Helper()
	resp, err := http.Post(u, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp, string(body)
}

func TestScratchCreatesAFolderForEitherTokenAndAnswersItsPath(t *testing.T) {
	g, scratch := scratchGateway(t)
	for _, token := range []string{testToken, localToken} {
		resp, body := post(t, scratchURL(g.URL(), token, ScratchPath, ""))
		if resp.StatusCode != 201 || resp.Header.Get("Access-Control-Allow-Origin") != "*" {
			t.Fatal(resp.StatusCode)
		}
		if body != `{"path":"`+scratchFolder+`"}` {
			t.Fatal(body)
		}
	}
	if !reflect.DeepEqual(scratch.calls, []string{"create", "create"}) {
		t.Fatal(scratch.calls)
	}
}

func TestScratchTrashesAndRestoresTheFolderNamedInTheQuery(t *testing.T) {
	g, scratch := scratchGateway(t)
	for _, endpoint := range []string{ScratchTrashPath, ScratchRestorePath} {
		if resp, _ := post(t, scratchURL(g.URL(), testToken, endpoint, scratchFolder)); resp.StatusCode != 204 {
			t.Fatal(endpoint, resp.StatusCode)
		}
	}
	if !reflect.DeepEqual(scratch.calls, []string{"trash " + scratchFolder, "restore " + scratchFolder}) {
		t.Fatal(scratch.calls)
	}
}

func TestScratchRefusesAWrongTokenAGetAndAMissingPath(t *testing.T) {
	g, scratch := scratchGateway(t)
	if resp, _ := post(t, scratchURL(g.URL(), "nope", ScratchPath, "")); resp.StatusCode != 401 {
		t.Fatal(resp.StatusCode)
	}
	resp, err := http.Get(scratchURL(g.URL(), testToken, ScratchPath, ""))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Fatal(resp.StatusCode)
	}
	if resp, _ := post(t, scratchURL(g.URL(), testToken, ScratchTrashPath, "")); resp.StatusCode != 400 {
		t.Fatal(resp.StatusCode)
	}
	if len(scratch.calls) != 0 {
		t.Fatal(scratch.calls)
	}
}

func TestScratchRefusedFolderAnswers400WithTheReasonAnyOtherFailure500(t *testing.T) {
	g, scratch := scratchGateway(t)
	scratch.fail = &NotAScratchWorkspace{Path: "/etc"}
	resp, body := post(t, scratchURL(g.URL(), testToken, ScratchTrashPath, "/etc"))
	if resp.StatusCode != 400 || body != `{"error":"/etc is not a Scratch Workspace"}` {
		t.Fatal(resp.StatusCode, body)
	}
	scratch.fail = errors.New("EACCES")
	if resp, _ := post(t, scratchURL(g.URL(), testToken, ScratchPath, "")); resp.StatusCode != 500 {
		t.Fatal(resp.StatusCode)
	}
}

func TestScratchWithoutFoldersIs404(t *testing.T) {
	g := startGateway(t, baseOptions(nil))
	if resp, _ := post(t, scratchURL(g.URL(), testToken, ScratchPath, "")); resp.StatusCode != 404 {
		t.Fatal(resp.StatusCode)
	}
}

const knownTranscript = "/Users/me/.factory/sessions/-Users-me-app/known.jsonl"

func sessionFileGateway(t *testing.T) (*Gateway, *[]string) {
	var mu sync.Mutex
	asked := []string{}
	opts := baseOptions(nil)
	opts.FindSessionFile = func(id string) string {
		mu.Lock()
		asked = append(asked, id)
		mu.Unlock()
		if id == "known" {
			return knownTranscript
		}
		return ""
	}
	return startGateway(t, opts), &asked
}

func TestSessionFileAnswersTheTranscriptPathForEitherToken(t *testing.T) {
	g, _ := sessionFileGateway(t)
	for _, token := range []string{testToken, localToken} {
		resp, err := http.Get(sessionFileURL(g.URL(), token, "known"))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || resp.Header.Get("Access-Control-Allow-Origin") != "*" {
			t.Fatal(resp.StatusCode)
		}
		if string(body) != `{"path":"`+knownTranscript+`"}` {
			t.Fatal(string(body))
		}
	}
}

func TestSessionFileOfASessionWithoutOneIs404(t *testing.T) {
	g, _ := sessionFileGateway(t)
	resp, err := http.Get(sessionFileURL(g.URL(), testToken, "gone"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatal(resp.StatusCode)
	}
}

func TestSessionFileRefusesAWrongTokenAndAPostWithoutLooking(t *testing.T) {
	g, asked := sessionFileGateway(t)
	resp, err := http.Get(sessionFileURL(g.URL(), "nope", "known"))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatal(resp.StatusCode)
	}
	if resp, _ := post(t, sessionFileURL(g.URL(), testToken, "known")); resp.StatusCode != 405 {
		t.Fatal(resp.StatusCode)
	}
	if len(*asked) != 0 {
		t.Fatal(*asked)
	}
}

func TestCloseEndsEveryBridge(t *testing.T) {
	g, err := Start(baseOptions(startStandInDaemon(t)))
	if err != nil {
		t.Fatal(err)
	}
	local := connect(t, daemonURL(g.URL(), localToken))
	remote := connect(t, daemonURL(g.URL(), testToken))
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	waitClosed(t, local)
	waitClosed(t, remote)
	if _, err := freshClient.Get(g.URL() + MetaPath); err == nil {
		t.Fatal("still listening")
	}
}
