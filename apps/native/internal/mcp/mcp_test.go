package mcp

import (
	"reflect"
	"testing"

	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

func entry(edit func(*protocol.MCPRegistryServer)) protocol.MCPRegistryServer {
	e := protocol.MCPRegistryServer{Name: "linear", Description: "Issue tracking", Type: protocol.MCPServerTypeHTTP, URL: "https://mcp.linear.app/mcp"}
	if edit != nil {
		edit(&e)
	}
	return e
}

func names(entries []protocol.MCPRegistryServer) []string {
	out := []string{}
	for _, e := range entries {
		out = append(out, e.Name)
	}
	return out
}

func TestNeedsSetup(t *testing.T) {
	stdio := func(args ...string) protocol.MCPRegistryServer {
		return entry(func(e *protocol.MCPRegistryServer) {
			e.Type, e.Command, e.Args, e.URL = protocol.MCPServerTypeStdio, "npx", args, ""
		})
	}
	for _, c := range []struct {
		e    protocol.MCPRegistryServer
		want bool
	}{
		{entry(nil), false},
		{stdio("-y", "snyk@latest", "mcp", "-t", "stdio"), false},
		{stdio("-y", "@adyen/mcp", "--adyenApiKey=ADYEN_API_KEY", "--env=TEST"), true},
		{stdio("YOUR_TOKEN"), true},
	} {
		if got := NeedsSetup(c.e); got != c.want {
			t.Errorf("NeedsSetup(%+v) = %v", c.e, got)
		}
	}
}

func TestFilterRegistry(t *testing.T) {
	all := []protocol.MCPRegistryServer{
		entry(nil),
		entry(func(e *protocol.MCPRegistryServer) {
			e.Name, e.Description = "sentry", "Error tracking and performance monitoring"
		}),
		entry(func(e *protocol.MCPRegistryServer) { e.Name, e.Description = "notion", "Notes and docs" }),
	}
	for _, c := range []struct {
		query string
		taken []string
		want  []string
	}{
		{"", nil, []string{"linear", "sentry", "notion"}},
		{"TRACKING", nil, []string{"linear", "sentry"}},
		{"error track", nil, []string{"sentry"}},
		{"tracking", []string{"linear"}, []string{"sentry"}},
	} {
		if got := names(FilterRegistry(all, c.query, c.taken)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("FilterRegistry(%q, %v) = %v, want %v", c.query, c.taken, got, c.want)
		}
	}
}

func server(name string, source protocol.SettingsLevel) protocol.MCPServerStatusInfo {
	return protocol.MCPServerStatusInfo{Name: name, Status: protocol.MCPServerStatusConnected, Source: source, ServerType: protocol.MCPServerType2Stdio}
}

func TestParseServerForm(t *testing.T) {
	form := func(edit func(*ServerForm)) ServerForm {
		f := EmptyServerForm
		edit(&f)
		return f
	}
	for _, f := range []ServerForm{
		form(func(f *ServerForm) { f.Command = "npx x" }),
		form(func(f *ServerForm) { f.Name, f.Command = "bad name", "x" }),
		form(func(f *ServerForm) { f.Name, f.Type, f.URL = "api", protocol.MCPServerTypeHTTP, "localhost" }),
		form(func(f *ServerForm) {
			f.Name, f.Type, f.URL, f.Headers = "api", protocol.MCPServerTypeSse, "https://x.y/sse", "nonsense"
		}),
	} {
		if _, err := ParseServerForm(f); err == nil {
			t.Errorf("ParseServerForm(%+v) should fail", f)
		}
	}

	got, err := ParseServerForm(form(func(f *ServerForm) { f.Name, f.Command, f.Args = "fs", "npx", " -y  server " }))
	want := protocol.AddMCPServerParams{Name: "fs", Type: protocol.MCPServerTypeStdio, Command: "npx", Args: []string{"-y", "server"}}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("stdio: got %+v, %v", got, err)
	}

	got, err = ParseServerForm(form(func(f *ServerForm) {
		f.Name, f.Type, f.URL = "api", protocol.MCPServerTypeHTTP, "https://mcp.example.com/mcp"
		f.Headers = "Authorization: Bearer x\n\nX-Team: a: b"
	}))
	want = protocol.AddMCPServerParams{
		Name: "api", Type: protocol.MCPServerTypeHTTP, URL: "https://mcp.example.com/mcp",
		Headers: map[string]string{"Authorization": "Bearer x", "X-Team": "a: b"},
	}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("http: got %+v, %v", got, err)
	}
}

func TestFormFromRegistry(t *testing.T) {
	got := FormFromRegistry(protocol.MCPRegistryServer{
		Name: "github", Description: "GitHub", Type: protocol.MCPServerTypeHTTP, URL: "https://api.githubcopilot.com/mcp/",
	})
	want := ServerForm{Name: "github", Type: protocol.MCPServerTypeHTTP, URL: "https://api.githubcopilot.com/mcp/"}
	if got != want {
		t.Errorf("got %+v", got)
	}
}

func TestOrganizationServersAreReadOnlyAndLast(t *testing.T) {
	org := server("a-org", protocol.SettingsLevelOrg)
	project := server("z-proj", protocol.SettingsLevelProject)
	mine := server("m-mine", protocol.SettingsLevelUser)
	var got []string
	for _, s := range SortServers([]protocol.MCPServerStatusInfo{org, project, mine}) {
		got = append(got, s.Name)
	}
	if !reflect.DeepEqual(got, []string{"m-mine", "z-proj", "a-org"}) {
		t.Errorf("order = %v", got)
	}
	if !IsReadOnly(org) || CanRemove(project) || !CanRemove(mine) {
		t.Error("read-only and removable servers")
	}
}

func TestDroiMemoryServerIsTheShells(t *testing.T) {
	memory := server("droi-memory", protocol.SettingsLevelUser)
	if !IsDroiMemory(memory) || !IsReadOnly(memory) || CanRemove(memory) {
		t.Error("Droi's Memory Server should be read-only and never removed from here")
	}
	if IsDroiMemory(server("memory", protocol.SettingsLevelUser)) {
		t.Error("only the Shell's name is Droi's Memory Server")
	}
}

func TestNeedsSignIn(t *testing.T) {
	yes := true
	s := server("a", protocol.SettingsLevelUser)
	if NeedsSignIn(s) {
		t.Error("no auth needed")
	}
	s.RequiresAuth = &yes
	if !NeedsSignIn(s) {
		t.Error("auth needed and no tokens")
	}
	s.HasAuthTokens = &yes
	if NeedsSignIn(s) {
		t.Error("tokens held")
	}
}
