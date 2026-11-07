package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

// fixturesDir is the contract's samples, which Kernel's tests parse too.
const fixturesDir = "../../../kernel/protocol/fixtures"

// fixture is one file of the contract's samples.
type fixture struct {
	Kind         string          `json:"kind"`
	Method       string          `json:"method"`
	Request      json.RawMessage `json:"request"`
	Response     json.RawMessage `json:"response"`
	Notification json.RawMessage `json:"notification"`
	Error        *struct {
		Code int    `json:"code"`
		Name string `json:"name"`
	} `json:"error"`
}

type fixtureIndex struct {
	Methods       []string `json:"methods"`
	Notifications []string `json:"notifications"`
	Errors        []string `json:"errors"`
}

func readFixture(t *testing.T, name string) fixture {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixturesDir, name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	var f fixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	return f
}

func readIndex(t *testing.T) fixtureIndex {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(fixturesDir, "index.json"))
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}
	var idx fixtureIndex
	if err := json.Unmarshal(b, &idx); err != nil {
		t.Fatalf("index.json: %v", err)
	}
	return idx
}

// methodParams is the params type of a method: a pointer to the struct
// conversation.create takes, or nil for the methods without params.
var methodParams = map[string]any{
	MethodHarnessInfo:         nil,
	MethodConversationCreate:  &CreateParams{},
	MethodConversationList:    &ListParams{},
	MethodConversationOpen:    &OpenParams{},
	MethodConversationUnwatch: &IDParams{},
	MethodConversationSubmit:  &SubmitParams{},
	MethodConversationAbort:   &AbortParams{},
	MethodSubmissionAbort:     &AbortSubmissionParams{},
	MethodSubmissionGet:       &AbortSubmissionParams{},
	MethodConversationConfig:  &ConfigureParams{},
	MethodConversationCompact: &CompactParams{},
	MethodConversationFork:    &ForkParams{},
	MethodConversationImport:  &ImportParams{},
	MethodConversationEntries: &EntriesParams{},
	MethodConversationUsage:   &UsageParams{},
	MethodTaskAbort:           &AbortTaskParams{},
	MethodIndexUpdate:         &IndexUpdateParams{},
	MethodProcessList:         &ProcessesParams{},
	MethodProcessStop:         &StopProcessParams{},
	MethodApprovalRespond:     &ApprovalRespondParams{},
	MethodAskRespond:          &AskRespondParams{},
	MethodMCPList:             nil,
	MethodMCPSet:              &MCPSetParams{},
	MethodMCPAuthorize:        &MCPAuthorizeParams{},
	MethodMCPAuthorizeCancel:  &MCPAuthorizeParams{},
	MethodSkillsList:          nil,
	MethodSkillsSetDisabled:   &SkillsSetDisabledParams{},
	MethodModelComplete:       &ModelCompleteParams{},
	// kernel.shutdown takes no params.
	MethodKernelShutdown: nil,
}

// methodResults is the result type of a method. An empty result is struct{}.
var methodResults = map[string]any{
	MethodHarnessInfo:         &HarnessInfo{},
	MethodConversationCreate:  &CreateResult{},
	MethodConversationList:    &ListResult{},
	MethodConversationOpen:    &OpenResult{},
	MethodConversationUnwatch: &struct{}{},
	MethodConversationSubmit:  &SubmitResult{},
	MethodConversationAbort:   &struct{}{},
	MethodSubmissionAbort:     &Status{},
	MethodSubmissionGet:       &SubmissionGet{},
	MethodConversationConfig:  &ConfigureResult{},
	MethodConversationCompact: &CompactResult{},
	MethodConversationFork:    &ForkResult{},
	MethodConversationImport:  &ImportResult{},
	MethodConversationEntries: &EntriesResult{},
	MethodConversationUsage:   &Usage{},
	MethodTaskAbort:           &Status{},
	MethodIndexUpdate:         &struct{}{},
	MethodProcessList:         &ProcessesResult{},
	MethodProcessStop:         &Status{},
	MethodApprovalRespond:     &Status{},
	MethodAskRespond:          &Status{},
	MethodMCPList:             &MCPServers{},
	MethodMCPSet:              &MCPServers{},
	MethodMCPAuthorize:        &MCPAuthorizeResult{},
	MethodMCPAuthorizeCancel:  &struct{}{},
	MethodSkillsList:          &Skills{},
	MethodSkillsSetDisabled:   &Skills{},
	MethodModelComplete:       &ModelCompleteResult{},
	MethodKernelShutdown:      &ShutdownResult{},
}

// notificationTypes is the type conversation.view's and the rest's params
// decode into.
var notificationTypes = map[string]func() any{
	NotifyConversationView:  func() any { return &ViewNotification{} },
	NotifyConversationBusy:  func() any { return &BusyNotification{} },
	NotifyApprovalRequested: func() any { return &ApprovalRequested{} },
	NotifyApprovalResolved:  func() any { return &ApprovalResolved{} },
	NotifyAskRequested:      func() any { return &AskRequested{} },
	NotifyAskResolved:       func() any { return &AskResolved{} },
	NotifyTaskChanged:       func() any { return &TaskChanged{} },
	NotifyMCPChanged:        func() any { return &MCPChanged{} },
	NotifyProcessChanged:    func() any { return &ProcessChanged{} },
	NotifyIndexChanged:      func() any { return &IndexChanged{} },
	NotifyWarning:           func() any { return &Warning{} },
}

// The contract's samples decode into this package's types without losing a
// field: a name Kernel changes, or one this client never modelled, fails here.
func TestFixturesDecodeIntoTheClientTypes(t *testing.T) {
	idx := readIndex(t)
	seen := map[string]bool{}
	for _, name := range idx.Methods {
		f := readFixture(t, name)
		method := f.Method
		seen[method] = true
		t.Run(method, func(t *testing.T) {
			checkMethodFixture(t, method, f)
		})
	}
	for _, method := range Methods {
		// Every method this client has must have a sample: a method without
		// one is a method no test exercises against Kernel's own shape.
		if !seen[method] {
			t.Errorf("the fixtures have no sample of %s", method)
		}
	}
}

func checkMethodFixture(t *testing.T, method string, f fixture) {
	t.Helper()
	if _, ok := methodParams[method]; !ok {
		t.Fatalf("no params type for %s", method)
	}
	var request rpcFrame
	if err := json.Unmarshal(f.Request, &request); err != nil {
		t.Fatalf("request: %v", err)
	}
	if request.Method != f.Method {
		t.Fatalf("the request names %s, the fixture %s", request.Method, f.Method)
	}
	if shape := methodParams[method]; shape != nil {
		if len(request.Params) == 0 {
			t.Fatalf("%s has a params type, the sample sends none", method)
		}
		params := reflect.New(reflect.TypeOf(shape).Elem()).Interface()
		if err := json.Unmarshal(request.Params, params); err != nil {
			t.Fatalf("params: %v", err)
		}
		encoded, err := json.Marshal(params)
		if err != nil {
			t.Fatalf("params: %v", err)
		}
		assertSameJSON(t, "params", request.Params, encoded)
	} else if len(request.Params) > 0 {
		t.Fatalf("%s takes no params, the sample sends %s", method, request.Params)
	}

	var response rpcFrame
	if err := json.Unmarshal(f.Response, &response); err != nil {
		t.Fatalf("response: %v", err)
	}
	if response.Error != nil {
		t.Fatalf("a method sample carries an error: %+v", response.Error)
	}
	result := reflect.New(reflect.TypeOf(methodResults[method]).Elem()).Interface()
	if len(response.Result) > 0 {
		if err := json.Unmarshal(response.Result, result); err != nil {
			t.Fatalf("result: %v", err)
		}
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("result: %v", err)
	}
	assertSameJSON(t, "result", response.Result, encoded)
}

// The contract's notifications decode into this package's types the same way,
// so the window reads what Kernel sends.
func TestNotificationFixturesDecodeIntoTheClientTypes(t *testing.T) {
	idx := readIndex(t)
	seen := map[string]bool{}
	for _, name := range idx.Notifications {
		f := readFixture(t, name)
		seen[f.Method] = true
		t.Run(strings.TrimSuffix(filepath.Base(name), ".json"), func(t *testing.T) {
			if _, ok := notificationTypes[f.Method]; !ok {
				t.Fatalf("no type for %s", f.Method)
			}
			var frame rpcFrame
			if err := json.Unmarshal(f.Notification, &frame); err != nil {
				t.Fatalf("notification: %v", err)
			}
			if frame.Method != f.Method {
				t.Fatalf("the sample names %s, the fixture %s", frame.Method, f.Method)
			}
			if len(frame.ID) > 0 {
				t.Fatalf("a notification carries an id: %s", frame.ID)
			}
			params := notificationTypes[f.Method]()
			var n Notification
			if err := json.Unmarshal(f.Notification, &n); err != nil {
				t.Fatalf("notification: %v", err)
			}
			if err := json.Unmarshal(n.Params, params); err != nil {
				t.Fatalf("params: %v", err)
			}
			encoded, err := json.Marshal(params)
			if err != nil {
				t.Fatalf("params: %v", err)
			}
			assertSameJSON(t, "params", n.Params, encoded)
		})
	}
	for method := range notificationTypes {
		if !seen[method] {
			t.Errorf("the fixtures have no sample of %s", method)
		}
	}
}

// The contract's errors come back as *RPCError with the code its table names,
// which is what a Client branches on.
func TestErrorFixturesDecodeIntoRPCError(t *testing.T) {
	for _, name := range readIndex(t).Errors {
		f := readFixture(t, name)
		t.Run(strings.TrimSuffix(filepath.Base(name), ".json"), func(t *testing.T) {
			if f.Error == nil {
				t.Fatal("the fixture names no error")
			}
			var response rpcFrame
			if err := json.Unmarshal(f.Response, &response); err != nil {
				t.Fatalf("response: %v", err)
			}
			if response.Error == nil {
				t.Fatal("the response carries no error")
			}
			if response.Error.Code != f.Error.Code {
				t.Fatalf("the response's code is %d, the fixture's %d", response.Error.Code, f.Error.Code)
			}
			if response.Error.Message == "" {
				t.Fatal("every error carries a message")
			}
			var err error = response.Error
			if !Is(err, f.Error.Code) {
				t.Fatalf("Is(%v, %d) is false", err, f.Error.Code)
			}
		})
	}
}

// A busy notification tells a run from idle, which is what the window's
// "working" state is.
func TestBusyFixturesSayRunningOrNot(t *testing.T) {
	cases := []struct {
		file string
		busy bool
	}{
		{"notifications/conversation.busy.json", true},
		{"notifications/conversation.busy.idle.json", false},
		{"notifications/conversation.busy.failed.json", false},
	}
	for _, c := range cases {
		f := readFixture(t, c.file)
		var n Notification
		if err := json.Unmarshal(f.Notification, &n); err != nil {
			t.Fatal(err)
		}
		var busy BusyNotification
		if err := n.Decode(&busy); err != nil {
			t.Fatal(err)
		}
		if busy.Busy() != c.busy {
			t.Errorf("%s: Busy() = %v", c.file, busy.Busy())
		}
		if c.file == "notifications/conversation.busy.failed.json" && len(busy.RunFailure) == 0 {
			t.Errorf("%s: RunFailure is empty", c.file)
		}
	}
}

// assertSameJSON checks that the client's encoding of a value carries
// everything the contract's sample carries, and nothing it does not: a field
// the client does not model is a field the window cannot show, and a field it
// invents is one Kernel will refuse.
//
// A null or an empty container in the sample may arrive as an absent field:
// both say "no value", and Go's omitempty writes the shorter one.
func assertSameJSON(t *testing.T, what string, sample, got []byte) {
	t.Helper()
	if len(sample) == 0 {
		return
	}
	var want, have any
	if err := json.Unmarshal(sample, &want); err != nil {
		t.Fatalf("%s sample: %v", what, err)
	}
	if len(got) > 0 {
		if err := json.Unmarshal(got, &have); err != nil {
			t.Fatalf("%s client: %v", what, err)
		}
	}
	if path, missing := uncovered(want, have, "", false); missing {
		t.Fatalf("%s: this client does not keep %s\n sample %s\n client %s", what, path, sample, got)
	}
	if path, extra := uncovered(have, want, "", true); extra {
		t.Fatalf("%s: this client sends %s, which the sample does not have\n sample %s\n client %s", what, path, sample, got)
	}
}

// emptyValue reports a value that says "nothing": null, an empty list, an
// empty object. Go's omitempty writes those as an absent field.
func emptyValue(value any) bool {
	switch v := value.(type) {
	case nil:
		return true
	case []any:
		return len(v) == 0
	case map[string]any:
		return len(v) == 0
	}
	return false
}

// zeroValue reports a value Go's omitempty also drops: false, "", 0. A
// sample's false and a client's absent field mean the same thing on the wire,
// so either side may spell it the shorter way.
func zeroValue(value any) bool {
	if emptyValue(value) {
		return true
	}
	switch v := value.(type) {
	case bool:
		return !v
	case string:
		return v == ""
	case float64:
		return v == 0
	}
	return false
}

// uncovered is the first path of want that have does not carry. On the
// sample side a zero value may be absent; on the client side a zero value the
// sample does not carry is not an invention. Any other missing value is a
// field the client does not model, and any other extra one is a field it
// invented.
func uncovered(want, have any, path string, fromClient bool) (string, bool) {
	switch w := want.(type) {
	case map[string]any:
		if len(w) == 0 {
			return "", false
		}
		if have == nil {
			if fromClient {
				return path, true
			}
			return "", false
		}
		h, ok := have.(map[string]any)
		if !ok {
			return path, true
		}
		for key, value := range w {
			child := key
			if path != "" {
				child = path + "." + key
			}
			if !fromClient && zeroValue(value) {
				continue
			}
			got, ok := h[key]
			if !ok {
				if fromClient && zeroValue(value) {
					continue
				}
				return child, true
			}
			if p, bad := uncovered(value, got, child, fromClient); bad {
				return p, true
			}
		}
		return "", false
	case []any:
		if len(w) == 0 {
			return "", false
		}
		if have == nil {
			if fromClient {
				return path, true
			}
			return "", false
		}
		h, ok := have.([]any)
		if !ok || len(h) != len(w) {
			return path, true
		}
		for i := range w {
			if p, bad := uncovered(w[i], h[i], path+"["+strconv.Itoa(i)+"]", fromClient); bad {
				return p, true
			}
		}
		return "", false
	default:
		if want != have {
			return path, true
		}
		return "", false
	}
}

// The three kinds a conversation.view push takes say whether a subscriber may
// keep the state it has: a "delta" subscriber applies ops, and replaces its
// state whenever a whole view arrives.
func TestViewKindSaysWhetherThePushIsWhole(t *testing.T) {
	cases := []struct {
		file   string
		whole  bool
		failed bool
	}{
		{"notifications/conversation.view.json", true, false},
		{"notifications/conversation.view.full.json", true, false},
		{"notifications/conversation.view.delta.json", false, false},
		{"notifications/conversation.view.failed.json", false, true},
	}
	for _, c := range cases {
		f := readFixture(t, c.file)
		var n Notification
		if err := json.Unmarshal(f.Notification, &n); err != nil {
			t.Fatal(err)
		}
		var view ViewNotification
		if err := n.Decode(&view); err != nil {
			t.Fatal(err)
		}
		if view.Whole() != c.whole {
			t.Errorf("%s: Whole() = %v, want %v", c.file, view.Whole(), c.whole)
		}
		if view.Failed() != c.failed {
			t.Errorf("%s: Failed() = %v, want %v", c.file, view.Failed(), c.failed)
		}
		if !c.whole && !c.failed && len(view.Ops) == 0 {
			t.Errorf("%s: a delta push carries no ops", c.file)
		}
		if view.Retired {
			t.Errorf("%s: retired", c.file)
		}
	}
}

// A delta subscription is what the phone asks for (M5); the window keeps the
// default, which is a whole view.
func TestOpenAsksForWholeViewsByDefault(t *testing.T) {
	if (OpenParams{}).Subscribe != "" {
		t.Fatal("OpenParams defaults to a value, not to the contract's \"view\"")
	}
	encoded, err := json.Marshal(OpenParams{ID: 7})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"id":7}` {
		t.Fatalf("the default open sends %s, so the kernel never sees a choice it did not ask for", encoded)
	}
	encoded, err = json.Marshal(OpenParams{ID: 7, Subscribe: SubscribeDelta})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"subscribe":"delta"`) {
		t.Fatalf("a delta open sends %s", encoded)
	}
}

// harness.info's limits are read, not assumed: the push rate is the kernel's.
func TestHarnessInfoCarriesThePushRate(t *testing.T) {
	f := readFixture(t, "methods/harness.info.json")
	var response rpcFrame
	if err := json.Unmarshal(f.Response, &response); err != nil {
		t.Fatal(err)
	}
	var info HarnessInfo
	if err := json.Unmarshal(response.Result, &info); err != nil {
		t.Fatal(err)
	}
	if info.Limits.ViewPushesPerSecond != 5 {
		t.Fatalf("limits = %+v, want 5 pushes a second", info.Limits)
	}
	if info.Capabilities.Models {
		t.Fatal("the sample says models is available: M1 does not implement model.complete")
	}
}

// The contract gives a change's field three states, and a pointer alone cannot
// say all three: absent, cleared (null), and set. This is the test the fixtures
// cannot make, because decoding a sample with `instructions: null` into a
// pointer field leaves it nil — the same as absent.
func TestAgentChangeHasThreeStates(t *testing.T) {
	absent, err := json.Marshal(AgentChange{})
	if err != nil {
		t.Fatal(err)
	}
	if string(absent) != "{}" {
		t.Fatalf("a change that sets nothing sends %s, want {}", absent)
	}
	cleared, err := json.Marshal(AgentChange{Instructions: None[string]()})
	if err != nil {
		t.Fatal(err)
	}
	if string(cleared) != `{"instructions":null}` {
		t.Fatalf("clearing a field sends %s, want {\"instructions\":null}", cleared)
	}
	set, err := json.Marshal(AgentChange{Instructions: Some("be brief"), MaxTurns: Some[int](3)})
	if err != nil {
		t.Fatal(err)
	}
	if string(set) != `{"instructions":"be brief","maxTurns":3}` {
		t.Fatalf("setting fields sends %s", set)
	}
	// A model is a value too, not a pointer to a pointer.
	model := Some(ModelRef{Provider: "factory", ModelID: "claude-haiku-5-5"})
	sent, err := json.Marshal(AgentChange{Model: model})
	if err != nil {
		t.Fatal(err)
	}
	if string(sent) != `{"model":{"provider":"factory","modelId":"claude-haiku-5-5"}}` {
		t.Fatalf("setting the model sends %s", sent)
	}
}

// The configure sample carries `instructions: null`, which is the contract's
// "clear it": this client sends it that way, not by leaving the field out.
func TestConfigureCanClearInstructions(t *testing.T) {
	change := AgentChange{Instructions: None[string]()}
	encoded, err := json.Marshal(ConfigureParams{ID: 7, Change: &change})
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Change map[string]json.RawMessage `json:"change"`
	}
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	raw, ok := decoded.Change["instructions"]
	if !ok || string(raw) != "null" {
		t.Fatalf("the change sends %s, want instructions: null", encoded)
	}
}

// Limits is what a client reads instead of guessing: the push rate, the page
// caps and the window a view holds. The kernel publishes all four, and a build
// that names none is read with the contract's values rather than with zero.
func TestLimitsCarryThePageCapsAndTheWindow(t *testing.T) {
	// What the kernel of PR #227 answers, limits included.
	const published = `{
		"viewPushesPerSecond": 5,
		"viewWindowDefault": {"min": 100, "max": 200},
		"maxEntriesPerPage": 200,
		"maxConversationsPerPage": 200
	}`
	var limits Limits
	if err := json.Unmarshal([]byte(published), &limits); err != nil {
		t.Fatal(err)
	}
	if got := limits.PushRate(); got != 5 {
		t.Errorf("PushRate() = %d, want 5", got)
	}
	if got := limits.EntriesPerPage(); got != 200 {
		t.Errorf("EntriesPerPage() = %d, want 200", got)
	}
	if got := limits.ConversationsPerPage(); got != 200 {
		t.Errorf("ConversationsPerPage() = %d, want 200", got)
	}
	if got := limits.Window(); got != (Window{Min: 100, Max: 200}) {
		t.Errorf("Window() = %+v, want {100 200}", got)
	}

	// A build that names none: the contract's values, not zero. A zero window
	// would ask Kernel for a view of nothing, and a zero page would look like
	// an answer with no entries.
	var none Limits
	if none.PushRate() != 5 || none.EntriesPerPage() != 200 ||
		none.ConversationsPerPage() != 200 || none.Window() != (Window{Min: 100, Max: 200}) {
		t.Fatalf("an empty limits reads as %+v (%d, %d, %d, %+v)", none,
			none.PushRate(), none.EntriesPerPage(), none.ConversationsPerPage(), none.Window())
	}

	// And what this client writes back carries only what it was given: the
	// published caps round-trip, and an empty limits writes nothing at all.
	encoded, err := json.Marshal(limits)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"maxEntriesPerPage":200`) {
		t.Errorf("the published caps do not round-trip: %s", encoded)
	}
	empty, err := json.Marshal(none)
	if err != nil {
		t.Fatal(err)
	}
	if string(empty) != "{}" {
		t.Errorf("an empty limits writes %s, want {}", empty)
	}
}
