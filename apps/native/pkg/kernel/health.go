package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// healthTimeout bounds one health check.
const healthTimeout = 2 * time.Second

// HTTPClient is the client CheckHealth uses; nil is http.DefaultClient.
var HTTPClient *http.Client

// CheckHealth asks Kernel's GET /health, which needs no token: it answers that
// the sidecar is up and what it runs, which an open port alone does not prove.
func CheckHealth(ctx context.Context, port int) (Health, error) {
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, HealthURL(port), nil)
	if err != nil {
		return Health{}, err
	}
	client := HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return Health{}, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return Health{}, fmt.Errorf("kernel: health answered %d", res.StatusCode)
	}
	var h Health
	if err := json.NewDecoder(res.Body).Decode(&h); err != nil {
		return Health{}, fmt.Errorf("kernel: health: %w", err)
	}
	if !h.OK {
		return h, errors.New("kernel: health says it is not ok")
	}
	return h, nil
}
