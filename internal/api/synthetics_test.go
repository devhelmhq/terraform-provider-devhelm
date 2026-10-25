package api

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
)

const bundleReply = `{"data":{"digest":"sha256:c","uploadDigest":"sha256:u","files":["a.ts"],"keys":[],"size":1}}`

func TestUploadBundle_TextGoesAsJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != PathBundles {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content type = %s", ct)
		}
		var body generated.CreateBundleRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Files["a.ts"] != "export {}" || body.Entrypoint == nil || *body.Entrypoint != "a.ts" || *body.RuntimeId != "2026.11" {
			t.Errorf("body = %+v", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(bundleReply))
	}))
	defer srv.Close()
	b, err := UploadBundle(context.Background(), newTestClient(t, srv), map[string][]byte{"a.ts": []byte("export {}")}, "a.ts", "2026.11")
	if err != nil {
		t.Fatal(err)
	}
	if b.Digest != "sha256:c" || b.UploadDigest != "sha256:u" {
		t.Errorf("bundle = %+v", b)
	}
}

func TestUploadBundle_BinaryGoesAsZipWithQueryParams(t *testing.T) {
	bin := []byte{0xff, 0xfe, 0x00}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/zip" {
			t.Errorf("content type = %s", ct)
		}
		if r.URL.Query().Get("entrypoint") != "a.ts" {
			t.Errorf("query = %s", r.URL.RawQuery)
		}
		raw, _ := io.ReadAll(r.Body)
		zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
		if err != nil {
			t.Fatal(err)
		}
		got := map[string][]byte{}
		for _, f := range zr.File {
			rc, _ := f.Open()
			got[f.Name], _ = io.ReadAll(rc)
			_ = rc.Close()
		}
		if !bytes.Equal(got["logo.png"], bin) || string(got["a.ts"]) != "x" {
			t.Errorf("zip = %v", got)
		}
		if r.Header.Get("Idempotency-Key") == "" {
			t.Error("zip upload must carry an Idempotency-Key")
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(bundleReply))
	}))
	defer srv.Close()
	if _, err := UploadBundle(context.Background(), newTestClient(t, srv),
		map[string][]byte{"a.ts": []byte("x"), "logo.png": bin}, "a.ts", ""); err != nil {
		t.Fatal(err)
	}
}

func TestUploadBundle_RefusalIsTypedValidationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(envelope(400, "IMPORT_NOT_IN_BUNDLE", "validation", `"details":{"file":"a.ts","line":1}`)))
	}))
	defer srv.Close()
	_, err := UploadBundle(context.Background(), newTestClient(t, srv), map[string][]byte{"a.ts": []byte("x")}, "a.ts", "")
	var ve *ValidationError
	if !errors.As(err, &ve) || ve.Code != "IMPORT_NOT_IN_BUNDLE" {
		t.Fatalf("err = %v", err)
	}
}

func runJSON(id, phase string, outcome *string) string {
	b, _ := json.Marshal(map[string]any{"data": map[string]any{
		"id": id, "organizationId": 1, "monitorId": "00000000-0000-0000-0000-000000000001",
		"revisionId": "00000000-0000-0000-0000-000000000002", "region": "us-east", "source": "remote_validation",
		"countsTowardHealth": false, "enqueuedAt": "2026-10-25T00:00:00Z", "phase": phase, "cancelRequested": false,
		"bundleDigest": "sha256:c", "capturePolicySnapshot": map[string]any{}, "bindingVersionSnapshot": map[string]any{},
		"stream": phase != "finished", "evidence": []any{}, "tabCounts": map[string]any{}, "attempt": 1, "attemptOf": 1,
		"attempts": []any{}, "executionAttempts": []any{}, "billable": true, "outcome": outcome,
		"url": "https://app.devhelm.io/runs/" + id,
	}})
	return string(b)
}

func TestWaitForRun_UsesEventStreamThenReadsTheRun(t *testing.T) {
	id := "11111111-1111-1111-1111-111111111111"
	passed := "passed"
	var gets, streams int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case RunEventsPath(id):
			atomic.AddInt32(&streams, 1)
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "id: 1\ndata: {\"type\":\"phase\"}\n\n")
		case RunPath(id):
			if atomic.AddInt32(&gets, 1) == 1 {
				_, _ = w.Write([]byte(runJSON(id, "queued", nil)))
				return
			}
			_, _ = w.Write([]byte(runJSON(id, "finished", &passed)))
		default:
			t.Errorf("unexpected %s", r.URL.Path)
		}
	}))
	defer srv.Close()
	run, err := WaitForRun(context.Background(), newTestClient(t, srv), id, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if !RunFinished(run) || !RunPassed(run) {
		t.Errorf("run = %s / %v", run.Phase, run.Outcome)
	}
	if streams != 1 || gets != 2 {
		t.Errorf("streams = %d, gets = %d; want 1 stream then one GET", streams, gets)
	}
}

func TestWaitForRun_FallsBackToPollingAndNeverThrowsOnFailed(t *testing.T) {
	id := "22222222-2222-2222-2222-222222222222"
	failed := "failed"
	var gets int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case RunEventsPath(id):
			w.WriteHeader(http.StatusNotFound)
		case RunPath(id):
			if atomic.AddInt32(&gets, 1) < 3 {
				_, _ = w.Write([]byte(runJSON(id, "running", nil)))
				return
			}
			_, _ = w.Write([]byte(runJSON(id, "finished", &failed)))
		}
	}))
	defer srv.Close()
	run, err := WaitForRun(context.Background(), newTestClient(t, srv), id, 30*time.Second)
	if err != nil {
		t.Fatalf("a failed run is a result, not an error: %v", err)
	}
	if RunPassed(run) || *run.Outcome != "failed" {
		t.Errorf("outcome = %v", run.Outcome)
	}
}

func TestWaitForRun_TimesOutWithPhaseAndNeverCancels(t *testing.T) {
	id := "33333333-3333-3333-3333-333333333333"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case RunEventsPath(id):
			w.WriteHeader(http.StatusServiceUnavailable)
		case RunPath(id):
			_, _ = w.Write([]byte(runJSON(id, "queued", nil)))
		default:
			t.Errorf("unexpected %s %s (wait must never cancel)", r.Method, r.URL.Path)
		}
	}))
	defer srv.Close()
	_, err := WaitForRun(context.Background(), newTestClient(t, srv), id, 1500*time.Millisecond)
	var wt *WaitTimeoutError
	if !errors.As(err, &wt) || wt.RunID != id || wt.Phase != "queued" {
		t.Fatalf("err = %v, want WaitTimeoutError{phase queued}", err)
	}
}

func TestRunPassed_FlakyPassCounts(t *testing.T) {
	for outcome, want := range map[string]bool{"passed": true, "passed_on_retry": true, "failed": false, "execution_error": false} {
		o := outcome
		if got := RunPassed(&generated.RunDto{Phase: "finished", Outcome: &o}); got != want {
			t.Errorf("%s: %v", outcome, got)
		}
	}
}
