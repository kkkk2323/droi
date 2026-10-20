package app

import (
	"testing"

	"github.com/kkkk2323/droid-sdk-go/fakedaemon"
)

var diagrams = map[string]string{
	"flowchart": "flowchart LR\n" +
		"  A[Composer] -->|add_user_message| B(Daemon)\n" +
		"  B --> C{Spec mode?}\n" +
		"  C -->|yes| D[Plan with ExitSpecMode]\n" +
		"  C -->|no| E[Work]\n" +
		"  D -.->|approved| E\n" +
		"  subgraph Review\n" +
		"    D\n" +
		"  end\n",
	"sequence": "sequenceDiagram\n" +
		"  actor U as User\n" +
		"  participant D as Droi\n" +
		"  participant S as Daemon\n" +
		"  U->>D: Send a message\n" +
		"  D->>+S: add_user_message\n" +
		"  S-->>D: request_permission\n" +
		"  alt approved\n" +
		"    D->>S: proceed_once\n" +
		"  else keep planning\n" +
		"    D->>S: cancel\n" +
		"  end\n" +
		"  S-->>-D: agent_turn_completed\n" +
		"  Note over D,S: The plan stays in the transcript\n",
}

// A reply's Mermaid diagrams are drawn in the window's theme.
func TestReplyDiagrams(t *testing.T) {
	for _, kind := range []string{"flowchart", "sequence"} {
		for _, theme := range []string{"light", "dark"} {
			t.Run(kind+"-"+theme, func(t *testing.T) {
				h := newHarness(t, fakedaemon.Scenario{Sessions: []fakedaemon.SessionSpec{{Title: "How Spec mode works", Cwd: "/Users/dev/acme-web", Messages: []fakedaemon.Message{
					{Role: "user", Text: "Draw how Spec mode works"},
					{Role: "assistant", Text: "Here it is:\n\n```mermaid\n" + diagrams[kind] + "```\n"},
				}}}}, theme)
				h.openSession("How Spec mode works")
				h.until("the diagram", func() bool { _, ok := h.tt.Find("Diagram: " + kind); return ok })
				h.shoot("diagram-" + kind + "-" + theme)
			})
		}
	}
}
