package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// ───────────────────────────────────────────────────────────────────────
// Negative API client tests (Class N)
//
// These tests exercise every error-class the provider's API client layer
// can encounter: HTTP 4xx/5xx statuses, malformed responses, empty
// bodies, and network-level failures. The positive paths (happy-path
// CRUD, pagination, retry logic) live in client_test.go.
// ───────────────────────────────────────────────────────────────────────

// ── 404 Not Found ───────────────────────────────────────────────────────

func TestGet_404_ReturnsNotFoundError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"message":"Monitor not found"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type item struct{ Name string }
	_, err := Get[item](context.Background(), c, "/api/v1/monitors/missing")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !IsNotFound(err) {
		t.Errorf("IsNotFound = false for 404 error: %v", err)
	}
	if !strings.Contains(err.Error(), "Monitor not found") {
		t.Errorf("error should contain API message, got: %v", err)
	}
}

func TestList_404_ReturnsNotFoundError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"message":"not found"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type item struct{ Name string }
	_, err := List[item](context.Background(), c, "/api/v1/things")
	if err == nil {
		t.Fatal("expected error for 404 on list")
	}
	if !IsNotFound(err) {
		t.Errorf("IsNotFound = false")
	}
}

func TestDelete_404_ReturnsNotFoundError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"message":"not found"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := Delete(context.Background(), c, "/api/v1/things/x")
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !IsNotFound(err) {
		t.Errorf("IsNotFound = false for Delete 404")
	}
}

// ── 400 Validation Error ────────────────────────────────────────────────

func TestCreate_400_ReturnsValidationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"message":"name: must not be blank, url: must be a valid URL"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type req struct{ Name string }
	type resp struct{ ID string }
	_, err := Create[resp](context.Background(), c, "/api/v1/monitors", req{})
	if err == nil {
		t.Fatal("expected error for 400")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Message, "name") {
		t.Errorf("error should contain field-level details, got: %q", apiErr.Message)
	}
	if !strings.Contains(apiErr.Message, "url") {
		t.Errorf("error should contain all field errors, got: %q", apiErr.Message)
	}
}

func TestUpdate_400_ReturnsValidationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"message":"frequency_seconds: must be at least 30"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type req struct{ FrequencySeconds int }
	type resp struct{ ID string }
	_, err := Update[resp](context.Background(), c, "/api/v1/monitors/abc", req{FrequencySeconds: 5})
	if err == nil {
		t.Fatal("expected error for 400")
	}
	if !strings.Contains(err.Error(), "frequency_seconds") {
		t.Errorf("error should contain field details, got: %v", err)
	}
}

// ── 409 Conflict ────────────────────────────────────────────────────────

func TestCreate_409_ReturnsConflictError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(409)
		_, _ = w.Write([]byte(`{"message":"slug already exists"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type req struct{ Slug string }
	type resp struct{ ID string }
	_, err := Create[resp](context.Background(), c, "/api/v1/environments", req{Slug: "prod"})
	if err == nil {
		t.Fatal("expected error for 409")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != 409 {
		t.Errorf("StatusCode = %d, want 409", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Message, "slug already exists") {
		t.Errorf("Message = %q, want to contain 'slug already exists'", apiErr.Message)
	}
	if IsNotFound(err) {
		t.Error("409 should not be treated as not-found")
	}
}

// ── 401/403 Auth Errors ────────────────────────────────────────────────

func TestGet_401_ReturnsAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"message":"invalid or expired token"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type item struct{ Name string }
	_, err := Get[item](context.Background(), c, "/api/v1/monitors/abc")
	if err == nil {
		t.Fatal("expected error for 401")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != 401 {
		t.Errorf("StatusCode = %d, want 401", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Message, "token") {
		t.Errorf("Message should mention token, got %q", apiErr.Message)
	}
}

func TestGet_403_ReturnsForbiddenError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"message":"insufficient permissions"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type item struct{ Name string }
	_, err := Get[item](context.Background(), c, "/api/v1/monitors/abc")
	if err == nil {
		t.Fatal("expected error for 403")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != 403 {
		t.Errorf("StatusCode = %d, want 403", apiErr.StatusCode)
	}
	if IsNotFound(err) {
		t.Error("403 should not be treated as not-found")
	}
}

// ── 500 Server Error ────────────────────────────────────────────────────

func TestGet_500_ReturnsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"message":"internal server error"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type item struct{ Name string }
	_, err := Get[item](context.Background(), c, "/api/v1/monitors/abc")
	if err == nil {
		t.Fatal("expected error for 500")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
	}
}

func TestCreate_500_ReturnsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":"unexpected failure"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type req struct{ Name string }
	type resp struct{ ID string }
	_, err := Create[resp](context.Background(), c, "/api/v1/monitors", req{Name: "x"})
	if err == nil {
		t.Fatal("expected error for 500")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type = %T, want *APIError", err)
	}
	if !strings.Contains(apiErr.Error(), "unexpected failure") {
		t.Errorf("error should contain API message, got: %q", apiErr.Error())
	}
}

// ── Malformed JSON Response ────────────────────────────────────────────

func TestGet_MalformedJSON_ReturnsDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{not valid json at all`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type item struct{ Name string }
	_, err := Get[item](context.Background(), c, "/api/v1/monitors/abc")
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if strings.Contains(err.Error(), "API error") {
		t.Errorf("malformed JSON should be a decode error, not API error: %v", err)
	}
	if !strings.Contains(err.Error(), "decoding") {
		t.Errorf("error should mention decoding, got: %v", err)
	}
}

func TestCreate_MalformedJSON_ReturnsDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": {broken`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type req struct{ Name string }
	type resp struct{ ID string }
	_, err := Create[resp](context.Background(), c, "/api/v1/monitors", req{Name: "x"})
	if err == nil {
		t.Fatal("expected error for malformed JSON response")
	}
}

func TestList_MalformedJSON_ReturnsDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type item struct{ Name string }
	_, err := List[item](context.Background(), c, "/api/v1/monitors")
	if err == nil {
		t.Fatal("expected error for malformed JSON list response")
	}
}

// ── Empty Response Body ────────────────────────────────────────────────

func TestGet_EmptyBody_ReturnsDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type item struct{ Name string }
	_, err := Get[item](context.Background(), c, "/api/v1/monitors/abc")
	if err == nil {
		t.Fatal("expected error for empty 200 body")
	}
}

func TestCreate_EmptyBody_ReturnsDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type req struct{ Name string }
	type resp struct{ ID string }
	_, err := Create[resp](context.Background(), c, "/api/v1/monitors", req{Name: "x"})
	if err == nil {
		t.Fatal("expected error for empty 200 body")
	}
}

// ── Empty Non-2xx Body ─────────────────────────────────────────────────

func TestGet_Empty404Body_StillReturnsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type item struct{ Name string }
	_, err := Get[item](context.Background(), c, "/api/v1/monitors/abc")
	if err == nil {
		t.Fatal("expected error for empty 404")
	}
	if !IsNotFound(err) {
		t.Errorf("empty 404 body should still be IsNotFound, got: %v", err)
	}
}

func TestCheckResponse_Empty500Body(t *testing.T) {
	err := checkResponse([]byte{}, 500)
	if err == nil {
		t.Fatal("expected error for 500 with empty body")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != 500 {
		t.Errorf("StatusCode = %d, want 500", apiErr.StatusCode)
	}
}

// ── Timeout / Network Errors ───────────────────────────────────────────

func TestGet_Timeout_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	c.HTTPClient.Timeout = 100 * time.Millisecond
	type item struct{ Name string }
	_, err := Get[item](context.Background(), c, "/api/v1/monitors/abc")
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestCreate_Timeout_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	c.HTTPClient.Timeout = 100 * time.Millisecond
	type req struct{ Name string }
	type resp struct{ ID string }
	_, err := Create[resp](context.Background(), c, "/api/v1/monitors", req{Name: "x"})
	if err == nil {
		t.Fatal("expected timeout error")
	}
}

func TestGet_ClosedServer_ReturnsNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(200)
	}))
	url := srv.URL
	srv.Close()

	c := NewClient(url, "t", "1", "1", "test")
	c.HTTPClient.Timeout = 2 * time.Second
	type item struct{ Name string }
	_, err := Get[item](context.Background(), c, "/api/v1/monitors/abc")
	if err == nil {
		t.Fatal("expected network error for closed server")
	}
}

// ── HTML error body (e.g. nginx 502) ───────────────────────────────────

func TestCheckResponse_HTMLBodyFallsBackToBody(t *testing.T) {
	body := []byte(`<html><body>502 Bad Gateway</body></html>`)
	err := checkResponse(body, 502)
	if err == nil {
		t.Fatal("expected error for 502")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != 502 {
		t.Errorf("StatusCode = %d, want 502", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Error(), "502") {
		t.Errorf("error should contain status code, got: %q", apiErr.Error())
	}
	if !strings.Contains(apiErr.Body, "Bad Gateway") {
		t.Errorf("Body should contain HTML content, got: %q", apiErr.Body)
	}
}

// ── JSON error with only "error" field (no "message") ──────────────────

func TestCheckResponse_UsesErrorFieldWhenNoMessage(t *testing.T) {
	err := checkResponse([]byte(`{"error":"validation failed"}`), 422)
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type = %T, want *APIError", err)
	}
	if apiErr.Message != "validation failed" {
		t.Errorf("Message = %q, want 'validation failed'", apiErr.Message)
	}
}

// ── 2xx with body returns nil error ────────────────────────────────────

func TestCheckResponse_2xxReturnsNil(t *testing.T) {
	for _, code := range []int{200, 201, 204} {
		if err := checkResponse([]byte(`{"data":{}}`), code); err != nil {
			t.Errorf("checkResponse(%d) = %v, want nil", code, err)
		}
	}
}

// ── Multiple status codes: IsNotFound is exclusive to 404 ──────────────

func TestIsNotFound_OnlyFor404(t *testing.T) {
	codes := []int{400, 401, 403, 409, 500, 502, 503}
	for _, code := range codes {
		err := &APIError{StatusCode: code, Message: "something"}
		if IsNotFound(err) {
			t.Errorf("IsNotFound(%d) = true, want false", code)
		}
	}
}

// ── APIError.Error() output format ─────────────────────────────────────

func TestAPIError_ErrorWithMessage(t *testing.T) {
	err := &APIError{StatusCode: 400, Message: "bad request"}
	got := err.Error()
	if !strings.HasPrefix(got, "API error 400:") {
		t.Errorf("Error() = %q, want prefix 'API error 400:'", got)
	}
	if !strings.Contains(got, "bad request") {
		t.Errorf("Error() should contain message, got: %q", got)
	}
}

func TestAPIError_ErrorWithBody(t *testing.T) {
	err := &APIError{StatusCode: 502, Body: "<html>oops</html>"}
	got := err.Error()
	if !strings.Contains(got, "oops") {
		t.Errorf("Error() should contain body when message is empty, got: %q", got)
	}
}

// ── Patch: error handling ──────────────────────────────────────────────

func TestPatch_400_ReturnsValidationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"message":"invalid field value"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type req struct{ Name string }
	type resp struct{ ID string }
	_, err := Patch[resp](context.Background(), c, "/api/v1/things/abc", req{Name: "x"})
	if err == nil {
		t.Fatal("expected error for 400 PATCH")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
	}
}

// ── DeleteWithBody: error handling ────────────────────────────────────

func TestDeleteWithBody_404_ReturnsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"message":"resource not found"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	err := DeleteWithBody(context.Background(), c, "/api/v1/things/abc/tags", map[string]any{"tagIds": []string{}})
	if err == nil {
		t.Fatal("expected error for 404")
	}
	if !IsNotFound(err) {
		t.Errorf("DeleteWithBody 404 should be IsNotFound, got: %v", err)
	}
}

// ── GetRaw / CreateRaw / UpdateRaw: error paths ────────────────────────

func TestGetRaw_404_ReturnsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"message":"not found"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type item struct{ Name string }
	_, _, err := GetRaw[item](context.Background(), c, "/api/v1/monitors/x")
	if err == nil {
		t.Fatal("expected error")
	}
	if !IsNotFound(err) {
		t.Errorf("GetRaw 404 should be IsNotFound")
	}
}

func TestCreateRaw_400_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"message":"invalid"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type req struct{ Name string }
	type resp struct{ ID string }
	_, _, err := CreateRaw[resp](context.Background(), c, "/api/v1/monitors", req{})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestUpdateRaw_500_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"message":"boom"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type req struct{ Name string }
	type resp struct{ ID string }
	_, _, err := UpdateRaw[resp](context.Background(), c, "/api/v1/monitors/x", req{})
	if err == nil {
		t.Fatal("expected error")
	}
}

// ── CreateList: error paths ────────────────────────────────────────────

func TestCreateList_400_ReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(400)
		_, _ = w.Write([]byte(`{"message":"bad tag ids"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type tag struct{ ID string }
	_, err := CreateList[tag](context.Background(), c, "/api/v1/monitors/x/tags", map[string]any{"tagIds": []string{"bad"}})
	if err == nil {
		t.Fatal("expected error")
	}
	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("err type = %T, want *APIError", err)
	}
	if apiErr.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", apiErr.StatusCode)
	}
}

func TestCreateList_MalformedJSON_ReturnsDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{broken`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	type tag struct{ ID string }
	_, err := CreateList[tag](context.Background(), c, "/api/v1/monitors/x/tags", nil)
	if err == nil {
		t.Fatal("expected decode error")
	}
}

// ── PathEscape ─────────────────────────────────────────────────────────

func TestPathEscape_SpecialCharacters(t *testing.T) {
	cases := map[string]string{
		"simple":     "simple",
		"with space": "with%20space",
		"with/slash": "with%2Fslash",
		"a+b":        "a+b",
	}
	for input, want := range cases {
		got := PathEscape(input)
		if got != want {
			t.Errorf("PathEscape(%q) = %q, want %q", input, got, want)
		}
	}
}
