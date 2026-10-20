package host

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Magpie is a local gateway that runs coding agents on the models of many
// providers behind one endpoint (https://usemagpie.ai). Droi does not run it
// and does not talk to it for a Session: it reads the gateway's model list and
// hands it to the Daemon as droid's custom models, in the Runtime Overlay, so
// the CLI's own settings.json stays as it is and only this Daemon sees them.

const (
	// DefaultMagpieBaseURL is where the gateway listens.
	DefaultMagpieBaseURL = "http://127.0.0.1:3425"
	// MagpieProvider is the droid provider a Magpie model speaks: the gateway
	// translates OpenAI Chat Completions into whatever the model wants.
	MagpieProvider = "generic-chat-completion-api"
	// magpieKey is what a loopback client sends: the gateway takes any key
	// from this computer, and nothing secret is stored for it.
	magpieKey = "magpie"
	// magpieCatalogTTL is how long a model list stands before it is read again.
	magpieCatalogTTL = time.Minute
	// magpieTimeout bounds one read: a localhost call that must not hold up a
	// frame or a Daemon start for long.
	magpieTimeout = 2 * time.Second
	// magpieDefaultOutput is the output limit of a model whose entry does not
	// say one.
	magpieDefaultOutput = 8192
)

// MagpieModalities is the shape of a model's modalities.
type MagpieModalities struct {
	Input []string `json:"input"`
}

// MagpieModel is one model the gateway offers.
type MagpieModel struct {
	// ID is the name the gateway knows it by, "provider/model".
	ID              string           `json:"id"`
	Name            string           `json:"display_name"`
	MaxOutputTokens int              `json:"max_output_tokens"`
	Modalities      MagpieModalities `json:"modalities"`
}

// Images reports that the model takes images.
func (m MagpieModel) Images() bool {
	for _, in := range m.Modalities.Input {
		if in == "image" {
			return true
		}
	}
	return false
}

// MagpieCatalog is what the gateway last told the Host.
type MagpieCatalog struct {
	// BaseURL is the gateway these models came from.
	BaseURL string
	Models  []MagpieModel
	// Err is why the last read failed. Models then keep the last list that was
	// read, so a restart of Magpie does not empty the picker.
	Err string
	// Read is when the last read finished, successfully or not.
	Read time.Time
}

// Running reports that the gateway answered the last read.
func (c MagpieCatalog) Running() bool { return c.Err == "" && !c.Read.IsZero() }

// MagpieBaseURL is the setting, else MAGPIE_BASE_URL, else Magpie's own.
func (h *Host) MagpieBaseURL() string {
	if u := NormalizeMagpieBaseURL(deref(h.Settings.Get().MagpieBaseURL)); u != "" {
		return u
	}
	if u := NormalizeMagpieBaseURL(h.cfg.Env("MAGPIE_BASE_URL")); u != "" {
		return u
	}
	return DefaultMagpieBaseURL
}

// NormalizeMagpieBaseURL is an address as the gateway is called: no spaces, no
// trailing slash, and http:// when what was typed has no scheme.
func NormalizeMagpieBaseURL(raw string) string {
	t := strings.TrimSpace(raw)
	if t == "" {
		return ""
	}
	if !strings.Contains(t, "://") {
		t = "http://" + t
	}
	return strings.TrimSuffix(t, "/")
}

// Magpie is the gateway's model list as the window shows it: what was last
// read, with a read in the background once it is a minute old.
func (h *Host) Magpie() MagpieCatalog {
	base := h.MagpieBaseURL()
	c := h.magpieCached()
	if c.BaseURL != base {
		// Another gateway: its models are not known yet. Whoever changed the
		// address reads them, and a Daemon starting reads them too.
		return MagpieCatalog{BaseURL: base}
	}
	if h.magpieStale() && h.claimBackgroundRead() {
		go h.refreshIfStale()
	}
	return c
}

// claimBackgroundRead takes the one background read a stale catalog needs, so
// the frames that draw the window while it runs do not each start another.
func (h *Host) claimBackgroundRead() bool {
	h.magpieMu.Lock()
	defer h.magpieMu.Unlock()
	if h.magpieReading {
		return false
	}
	h.magpieReading = true
	return true
}

// magpieCached is the last catalog, without asking for a read.
func (h *Host) magpieCached() MagpieCatalog {
	h.magpieMu.Lock()
	defer h.magpieMu.Unlock()
	return h.magpie
}

// magpieStale reports that the list is old enough to be read again, or that the
// address is one nothing was read from.
func (h *Host) magpieStale() bool {
	c := h.magpieCached()
	return c.BaseURL != h.MagpieBaseURL() || c.Read.IsZero() || time.Since(c.Read) > magpieCatalogTTL
}

// RefreshMagpie reads the model list now and reports whether the Daemon has to
// start again: it compares the models it read with the ones the Daemon was
// given, not with the cache, which a background read may have filled in the
// meantime. Reads take turns: one asked for while another runs waits for it and
// reads again.
func (h *Host) RefreshMagpie() bool {
	h.magpieReadMu.Lock()
	defer h.magpieReadMu.Unlock()
	h.readMagpie()
	h.changed()
	return !sameMagpieModels(h.magpieGiven(), h.magpieCached())
}

// magpieGiven is the catalog the running Daemon was started with; the zero
// catalog while none was.
func (h *Host) magpieGiven() MagpieCatalog {
	h.magpieMu.Lock()
	defer h.magpieMu.Unlock()
	return h.magpieGivenCatalog
}

// rememberMagpieGiven records what the Daemon's overlay was built from, so that
// Refresh knows what the Daemon has.
func (h *Host) rememberMagpieGiven(c MagpieCatalog) {
	h.magpieMu.Lock()
	h.magpieGivenCatalog = c
	h.magpieMu.Unlock()
}

// refreshIfStale is the read Magpie() asks for in the background; a read another
// caller finished meanwhile leaves nothing to do.
func (h *Host) refreshIfStale() {
	defer func() {
		h.magpieMu.Lock()
		h.magpieReading = false
		h.magpieMu.Unlock()
	}()
	h.magpieReadMu.Lock()
	defer h.magpieReadMu.Unlock()
	if !h.magpieStale() {
		return
	}
	h.readMagpie()
	h.changed()
}

// readMagpie reads the list and keeps it, success or failure; magpieReadMu is
// held.
func (h *Host) readMagpie() {
	before := h.magpieCached()
	base := h.MagpieBaseURL()
	models, err := ReadMagpieModels(base)
	h.magpieMu.Lock()
	cat := MagpieCatalog{BaseURL: base, Read: time.Now()}
	if err != nil {
		cat.Err = err.Error()
		if before.BaseURL == base {
			cat.Models = before.Models
		}
	} else {
		cat.Models = models
	}
	h.magpie = cat
	h.magpieMu.Unlock()
}

// magpieModels is the catalog to hand the Daemon: the cached one, or a read
// now when nothing has been read yet, since a Daemon starting without the
// models would leave them out of the picker until the next read.
func (h *Host) magpieModels() MagpieCatalog {
	if c := h.Magpie(); !c.Read.IsZero() {
		return c
	}
	h.RefreshMagpie()
	return h.magpieCached()
}

// ReadMagpieModels reads a gateway's model list, in the shape Magpie answers
// the OpenAI models endpoint with. Other services on that port answer
// something else, which is an error and not an empty list.
func ReadMagpieModels(base string) ([]MagpieModel, error) {
	ctx, cancel := context.WithTimeout(context.Background(), magpieTimeout)
	defer cancel()
	url := strings.TrimSuffix(base, "/") + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+magpieKey)
	resp, err := (&http.Client{Timeout: magpieTimeout}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the gateway answered HTTP %d", resp.StatusCode)
	}
	var list struct {
		Data []MagpieModel `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&list); err != nil {
		return nil, err
	}
	if list.Data == nil {
		return nil, errors.New("not a Magpie model list")
	}
	return list.Data, nil
}

// MagpieCustomModels are the catalog's models as droid's customModels entries.
// The ids are Droi's own and do not move when a display name or the order of
// the list does, so a Session keeps the model it was started with.
func MagpieCustomModels(c MagpieCatalog) []any {
	models := append([]MagpieModel(nil), c.Models...)
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	base := strings.TrimSuffix(c.BaseURL, "/") + "/v1"
	out := make([]any, 0, len(models))
	for _, m := range models {
		if strings.TrimSpace(m.ID) == "" {
			continue
		}
		id := MagpieModelID(m.ID)
		entry := map[string]any{
			"id":              id,
			"model":           m.ID,
			"displayName":     MagpieDisplayName(m),
			"baseUrl":         base,
			"apiKey":          magpieKey,
			"provider":        MagpieProvider,
			"maxOutputTokens": magpieOutput(m),
		}
		if !m.Images() {
			entry["noImageSupport"] = true
		}
		out = append(out, entry)
	}
	return out
}

// MagpieModelID is the droid id of a Magpie model. It has to start with
// "custom:", which is the only prefix droid resolves a custom model by: an
// entry without it is listed and then ignored, and the Session quietly runs
// another model. The rest names the gateway's provider and the model, and ends
// in a hash of the gateway's own id, so an id depends on nothing but the model:
// renaming its display name, reordering the catalog or adding a model that
// comes out the same in letters cannot move a Session to another model.
func MagpieModelID(model string) string {
	provider, name, found := strings.Cut(model, "/")
	if !found {
		provider, name = "", model
	}
	slug := magpieSlug(name)
	if slug == "" {
		slug = "model"
	}
	id := "custom:magpie-"
	if p := magpieSlug(provider); p != "" {
		id += p + "-"
	}
	return id + slug + "-" + magpieHash(model)
}

// magpieHash is a short, fixed-length digest of a gateway model id.
func magpieHash(model string) string {
	sum := sha256.Sum256([]byte(model))
	return hex.EncodeToString(sum[:3])
}

// sameMagpieModels reports that two catalogs would give the Daemon the same
// models, so that a refresh which changed nothing need not restart it under
// working Sessions. What the overlay carries of a model counts, not just its
// name: a new output limit or a display name reaches the picker only through a
// restart. A failed read is not a change: its catalog keeps the models before
// it, and the Daemon has those.
func sameMagpieModels(a, b MagpieCatalog) bool {
	if a.BaseURL != b.BaseURL || len(a.Models) != len(b.Models) {
		return false
	}
	seen := make(map[string]magpieModelFacts, len(b.Models))
	for _, m := range b.Models {
		seen[m.ID] = magpieFacts(m)
	}
	for _, m := range a.Models {
		facts, ok := seen[m.ID]
		if !ok || facts != magpieFacts(m) {
			return false
		}
	}
	return true
}

// magpieModelFacts is what the overlay says of a model, with the modalities cut
// to the one thing it uses, so a gateway that lists them in another order is
// not a change.
type magpieModelFacts struct {
	name   string
	output int
	images bool
}

func magpieFacts(m MagpieModel) magpieModelFacts {
	return magpieModelFacts{name: m.Name, output: m.MaxOutputTokens, images: m.Images()}
}

// MagpieDisplayName is what the picker calls a Magpie model.
func MagpieDisplayName(m MagpieModel) string {
	name := strings.TrimSpace(m.Name)
	if name == "" {
		name = m.ID
	}
	return "Magpie · " + name
}

func magpieOutput(m MagpieModel) int {
	if m.MaxOutputTokens > 0 {
		return m.MaxOutputTokens
	}
	return magpieDefaultOutput
}

// magpieSlug lowercases a name and keeps its letters and digits, a run of
// anything else becoming one dash.
func magpieSlug(s string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		default:
			if !dash && b.Len() > 0 {
				b.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}
