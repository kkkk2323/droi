package app

// The Skills and MCP servers dialog of a conversation Kernel drives: Kernel's lists, put in the dialog's own shapes,
// and every change sent to Kernel. The Daemon's lists are another runtime's and must not show here.

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/pkg/kernel"
	"github.com/kkkk2323/droi/apps/native/pkg/kit"
)

// kernelTools is the conversation and connection the dialog asks, when Kernel drives the Session.
func (v *sessionView) kernelTools() (kernel.ID, *kernel.Client, bool) {
	conv, ok := v.a.kernelID(v.id)
	if !ok {
		return 0, nil, false
	}
	c, err := v.a.kernel.client()
	if err != nil {
		return 0, nil, false
	}
	return conv, c, true
}

// repoRootOf is the repository Kernel lists the Session under, which names the repository's own servers.
func (a *App) repoRootOf(id string) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s := findSummary(a.listed, id); s != nil {
		return s.RepoRoot
	}
	return ""
}

// loadKernelTools is loadTools for a conversation on Kernel.
func (v *sessionView) loadKernelTools(conv kernel.ID, c *kernel.Client) {
	ts := &v.tools
	root := v.a.repoRootOf(v.id)
	go func() {
		defer v.a.redraw()
		ctx, cancel := context.WithTimeout(v.a.ctx, 15*time.Second)
		defer cancel()
		sk, err1 := c.SkillsOf(ctx, conv)
		sv, err2 := c.MCP(ctx, kernel.MCPListParams{RepoRoot: root})
		ts.mu.Lock()
		defer ts.mu.Unlock()
		ts.loaded, ts.onKernel = true, true
		ts.skillsErr, ts.serversErr = "", ""
		if err1 != nil {
			ts.skillsErr = err1.Error()
		} else {
			ts.skills = kernelSkills(sk.Skills)
			ts.projectAvailable = true
		}
		if err2 != nil {
			ts.serversErr = err2.Error()
		} else {
			ts.kservers = sv.Servers
			for _, s := range sv.Servers {
				if s.Status == "connected" {
					delete(ts.signIn, s.Name)
				}
			}
			ts.servers, ts.mcpTools = kernelServers(sv.Servers, ts.signIn)
		}
	}()
}

func kernelSkills(list []kernel.Skill) []protocol.SkillInfo {
	out := make([]protocol.SkillInfo, len(list))
	for i, s := range list {
		enabled := s.Enabled
		location := protocol.SkillLocationPersonal
		if s.Scope == "workspace" {
			location = protocol.SkillLocationProject
		}
		out[i] = protocol.SkillInfo{Name: s.Name, Description: s.Description, Location: location, FilePath: s.Path, Enabled: &enabled}
	}
	return out
}

// kernelServers are Kernel's servers as the dialog draws them: a repository's are "project" servers, and the
// sign-in a person started shows its page until the server connects.
func kernelServers(list []kernel.MCPServer, signIn map[string]string) ([]protocol.MCPServerStatusInfo, []protocol.MCPToolInfo) {
	servers := make([]protocol.MCPServerStatusInfo, 0, len(list))
	var tools []protocol.MCPToolInfo
	for _, s := range list {
		count := float64(len(s.Tools))
		source := protocol.SettingsLevelUser
		if s.RepoRoot != "" {
			source = protocol.SettingsLevel("project")
		}
		info := protocol.MCPServerStatusInfo{Name: s.Name, Status: protocol.MCPServerStatus(s.Status), Source: source,
			ServerType: protocol.MCPServerType2(cmpOr(s.Transport, "stdio")), ToolCount: &count}
		if s.Error != nil {
			info.Error = *s.Error
		}
		if needsKernelSignIn(s) {
			yes := true
			info.RequiresAuth = &yes
			info.PendingAuthURL = signIn[s.Name]
		}
		servers = append(servers, info)
		for _, t := range s.Tools {
			tools = append(tools, protocol.MCPToolInfo{ServerName: s.Name, Name: t, IsEnabled: true})
		}
	}
	return servers, tools
}

func needsKernelSignIn(s kernel.MCPServer) bool {
	return s.Status == "failed" && s.Error != nil && strings.HasPrefix(*s.Error, "authorization required")
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// kernelServer is Kernel's own record of a server the dialog shows.
func (v *sessionView) kernelServer(name string, repoRoot bool) (kernel.MCPServer, bool) {
	ts := &v.tools
	ts.mu.Lock()
	defer ts.mu.Unlock()
	for _, s := range ts.kservers {
		if s.Name == name && (s.RepoRoot != "") == repoRoot {
			return s, true
		}
	}
	return kernel.MCPServer{}, false
}

// kernelCall runs one change through Kernel, then lists everything again.
func (v *sessionView) kernelCall(name string, run func(ctx context.Context, c *kernel.Client) error, done func(ok bool)) {
	v.toolsAct(name, func() error {
		_, c, ok := v.kernelTools()
		if !ok {
			return errNoKernel
		}
		ctx, cancel := context.WithTimeout(v.a.ctx, 30*time.Second)
		defer cancel()
		return run(ctx, c)
	}, done)
}

// setKernelSkill turns a skill on or off everywhere, which is what Kernel's switch is.
func (v *sessionView) setKernelSkill(name string, disabled bool) {
	conv, _, _ := v.kernelTools()
	v.kernelCall(name, func(ctx context.Context, c *kernel.Client) error {
		_, err := c.SetSkillDisabled(ctx, kernel.SkillsSetDisabledParams{Name: name, Disabled: disabled, ID: &conv})
		return err
	}, nil)
}

// setKernelServer switches a global server on or off.
func (v *sessionView) setKernelServer(name string, enabled bool) {
	v.kernelCall(name, func(ctx context.Context, c *kernel.Client) error {
		_, err := c.SetMCP(ctx, kernel.MCPSetParams{Name: name, Enabled: &enabled})
		return err
	}, nil)
}

func (v *sessionView) removeKernelServer(name string) {
	v.kernelCall(name, func(ctx context.Context, c *kernel.Client) error {
		_, err := c.SetMCP(ctx, kernel.MCPSetParams{Name: name, Remove: true})
		return err
	}, nil)
}

// addKernelServer adds a server to the global mcp.json through Kernel.
func (v *sessionView) addKernelServer(p protocol.AddMCPServerParams) {
	config := &kernel.MCPServerConfig{Transport: string(p.Type), Command: p.Command, Args: p.Args, Env: p.Env, URL: p.URL, Headers: p.Headers}
	if config.Transport == "" {
		config.Transport = "stdio"
	}
	v.kernelCall(p.Name, func(ctx context.Context, c *kernel.Client) error {
		_, err := c.SetMCP(ctx, kernel.MCPSetParams{Name: p.Name, Server: config})
		return err
	}, func(ok bool) {
		if ok {
			v.tools.view = ""
		}
	})
}

// signInKernel starts a server's sign-in; the page to open shows on its row until the server connects.
func (v *sessionView) signInKernel(name string) {
	ts := &v.tools
	v.kernelCall(name, func(ctx context.Context, c *kernel.Client) error {
		res, err := c.AuthorizeMCP(ctx, kernel.MCPAuthorizeParams{Name: name})
		if err != nil {
			return err
		}
		ts.mu.Lock()
		if ts.signIn == nil {
			ts.signIn = map[string]string{}
		}
		if res.AuthorizationURL != "" {
			ts.signIn[name] = res.AuthorizationURL
		}
		ts.mu.Unlock()
		return nil
	}, nil)
}

func (v *sessionView) cancelKernelSignIn(name string) {
	ts := &v.tools
	ts.mu.Lock()
	delete(ts.signIn, name)
	ts.mu.Unlock()
	v.kernelCall(name, func(ctx context.Context, c *kernel.Client) error {
		return c.CancelMCPAuthorize(ctx, kernel.MCPAuthorizeParams{Name: name})
	}, nil)
}

// trustNotice is a repository's server waiting for the person's OK: what it would run, and the two answers.
func (v *sessionView) trustNotice(c *ui.Context, s kernel.MCPServer, busy bool) {
	a := v.a
	k, t := a.kit, a.kit.T
	what := s.URL
	if s.Command != "" {
		what = strings.TrimSpace(s.Command + " " + strings.Join(s.Args, " "))
	}
	ui.Column(c).Role(ui.RoleStatus).Label(L("Allow %s?", s.Name)).Gap(k.Px(6)).Margin(k.Px(4), 0, 0, 0).Padding(k.Px(8), k.Px(10)).
		Radius(k.Px(6)).Background(t.Attention.Alpha(0.1)).Children(func() {
		k.Text(c, L("This repository's .factory/mcp.json wants to run:"), 13, 20)
		k.Text(c, what, 12, 18).Font(k.Mono).Selectable()
		ui.Row(c).Gap(k.Px(6)).Children(func() {
			b := k.Button(c, kit.Outline, 24, false, L("Allow %s", s.Name)).PaddingX(k.Px(8)).Disabled(busy)
			b.Children(func() { k.Text(c, L("Allow"), 12, 16).FontWeight(500) })
			if b.Clicked() {
				name, root := s.Name, s.RepoRoot
				yes := true
				v.kernelCall(name, func(ctx context.Context, c *kernel.Client) error {
					_, err := c.SetMCP(ctx, kernel.MCPSetParams{Name: name, RepoRoot: root, Trust: &yes})
					return err
				}, nil)
			}
		})
	})
}
