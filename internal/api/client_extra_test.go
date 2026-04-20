package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// ───────────────────────────────────────────────────────────────────────
// Class N — API client gap fill.
//
// Existing client_test.go covers retry policy, GET/POST decoding, basic
// pagination, and not-found mapping. The cases below close the remaining
// gaps the resource code actually exercises: Update (PUT), Patch (PATCH),
// Delete-with-body, 204 no-content responses, header round-tripping, and
// pagination path edge cases (URLs that already carry a query string,
// empty pages).
// ───────────────────────────────────────────────────────────────────────

// TestUpdate_PutsBodyAndDecodes verifies Update sends a PUT with the JSON
// body and unwraps the SingleValueResponse envelope. Resources rely on
// this behavior for almost every Update().
func TestUpdate_PutsBodyAndDecodes(t *testing.T) {
	type updateReq struct {
		Name *string `json:"name,omitempty"`
	}
	type resp struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var capturedBody []byte
	var capturedMethod, capturedCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedCT = r.Header.Get("Content-Type")
		capturedBody, _ = io.ReadAll(r.Body)
		_, _ = w.Write([]byte(`{"data":{"id":"abc","name":"renamed"}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	name := "renamed"
	got, err := Update[resp](context.Background(), c, "/things/abc", updateReq{Name: &name})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if capturedMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", capturedMethod)
	}
	if capturedCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", capturedCT)
	}
	if !strings.Contains(string(capturedBody), `"name":"renamed"`) {
		t.Errorf("body = %s, want to contain renamed", capturedBody)
	}
	if got.ID != "abc" || got.Name != "renamed" {
		t.Errorf("got = %+v, want {abc renamed}", got)
	}
}

// TestPatch_SendsPatchAndDecodes verifies the PATCH wrapper. Used by
// resources that perform partial updates (currently we don't lean on it
// heavily, but it's part of the public surface).
func TestPatch_SendsPatchAndDecodes(t *testing.T) {
	type body struct {
		Enabled *bool `json:"enabled,omitempty"`
	}
	type resp struct {
		Enabled bool `json:"enabled"`
	}
	var captured string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Method
		_, _ = w.Write([]byte(`{"data":{"enabled":false}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	off := false
	got, err := Patch[resp](context.Background(), c, "/things/abc", body{Enabled: &off})
	if err != nil {
		t.Fatalf("Patch: %v", err)
	}
	if captured != http.MethodPatch {
		t.Errorf("method = %q, want PATCH", captured)
	}
	if got.Enabled != false {
		t.Errorf("Enabled = %v, want false", got.Enabled)
	}
}

// TestDelete_NoContent204 confirms an empty 204 body does not produce an
// error. Several resources delete and then drop the response, expecting
// nil even when the server returns no envelope.
func TestDelete_NoContent204(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := Delete(context.Background(), c, "/things/abc"); err != nil {
		t.Fatalf("Delete on 204 returned err: %v", err)
	}
}

// TestDeleteWithBody_SendsBody verifies DELETE with body — the only way
// the API supports bulk monitor-tag removal — sends the JSON payload and
// surfaces 2xx as success.
func TestDeleteWithBody_SendsBody(t *testing.T) {
	type body struct {
		TagIds []string `json:"tagIds"`
	}
	var capturedMethod, capturedCT string
	var capturedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedCT = r.Header.Get("Content-Type")
		capturedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := DeleteWithBody(context.Background(), c, "/monitors/m1/tags", body{TagIds: []string{"t1", "t2"}})
	if err != nil {
		t.Fatalf("DeleteWithBody: %v", err)
	}
	if capturedMethod != http.MethodDelete {
		t.Errorf("method = %q, want DELETE", capturedMethod)
	}
	if capturedCT != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", capturedCT)
	}
	if !strings.Contains(string(capturedBody), `"tagIds":["t1","t2"]`) {
		t.Errorf("body = %s, want to contain tagIds payload", capturedBody)
	}
}

// TestDeleteWithBody_PropagatesAPIError confirms non-2xx responses are
// surfaced as *APIError so callers can branch on IsNotFound and friends
// even on the body-bearing DELETE path.
func TestDeleteWithBody_PropagatesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"message":"tag in use"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := DeleteWithBody(context.Background(), c, "/monitors/m1/tags", map[string]any{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusConflict {
		t.Errorf("StatusCode = %d, want 409", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Error(), "tag in use") {
		t.Errorf("Error() = %q, want to contain server message", apiErr.Error())
	}
}

// TestRequest_SendsAuthAndContextHeaders pins the contract that every
// outbound request carries the bearer token plus the org/workspace
// scoping headers and the User-Agent. These have caused production
// bugs before (e.g. requests landing in the wrong workspace) so we
// assert them explicitly rather than trusting code review.
func TestRequest_SendsAuthAndContextHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "tok-xyz", "org-7", "ws-9", "1.2.3")
	if _, _, err := c.doRequest(context.Background(), http.MethodGet, "/anything", nil); err != nil {
		t.Fatalf("doRequest: %v", err)
	}

	if got.Get("Authorization") != "Bearer tok-xyz" {
		t.Errorf("Authorization = %q, want 'Bearer tok-xyz'", got.Get("Authorization"))
	}
	if got.Get("x-phelm-org-id") != "org-7" {
		t.Errorf("x-phelm-org-id = %q, want 'org-7'", got.Get("x-phelm-org-id"))
	}
	if got.Get("x-phelm-workspace-id") != "ws-9" {
		t.Errorf("x-phelm-workspace-id = %q, want 'ws-9'", got.Get("x-phelm-workspace-id"))
	}
	if !strings.Contains(got.Get("User-Agent"), "terraform-provider-devhelm/1.2.3") {
		t.Errorf("User-Agent = %q, want to contain version", got.Get("User-Agent"))
	}
}

// TestRequest_OmitsContentTypeWhenBodyless guards against accidentally
// declaring a JSON content type on bodyless verbs (some upstreams reject
// those requests).
func TestRequest_OmitsContentTypeWhenBodyless(t *testing.T) {
	var ct string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ct = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, _, err := c.doRequest(context.Background(), http.MethodGet, "/anything", nil); err != nil {
		t.Fatalf("doRequest: %v", err)
	}
	if ct != "" {
		t.Errorf("Content-Type = %q on bodyless GET, want empty", ct)
	}
}

// TestList_AppendsPageWhenURLAlreadyHasQuery exercises the `&` branch of
// List's pagination URL construction. This previously regressed when
// callers started passing filter query strings into List.
func TestList_AppendsPageWhenURLAlreadyHasQuery(t *testing.T) {
	type item struct {
		Name string `json:"name"`
	}
	var capturedURLs []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURLs = append(capturedURLs, r.URL.RequestURI())
		// Always return one item, hasNext=false so we stop after one page.
		_, _ = w.Write([]byte(`{"data":[{"name":"only"}],"hasNext":false}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if _, err := List[item](context.Background(), c, "/things?env=staging"); err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(capturedURLs) != 1 {
		t.Fatalf("captured %d URLs, want 1", len(capturedURLs))
	}
	u := capturedURLs[0]
	if !strings.Contains(u, "env=staging") || !strings.Contains(u, "page=0") || !strings.Contains(u, "size=100") {
		t.Errorf("URL = %q, want to keep env=staging and append page/size", u)
	}
	if strings.Contains(u, "?page=") {
		t.Errorf("URL = %q, want second param appended with '&', not '?'", u)
	}
}

// TestList_EmptyFirstPageIsHandled covers the legitimate case where the
// API returns no results at all. We expect a non-nil zero-length slice
// (callers commonly len()-check), not a panic or a stale slice from a
// previous call.
func TestList_EmptyFirstPageIsHandled(t *testing.T) {
	type item struct {
		Name string `json:"name"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[],"hasNext":false}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := List[item](context.Background(), c, "/things")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %d items, want 0", len(got))
	}
}

// TestCreate_PropagatesStructuredError confirms that 4xx responses with a
// JSON `message` field surface as *APIError carrying the human-readable
// message, which the resource layer relies on for diagnostics.
func TestCreate_PropagatesStructuredError(t *testing.T) {
	type resp struct{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"message":"slug already taken"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := Create[resp](context.Background(), c, "/things", map[string]any{})
	if err == nil {
		t.Fatal("Create returned nil error on 422")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("StatusCode = %d, want 422", apiErr.StatusCode)
	}
	if apiErr.Message != "slug already taken" {
		t.Errorf("Message = %q, want 'slug already taken'", apiErr.Message)
	}
}

// TestUpdate_DecodingErrorIsSurfaced ensures that a 2xx with an
// undecodable body produces a clear error rather than silently returning
// a zero-valued T (which would corrupt state). 2xx + bad body is a
// genuine API contract violation that we want to fail loudly on.
func TestUpdate_DecodingErrorIsSurfaced(t *testing.T) {
	type resp struct{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := Update[resp](context.Background(), c, "/things/abc", map[string]any{})
	if err == nil {
		t.Fatal("Update returned nil error for non-JSON body")
	}
	if !strings.Contains(err.Error(), "decoding") {
		t.Errorf("err = %v, want to mention decoding", err)
	}
}

// TestPathEscape_HandlesSpecialChars guards the wrapper helper used by
// resources that build paths from user-supplied IDs (e.g. tag names that
// contain slashes or spaces). Pure passthrough today, but pinning the
// contract prevents future refactors from regressing it.
func TestPathEscape_HandlesSpecialChars(t *testing.T) {
	cases := map[string]string{
		"simple":     "simple",
		"with space": "with%20space",
		"a/b":        "a%2Fb",
	}
	for in, want := range cases {
		if got := PathEscape(in); got != want {
			t.Errorf("PathEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestList_ReturnsErrorOnDecodeFailure pairs with TestUpdate_DecodingErrorIsSurfaced
// for the list path, which has its own json.Unmarshal site.
func TestList_ReturnsErrorOnDecodeFailure(t *testing.T) {
	type item struct{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":"not an array","hasNext":false}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := List[item](context.Background(), c, "/things")
	if err == nil {
		t.Fatal("List returned nil error for malformed envelope")
	}
}

// TestRetry_SkippedOnPATCH guards the safety contract that PATCH (like
// POST) is never retried because it can carry non-idempotent operations
// in some APIs. The shared isIdempotent already encodes this; this test
// is the end-to-end proof that doRequest honors it.
func TestRetry_SkippedOnPATCH(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, status, err := c.doRequest(context.Background(), http.MethodPatch, "/things", map[string]any{})
	if err != nil {
		t.Fatalf("doRequest: %v", err)
	}
	if status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", status)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (PATCH must not retry)", got)
	}
}

// TestUpdate_RetriesOn503 anchors the policy that PUT is in the
// idempotent retry set; resource updates rely on this for resilience
// during API rolling deploys.
func TestUpdate_RetriesOn503(t *testing.T) {
	type resp struct {
		Name string `json:"name"`
	}
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 2 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(`{"data":{"name":"ok"}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := Update[resp](context.Background(), c, "/things/abc", map[string]any{})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got.Name != "ok" {
		t.Errorf("Name = %q, want 'ok'", got.Name)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("calls = %d, want 2 (one retry then success)", got)
	}
}

// TestSingleValueResponse_ZeroDataDocumentedContract pins the documented
// behavior that a 2xx response without a `data` field decodes into the
// zero value of T. This is what callers fall back on when checking
// dto.Id != uuid.Nil, and accidentally changing it would mask real bugs.
// With ValidateDTO wired into all client methods, a response with missing
// required fields is now properly rejected rather than silently returning
// a zero-valued DTO.
func TestSingleValueResponse_ZeroDataDocumentedContract(t *testing.T) {
	type resp struct {
		Name string `json:"name"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := Get[resp](context.Background(), c, "/things/abc")
	if err == nil {
		t.Fatal("expected validation error for empty response, got nil")
	}
	if !strings.Contains(err.Error(), "name: required field is missing or zero") {
		t.Errorf("expected name validation error, got: %v", err)
	}
}

// helper to keep encoding/json imported for body diagnostics if a test
// later reaches for it without re-importing.
var _ = json.Marshal
