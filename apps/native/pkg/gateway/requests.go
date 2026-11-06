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

// readJSON reads a request's JSON body of up to limit bytes into out; on a
// failure, status and err are the answer: 413 for a larger body, 400 for
// one that is not JSON.
func readJSON(w http.ResponseWriter, r *http.Request, out any, limit int64) (status int, err error) {
	b, err := io.ReadAll(http.MaxBytesReader(w, r.Body, limit))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return http.StatusRequestEntityTooLarge, errors.New("The request is too large")
		}
		return http.StatusBadRequest, err
	}
	if err := json.Unmarshal(b, out); err != nil {
		return http.StatusBadRequest, errors.New("The request is not JSON")
	}
	return 0, nil
}
