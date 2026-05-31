// Package hubclient performs the one and only network call the bridge ever makes:
// a health check against the Scrubadubber Hub's control plane.
//
// Contract (verified against scrubadubber-hub):
//   - The health endpoint is GET /healthz on the control-plane port (8384).
//   - A healthy Hub responds 200 with body {"status":"ok"}.
//   - There is NO health endpoint on the proxy port (8383); do not probe it.
//   - /healthz exists only when the Hub's review and control_api are both enabled,
//     so "unreachable" can mean either the Hub is down or the control API is off.
//     Callers should say so in their user-facing error.
//
// This package never reads request/response payloads, credentials, or anything
// beyond the tiny health JSON. It does not follow redirects (a health endpoint
// has no business redirecting us elsewhere).
package hubclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// maxHealthBody caps how much of the health response we read. The real body is a
// few bytes; this guards against a misconfigured endpoint streaming at us.
const maxHealthBody = 64 << 10 // 64 KiB

// HubStatus is the result of a successful health probe.
type HubStatus struct {
	// Reachable is true iff the control plane answered with a 2xx status.
	Reachable bool
	// Status is the "status" field from the health JSON (e.g. "ok"). It is empty
	// if the body was not JSON or omitted the field; this does not make the Hub
	// unreachable.
	Status string
}

// Check probes the Hub control-plane health endpoint at controlHealthURL
// (typically http://<hub-host>:8384/healthz). The supplied context bounds the
// call; callers should pass a context with the configured timeout.
//
// On success it returns HubStatus{Reachable: true, ...} and a nil error. On any
// failure it returns HubStatus{Reachable: false} and a human-readable error
// describing what went wrong (connection refused, timeout, non-2xx, ...).
func Check(ctx context.Context, controlHealthURL string) (HubStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, controlHealthURL, nil)
	if err != nil {
		return HubStatus{}, fmt.Errorf("building health request for %s: %w", controlHealthURL, err)
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{
		// A health endpoint must not redirect us. Refuse to follow so we never
		// chase traffic to an unexpected host.
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("Hub health endpoint returned an unexpected redirect")
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return HubStatus{}, fmt.Errorf("contacting Hub control plane at %s: %w", controlHealthURL, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxHealthBody))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return HubStatus{}, fmt.Errorf("Hub control plane at %s returned HTTP %d", controlHealthURL, resp.StatusCode)
	}

	// Parse leniently: a 2xx with a non-JSON or field-less body still means the
	// control plane is up. We do not fail a developer over the body's shape.
	var payload struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(body, &payload)

	return HubStatus{Reachable: true, Status: payload.Status}, nil
}
