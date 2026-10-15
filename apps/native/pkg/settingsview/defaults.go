package settingsview

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/kkkk2323/droid-sdk-go/controller"

	"github.com/kkkk2323/droi/apps/native/pkg/defaults"
)

// Defaults are the Daemon's Session Defaults as the app last read them.
// Settings and the page that starts a Session share one, so that a change
// saved in one shows in the other.
type Defaults struct {
	Controller *controller.Controller
	// Ctx ends the reads and saves; context.Background when nil.
	Ctx context.Context
	// Update runs fn on the UI thread and draws; it is called from any
	// goroutine.
	Update func(fn func())

	mu     sync.Mutex
	view   *defaults.View
	readAt time.Time
}

func (d *Defaults) ctx() context.Context {
	if d.Ctx == nil {
		return context.Background()
	}
	return d.Ctx
}

// Get is the defaults read last, nil until the first read answers. It reads
// them again a minute after they were, and draws once they come.
func (d *Defaults) Get() *defaults.View {
	d.mu.Lock()
	defer d.mu.Unlock()
	// Before the connection is up the Daemon would refuse the request.
	if time.Since(d.readAt) > time.Minute && d.Controller != nil && d.Controller.Status().Connected {
		d.readAt = time.Now()
		failed := func() {
			d.mu.Lock()
			d.readAt = time.Now().Add(5*time.Second - time.Minute)
			d.mu.Unlock()
		}
		go func() {
			cl, err := d.Controller.Client()
			if err != nil {
				failed()
				return
			}
			res, err := cl.GetDefaultSettings(d.ctx())
			if err != nil {
				failed()
				return
			}
			var raw map[string]any
			b, _ := json.Marshal(res)
			_ = json.Unmarshal(b, &raw)
			view := defaults.ToSessionDefaults(raw)
			d.mu.Lock()
			d.view = &view
			d.mu.Unlock()
			d.Update(func() {})
		}()
	}
	return d.view
}

// Save shows dv patched with p at once and has the Daemon save p; failed
// hears why it did not, on the UI thread. The next Get reads them again.
func (d *Defaults) Save(dv defaults.View, p defaults.Patch, failed func(error)) {
	view := defaults.ApplyPatch(dv, p)
	d.mu.Lock()
	d.view = &view
	d.mu.Unlock()
	go func() {
		cl, err := d.Controller.Client()
		if err == nil {
			err = cl.Call(d.ctx(), "daemon.update_session_defaults", p.Params(), nil)
		}
		d.Update(func() {
			if err != nil && failed != nil {
				failed(err)
			}
			d.mu.Lock()
			d.readAt = time.Time{}
			d.mu.Unlock()
		})
	}()
}
