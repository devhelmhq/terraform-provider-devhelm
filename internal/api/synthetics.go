package api

// Synthetics (code monitor) calls: bundle upload, runs and waiting.
//
// UploadBundle maps to `POST /bundles`; WaitForRun is the documented doc 42
// §5.4 composition (SSE first, then polling) and the only wait loop in the
// provider.

import (
	"archive/zip"
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// Run phases and outcomes the provider branches on. Responses decode these
// as plain strings (Postel); unknown future values are treated as
// "not finished" / "not passed".
const (
	RunPhaseFinished    = "finished"
	RunOutcomePassed    = "passed"
	RunOutcomeRetryPass = "passed_on_retry"
)

// Wait timing (doc 42 §5.4: 5-minute default wait).
const (
	DefaultRunWait       = 5 * time.Minute
	runPollInitialDelay  = 2 * time.Second
	runPollMaxDelay      = 10 * time.Second
	runEventStreamMaxAge = 4 * time.Minute
)

// UploadBundle sends a file map to `POST /bundles` and returns the server's
// BundleDto (closure `digest`, `uploadDigest`, `files`, `keys`, `size`).
//
// The JSON form (`CreateBundleRequest`) carries UTF-8 text only, so a map
// with any non-UTF-8 file (an image fixture, say) goes as `application/zip`
// with `entrypoint` / `runtimeId` as query parameters. Both forms run the
// same server validation and return the same digest for the same files.
//
// include lists the files that must stay in the closure even though no
// import reaches them (fixtures, data): `CreateBundleRequest.include`,
// or the repeated `include` query parameter on the zip form.
func UploadBundle(ctx context.Context, c *Client, files map[string][]byte, entrypoint, runtimeID string, include []string) (*generated.BundleDto, error) {
	allText := true
	for _, content := range files {
		if !utf8.Valid(content) {
			allText = false
			break
		}
	}
	if allText {
		body := generated.CreateBundleRequest{Files: make(map[string]string, len(files))}
		for p, content := range files {
			body.Files[p] = string(content)
		}
		if entrypoint != "" {
			body.Entrypoint = &entrypoint
		}
		if runtimeID != "" {
			body.RuntimeId = &runtimeID
		}
		if len(include) > 0 {
			body.Include = &include
		}
		return Create[generated.BundleDto](ctx, c, PathBundles, body)
	}

	zipped, err := zipFiles(files)
	if err != nil {
		return nil, fmt.Errorf("building bundle zip: %w", err)
	}
	q := url.Values{}
	if entrypoint != "" {
		q.Set("entrypoint", entrypoint)
	}
	if runtimeID != "" {
		q.Set("runtimeId", runtimeID)
	}
	for _, p := range include {
		q.Add("include", p)
	}
	p := PathBundles
	if len(q) > 0 {
		p += "?" + q.Encode()
	}
	return send[generated.BundleDto](ctx, c, rawRequest{Method: http.MethodPost, Path: p, Body: zipped, ContentType: "application/zip"})
}

func zipFiles(files map[string][]byte) ([]byte, error) {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, p := range paths {
		f, err := w.CreateHeader(&zip.FileHeader{Name: p, Method: zip.Deflate})
		if err != nil {
			return nil, err
		}
		if _, err := f.Write(files[p]); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WaitTimeoutError is returned by WaitForRun when the run hasn't finished
// before the deadline. The run keeps going on the server; WaitForRun never
// cancels it.
type WaitTimeoutError struct {
	RunID string
	Phase string
	URL   string
}

func (e *WaitTimeoutError) Error() string {
	msg := fmt.Sprintf("run %s did not finish in time (phase %s)", e.RunID, e.Phase)
	if e.URL != "" {
		msg += ": " + e.URL
	}
	return msg
}

// RunFinished reports whether a run has reached its terminal phase.
func RunFinished(run *generated.RunDto) bool {
	return run != nil && run.Phase == RunPhaseFinished
}

// RunPassed reports whether a finished run passed. `passed_on_retry` (a
// flaky pass) counts as a pass for gating a publish.
func RunPassed(run *generated.RunDto) bool {
	if run == nil || run.Outcome == nil {
		return false
	}
	return *run.Outcome == RunOutcomePassed || *run.Outcome == RunOutcomeRetryPass
}

// WaitForRun waits for a run to finish and returns it (doc 42 §5.4). It
// listens on `GET /runs/{id}/events` (SSE; the server closes the stream when
// the run finishes) and falls back to polling `GET /runs/{id}` with back-off
// when the stream isn't available. It never throws on a failed run, never
// cancels the run, and returns *WaitTimeoutError when timeout (or the
// context deadline, whichever is first) passes.
func WaitForRun(ctx context.Context, c *Client, runID string, timeout time.Duration) (*generated.RunDto, error) {
	if timeout <= 0 {
		timeout = DefaultRunWait
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	run, err := Get[generated.RunDto](waitCtx, c, RunPath(runID))
	if err != nil {
		return nil, timeoutOr(waitCtx, err, runID, "")
	}
	if RunFinished(run) {
		return run, nil
	}

	// SSE first. A stream that ends (finished run, server-side timeout, or a
	// proxy cutting it) is followed by a GET; anything else falls through
	// to polling.
	if streamErr := c.streamRunEvents(waitCtx, runID); streamErr != nil {
		tflog.Debug(ctx, "run event stream unavailable; polling", map[string]any{"run": runID, "error": streamErr.Error()})
	}
	delay := runPollInitialDelay
	for {
		latest, err := Get[generated.RunDto](waitCtx, c, RunPath(runID))
		if err != nil {
			return nil, timeoutOr(waitCtx, err, runID, run.Phase)
		}
		run = latest
		if RunFinished(run) {
			return run, nil
		}
		waitFor(waitCtx, delay)
		if waitCtx.Err() != nil {
			return nil, &WaitTimeoutError{RunID: runID, Phase: run.Phase, URL: deref(run.Url)}
		}
		delay *= 2
		if delay > runPollMaxDelay {
			delay = runPollMaxDelay
		}
	}
}

// timeoutOr turns an error caused by the wait deadline into a
// WaitTimeoutError; any other error is returned unchanged.
func timeoutOr(ctx context.Context, err error, runID, phase string) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &WaitTimeoutError{RunID: runID, Phase: phase}
	}
	return err
}

// streamRunEvents opens the run's SSE stream and consumes it until the server
// closes it or the context ends. A non-200 answer is returned as an error so
// the caller polls instead.
func (c *Client) streamRunEvents(ctx context.Context, runID string) error {
	streamCtx, cancel := context.WithTimeout(ctx, runEventStreamMaxAge)
	defer cancel()
	u := c.BaseURL + RunEventsPath(runID)
	req, err := http.NewRequestWithContext(streamCtx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	c.setCommonHeaders(req)
	req.Header.Set("Accept", "text/event-stream")
	// The per-request timeout on c.HTTPClient would cut a healthy stream;
	// the stream is bounded by streamCtx instead.
	streamClient := *c.HTTPClient
	streamClient.Timeout = 0
	resp, err := streamClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("event stream returned HTTP %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" && !strings.HasPrefix(ct, "text/event-stream") {
		return fmt.Errorf("event stream returned %s", ct)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	// Event payloads are progress only; the terminal state is read with
	// GET /runs/{id} once the server closes the stream.
	events := 0
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data:") {
			events++
		}
	}
	tflog.Debug(ctx, "run event stream closed", map[string]any{"run": runID, "events": events})
	if err := scanner.Err(); err != nil && streamCtx.Err() == nil {
		return err
	}
	return nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
