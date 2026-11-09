package automationwork

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/kkkk2323/droid-sdk-go/protocol"

	"github.com/kkkk2323/droi/apps/native/v2/internal/automations"
	"github.com/kkkk2323/droi/apps/native/v2/pkg/gateway"
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
	// LocalZone, optional, names this computer's time zone; the package's
	// LocalZone when nil.
	LocalZone func() string
	// Log, optional, hears what the Gateway's answers leave out.
	Log func(string)

	// moving serialises moves, so that one Factory automation is copied
	// once.
	moving sync.Mutex
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

func (h *GatewayHost) offset() int {
	if h.Offset != nil {
		return h.Offset()
	}
	_, offset := time.Now().Zone()
	return offset
}

func (h *GatewayHost) localZone() string {
	if h.LocalZone != nil {
		return h.LocalZone()
	}
	return LocalZone()
}

// Zone is this computer's time zone, which a schedule without one of its
// own is read in.
func (h *GatewayHost) Zone(context.Context) (string, error) { return h.localZone(), nil }

func (h *GatewayHost) gatewayItem(it Item) gateway.Automation {
	a := gateway.Automation{
		ID: it.ID, Name: it.Name, Prompt: it.Prompt, Schedule: it.Schedule, Timezone: it.Timezone,
		Model: it.Model, ReasoningEffort: it.ReasoningEffort, Autonomy: it.Autonomy,
		WorkingDirectory: it.WorkingDirectory, Paused: it.Paused, CreatedAt: rfc3339(it.CreatedAt),
		ForkedFrom: it.ForkedFrom, ForkedFromName: it.ForkedFromName,
		Folder: h.Scheduler.Folder(it.ID), NextRunAt: rfc3339(it.NextRunAt), Running: it.Running, Invalid: it.Invalid,
		Zone: it.Timezone,
	}
	if a.Zone == "" {
		a.Zone = h.localZone()
	}
	if it.LastRun != nil {
		r := gatewayRun(*it.LastRun)
		a.LastRun = &r
	}
	return a
}

// hostError is err as the Gateway answers it: what cannot be read or saved
// as asked is refused with the reason.
func hostError(err error) error {
	var invalid *automations.InvalidError
	switch {
	case err == nil:
		return nil
	case errors.Is(err, automations.ErrNotFound):
		return fmt.Errorf("%w: %v", gateway.ErrAutomationNotFound, err)
	case errors.Is(err, ErrRunning):
		return fmt.Errorf("%w: %v", gateway.ErrAutomationRunning, err)
	case errors.As(err, &invalid), errors.Is(err, automations.ErrUnreadable):
		return &gateway.RefusedRequest{Reason: err.Error()}
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
		return nil, hostError(err)
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
		TargetsSession: e.TargetSessionID != "",
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
	offset := h.offset()
	for _, e := range entries {
		f := factoryAutomation(e)
		// What a move would keep, so that a Client never reads a Host's
		// schedule at its own offset.
		if schedule, zone, err := automations.FactorySchedule(f.Schedule, offset); err == nil {
			f.LocalSchedule, f.LocalTimezone = schedule, zone
		} else {
			f.NotMovable = err.Error()
		}
		out = append(out, f)
	}
	return out, nil
}

// MoveFactory copies a Factory automation into Droi's, with its folder
// (automations.CopyFactoryFolder), its schedule in this computer's time
// when the Automations page has a preset for it, and runs it as the Daemon
// did, without asking; then has the Daemon delete the original. A failed
// delete keeps the copy and says why in DeleteError; moved again, it is
// not copied again, and the delete is tried again. One that posts into an
// existing Session is refused: Droi's runs start their own.
//
// A copy keeps the original's uuid (its folder name when it has none): a
// Factory automation made later may take a deleted one's folder name, and
// the Daemon's delete removes the folder.
func (h *GatewayHost) MoveFactory(ctx context.Context, id string) (gateway.AutomationMoved, error) {
	h.moving.Lock()
	defer h.moving.Unlock()
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
	if f.TargetsSession {
		return gateway.AutomationMoved{}, &gateway.RefusedRequest{Reason: "This automation posts into an existing session, which Droi's automations cannot do."}
	}
	key := cmp.Or(found.Uuid, found.ID)
	moved := gateway.AutomationMoved{}
	if it, ok, err := h.Scheduler.ForkedFrom(key); err != nil {
		return gateway.AutomationMoved{}, err
	} else if ok {
		moved.Automation = h.gatewayItem(it)
	} else if moved.Automation, err = h.copyFactory(f, key); err != nil {
		return gateway.AutomationMoved{}, err
	}
	// By folder name, which every droid takes: found is the entry of that
	// name in the list just read, and has the copy's uuid.
	res, err := d.DeleteAutomation(ctx, protocol.DeleteAutomationParams{AutomationID: found.ID})
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

// copyFactory copies f, whose uuid or folder name is key, into Droi's
// Automations.
func (h *GatewayHost) copyFactory(f gateway.FactoryAutomation, key string) (gateway.Automation, error) {
	schedule, zone, err := automations.FactorySchedule(f.Schedule, h.offset())
	if err != nil {
		return gateway.Automation{}, &gateway.RefusedRequest{Reason: err.Error()}
	}
	copied := automations.Automation{
		Name: f.Name, Prompt: f.Prompt, Schedule: schedule, Timezone: zone, Model: f.Model,
		ReasoningEffort: f.ReasoningEffort, WorkingDirectory: f.WorkingDirectory, Paused: f.Paused,
		ForkedFrom: key, ForkedFromName: f.ID,
	}
	saved, err := h.Scheduler.Adopt(copied, f.Path)
	if errors.Is(err, ErrCopy) {
		// The error names the copy's own folder, which is no one's business.
		if h.Log != nil {
			h.Log("move " + f.ID + ": " + err.Error())
		}
		return gateway.Automation{}, errors.New("Droi could not copy the automation's folder.")
	}
	if err != nil {
		return gateway.Automation{}, hostError(err)
	}
	return h.get(saved.ID)
}
