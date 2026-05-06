package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// monitorTestDTO mimics the shape of MonitorDto used in the round-3 DevEx
// reproducer (`currentStatus` regression). It deliberately omits some fields
// (e.g. `currentStatus`, `pingUrl`) that the API may add over time so we can
// assert that decode tolerates them without breaking apply.
type monitorTestDTO struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Type             string `json:"type"`
	FrequencySeconds int    `json:"frequencySeconds"`
}

// TestCreate_LenientDecode_TolerantOfNewResponseFields is the round-3 DevEx
// regression test. The API added `currentStatus` to the monitor response and
// the previous strict decoder rejected the entire response with
// `json: unknown field "currentStatus"`, blocking every `terraform apply`.
//
// With the lenient decoder this test must succeed and ignore the new field
// without raising an error.
func TestCreate_LenientDecode_TolerantOfNewResponseFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		// Server adds `currentStatus`, `managedBy`, `enabled`, etc. that
		// the test DTO doesn't know about.
		_, _ = w.Write([]byte(`{
			"data": {
				"id": "f8a9959f-c489-4328-9dbf-97112719ec30",
				"name": "test-monitor",
				"type": "HTTP",
				"frequencySeconds": 60,
				"currentStatus": "up",
				"managedBy": "TERRAFORM",
				"enabled": true,
				"pingUrl": "https://probe.devhelm.io/ping/abc",
				"futureField": "future-value"
			}
		}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	got, err := Create[monitorTestDTO](context.Background(), c, "/api/v1/monitors", map[string]any{"name": "test-monitor"})
	if err != nil {
		t.Fatalf("Create unexpectedly failed: %v", err)
	}
	if got.ID != "f8a9959f-c489-4328-9dbf-97112719ec30" {
		t.Errorf("got ID %q, want f8a9959f-…", got.ID)
	}
	if got.Name != "test-monitor" {
		t.Errorf("got Name %q, want test-monitor", got.Name)
	}
	if got.FrequencySeconds != 60 {
		t.Errorf("got FrequencySeconds %d, want 60", got.FrequencySeconds)
	}
}

// TestCreate_LenientDecode_StillRejectsMalformedJSON ensures the lenient
// switch did not weaken the contract for actually broken responses.
func TestCreate_LenientDecode_StillRejectsMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": {"id": "x", "name":`)) // truncated JSON
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := Create[monitorTestDTO](context.Background(), c, "/api/v1/monitors", nil)
	if err == nil {
		t.Fatal("expected decode error on truncated JSON, got nil")
	}
	if !strings.Contains(err.Error(), "decoding response") {
		t.Errorf("error message should mention decode failure: %v", err)
	}
}

// TestJsonFieldNames_EnvelopeUnwrap verifies that the drift-detection helper
// looks through SingleValueResponse[T] envelopes to the inner DTO so the
// drift warning is emitted against the user-facing fields.
func TestJsonFieldNames_EnvelopeUnwrap(t *testing.T) {
	envelope := &SingleValueResponse[monitorTestDTO]{}
	keys := jsonFieldNames(envelope)
	want := []string{"id", "name", "type", "frequencyseconds"}
	for _, w := range want {
		if _, ok := keys[w]; !ok {
			t.Errorf("expected key %q in jsonFieldNames(envelope), got %v", w, keys)
		}
	}
	if _, ok := keys["data"]; ok {
		t.Errorf("envelope key 'data' should not appear after unwrap; got %v", keys)
	}
}

// TestLogUnknownTopLevelKeys_PicksUpDrift sanity-checks the warn helper
// without asserting log output (tflog destination is process-wide).
func TestLogUnknownTopLevelKeys_PicksUpDrift(t *testing.T) {
	body := []byte(`{"id":"x","name":"y","type":"HTTP","frequencySeconds":60,"newField":"surprise"}`)
	target := &monitorTestDTO{}
	// Should not panic, should not error — drift logging is fire-and-forget.
	logUnknownTopLevelKeys(context.Background(), body, target, "test")

	// Sanity-check that the helper considers `newField` unknown by spying
	// on the field-set extractor.
	known := jsonFieldNames(target)
	if _, ok := known["newfield"]; ok {
		t.Error("monitorTestDTO should not declare newField; jsonFieldNames is reporting wrong set")
	}

	// Also confirm a malformed body doesn't crash.
	logUnknownTopLevelKeys(context.Background(), []byte(`not json`), target, "test")
}

// _ uses encoding/json to silence the lint about unused imports when the
// test file is trimmed.
var _ = json.Marshal
