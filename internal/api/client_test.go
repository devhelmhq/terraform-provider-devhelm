package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient returns a Client pointing at the given test server with a
// short timeout so any accidental hang fails fast in CI.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c := NewClient(srv.URL, "test-token", "1", "1", "test")
	c.HTTPClient.Timeout = 5 * time.Second
	return c
}

func TestIsIdempotent(t *testing.T) {
	cases := map[string]bool{
		http.MethodGet:     true,
		http.MethodPut:     true,
		http.MethodDelete:  true,
		http.MethodHead:    true,
		http.MethodOptions: true,
		http.MethodPost:    false,
		http.MethodPatch:   false,
	}
	for method, want := range cases {
		if got := isIdempotent(method); got != want {
			t.Errorf("isIdempotent(%q) = %v, want %v", method, got, want)
		}
	}
}

func TestShouldRetryStatus(t *testing.T) {
	retryable := []int{408, 429, 502, 503, 504}
	for _, s := range retryable {
		if !shouldRetryStatus(s) {
			t.Errorf("shouldRetryStatus(%d) = false, want true", s)
		}
	}
	// 500 is intentionally NOT retried (deterministic bug signal).
	nonRetryable := []int{200, 400, 401, 403, 404, 500, 501}
	for _, s := range nonRetryable {
		if shouldRetryStatus(s) {
			t.Errorf("shouldRetryStatus(%d) = true, want false", s)
		}
	}
}

func TestRetryDelay_BoundedAndJittered(t *testing.T) {
	// retryDelay returns base/2 + jitter in [0, base*2^n).
	// Verify it grows roughly exponentially and stays within bounds.
	for attempt := 0; attempt < 4; attempt++ {
		minD := retryBaseDelay << attempt / 2
		maxD := retryBaseDelay<<attempt + retryBaseDelay<<attempt/2
		// Sample a few values; jitter is random but bounded.
		for i := 0; i < 50; i++ {
			d := retryDelay(attempt)
			if d < minD || d > maxD {
				t.Errorf("retryDelay(%d) = %v, want in [%v, %v)", attempt, d, minD, maxD)
			}
		}
	}
}

// TestDoRequest_RetriesOn503ForGet exercises the retry loop on a transient
// 503 followed by a 200, ensuring GET is retried up to retryMaxAttempts.
func TestDoRequest_RetriesOn503ForGet(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"upstream cold"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"ok":true}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	resp, err := c.doRequest(context.Background(), http.MethodGet, "/anything", nil)
	if err != nil {
		t.Fatalf("doRequest: %v", err)
	}
	if resp.Status != 200 {
		t.Errorf("status = %d, want 200", resp.Status)
	}
	if !strings.Contains(string(resp.Body), `"ok":true`) {
		t.Errorf("body = %s, want success payload", resp.Body)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("calls = %d, want 3", got)
	}
}

// TestDoRequest_DoesNotRetryPOST asserts the safety guarantee that POST is
// never retried, even on retryable statuses, to avoid duplicate creates.
func TestDoRequest_DoesNotRetryPOST(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	resp, err := c.doRequest(context.Background(), http.MethodPost, "/things", map[string]string{"x": "y"})
	if err != nil {
		t.Fatalf("doRequest returned err for non-network failure: %v", err)
	}
	if resp.Status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (no retry)", resp.Status)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (POST must never retry)", got)
	}
}

// TestDoRequest_DoesNotRetryNonRetryableStatus confirms that 500 is treated
// as non-transient and surfaces immediately even on idempotent verbs.
func TestDoRequest_DoesNotRetryNonRetryableStatus(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	resp, err := c.doRequest(context.Background(), http.MethodGet, "/anything", nil)
	if err != nil {
		t.Fatalf("doRequest: %v", err)
	}
	if resp.Status != 500 {
		t.Errorf("status = %d, want 500", resp.Status)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1 (500 is not retryable)", got)
	}
}

// TestDoRequest_MaxAttemptsReached exhausts retries and returns the final
// non-2xx response (without an error) so checkResponse can surface it.
func TestDoRequest_MaxAttemptsReached(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	resp, err := c.doRequest(context.Background(), http.MethodGet, "/anything", nil)
	if err != nil {
		t.Fatalf("doRequest: %v", err)
	}
	if resp.Status != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", resp.Status)
	}
	if got, want := atomic.LoadInt32(&calls), int32(retryMaxAttempts); got != want {
		t.Errorf("calls = %d, want %d (retryMaxAttempts)", got, want)
	}
}

// TestDoRequest_HonorsContextCancel ensures a cancelled context aborts mid-
// retry rather than waiting through the full back-off.
func TestDoRequest_HonorsContextCancel(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)

	// Cancel after the first response is in flight; the retry loop should
	// notice the cancellation on its next iteration and bail out without
	// making all retryMaxAttempts requests.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := c.doRequest(ctx, http.MethodGet, "/anything", nil)
	if err == nil {
		// The retry loop may also surface a final non-2xx without error if
		// it raced; in that case ensure we did not spend full back-off.
		if got := atomic.LoadInt32(&calls); got >= int32(retryMaxAttempts) {
			t.Errorf("calls = %d (full retry budget); expected early exit on cancel", got)
		}
		return
	}
	if !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.{DeadlineExceeded,Canceled}", err)
	}
}

// TestDoRequest_RetriesNetworkErrorsOnIdempotent ensures connection-level
// failures (closed listener) are retried on GET. We validate by configuring
// the client to point at a closed port for the first attempts.
func TestDoRequest_RetriesNetworkErrorsOnIdempotent(t *testing.T) {
	// Build a server that returns 200 immediately; we don't actually use
	// it but use its URL after closing to simulate connection failure.
	closed := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	closedURL := closed.URL
	closed.Close() // intentionally close so dials fail

	c := NewClient(closedURL, "t", "1", "1", "test")
	c.HTTPClient.Timeout = 1 * time.Second

	start := time.Now()
	_, err := c.doRequest(context.Background(), http.MethodGet, "/anything", nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("expected network error, got nil")
	}
	// Should have attempted retries (at least 2 backoffs of base/2 jitter ≈ 250ms each).
	if elapsed < retryBaseDelay {
		t.Errorf("elapsed %v < base retry delay %v; retries didn't fire", elapsed, retryBaseDelay)
	}
}

func TestIsNotFound(t *testing.T) {
	if IsNotFound(nil) {
		t.Error("IsNotFound(nil) should be false")
	}
	if IsNotFound(errors.New("boom")) {
		t.Error("IsNotFound(plain error) should be false")
	}
	notFound := &DevhelmAPIError{StatusCode: 404}
	if !IsNotFound(notFound) {
		t.Error("IsNotFound(*DevhelmAPIError{404}) should be true")
	}
	other := &DevhelmAPIError{StatusCode: 500}
	if IsNotFound(other) {
		t.Error("IsNotFound(*DevhelmAPIError{500}) should be false")
	}
}

func TestCheckResponse_ParsesStructuredError(t *testing.T) {
	// `message` field wins over `error`.
	err := checkResponse(httpResponse{Body: []byte(`{"message":"bad slug","error":"validation"}`), Status: 400})
	apiErr, ok := err.(*DevhelmAPIError)
	if !ok {
		t.Fatalf("err type = %T, want *DevhelmAPIError", err)
	}
	if apiErr.Message != "bad slug" {
		t.Errorf("Message = %q, want 'bad slug'", apiErr.Message)
	}
	if !strings.Contains(apiErr.Error(), "bad slug") {
		t.Errorf("Error() = %q, want to contain 'bad slug'", apiErr.Error())
	}
}

func TestCheckResponse_FallsBackToBody(t *testing.T) {
	err := checkResponse(httpResponse{Body: []byte(`<html>oops</html>`), Status: 502})
	apiErr, ok := err.(*DevhelmAPIError)
	if !ok {
		t.Fatalf("err type = %T, want *DevhelmAPIError", err)
	}
	if !strings.Contains(apiErr.Error(), "oops") {
		t.Errorf("Error() = %q, want to contain raw body", apiErr.Error())
	}
}

// TestList_FollowsPagination exercises the List helper across multiple pages
// and ensures the page query parameter is used.
func TestList_FollowsPagination(t *testing.T) {
	type item struct {
		Name string `json:"name"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Query().Get("page") {
		case "0":
			_, _ = w.Write([]byte(`{"data":[{"name":"a"},{"name":"b"}],"hasNext":true}`))
		case "1":
			_, _ = w.Write([]byte(`{"data":[{"name":"c"}],"hasNext":false}`))
		default:
			t.Errorf("unexpected page %q", r.URL.Query().Get("page"))
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := List[item](context.Background(), c, "/things")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("got %d items, want %d", len(got), len(want))
	}
	for i, n := range want {
		if got[i].Name != n {
			t.Errorf("got[%d].Name = %q, want %q", i, got[i].Name, n)
		}
	}
}

// TestGet_DecodesData verifies the SingleValueResponse envelope is unwrapped.
func TestGet_DecodesData(t *testing.T) {
	type item struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"id":"abc","name":"thing"}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := Get[item](context.Background(), c, "/things/abc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != "abc" || got.Name != "thing" {
		t.Errorf("Get returned %+v, want {abc thing}", got)
	}
}

// TestCreate_SendsBodyAndDecodes verifies the Create helper sends JSON and
// unwraps the response.
func TestCreate_SendsBodyAndDecodes(t *testing.T) {
	type req struct {
		Name string `json:"name"`
	}
	type resp struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"name":"new"`) {
			t.Errorf("body = %s, want to contain new name", body)
		}
		_, _ = w.Write([]byte(`{"data":{"id":"123","name":"new"}}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := Create[resp](context.Background(), c, "/things", req{Name: "new"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.ID != "123" {
		t.Errorf("ID = %q, want '123'", got.ID)
	}
}

// TestDelete_ReturnsNotFoundOnce verifies Delete + IsNotFound interplay.
func TestDelete_ReturnsNotFoundOnce(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"message":"not here"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := Delete(context.Background(), c, "/things/missing")
	if err == nil {
		t.Fatal("Delete returned nil error for 404")
	}
	if !IsNotFound(err) {
		t.Errorf("IsNotFound(%v) = false", err)
	}
	// 404 is not retryable; ensure single call.
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("calls = %d, want 1", got)
	}
}

// Sanity: the `_ = fmt.Sprintf("")` keeps fmt imported when no subtest needs
// it; harmless but explicit.
var _ = fmt.Sprintf
