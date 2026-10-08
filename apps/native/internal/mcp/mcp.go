// Package mcp is MCP Servers as the Daemon reports them for a Session, and
// what a Client may do with each. The Daemon owns the configuration
// (`~/.factory/mcp.json` at the user level, the Workspace's
// `.factory/mcp.json` at the project level, the organization's policy above
// both) and the connections; a Client lists, switches, adds and removes
// through it, as the droid CLI's `/mcp` does (a port of mcp.ts).
package mcp

import (
	"errors"
	"regexp"
	"slices"
	"strings"

	"github.com/kkkk2323/droi/apps/native/internal/collate"
	"github.com/kkkk2323/droi/apps/native/internal/l10n"
	"github.com/kkkk2323/droi/packages/droid-sdk-go/protocol"
)

var StatusLabels = map[string]string{
	"connecting":   l10n.N("Connecting"),
	"connected":    l10n.N("Connected"),
	"disconnected": l10n.N("Disconnected"),
	"failed":       l10n.N("Failed"),
	"disabled":     l10n.N("Disabled"),
}

// DroiMemoryServer is Droi's Memory Server (ADR 0010), which the Desktop Shell
// attaches through the Runtime Overlay. The name is the one the Shell
// registers it under.
const DroiMemoryServer = "droi-memory"

// IsDroiMemory: its only switch is the Memory setting in the Desktop Shell.
func IsDroiMemory(server protocol.MCPServerStatusInfo) bool { return server.Name == DroiMemoryServer }

// IsReadOnly: the organization's servers are policy and Droi's Memory is the
// Shell's; nothing here changes them.
func IsReadOnly(server protocol.MCPServerStatusInfo) bool {
	return server.Source == protocol.SettingsLevelOrg || IsDroiMemory(server)
}

// CanRemove: only a server in the user's own `mcp.json` can be removed from here.
func CanRemove(server protocol.MCPServerStatusInfo) bool {
	return server.Source == protocol.SettingsLevelUser && !IsDroiMemory(server)
}

func NeedsSignIn(server protocol.MCPServerStatusInfo) bool {
	return server.RequiresAuth != nil && *server.RequiresAuth && !(server.HasAuthTokens != nil && *server.HasAuthTokens)
}

// ServerForm is what a Client's form collects for a new server; every field a string.
type ServerForm struct {
	Name    string
	Type    protocol.MCPServerType
	Command string
	// Args are whitespace-separated.
	Args string
	URL  string
	// Headers are one `Name: value` per line.
	Headers string
}

var EmptyServerForm = ServerForm{Type: protocol.MCPServerTypeStdio}

var serverName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ParseServerForm turns the form into the Daemon's parameters (SessionID is
// the caller's to fill in), or says what is wrong with it.
func ParseServerForm(form ServerForm) (protocol.AddMCPServerParams, error) {
	name := strings.TrimSpace(form.Name)
	if name == "" {
		return protocol.AddMCPServerParams{}, errors.New(l10n.L("Give the server a name."))
	}
	if !serverName.MatchString(name) {
		return protocol.AddMCPServerParams{}, errors.New(l10n.L("A name has letters, digits, \"-\" and \"_\" only."))
	}
	if form.Type == protocol.MCPServerTypeStdio {
		command := strings.TrimSpace(form.Command)
		if command == "" {
			return protocol.AddMCPServerParams{}, errors.New(l10n.L("Give the command that starts the server."))
		}
		return protocol.AddMCPServerParams{Name: name, Type: form.Type, Command: command, Args: strings.Fields(form.Args)}, nil
	}
	url := strings.TrimSpace(form.URL)
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return protocol.AddMCPServerParams{}, errors.New(l10n.L("The URL starts with http:// or https://."))
	}
	var headers map[string]string
	for _, line := range strings.Split(form.Headers, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		colon := strings.Index(line, ":")
		if colon <= 0 {
			return protocol.AddMCPServerParams{}, errors.New(l10n.L("A header is \"Name: value\"; \"%s\" is not.", strings.TrimSpace(line)))
		}
		if headers == nil {
			headers = map[string]string{}
		}
		headers[strings.TrimSpace(line[:colon])] = strings.TrimSpace(line[colon+1:])
	}
	return protocol.AddMCPServerParams{Name: name, Type: form.Type, URL: url, Headers: headers}, nil
}

// FormFromRegistry is a registry entry as the form would hold it, ready to
// adjust and add.
func FormFromRegistry(entry protocol.MCPRegistryServer) ServerForm {
	return ServerForm{
		Name:    entry.Name,
		Type:    entry.Type,
		Command: entry.Command,
		Args:    strings.Join(entry.Args, " "),
		URL:     entry.URL,
	}
}

// A catalogue entry's command may carry a value to fill in first, written as
// an upper-case name (`--adyenApiKey=ADYEN_API_KEY`).
var placeholder = regexp.MustCompile(`(?:^|=)[A-Z][A-Z0-9]*_[A-Z0-9_]+$`)

// NeedsSetup is whether a catalogue entry has a value to fill in before it
// can be added as it is.
func NeedsSetup(entry protocol.MCPRegistryServer) bool {
	return slices.ContainsFunc(append(slices.Clone(entry.Args), entry.URL), placeholder.MatchString)
}

// FilterRegistry are the catalogue entries not added yet whose name or
// description matches the search.
func FilterRegistry(entries []protocol.MCPRegistryServer, query string, taken []string) []protocol.MCPRegistryServer {
	words := strings.Fields(strings.ToLower(query))
	var out []protocol.MCPRegistryServer
	for _, entry := range entries {
		if slices.Contains(taken, entry.Name) {
			continue
		}
		text := strings.ToLower(entry.Name + " " + entry.Description)
		if !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(text, w) }) {
			out = append(out, entry)
		}
	}
	return out
}

// SortServers sorts for a list: the user's and the project's first, by name;
// the organization's after.
func SortServers(servers []protocol.MCPServerStatusInfo) []protocol.MCPServerStatusInfo {
	out := slices.Clone(servers)
	rank := func(s protocol.MCPServerStatusInfo) int {
		if IsReadOnly(s) {
			return 1
		}
		return 0
	}
	slices.SortStableFunc(out, func(a, b protocol.MCPServerStatusInfo) int {
		if c := rank(a) - rank(b); c != 0 {
			return c
		}
		return collate.Compare(a.Name, b.Name)
	})
	return out
}
