package sessionview

import (
	"sync"

	"github.com/egoist/mygo/ui"

	"github.com/kkkk2323/droi/apps/native/pkg/kit"
	"github.com/kkkk2323/droi/apps/native/pkg/transcript"
)

// ToolView is how a tool's calls show in the transcript in place of the
// input and the first lines of the result, such as a query's rows as a
// table.
type ToolView struct {
	// Icon, optional, is the row's icon: a name of package icons.
	Icon string
	// Open shows the panel until the user closes it, rather than after a
	// click.
	Open bool
	// Detail draws the panel's content, built every frame; nil keeps the
	// input and the result.
	Detail func(c *ui.Context, t ToolContext)
}

// ToolContext is what a ToolView draws from.
type ToolContext struct {
	Kit    *kit.Kit
	Call   *transcript.ToolCall
	Result transcript.ToolResultView
}

// Component draws a json-render element of a type the Session view does
// not draw itself: its props as the reply gives them, and its children.
type Component func(c *ui.Context, k *kit.Kit, props map[string]any, children func())

var (
	extendMu   sync.RWMutex
	toolViews  = map[string]ToolView{}
	components = map[string]Component{}
)

// RegisterToolView shows the calls of the tool named name, as the Daemon
// names it ("Execute", or an MCP tool's "corp-db___query"), with v. Call it
// before the window opens.
func RegisterToolView(name string, v ToolView) {
	extendMu.Lock()
	defer extendMu.Unlock()
	toolViews[name] = v
}

// RegisterComponent draws json-render elements of type typ, such as
// "LineChart", with draw. A type the Session view draws itself keeps its
// own. Call it before the window opens.
func RegisterComponent(typ string, draw Component) {
	extendMu.Lock()
	defer extendMu.Unlock()
	components[typ] = draw
}

func toolViewOf(name string) (ToolView, bool) {
	extendMu.RLock()
	defer extendMu.RUnlock()
	v, ok := toolViews[name]
	return v, ok
}

func componentOf(typ string) Component {
	extendMu.RLock()
	defer extendMu.RUnlock()
	return components[typ]
}
