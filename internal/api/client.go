package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-log/tflog"
)

// defaultHTTPTimeout bounds each individual request, protecting against hung
// connections to the API. It is intentionally generous: we support imports and
// plan dry-runs that may fan out many list calls.
const defaultHTTPTimeout = 60 * time.Second

// requestIDHeader is the response header the API sets on every reply (success
// or error). We thread it through the client so that every DevhelmAPIError
// carries the id, which is the single most useful piece of context for a
// support ticket. See `mono/api/.../RequestCorrelationFilter.java`.
const requestIDHeader = "X-Request-Id"

// Retry tuning for transient 5xx and connection-level failures on idempotent
// verbs (GET, PUT, DELETE — POST is excluded, see isIdempotent below).
//
//   - retryMaxAttempts is total attempts including the first try.
//   - retryBaseDelay is the initial back-off; each subsequent retry doubles
//     the delay and adds jitter (50–150% of the computed value) so a spike
//     of providers retrying simultaneously does not produce a thundering
//     herd against a recovering API.
//
// Five attempts is the doc 42 §5.2 ceiling shared by every surface. Without a
// Retry-After hint the worst-case back-off stays under ~12s (0.5+1+2+4s plus
// jitter), well below the per-request timeout.
const (
	retryMaxAttempts = 5
	// maxRetryAfter caps a server-requested wait (Retry-After) per attempt.
	maxRetryAfter = 60 * time.Second
)

// retryBaseDelay is a var only so tests can shrink it.
var retryBaseDelay = 500 * time.Millisecond

// Header names the provider sends on every call (doc 42 §5.2).
const (
	clientHeader         = "X-DevHelm-Client"
	idempotencyKeyHeader = "Idempotency-Key"
)

type Client struct {
	BaseURL     string
	Token       string
	OrgID       string
	WorkspaceID string
	HTTPClient  *http.Client
	UserAgent   string
	// ClientID is the `X-DevHelm-Client: terraform/<version>` value. The API
	// records it as `createdByKind` on drafts and in the audit log.
	ClientID string
	// SurfaceHeaders is the X-DevHelm-Surface* set sent on every request so
	// the API can attribute usage to the Terraform provider for adoption /
	// version-distribution telemetry. Computed once in NewClient — empty
	// when DEVHELM_TELEMETRY=0 so the user opt-out lives in one env var
	// rather than being threaded through every resource. Wire contract:
	// https://devhelm.io/telemetry.
	SurfaceHeaders map[string]string

	keyTool struct {
		once  sync.Once
		tool  string
		known bool
	}
}

func NewClient(baseURL, token, orgID, workspaceID, version string) *Client {
	return &Client{
		BaseURL:        strings.TrimRight(baseURL, "/"),
		Token:          token,
		OrgID:          orgID,
		WorkspaceID:    workspaceID,
		HTTPClient:     &http.Client{Timeout: defaultHTTPTimeout},
		UserAgent:      fmt.Sprintf("terraform-provider-devhelm/%s", version),
		ClientID:       "terraform/" + version,
		SurfaceHeaders: buildSurfaceHeaders(version),
	}
}

// buildSurfaceHeaders returns the X-DevHelm-Surface* headers for one
// Terraform run. Mirrors the contract documented at
// https://devhelm.io/telemetry; the matching API-side handler lives in
// devhelmhq/mono.
//
// Returns nil when DEVHELM_TELEMETRY=0 so the API receives no surface
// signal at all. The opt-out is intentionally a single env var rather than
// a provider attribute — users opt out once for their workstation / CI,
// not per resource.
//
// Strict equality on "0" so DEVHELM_TELEMETRY=on / true / yes don't
// accidentally read as opt-out.
func buildSurfaceHeaders(version string) map[string]string {
	if strings.TrimSpace(os.Getenv("DEVHELM_TELEMETRY")) == "0" {
		return nil
	}
	return map[string]string{
		"X-DevHelm-Surface":         "tf",
		"X-DevHelm-Surface-Version": version,
		// The provider's go runtime + os/arch fingerprint helps the
		// platform team prioritise binary builds. Cheap to ship — the
		// runtime values are already constants once the binary is built.
		"X-DevHelm-Tf-Os": fmt.Sprintf("%s-%s", runtime.GOOS, runtime.GOARCH),
	}
}

// isIdempotent returns true for HTTP verbs whose retry is safe under the
// REST contract. POST is not in this set: a POST is retried only because it
// always carries an `Idempotency-Key` (see doRequest), which makes the API
// replay the original response instead of creating a duplicate. PATCH has no
// key and is never retried.
func isIdempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

// retryableMethod reports whether a request may be retried at all. POST is
// retryable because doRequest stamps one Idempotency-Key per logical call and
// reuses it on every transport retry (doc 32 D-7).
func retryableMethod(method string) bool {
	return isIdempotent(method) || method == http.MethodPost
}

// shouldRetryStatus identifies HTTP status codes that warrant a retry.
// 429 and 503 are the doc 42 §5.2 contract (both honour `Retry-After`);
// 408, 502 and 504 are transient gateway blips during rolling deploys. 500
// is intentionally NOT retried because it commonly indicates a deterministic
// bug that retrying would only mask. Retryable 409s (`DEPLOY_LOCKED`,
// `IDEMPOTENCY_KEY_IN_PROGRESS`) are decided from the body in shouldRetry.
func shouldRetryStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// shouldRetry extends shouldRetryStatus with the body-driven rule: a 409
// whose ErrorResponse says `retryable: true` is a lock or an in-flight
// idempotent replay, and a later attempt can succeed unchanged.
func shouldRetry(resp httpResponse) bool {
	if shouldRetryStatus(resp.Status) {
		return true
	}
	if resp.Status != http.StatusConflict {
		return false
	}
	var probe struct {
		Retryable bool `json:"retryable"`
	}
	return json.Unmarshal(resp.Body, &probe) == nil && probe.Retryable
}

// retryDelay returns the back-off for the n-th retry (0-indexed). The result
// is `base * 2^n` with ±50% jitter applied via the package-level rand source,
// which is sufficient for back-off jitter (we explicitly do not need crypto-
// strong randomness here).
func retryDelay(attempt int) time.Duration {
	d := retryBaseDelay << attempt
	jitter := time.Duration(rand.Int63n(int64(d)))
	return d/2 + jitter
}

// retryAfter returns the server-requested wait for a retryable response:
// the `Retry-After` header (delta-seconds or HTTP date), else the envelope's
// `retryAfterSeconds`. Zero means "no hint, use back-off". The value is
// capped so a misconfigured header can't stall an apply indefinitely.
func retryAfter(resp httpResponse) time.Duration {
	var d time.Duration
	if h := strings.TrimSpace(resp.RetryAfter); h != "" {
		if secs, err := strconv.Atoi(h); err == nil && secs >= 0 {
			d = time.Duration(secs) * time.Second
		} else if at, err := http.ParseTime(h); err == nil {
			d = time.Until(at)
		}
	}
	if d <= 0 {
		var probe struct {
			RetryAfterSeconds *int64 `json:"retryAfterSeconds"`
		}
		if json.Unmarshal(resp.Body, &probe) == nil && probe.RetryAfterSeconds != nil && *probe.RetryAfterSeconds > 0 {
			d = time.Duration(*probe.RetryAfterSeconds) * time.Second
		}
	}
	if d > maxRetryAfter {
		d = maxRetryAfter
	}
	if d < 0 {
		d = 0
	}
	return d
}

// RequestBody is the P5-tracked boundary type for any request payload that
// `doRequest` will serialize via `json.Marshal`. Callers must pass a typed
// struct from `internal/generated` (or a properly tagged handwritten
// equivalent), never a raw `map[string]any`. The alias is here so a future
// audit can grep for `RequestBody` and find every site that crosses the
// json.Marshal boundary, even though Go's type system can't prevent the
// `map[string]any` case at compile time.
type RequestBody = any

// httpResponse captures everything `doRequest`'s callers need from a single
// successful round trip: the body bytes, the HTTP status, and the per-request
// id from the API's `X-Request-Id` response header. Carrying the request id
// on the value (rather than only on errors) lets us include it in any future
// debug/info diagnostic a resource may want to surface alongside a successful
// reply.
type httpResponse struct {
	Body       []byte
	Status     int
	RequestID  string
	RetryAfter string
}

// rawRequest is one logical API call before retries: the method, the path
// (with query), the encoded body and its media type, and the idempotency key
// shared by every attempt.
type rawRequest struct {
	Method         string
	Path           string
	Body           []byte
	ContentType    string
	IdempotencyKey string
}

func (c *Client) doRequest(ctx context.Context, method, path string, body RequestBody) (httpResponse, error) {
	req := rawRequest{Method: method, Path: path}
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return httpResponse{}, fmt.Errorf("marshaling request body: %w", err)
		}
		req.Body = encoded
		req.ContentType = "application/json"
	}
	return c.do(ctx, req)
}

// do runs one logical call with the retry policy of doc 42 §5.2: at most
// retryMaxAttempts attempts with jittered back-off, honouring `Retry-After`.
// Every POST gets one `Idempotency-Key` (UUIDv4) that is reused on each
// transport retry, so the API replays rather than duplicates the write. Two
// separate calls always get two keys (keys are never derived from the body).
func (c *Client) do(ctx context.Context, req rawRequest) (httpResponse, error) {
	if req.Method == http.MethodPost && req.IdempotencyKey == "" {
		req.IdempotencyKey = uuid.NewString()
	}
	u := c.BaseURL + req.Path

	var (
		resp    httpResponse
		lastErr error
	)

	for attempt := 0; attempt < retryMaxAttempts; attempt++ {
		// Honor caller cancellation between attempts so a Ctrl-C during the
		// retry sleep aborts immediately instead of finishing the back-off.
		if err := ctx.Err(); err != nil {
			return httpResponse{}, err
		}

		resp, lastErr = c.doRequestOnce(ctx, req, u)
		last := attempt == retryMaxAttempts-1

		// Network/transport errors are retryable on idempotent verbs and on
		// keyed POSTs (we cannot distinguish "request never reached the
		// server" from "we missed the response"; the key makes both safe).
		if lastErr != nil {
			if !retryableMethod(req.Method) || last {
				return httpResponse{}, lastErr
			}
			waitFor(ctx, retryDelay(attempt))
			continue
		}

		if !last && retryableMethod(req.Method) && shouldRetry(resp) {
			wait := retryAfter(resp)
			if wait == 0 {
				wait = retryDelay(attempt)
			}
			tflog.Debug(ctx, "retrying DevHelm API request", map[string]any{
				"method": req.Method, "path": req.Path, "status": resp.Status,
				"attempt": attempt + 1, "wait_ms": wait.Milliseconds(),
			})
			waitFor(ctx, wait)
			continue
		}

		return resp, nil
	}

	if lastErr != nil {
		return httpResponse{}, lastErr
	}
	return resp, nil
}

// doRequestOnce performs a single HTTP round trip without retry logic. Split
// from do so the retry loop can call it cleanly.
//
// Network/transport-level failures (request construction, dial, read) are
// wrapped in a *DevhelmTransportError so callers can `errors.As` to tell
// "the API said no" apart from "we never reached the API". Non-2xx responses
// are not errors at this layer — they flow up as a populated httpResponse and
// are converted to typed errors by checkResponse.
func (c *Client) doRequestOnce(ctx context.Context, r rawRequest, u string) (httpResponse, error) {
	var bodyReader io.Reader
	if r.Body != nil {
		bodyReader = bytes.NewReader(r.Body)
	}

	req, err := http.NewRequestWithContext(ctx, r.Method, u, bodyReader)
	if err != nil {
		return httpResponse{}, &DevhelmTransportError{Op: "build request", URL: u, Err: err}
	}

	c.setCommonHeaders(req)
	if r.ContentType != "" {
		req.Header.Set("Content-Type", r.ContentType)
	}
	if r.IdempotencyKey != "" {
		req.Header.Set(idempotencyKeyHeader, r.IdempotencyKey)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return httpResponse{}, &DevhelmTransportError{Op: "send request", URL: u, Err: err}
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return httpResponse{}, &DevhelmTransportError{Op: "read response", URL: u, Err: err}
	}

	return httpResponse{
		Body:       respBody,
		Status:     resp.StatusCode,
		RequestID:  resp.Header.Get(requestIDHeader),
		RetryAfter: resp.Header.Get("Retry-After"),
	}, nil
}

// setCommonHeaders stamps the headers every DevHelm call carries: auth, the
// org/workspace scope, the client identity (`X-DevHelm-Client`, which sets
// `createdByKind` on drafts) and the opt-out-able surface telemetry.
func (c *Client) setCommonHeaders(req *http.Request) {
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("x-phelm-org-id", c.OrgID)
	req.Header.Set("x-phelm-workspace-id", c.WorkspaceID)
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set(clientHeader, c.ClientID)
	// Devtool surface telemetry — see buildSurfaceHeaders. Empty map (and
	// therefore no headers set) when the user has opted out via
	// DEVHELM_TELEMETRY=0.
	for k, v := range c.SurfaceHeaders {
		req.Header.Set(k, v)
	}
}

// waitFor sleeps for d, returning early when the context is cancelled. We
// ignore the context error here; the next iteration of the retry loop checks
// ctx.Err() and surfaces it to the caller.
func waitFor(ctx context.Context, d time.Duration) {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
	case <-ctx.Done():
	}
}

// IsNotFound reports whether err carries an HTTP 404 / `not_found` refusal.
// Used by resources during Read to translate "deleted out-of-band" into the
// disappear-from-state path Terraform expects.
func IsNotFound(err error) bool {
	var apiErr *DevhelmAPIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == http.StatusNotFound
	}
	return false
}

// checkResponse converts a non-2xx reply into the typed error for its
// `category` (see errors.go). 2xx returns nil.
func checkResponse(resp httpResponse) error {
	if resp.Status >= 200 && resp.Status < 300 {
		return nil
	}
	return newAPIError(resp)
}

// decodeResponse unmarshals body into out using a lenient decoder that
// silently ignores unknown fields, then logs the names of any unknown
// top-level keys at WARN so spec drift is visible in `TF_LOG=warn` output
// without breaking apply.
//
// Why lenient: the provider is one client of an evolving REST API. Strict
// decoding (`DisallowUnknownFields`) means every additive API change
// (e.g. a new response field) bricks every released provider version until
// a new release ships. That is the wrong default for a public REST client —
// see the round-3 DevEx report on `currentStatus` for a real-world failure
// mode (a single new response field broke every `terraform apply` against
// prod).
//
// Drift visibility is preserved by walking the body's top-level keys after
// decode and emitting a `tflog.Warn` for any key that is not present on the
// target struct. Operators running with `TF_LOG=warn` will see the same
// signal that strict decoding used to surface as a hard error, but apply
// continues unblocked.
//
// Error bodies stay lenient (see checkResponse).
func decodeResponse(ctx context.Context, body []byte, out any, contextLabel string) error {
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("decoding response (%s): %w", contextLabel, err)
	}
	logUnknownTopLevelKeys(ctx, body, out, contextLabel)
	return nil
}

// logUnknownTopLevelKeys emits a tflog.Warn for any top-level JSON key in
// `body` that is not declared (case-insensitively) on the target struct's
// json tags. Used by decodeResponse to surface API spec drift without
// failing the request.
func logUnknownTopLevelKeys(ctx context.Context, body []byte, target any, contextLabel string) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return
	}
	known := jsonFieldNames(target)
	if len(known) == 0 {
		return
	}
	var unknown []string
	for k := range top {
		if _, ok := known[strings.ToLower(k)]; !ok {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) > 0 {
		tflog.Warn(ctx, "API response contains unknown field(s); ignored by client", map[string]any{
			"context": contextLabel,
			"fields":  unknown,
			"hint":    "this provider version may be older than the API; consider upgrading",
		})
	}
}

// jsonFieldNames returns the lower-cased set of json tag names on `target`'s
// struct fields. Walks one level deep into envelope structs (e.g.
// SingleValueResponse[T] → T) so the keys returned describe the body the
// caller actually cares about.
func jsonFieldNames(target any) map[string]struct{} {
	v := reflect.ValueOf(target)
	for v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	t := v.Type()
	// Envelope unwrap: a single-field struct named SingleValueResponse[…]
	// or TableResponse[…] is just the wire wrapper; descend into the inner
	// type so warnings are emitted against the user-facing DTO fields.
	if t.NumField() == 1 && (strings.HasPrefix(t.Name(), "SingleValueResponse") || strings.HasPrefix(t.Name(), "TableResponse")) {
		return jsonFieldNames(v.Field(0).Addr().Interface())
	}
	out := make(map[string]struct{}, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		tag := t.Field(i).Tag.Get("json")
		if tag == "" || tag == "-" {
			continue
		}
		name, _, _ := strings.Cut(tag, ",")
		if name == "" {
			continue
		}
		out[strings.ToLower(name)] = struct{}{}
	}
	return out
}

// SingleValueResponse is the single-value envelope used by most endpoints.
//
// Note on the zero-value contract: when the upstream JSON is malformed or the
// server returns a 2xx without a `data` field (which the API contract should
// never produce, but is worth being explicit about), `Data` will be the zero
// value of `T` and `Get`/`Create`/`Update`/`Patch` will return a non-nil
// pointer to that zero value. Callers should NOT treat a non-nil return as
// "the resource exists" — they should rely on the per-resource semantic
// invariants instead (e.g. `dto.Id != uuid.Nil` for newly-created resources,
// or `IsNotFound(err)` for explicit 404 handling). Nil-checking `&resp.Data`
// itself is meaningless because the address of a value-typed struct field
// inside a stack-allocated struct is always non-nil.
type SingleValueResponse[T any] struct {
	Data T `json:"data"`
}

// Table response wrapper used by list endpoints. Mirrors the API's full
// pagination envelope. Fields beyond Data/HasNext are unused by the provider
// today; with the lenient decoder they would be ignored regardless, but
// declaring them keeps the drift-warn signal honest (otherwise every list
// response would warn about hasPrev/totalElements/totalPages).
type TableResponse[T any] struct {
	Data          []T    `json:"data"`
	HasNext       bool   `json:"hasNext"`
	HasPrev       bool   `json:"hasPrev,omitempty"`
	TotalElements *int64 `json:"totalElements,omitempty"`
	TotalPages    *int32 `json:"totalPages,omitempty"`
}

func Get[T any](ctx context.Context, c *Client, path string) (*T, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, err
	}

	var envelope SingleValueResponse[T]
	if err := decodeResponse(ctx, resp.Body, &envelope, "GET "+path); err != nil {
		return nil, err
	}
	if err := ValidateDTO(&envelope.Data, "GET "+path); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}

// GetRaw is the escape-hatch variant of Get that also returns the raw
// response body so callers can extract polymorphic / discriminated-union
// fields whose generated Go type loses information during a typed unmarshal
// (e.g. monitor `auth`, where the spec collapsed the oneOf into a base
// `MonitorAuthConfig{Type string}` and only the `type` discriminator survives).
// Use the typed result for everything else; only reach into the raw body for
// the specific field whose round-trip you need to preserve.
func GetRaw[T any](ctx context.Context, c *Client, path string) (*T, []byte, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, nil, err
	}

	var envelope SingleValueResponse[T]
	if err := decodeResponse(ctx, resp.Body, &envelope, "GET "+path); err != nil {
		return nil, nil, err
	}
	if err := ValidateDTO(&envelope.Data, "GET "+path); err != nil {
		return nil, nil, err
	}
	return &envelope.Data, resp.Body, nil
}

func List[T any](ctx context.Context, c *Client, basePath string) ([]T, error) {
	var all []T
	page := 0

	for {
		sep := "?"
		if strings.Contains(basePath, "?") {
			sep = "&"
		}
		path := fmt.Sprintf("%s%spage=%d&size=100", basePath, sep, page)

		resp, err := c.doRequest(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		if err := checkResponse(resp); err != nil {
			return nil, err
		}

		var envelope TableResponse[T]
		if err := decodeResponse(ctx, resp.Body, &envelope, "LIST "+basePath); err != nil {
			return nil, err
		}

		for i := range envelope.Data {
			if err := ValidateDTO(&envelope.Data[i], fmt.Sprintf("LIST %s[%d]", basePath, len(all)+i)); err != nil {
				return nil, err
			}
		}

		all = append(all, envelope.Data...)
		if !envelope.HasNext {
			break
		}
		page++
	}

	return all, nil
}

func Create[T any](ctx context.Context, c *Client, path string, body any) (*T, error) {
	req := rawRequest{Method: http.MethodPost, Path: path}
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshaling request body: %w", err)
		}
		req.Body, req.ContentType = encoded, "application/json"
	}
	return send[T](ctx, c, req)
}

// send runs one request and decodes its SingleValueResponse[T]. Create and
// non-JSON uploads (the bundle zip) share it.
func send[T any](ctx context.Context, c *Client, req rawRequest) (*T, error) {
	resp, err := c.do(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, err
	}
	label := req.Method + " " + req.Path
	var envelope SingleValueResponse[T]
	if err := decodeResponse(ctx, resp.Body, &envelope, label); err != nil {
		return nil, err
	}
	if err := ValidateDTO(&envelope.Data, label); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}

// CreateList POSTs to an endpoint that returns a TableResponse[T] (e.g. the
// tag-management sub-resources on monitors, which return the full collection
// after the mutation rather than a single entity). Use Create when the
// endpoint returns SingleValueResponse[T].
func CreateList[T any](ctx context.Context, c *Client, path string, body any) ([]T, error) {
	resp, err := c.doRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, err
	}

	var envelope TableResponse[T]
	if err := decodeResponse(ctx, resp.Body, &envelope, "POST "+path); err != nil {
		return nil, err
	}
	for i := range envelope.Data {
		if err := ValidateDTO(&envelope.Data[i], fmt.Sprintf("POST %s[%d]", path, i)); err != nil {
			return nil, err
		}
	}
	return envelope.Data, nil
}

// CreateRaw mirrors Create but also returns the raw response body. See GetRaw.
func CreateRaw[T any](ctx context.Context, c *Client, path string, body any) (*T, []byte, error) {
	resp, err := c.doRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, nil, err
	}

	var envelope SingleValueResponse[T]
	if err := decodeResponse(ctx, resp.Body, &envelope, "POST "+path); err != nil {
		return nil, nil, err
	}
	if err := ValidateDTO(&envelope.Data, "POST "+path); err != nil {
		return nil, nil, err
	}
	return &envelope.Data, resp.Body, nil
}

func Update[T any](ctx context.Context, c *Client, path string, body any) (*T, error) {
	resp, err := c.doRequest(ctx, http.MethodPut, path, body)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, err
	}

	var envelope SingleValueResponse[T]
	if err := decodeResponse(ctx, resp.Body, &envelope, "PUT "+path); err != nil {
		return nil, err
	}
	if err := ValidateDTO(&envelope.Data, "PUT "+path); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}

// UpdateRaw mirrors Update but also returns the raw response body. See GetRaw.
func UpdateRaw[T any](ctx context.Context, c *Client, path string, body any) (*T, []byte, error) {
	resp, err := c.doRequest(ctx, http.MethodPut, path, body)
	if err != nil {
		return nil, nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, nil, err
	}

	var envelope SingleValueResponse[T]
	if err := decodeResponse(ctx, resp.Body, &envelope, "PUT "+path); err != nil {
		return nil, nil, err
	}
	if err := ValidateDTO(&envelope.Data, "PUT "+path); err != nil {
		return nil, nil, err
	}
	return &envelope.Data, resp.Body, nil
}

func Patch[T any](ctx context.Context, c *Client, path string, body any) (*T, error) {
	resp, err := c.doRequest(ctx, http.MethodPatch, path, body)
	if err != nil {
		return nil, err
	}
	if err := checkResponse(resp); err != nil {
		return nil, err
	}

	var envelope SingleValueResponse[T]
	if err := decodeResponse(ctx, resp.Body, &envelope, "PATCH "+path); err != nil {
		return nil, err
	}
	if err := ValidateDTO(&envelope.Data, "PATCH "+path); err != nil {
		return nil, err
	}
	return &envelope.Data, nil
}

func Delete(ctx context.Context, c *Client, path string) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// DeleteWithBody issues a DELETE with a JSON request body. Used for endpoints
// that accept a body (e.g. DELETE /monitors/{id}/tags with a list of tag IDs).
func DeleteWithBody(ctx context.Context, c *Client, path string, body any) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, path, body)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

func PathEscape(s string) string {
	return url.PathEscape(s)
}

// PutNoContent issues a PUT whose success response carries no body (204),
// e.g. writing a secret's per-environment value.
func PutNoContent(ctx context.Context, c *Client, path string, body any) error {
	resp, err := c.doRequest(ctx, http.MethodPut, path, body)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// KeyTool returns the tool this provider's API key writes as on
// owner-enforced monitors (`GET /auth/me` → `key.tool`; null means API), and
// whether it could be read. Non-key credentials report false. It is read
// once per provider run.
func (c *Client) KeyTool(ctx context.Context) (string, bool) {
	c.keyTool.once.Do(func() {
		// Only `key` matters here; the rest of AuthMeResponse (plan, rate
		// limits) is ignored so its evolution can't hide the warning.
		me, err := Get[struct {
			Key generated.KeyInfo `json:"key"`
		}](ctx, c, "/api/v1/auth/me")
		if err != nil {
			return
		}
		c.keyTool.known = true
		c.keyTool.tool = ToolAPI
		if me.Key.Tool != nil && *me.Key.Tool != "" {
			c.keyTool.tool = *me.Key.Tool
		}
	})
	return c.keyTool.tool, c.keyTool.known
}
