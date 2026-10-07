// Package drafts is what the composer held when the user left a Session, and
// the first message that travels from the New session page to the Session it
// creates (a port of drafts.ts and pending-prompt.ts). The text survives a
// restart (preferences, per device); images are kept only in memory, as their
// base64 would not fit in the preference file.
package drafts

import (
	"sync"

	"github.com/kkkk2323/droi/apps/native/internal/attachments"
	"github.com/kkkk2323/droi/apps/native/internal/prefs"
)

type Draft struct {
	Text   string
	Images []attachments.Image
}

type Drafts struct {
	store  *prefs.Store
	mu     sync.Mutex
	images map[string][]attachments.Image
}

func New(store *prefs.Store) *Drafts {
	return &Drafts{store: store, images: map[string][]attachments.Image{}}
}

func (d *Drafts) Load(key string) Draft {
	text, _ := d.store.Get(prefs.DraftKey(key))
	d.mu.Lock()
	defer d.mu.Unlock()
	return Draft{Text: text, Images: d.images[key]}
}

func (d *Drafts) Save(key string, draft Draft) {
	d.mu.Lock()
	if len(draft.Images) > 0 {
		d.images[key] = draft.Images
	} else {
		delete(d.images, key)
	}
	d.mu.Unlock()
	if draft.Text != "" {
		d.store.Set(prefs.DraftKey(key), draft.Text)
	} else {
		d.store.Remove(prefs.DraftKey(key))
	}
}

// PendingPrompt is the first message typed on the New session page. It
// travels to the Session view through here, since the Session does not exist
// until the Daemon answers and the view mounts on its own route.
type PendingPrompt struct {
	Text   string
	Images []attachments.Image
}

type Pending struct {
	mu      sync.Mutex
	prompts map[string]PendingPrompt
}

func (p *Pending) Set(sessionID string, prompt PendingPrompt) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.prompts == nil {
		p.prompts = map[string]PendingPrompt{}
	}
	p.prompts[sessionID] = prompt
}

// Take returns the queued message once.
func (p *Pending) Take(sessionID string) (PendingPrompt, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	prompt, ok := p.prompts[sessionID]
	delete(p.prompts, sessionID)
	return prompt, ok
}
