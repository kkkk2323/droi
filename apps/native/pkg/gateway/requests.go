package gateway

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// RefusedRequest is a request the Host will not carry out as asked, such as
// one without a goal or for a Workstream it does not have. It answers 400
// with its reason; any other error of a Host's feature answers 500.
type RefusedRequest struct{ Reason string }

func (e *RefusedRequest) Error() string { return e.Reason }

// readJSON reads a request's JSON body of up to limit bytes into out.
func readJSON(r *http.Request, out any, limit int64) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, limit))
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return errors.New("The request is not JSON")
	}
	return nil
}
