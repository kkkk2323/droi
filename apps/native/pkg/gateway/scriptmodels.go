package gateway

import (
	"context"
	"net/http"
	"net/url"
)

// ScriptModelsPath answers which models may call tools from a Script program
// on this Host, to a GET with a ScriptModelList.
const ScriptModelsPath = "/script-models"

// ScriptModelList is where the models that may use Script stand on the
// Host's computer: droid gives Script to a model on Factory's list and
// resolves any other to Direct, whatever a Session asks for.
type ScriptModelList struct {
	// Factory are the models Factory lists, without the ones Settings added.
	Factory []string `json:"factory"`
	// Added are the models Settings → Script models lets use Script too.
	Added []string `json:"added"`
	// Known is whether Factory's list is: a Daemon that has not fetched its
	// feature flags yet leaves it unknown, and Factory then holds none.
	Known bool `json:"known"`
}

// Allows reports that model may use Script: it is on Factory's list or was
// added. A model is not known to be left out unless Known.
func (l ScriptModelList) Allows(model string) (allowed, known bool) {
	for _, m := range l.Added {
		if m == model {
			return true, true
		}
	}
	for _, m := range l.Factory {
		if m == model {
			return true, true
		}
	}
	return false, l.Known
}

func (g *Gateway) answerScriptModels(r *http.Request, q url.Values) (int, any) {
	if g.classify(q.Get(TokenQuery)) == "" {
		return http.StatusUnauthorized, nil
	}
	if g.opts.ScriptModels == nil {
		return http.StatusNotFound, nil
	}
	if r.Method != http.MethodGet {
		return http.StatusMethodNotAllowed, nil
	}
	list := g.opts.ScriptModels()
	if list.Factory == nil {
		list.Factory = []string{}
	}
	if list.Added == nil {
		list.Added = []string{}
	}
	return http.StatusOK, list
}

// ScriptModels are the models that may use Script on the Gateway's
// computer. ErrNotFound is an older Host, which tells nothing of them.
func (c *Client) ScriptModels(ctx context.Context) (ScriptModelList, error) {
	var list ScriptModelList
	err := c.call(ctx, http.MethodGet, ScriptModelsPath, nil, &list)
	return list, err
}
