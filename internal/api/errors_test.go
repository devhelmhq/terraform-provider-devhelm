package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
)

func envelope(status int, code, category string, extra string) string {
	if extra != "" {
		extra = "," + extra
	}
	return fmt.Sprintf(`{"status":%d,"code":%q,"category":%q,"retryable":false,"message":"%s refused","timestamp":1%s}`,
		status, code, category, code, extra)
}

// One test per category: the typed error, the category, and every
// ErrorResponse field reachable through the shared base.
func TestCheckResponse_OneTypedErrorPerCategory(t *testing.T) {
	cases := []struct {
		status   int
		category string
		code     string
		check    func(error) bool
	}{
		{400, CategoryValidation, "FIELD_UNKNOWN", func(e error) bool { var x *ValidationError; return errors.As(e, &x) }},
		{409, CategoryBlocked, "RUN_KEYS_MISSING", func(e error) bool { var x *BlockedError; return errors.As(e, &x) }},
		{401, CategoryAuth, "API_KEY_REVOKED", func(e error) bool { var x *AuthError; return errors.As(e, &x) }},
		{403, CategoryPermission, "SCOPE_REQUIRED", func(e error) bool { var x *PermissionError; return errors.As(e, &x) }},
		{409, CategoryOwnership, "MONITOR_OWNED_BY_OTHER", func(e error) bool { var x *OwnershipError; return errors.As(e, &x) }},
		{402, CategoryPlan, "MONITOR_CODE_CHECK_LIMIT_REACHED", func(e error) bool { var x *PlanLimitError; return errors.As(e, &x) }},
		{402, CategoryQuota, "SYNTHETICS_USAGE_CAPPED", func(e error) bool { var x *QuotaError; return errors.As(e, &x) }},
		{429, CategoryRateLimit, "RATE_LIMITED", func(e error) bool { var x *RateLimitError; return errors.As(e, &x) }},
		{409, CategoryConflict, "REVISION_UNTESTED", func(e error) bool { var x *ConflictError; return errors.As(e, &x) }},
		{409, CategoryConfirmationRequired, "CONFIRMATION_REQUIRED", func(e error) bool { var x *ConfirmationRequiredError; return errors.As(e, &x) }},
		{404, CategoryNotFound, "BUNDLE_NOT_FOUND", func(e error) bool { var x *NotFoundError; return errors.As(e, &x) }},
		{410, CategoryGone, "RUN_EXPIRED", func(e error) bool { var x *GoneError; return errors.As(e, &x) }},
		{503, CategoryPlatform, "RUNTIME_UNAVAILABLE", func(e error) bool { var x *PlatformError; return errors.As(e, &x) }},
	}
	for _, tc := range cases {
		t.Run(tc.category, func(t *testing.T) {
			body := envelope(tc.status, tc.code, tc.category,
				`"requestId":"req_1","next":{"command":"devhelm auth me","docs":"https://docs.devhelm.io/errors/X"},`+
					`"errors":[{"code":"FIELD_UNKNOWN","field":"code.bundle","message":"unknown"}],"owner":"TERRAFORM",`+
					`"retryAfterSeconds":3,"details":{"rolledBackDigest":"sha256:abc"}`)
			err := checkResponse(httpResponse{Status: tc.status, Body: []byte(body)})
			if !tc.check(err) {
				t.Fatalf("err = %T, want the %s error type", err, tc.category)
			}
			base, ok := asAPIError(err)
			if !ok {
				t.Fatalf("errors.As(*DevhelmAPIError) failed for %T", err)
			}
			if base.Category != tc.category || base.Code != tc.code || base.RequestID != "req_1" {
				t.Errorf("base = %+v", base)
			}
			if CategoryOf(err) != tc.category || CodeOf(err) != tc.code {
				t.Errorf("CategoryOf/CodeOf = %s/%s", CategoryOf(err), CodeOf(err))
			}
			if base.NextCommand() != "devhelm auth me" || base.NextDocs() == "" {
				t.Errorf("next = %q / %q", base.NextCommand(), base.NextDocs())
			}
			if fe := base.FieldErrors(); len(fe) != 1 || *fe[0].Field != "code.bundle" {
				t.Errorf("errors[] = %+v", fe)
			}
			var digest string
			if !base.Detail("rolledBackDigest", &digest) || digest != "sha256:abc" {
				t.Errorf("details = %q", digest)
			}
			if base.Response == nil || base.Response.Owner == nil || *base.Response.RetryAfterSeconds != 3 {
				t.Errorf("envelope fields not carried: %+v", base.Response)
			}
		})
	}
}

func TestCheckResponse_LegacyBodyCategoryFromStatus(t *testing.T) {
	for status, want := range map[int]string{
		400: CategoryValidation, 401: CategoryAuth, 402: CategoryPlan, 403: CategoryPermission,
		404: CategoryNotFound, 409: CategoryConflict, 410: CategoryGone, 429: CategoryRateLimit,
		500: CategoryPlatform, 426: CategoryValidation,
	} {
		err := checkResponse(httpResponse{Status: status, Body: []byte(`{"message":"legacy"}`)})
		if got := CategoryOf(err); got != want {
			t.Errorf("status %d: category = %s, want %s", status, got, want)
		}
	}
}

func TestCheckResponse_UnknownCategoryKeepsValueAndClassifiesByStatus(t *testing.T) {
	err := checkResponse(httpResponse{Status: 409, Body: []byte(envelope(409, "NEW_THING", "brand_new", ""))})
	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %T, want *ConflictError by status", err)
	}
	if conflict.Category != "brand_new" {
		t.Errorf("category = %s, want the server's value kept", conflict.Category)
	}
}

func TestTransportErrorCategory(t *testing.T) {
	err := &DevhelmTransportError{Op: "send request", URL: "http://x", Err: errors.New("dial")}
	if CategoryOf(err) != CategoryTransport {
		t.Errorf("category = %s", CategoryOf(err))
	}
}

func TestAddAPIError_SummaryCarriesCategoryCodeAndNextAction(t *testing.T) {
	err := checkResponse(httpResponse{Status: 409, RequestID: "req_9", Body: []byte(envelope(409, "REVISION_UNTESTED", "conflict",
		`"next":{"command":"devhelm monitors revisions test"},"errors":[{"code":"REVISION_UNTESTED","field":"revisionId","message":"no passing test"}]`))})
	var diags diag.Diagnostics
	AddAPIError(&diags, "publish the new code", err, path.Empty())
	if len(diags) != 1 {
		t.Fatalf("diags = %v", diags)
	}
	d := diags[0]
	if d.Summary() != "[conflict] REVISION_UNTESTED: REVISION_UNTESTED refused" {
		t.Errorf("summary = %q", d.Summary())
	}
	for _, want := range []string{"While trying to publish the new code.", "Next: devhelm monitors revisions test", "Request ID: req_9"} {
		if !strings.Contains(d.Detail(), want) {
			t.Errorf("detail missing %q:\n%s", want, d.Detail())
		}
	}
	if wp, ok := d.(diag.DiagnosticWithPath); !ok || !wp.Path().Equal(path.Root("revision_id")) {
		t.Errorf("diagnostic should point at revision_id: %#v", d)
	}
}

func TestAddAPIError_OwnershipHintNamesTheKeyTool(t *testing.T) {
	err := checkResponse(httpResponse{Status: 409, Body: []byte(envelope(409, "MONITOR_OWNED_BY_OTHER", "ownership",
		`"owner":"TERRAFORM","details":{"owner":"TERRAFORM","caller":"API"}`))})
	var diags diag.Diagnostics
	AddAPIError(&diags, "update monitor", err, path.Empty())
	if !strings.Contains(diags[0].Detail(), "writes as API") || !strings.Contains(diags[0].Summary(), "[ownership]") {
		t.Errorf("diag = %s / %s", diags[0].Summary(), diags[0].Detail())
	}
}

func TestDoRequest_HonoursRetryAfterAndRetryable409(t *testing.T) {
	var calls int32
	var gaps []time.Duration
	last := time.Now()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		now := time.Now()
		gaps = append(gaps, now.Sub(last))
		last = now
		switch n {
		case 1:
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
		case 2:
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"code":"DEPLOY_LOCKED","category":"conflict","retryable":true,"message":"locked","retryAfterSeconds":0}`))
		default:
			_, _ = w.Write([]byte(`{"data":{}}`))
		}
	}))
	defer srv.Close()
	resp, err := newTestClient(t, srv).doRequest(context.Background(), http.MethodPost, "/x", map[string]string{})
	if err != nil || resp.Status != 200 {
		t.Fatalf("resp = %d, err = %v", resp.Status, err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
	if gaps[1] < 900*time.Millisecond {
		t.Errorf("Retry-After not honoured: waited %v", gaps[1])
	}
}

func TestDoRequest_NonRetryable409IsNotRetried(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(envelope(409, "LIVE_REVISION_CHANGED", "conflict", "")))
	}))
	defer srv.Close()
	resp, _ := newTestClient(t, srv).doRequest(context.Background(), http.MethodPost, "/x", map[string]string{})
	if resp.Status != 409 || calls != 1 {
		t.Fatalf("status %d after %d calls, want one 409", resp.Status, calls)
	}
}

func TestRequest_SendsClientHeaderOnEveryCall(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("X-DevHelm-Client"); got != "terraform/test" {
			t.Errorf("%s %s: X-DevHelm-Client = %q", r.Method, r.URL.Path, got)
		}
		if r.Method != http.MethodPost && r.Header.Get("Idempotency-Key") != "" {
			t.Errorf("%s must not carry an Idempotency-Key", r.Method)
		}
		_, _ = w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()
	c := newTestClient(t, srv)
	for _, m := range []string{http.MethodGet, http.MethodPut, http.MethodDelete, http.MethodPost} {
		if _, err := c.doRequest(context.Background(), m, "/x", nil); err != nil {
			t.Fatal(err)
		}
	}
}
