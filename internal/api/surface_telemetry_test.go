package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
)

// captureHeaders spins up a tiny test server, fires one GET through the
// client, and returns the headers the server saw. Lets us assert on the
// outbound wire shape without coupling to the openapi-fetch internals.
func captureHeaders(t *testing.T, version string) http.Header {
	t.Helper()
	var captured http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-token", "1", "1", version)
	_, err := c.doRequest(context.Background(), http.MethodGet, "/api/v1/_", nil)
	if err != nil {
		t.Fatalf("doRequest: %v", err)
	}
	if captured == nil {
		t.Fatal("server never saw a request")
	}
	return captured
}

func TestSurfaceTelemetryHeaders_DefaultEmits(t *testing.T) {
	t.Setenv("DEVHELM_TELEMETRY", "")
	headers := captureHeaders(t, "9.9.9")

	if got := headers.Get("X-Devhelm-Surface"); got != "tf" {
		t.Errorf("X-DevHelm-Surface = %q, want %q", got, "tf")
	}
	if got := headers.Get("X-Devhelm-Surface-Version"); got != "9.9.9" {
		t.Errorf("X-DevHelm-Surface-Version = %q, want %q", got, "9.9.9")
	}
	wantOS := runtime.GOOS + "-" + runtime.GOARCH
	if got := headers.Get("X-Devhelm-Tf-Os"); got != wantOS {
		t.Errorf("X-DevHelm-Tf-Os = %q, want %q", got, wantOS)
	}

	// Sanity: existing auth + tenant headers are unchanged.
	if got := headers.Get("Authorization"); got != "Bearer test-token" {
		t.Errorf("Authorization = %q, want Bearer test-token", got)
	}
	if got := headers.Get("X-Phelm-Org-Id"); got != "1" {
		t.Errorf("x-phelm-org-id = %q, want 1", got)
	}
}

func TestSurfaceTelemetryHeaders_OptOut(t *testing.T) {
	t.Setenv("DEVHELM_TELEMETRY", "0")
	headers := captureHeaders(t, "9.9.9")

	for _, h := range []string{"X-Devhelm-Surface", "X-Devhelm-Surface-Version", "X-Devhelm-Tf-Os"} {
		if got := headers.Get(h); got != "" {
			t.Errorf("expected %s to be unset under DEVHELM_TELEMETRY=0, got %q", h, got)
		}
	}
	// Auth + tenant must still be there — opt-out is for telemetry only.
	if got := headers.Get("Authorization"); got != "Bearer test-token" {
		t.Errorf("Authorization stripped under opt-out: got %q", got)
	}
}

func TestSurfaceTelemetryHeaders_StrictEqualityOnZero(t *testing.T) {
	// Only the literal "0" disables. Any other value (including "on",
	// "true", "false", "1") leaves telemetry on. Avoids surprising users
	// who set DEVHELM_TELEMETRY=on expecting it to mean "yes please".
	for _, value := range []string{"1", "on", "true", "false", "yes", " "} {
		t.Run("value="+value, func(t *testing.T) {
			t.Setenv("DEVHELM_TELEMETRY", value)
			headers := captureHeaders(t, "x.y.z")
			if got := headers.Get("X-Devhelm-Surface"); got != "tf" {
				t.Errorf("DEVHELM_TELEMETRY=%q dropped surface header (got %q)", value, got)
			}
		})
	}
}
