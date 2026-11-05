package automationwork

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/internal/automations"
	"github.com/kkkk2323/droi/apps/native/pkg/gateway"
	"github.com/kkkk2323/droi/apps/native/pkg/l10n"
)

// FactoryDaemon is what moving a Factory automation needs of the Host's
// connection to its Daemon (*droid.Client).
type FactoryDaemon interface {
	ListAutomations(ctx context.Context, p protocol.ListAutomationsParams) (*protocol.ListAutomationsResult, error)
	DeleteAutomation(ctx context.Context, p protocol.DeleteAutomationParams) (*protocol.DeleteAutomationResult, error)
}

// GatewayHost answers the Gateway's Automation endpoints
// (gateway.Automations) with a Scheduler, and Factory's automations with
// the Host's connection to its Daemon.
type GatewayHost struct {
	Scheduler *Scheduler
	// Daemon is the Host's connection to its Daemon.
	Daemon func() (FactoryDaemon, error)
	// Offset, optional, is this computer's zone's offset now, in seconds
	// east of UTC; time.Now's when nil.
	Offset func() int
}

var _ gateway.Automations = (*GatewayHost)(nil)

func rfc3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}

func gatewayRun(r automations.Run) gateway.AutomationRun {
	return gateway.AutomationRun{
		RunID: r.RunID, SessionID: r.SessionID, Trigger: string(r.Trigger), Status: string(r.Status),
		StartedAt: rfc3339(r.StartedAt), EndedAt: rfc3339(r.EndedAt), Error: r.Error,
	}
}

func (h *GatewayHost) gatewayItem(it Item) gateway.Automation {
	a := gateway.Automation{
		ID: it.ID, Name: it.Name, Prompt: it.Prompt, Schedule: it.Schedule, Timezone: it.Timezone,
		Model: it.Model, ReasoningEffort: it.ReasoningEffort, Autonomy: it.Autonomy,
		WorkingDirectory: it.WorkingDirectory, Paused: it.Paused, CreatedAt: rfc3339(it.CreatedAt),
		Folder: h.Scheduler.Folder(it.ID), NextRunAt: rfc3339(it.NextRunAt), Running: it.Running, Invalid: it.Invalid,
	}
	if it.LastRun != nil {
		r := gatewayRun(*it.LastRun)
		a.LastRun = &r
	}
	return a
}

// hostError is err as the Gateway answers it.
func hostError(err error) error {
	if errors.Is(err, automations.ErrNotFound) {
		return fmt.Errorf("%w: %v", gateway.ErrAutomationNotFound, err)
	}
	return err
}

func (h *GatewayHost) List(context.Context) ([]gateway.Automation, error) {
	items, err := h.Scheduler.List()
	if err != nil {
		return nil, err
	}
	out := make([]gateway.Automation, 0, len(items))
	for _, it := range items {
		out = append(out, h.gatewayItem(it))
	}
	return out, nil
}

func (h *GatewayHost) Runs(_ context.Context, id string, limit int) ([]gateway.AutomationRun, error) {
	if _, err := h.Scheduler.Get(id); err != nil {
		return nil, hostError(err)
	}
	runs, err := h.Scheduler.Runs(id, limit)
	if err != nil {
		return nil, err
	}
	out := make([]gateway.AutomationRun, 0, len(runs))
	for _, r := range runs {
		out = append(out, gatewayRun(r))
	}
	return out, nil
}

func (h *GatewayHost) Save(_ context.Context, in gateway.Automation) (gateway.Automation, error) {
	a := automations.Automation{
		ID: in.ID, Name: in.Name, Prompt: in.Prompt, Schedule: in.Schedule, Timezone: in.Timezone,
		Model: in.Model, ReasoningEffort: in.ReasoningEffort, Autonomy: in.Autonomy,
		WorkingDirectory: in.WorkingDirectory, Paused: in.Paused,
	}
	if err := automations.Validate(a); err != nil {
		return gateway.Automation{}, &gateway.RefusedRequest{Reason: err.Error()}
	}
	saved, err := h.Scheduler.Save(a)
	if err != nil {
		return gateway.Automation{}, hostError(err)
	}
	return h.get(saved.ID)
}

func (h *GatewayHost) get(id string) (gateway.Automation, error) {
	it, err := h.Scheduler.Get(id)
	if err != nil {
		return gateway.Automation{}, hostError(err)
	}
	return h.gatewayItem(it), nil
}

func (h *GatewayHost) SetPaused(_ context.Context, id string, paused bool) error {
	return hostError(h.Scheduler.SetPaused(id, paused))
}

func (h *GatewayHost) Run(ctx context.Context, id string) (gateway.AutomationRun, error) {
	run, err := h.Scheduler.RunNow(ctx, id)
	if errors.Is(err, ErrRunning) {
		name := id
		if it, gerr := h.Scheduler.Get(id); gerr == nil {
			name = it.Name
		}
		return gateway.AutomationRun{}, &gateway.RefusedRequest{Reason: l10n.L("A run of “%s” is already working.", name)}
	}
	if err != nil {
		return gateway.AutomationRun{}, hostError(err)
	}
	return gatewayRun(run), nil
}

func (h *GatewayHost) Delete(_ context.Context, id string) error {
	return hostError(h.Scheduler.Delete(id))
}

func (h *GatewayHost) factoryEntries(ctx context.Context) (FactoryDaemon, []protocol.AutomationEntry, error) {
	d, err := h.Daemon()
	if err != nil {
		return nil, nil, err
	}
	res, err := d.ListAutomations(ctx, protocol.ListAutomationsParams{})
	if err != nil {
		return nil, nil, err
	}
	return d, res.Automations, nil
}

// factoryAutomation is e with what the Daemon's list leaves out read from
// its HEARTBEAT.md.
func factoryAutomation(e protocol.AutomationEntry) gateway.FactoryAutomation {
	f := gateway.FactoryAutomation{
		ID: e.ID, Name: e.Name, Prompt: e.Prompt, Schedule: e.Schedule, Model: e.Model,
		ReasoningEffort: e.ReasoningEffort, WorkingDirectory: e.WorkingDirectory, Paused: e.Status == "paused", Path: e.Path,
	}
	if hb, err := automations.ReadHeartbeat(e.Path); err == nil {
		if f.Prompt == "" {
			f.Prompt = hb.Prompt
		}
		if f.ReasoningEffort == "" {
			f.ReasoningEffort = hb.ReasoningEffort
		}
		if f.WorkingDirectory == "" {
			f.WorkingDirectory = hb.WorkingDirectory
		}
	}
	return f
}

func (h *GatewayHost) Factory(ctx context.Context) ([]gateway.FactoryAutomation, error) {
	_, entries, err := h.factoryEntries(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gateway.FactoryAutomation, 0, len(entries))
	for _, e := range entries {
		out = append(out, factoryAutomation(e))
	}
	return out, nil
}

// MoveFactory copies a Factory automation into Droi's, its schedule in this
// computer's time when the Automations page has a preset for it, and runs
// it as the Daemon did, without asking; then has the Daemon delete the
// original. A failed delete keeps the copy and says why in DeleteError.
func (h *GatewayHost) MoveFactory(ctx context.Context, id string) (gateway.AutomationMoved, error) {
	d, entries, err := h.factoryEntries(ctx)
	if err != nil {
		return gateway.AutomationMoved{}, err
	}
	var found *protocol.AutomationEntry
	for i := range entries {
		if entries[i].ID == id {
			found = &entries[i]
		}
	}
	if found == nil {
		return gateway.AutomationMoved{}, fmt.Errorf("%w: %s", gateway.ErrAutomationNotFound, id)
	}
	f := factoryAutomation(*found)
	offset := 0
	if h.Offset != nil {
		offset = h.Offset()
	} else {
		_, offset = time.Now().Zone()
	}
	schedule, zone, err := automations.FactorySchedule(f.Schedule, offset)
	if err != nil {
		return gateway.AutomationMoved{}, &gateway.RefusedRequest{Reason: err.Error()}
	}
	copied := automations.Automation{
		Name: f.Name, Prompt: f.Prompt, Schedule: schedule, Timezone: zone, Model: f.Model,
		ReasoningEffort: f.ReasoningEffort, WorkingDirectory: f.WorkingDirectory, Paused: f.Paused,
	}
	if err := automations.Validate(copied); err != nil {
		return gateway.AutomationMoved{}, &gateway.RefusedRequest{Reason: err.Error()}
	}
	saved, err := h.Scheduler.Save(copied)
	if err != nil {
		return gateway.AutomationMoved{}, err
	}
	moved := gateway.AutomationMoved{}
	if moved.Automation, err = h.get(saved.ID); err != nil {
		return gateway.AutomationMoved{}, err
	}
	res, err := d.DeleteAutomation(ctx, protocol.DeleteAutomationParams{AutomationID: id})
	switch {
	case err != nil:
		moved.DeleteError = err.Error()
	case !res.Success && res.Error != "":
		moved.DeleteError = res.Error
	case !res.Success:
		moved.DeleteError = "the Daemon did not delete it"
	}
	return moved, nil
}
