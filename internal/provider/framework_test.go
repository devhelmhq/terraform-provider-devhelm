// Package provider's framework_test.go bootstraps an in-process Terraform
// Plugin Framework acceptance harness backed by a `httptest` mock API.
//
// Why this exists
// ---------------
// The framework-level CRUD methods (`Create`, `Read`, `Update`, `Delete`,
// `ImportState`) on every resource were previously only exercised by the
// Python surface-test suite, which:
//
//   - requires the full DevHelm API stack (`make test-up`),
//   - re-builds the provider binary on every run,
//   - measures in seconds per test, not milliseconds.
//
// That coverage is real, but it makes the inner-loop feedback for protocol-
// level bugs (state reads, plan modifications, import wiring, 404 handling)
// painfully slow. The official `terraform-plugin-testing` harness lets us
// run real `terraform plan/apply/import` against a `ProtoV6ProviderFactories`
// pointing at a mock HTTP API — same provider code, no test stack, runs in
// <1s per scenario, hooked into `go test ./...`.
//
// How it works
// ------------
// `newMockAPI` spins up a `httptest.Server` that mounts a register-by-path
// handler map. `newProviderFactories` returns a factory that builds the
// real `DevhelmProvider` configured to point at that server. Tests then
// drive `resource.Test{...}` with a sequence of `TestStep`s.
//
// IMPORTANT: `resource.Test` requires `TF_ACC=1`. Tests SKIP otherwise so a
// regular `go test ./...` doesn't need terraform installed locally. CI sets
// `TF_ACC=1` (and provides `terraform` on PATH) to enable them.
package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// mockAPI is a minimal in-process stand-in for the DevHelm API. Each test
// constructs one, registers handlers per method+path, and tears it down via
// `t.Cleanup`. The handler map is intentionally tiny — we want the harness
// to fail loudly when an unexpected route is hit, since silent fall-through
// has historically masked regressions where a resource quietly issued an
// unintended API call (e.g. Update fires a list call instead of a PATCH).
type mockAPI struct {
	mu       sync.Mutex
	server   *httptest.Server
	handlers map[string]http.HandlerFunc
	calls    []string
}

func newMockAPI(t *testing.T) *mockAPI {
	t.Helper()
	m := &mockAPI{
		handlers: map[string]http.HandlerFunc{},
	}
	m.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Method + " " + r.URL.Path
		m.mu.Lock()
		h, ok := m.handlers[key]
		m.calls = append(m.calls, key)
		m.mu.Unlock()
		if !ok {
			t.Errorf("mockAPI: unexpected request %s — no handler registered", key)
			http.Error(w, "no handler", http.StatusInternalServerError)
			return
		}
		h(w, r)
	}))
	t.Cleanup(m.server.Close)
	return m
}

// Handle registers a handler for `METHOD /path` (exact match). Tests register
// each route they expect to hit. Re-registering the same key replaces the
// previous handler (useful for swapping a 200 → 404 mid-test to simulate
// out-of-band deletion).
func (m *mockAPI) Handle(method, path string, h http.HandlerFunc) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.handlers[method+" "+path] = h
}

// Calls returns a snapshot of every incoming METHOD+path the mock has seen,
// in order. Useful for asserting "Read was called exactly twice during this
// plan/apply cycle".
func (m *mockAPI) Calls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.calls))
	copy(out, m.calls)
	return out
}

// URL returns the base URL of the mock server (e.g. http://127.0.0.1:NNNN).
func (m *mockAPI) URL() string { return m.server.URL }

// jsonResponse writes a 200 OK with a JSON body wrapped in the `{"data": ...}`
// envelope used by `SingleValueResponse[T]`. Most CRUD endpoints use this
// shape.
func jsonResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

// jsonListResponse writes a 200 OK with a `TableResponse[T]` envelope used
// by paginated list endpoints (`{"data": [...], "hasNext": false}`).
func jsonListResponse(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "hasNext": false})
}

// httpError writes a JSON error matching the API's error shape so the
// provider's checkResponse parses it cleanly.
func httpError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"message": message})
}

// readBody decodes the request body into the given destination. On error
// it writes a 400 to the response writer and returns false; callers should
// `return` immediately.
func readBody(w http.ResponseWriter, r *http.Request, dest any) bool {
	if err := json.NewDecoder(r.Body).Decode(dest); err != nil {
		httpError(w, http.StatusBadRequest, fmt.Sprintf("decode body: %v", err))
		return false
	}
	return true
}

// providerConfigBlock is the HCL the test harness prepends to every config
// step. It points the provider at our mock server and supplies fake
// auth/tenant credentials so the Configure step doesn't error.
func providerConfigBlock(baseURL string) string {
	return fmt.Sprintf(`
provider "devhelm" {
  base_url     = %q
  token        = "test-token"
  org_id       = "1"
  workspace_id = "1"
}
`, baseURL)
}

// newProviderFactories returns the `ProtoV6ProviderFactories` map expected
// by `resource.Test{}`. Each call instantiates a fresh `DevhelmProvider`
// so test isolation is preserved (no shared *api.Client between tests).
func newProviderFactories() map[string]func() (tfprotov6.ProviderServer, error) {
	return map[string]func() (tfprotov6.ProviderServer, error){
		"devhelm": providerserver.NewProtocol6WithError(New("test")()),
	}
}

// requireAcc skips the test unless TF_ACC=1 is set. The plugin-testing
// harness depends on a real terraform binary on PATH — running it without
// TF_ACC produces an unhelpful error, so we fast-fail with a clear skip.
//
// (`go test ./...` from a developer laptop should still pass green; CI
// flips TF_ACC=1 in the test workflow to enable these.)
func requireAcc(t *testing.T) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("set TF_ACC=1 (and ensure `terraform` is on PATH) to run framework acceptance tests")
	}
}

// hasTerraformBinary returns true iff `terraform` is invokable. We use this
// to skip with a clear message rather than fail with a cryptic ENOENT when
// TF_ACC=1 is set in an environment without terraform installed.
func hasTerraformBinary() bool {
	for _, p := range strings.Split(os.Getenv("PATH"), string(os.PathListSeparator)) {
		if p == "" {
			continue
		}
		if _, err := os.Stat(p + "/terraform"); err == nil {
			return true
		}
	}
	return false
}

