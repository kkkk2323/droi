package automationwork

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/automations"
	"github.com/kkkk2323/droi/apps/native/internal/hosttools"
	"github.com/kkkk2323/droi/apps/native/pkg/mcpserver"
)

// ServerName is the tool server through which Droid manages Droi's
// Automations, carried through the Host tool bridge.
const ServerName = "droi-automations"

// The tools; there is no delete, which only the Automations page does.
const (
	ToolList   = "list_automations"
	ToolCreate = "create_automation"
	ToolUpdate = "update_automation"
	ToolPause  = "pause_automation"
	ToolRun    = "run_automation"
)

// defaultAutonomy is a tool-made Automation's: its runs ask before what is
// above high. Only the Automations page sets one to run without asking.
const defaultAutonomy = "high"

// askFree refuses a tool's change to what an Automation that runs without
// asking does: through it, a Session could have such a run do anything.
const askFree = "This automation runs without asking for approval, so its prompt, folder and model can only be changed in Droi's Automations page."

func object(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

// toolList is the tools/list answer's "tools", with zone, this computer's
// time zone, in what reads a schedule.
func toolList(zone string) []map[string]any {
	schedule := str(fmt.Sprintf("Five cron fields (minute hour day-of-month month day-of-week) in this computer's time zone, %s, such as 0 9 * * 1-5 for 09:00 on weekdays. Numbers, *, lists, ranges and steps; no names.", zone))
	id := str("The automation's id, from list_automations.")
	fields := func(required ...string) map[string]any {
		return object(map[string]any{
			"name":             str("A short name."),
			"prompt":           str("What each run does: the first message of the new session every run starts."),
			"schedule":         schedule,
			"workingDirectory": str("The folder runs work in, with its skills and settings. Leave it out for the automation's own folder in Droi's data."),
			"model":            str("The model runs use; leave it out for the default."),
			"reasoningEffort":  str("The reasoning effort runs use; leave it out for the default."),
			"autonomy": map[string]any{"type": "string", "enum": []string{"low", "medium", "high"},
				"description": "How much a run does without asking; it waits for the user's approval of anything above. Defaults to high."},
		}, required...)
	}
	update := fields()
	update["properties"].(map[string]any)["id"] = id
	update["required"] = []string{"id"}
	return []map[string]any{
		{
			"name":        ToolList,
			"description": fmt.Sprintf("List Droi's automations on this computer: id, name, schedule (in %s unless a time zone is given), whether paused or running, next run and last run.", zone),
			"inputSchema": object(map[string]any{}),
		},
		{
			"name":        ToolCreate,
			"description": "Create one of Droi's automations: Droi starts a new session with the prompt on the schedule, on this computer.",
			"inputSchema": fields("name", "prompt", "schedule"),
		},
		{
			"name":        ToolUpdate,
			"description": "Change an automation; the fields left out stay as they are.",
			"inputSchema": update,
		},
		{
			"name":        ToolPause,
			"description": "Pause an automation, or resume it with paused false. A resumed one does not catch up the times it missed.",
			"inputSchema": object(map[string]any{"id": id, "paused": map[string]any{"type": "boolean", "description": "true to pause, false to resume."}}, "id", "paused"),
		},
		{
			"name":        ToolRun,
			"description": "Start a run of an automation now, in a new session; refused while a run of it works.",
			"inputSchema": object(map[string]any{"id": id}, "id"),
		},
	}
}

// ToolServer is the droi-automations server for the Host tool bridge, over
// s. zone names this computer's time zone in the tools' descriptions; they
// are read when the Daemon starts the server. Listing runs without asking
// from autonomy low, the others from medium.
func ToolServer(s *Scheduler, zone func() string) hosttools.Server {
	return ToolServerOf(func() *Scheduler { return s }, zone)
}

// notHeld answers a tool while this Droi does not run the Automations.
const notHeld = "Another Droi on this computer runs the automations."

// ToolServerOf is ToolServer over the Scheduler current gives, nil while
// another Droi runs the Automations, whose tools then say so.
func ToolServerOf(current func() *Scheduler, zone func() string) hosttools.Server {
	return hosttools.Server{
		Tools: func() json.RawMessage {
			b, _ := json.Marshal(toolList(zone()))
			return b
		},
		Call: func(ctx context.Context, tool string, args map[string]any, _ string) (mcpserver.Result, error) {
			s := current()
			if s == nil {
				return mcpserver.Result{}, errors.New(notHeld)
			}
			return callTool(ctx, s, zone(), tool, args)
		},
		Autonomy:     string(protocol.AutonomyLevelMedium),
		ToolAutonomy: map[string]string{ToolList: string(protocol.AutonomyLevelLow)},
	}
}

// listed is an Automation as list_automations answers it.
type listed struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Schedule         string `json:"schedule"`
	Timezone         string `json:"timezone,omitempty"`
	Paused           bool   `json:"paused"`
	Running          bool   `json:"running"`
	Autonomy         string `json:"autonomy"`
	Model            string `json:"model,omitempty"`
	ReasoningEffort  string `json:"reasoningEffort,omitempty"`
	WorkingDirectory string `json:"workingDirectory,omitempty"`
	NextRunAt        string `json:"nextRunAt,omitempty"`
	LastRun          string `json:"lastRun,omitempty"`
	Invalid          string `json:"invalid,omitempty"`
	Prompt           string `json:"prompt"`
}

func callTool(ctx context.Context, s *Scheduler, zone, tool string, args map[string]any) (mcpserver.Result, error) {
	switch tool {
	case ToolList:
		items, err := s.List()
		if err != nil {
			return mcpserver.Result{}, err
		}
		out := make([]listed, 0, len(items))
		for _, it := range items {
			l := listed{
				ID: it.ID, Name: it.Name, Schedule: it.Schedule, Timezone: it.Timezone, Paused: it.Paused, Running: it.Running,
				Autonomy: it.Autonomy, Model: it.Model, ReasoningEffort: it.ReasoningEffort, WorkingDirectory: it.WorkingDirectory,
				NextRunAt: rfc3339(it.NextRunAt), Invalid: it.Invalid, Prompt: it.Prompt,
			}
			if l.Autonomy == "" {
				l.Autonomy = "runs without asking"
			}
			if r := it.LastRun; r != nil {
				l.LastRun = fmt.Sprintf("%s, %s at %s", r.Status, r.Trigger, rfc3339(r.StartedAt))
				if r.Error != "" {
					l.LastRun += ": " + r.Error
				}
			}
			out = append(out, l)
		}
		if len(out) == 0 {
			return mcpserver.Text("Droi has no automations on this computer.", false), nil
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		return mcpserver.Text(string(b), false), nil
	case ToolCreate, ToolUpdate:
		var a, old automations.Automation
		if tool == ToolUpdate {
			it, err := s.Get(text(args, "id"))
			if err != nil {
				return mcpserver.Result{}, toolError(text(args, "id"), err)
			}
			a, old = it.Automation, it.Automation
		} else {
			a.Autonomy = defaultAutonomy
		}
		for key, field := range map[string]*string{
			"name": &a.Name, "prompt": &a.Prompt, "schedule": &a.Schedule, "workingDirectory": &a.WorkingDirectory,
			"model": &a.Model, "reasoningEffort": &a.ReasoningEffort, "autonomy": &a.Autonomy,
		} {
			if v, ok := args[key].(string); ok {
				*field = v
			}
		}
		if tool == ToolUpdate && old.Autonomy == "" {
			// As saved: a path or a model spaced or spelled otherwise is
			// the same one.
			was, now := automations.Clean(old), automations.Clean(a)
			if now.Prompt != was.Prompt || now.WorkingDirectory != was.WorkingDirectory || now.Model != was.Model {
				return mcpserver.Result{}, errors.New(askFree)
			}
		}
		if _, ok := args["schedule"]; ok || tool == ToolCreate {
			// The tools read a schedule in this computer's zone.
			a.Timezone = ""
			if _, err := automations.ParseCron(a.Schedule); err != nil {
				return mcpserver.Result{}, fmt.Errorf("The schedule %q is not understood (%v). Write five cron fields in this computer's time zone, %s, such as 0 9 * * 1-5.", a.Schedule, err, zone)
			}
		}
		if v, ok := args["autonomy"].(string); ok && v != "low" && v != "medium" && v != "high" {
			return mcpserver.Result{}, errors.New("autonomy is low, medium or high.")
		}
		saved, err := s.Save(a)
		if err != nil {
			return mcpserver.Result{}, toolError(a.ID, err)
		}
		it, err := s.Get(saved.ID)
		if err != nil {
			return mcpserver.Result{}, err
		}
		verb := "Created"
		if tool == ToolUpdate {
			verb = "Updated"
		}
		msg := fmt.Sprintf("%s automation %q (id %s), schedule %s in %s.", verb, it.Name, it.ID, it.Schedule, zoneOf(it.Automation, zone))
		if !it.NextRunAt.IsZero() {
			msg += " Next run: " + rfc3339(it.NextRunAt) + "."
		}
		if it.Paused {
			msg += " It is paused."
		}
		return mcpserver.Text(msg, false), nil
	case ToolPause:
		id := text(args, "id")
		paused, ok := args["paused"].(bool)
		if !ok {
			return mcpserver.Result{}, errors.New("paused is true or false.")
		}
		if err := s.SetPaused(id, paused); err != nil {
			return mcpserver.Result{}, toolError(id, err)
		}
		if paused {
			return mcpserver.Text("Paused "+id+".", false), nil
		}
		return mcpserver.Text("Resumed "+id+".", false), nil
	case ToolRun:
		id := text(args, "id")
		run, err := s.RunNow(ctx, id)
		if err != nil {
			return mcpserver.Result{}, toolError(id, err)
		}
		return mcpserver.Text(fmt.Sprintf("Started run %s of %s in session %s.", run.RunID, id, run.SessionID), false), nil
	}
	return mcpserver.Result{}, fmt.Errorf("droi-automations has no tool %s.", tool)
}

func text(args map[string]any, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func zoneOf(a automations.Automation, local string) string {
	if a.Timezone != "" {
		return a.Timezone
	}
	return local
}

// toolError is err of the Automation id as the tools answer it.
func toolError(id string, err error) error {
	switch {
	case errors.Is(err, automations.ErrNotFound):
		return fmt.Errorf("Droi has no automation %q; list_automations lists them.", id)
	case errors.Is(err, ErrRunning):
		return fmt.Errorf("A run of %s is already working.", id)
	}
	return err
}
