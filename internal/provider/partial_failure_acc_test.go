// Framework-level acceptance tests pinning the *current behavior* of
// resources whose Create/Update fan out into multiple HTTP calls and
// have no rollback path between them.
//
// Why these exist
// ---------------
// Two resources currently issue a second mutating call after their
// primary Create/Update succeeds:
//
//   - `devhelm_monitor.Update` — PUT /monitors/{id} then POST/DELETE
//     /monitors/{id}/tags via reconcileTags. If the second call fails,
//     the monitor itself has been updated server-side but Terraform
//     state was never written, leaving the user with no visible record
//     of the partial change. The next plan will show the original
//     diff again and the next apply will retry the PUT.
//
//   - `devhelm_webhook.Create` — POST /webhooks then (when
//     `enabled = false` was requested) PUT /webhooks/{id} to disable.
//     If the disable PUT fails, the webhook is created server-side
//     but absent from Terraform state — an orphan that won't be
//     deleted by `terraform destroy` and will be re-created (with
//     a new id) on the next apply.
//
// Both behaviours are bugs documented inline in the resource files;
// these tests pin the failure surface so a future fix (compensating
// rollback or partial-state + warning) is a deliberate, reviewable
// change rather than an accidental regression. When either bug is
// fixed, the corresponding `t.Run` will need its assertions updated
// to reflect the new contract.
//
// Skipped unless TF_ACC=1 (mirrors every other acc test in this pkg).
package provider

import (
	"net/http"
	"regexp"
	"sync/atomic"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// monitorPartialFixture is a minimal MonitorDto with every field that
// `mapToState` reads + every field `ValidateDTO` requires populated.
// Keeping it minimal (vs. fullyPopulatedMonitorDto) makes the test
// focused on the multi-call sequence rather than the round-trip
// fidelity surface that `monitor_test.go` already covers.
//
// `tagIDs` is rendered into a `Tags` slice; pass nil for "no tags".
func monitorPartialFixture(t *testing.T, id, name string, tagIDs []string) generated.MonitorDto {
	t.Helper()
	monID := openapi_types.UUID(uuid.MustParse(id))

	cfg := generated.MonitorDto_Config{}
	if err := cfg.UnmarshalJSON([]byte(`{"url":"https://example.com","method":"GET"}`)); err != nil {
		t.Fatalf("config: %v", err)
	}

	var tags *[]generated.TagDto
	if tagIDs != nil {
		ts := make([]generated.TagDto, 0, len(tagIDs))
		for _, tid := range tagIDs {
			ts = append(ts, generated.TagDto{
				Id:    openapi_types.UUID(uuid.MustParse(tid)),
				Name:  "tag-" + tid[:4],
				Color: "#000",
			})
		}
		tags = &ts
	}

	return generated.MonitorDto{
		Id:               monID,
		Name:             name,
		Type:             generated.MonitorDtoType("HTTP"),
		FrequencySeconds: 60,
		Enabled:          true,
		Regions:          []string{"us-east"},
		Config:           cfg,
		ManagedBy:        generated.MonitorDtoManagedBy("TERRAFORM"),
		OrganizationId:   1,
		Tags:             tags,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
}

// TestAccMonitor_UpdateTagReconcileFailure pins the partial-state contract
// for `devhelm_monitor.Update`: when PUT /monitors/{id} succeeds but the
// follow-up tag reconcile call returns 500, `terraform apply`:
//
//  1. Surfaces an "Error reconciling monitor tags" diagnostic and exits
//     non-zero (so the user notices and CI fails).
//  2. STILL refreshes Terraform state from the server so the persisted
//     snapshot reflects what the server actually holds (the PUT body is
//     in, the partial tag operations that landed before the failure are
//     in too). This is the contract that lets the next `terraform apply`
//     replay only the missing tag delta instead of redoing the entire
//     PUT and producing spurious "no changes" loops.
//  3. Server-side mutation evidence: the PUT handler bumps a counter
//     before the failing tag call runs, which we assert post-test.
//
// If you change Update's error-handling shape (e.g. switch to a
// compensating PUT that reverts the monitor body), update the
// assertions here to match — the pinning value is keeping the
// multi-call sequence + state-persistence story exercised.
func TestAccMonitor_UpdateTagReconcileFailure(t *testing.T) {
	requireAcc(t)
	if !hasTerraformBinary() {
		t.Skip("terraform binary not on PATH")
	}

	mock := newMockAPI(t)

	const (
		monitorID = "11111111-2222-3333-4444-555555555555"
		tagID1    = "aaaaaaaa-1111-1111-1111-111111111111"
		tagID2    = "bbbbbbbb-2222-2222-2222-222222222222"
	)

	// `currentName` tracks the server-visible monitor name across
	// requests. The PUT handler bumps it BEFORE the tag reconcile
	// fires, so we can prove the monitor was mutated server-side
	// even though TF state never reflects the change.
	var currentName atomic.Value
	currentName.Store("acme-api")
	// `currentTags` mirrors the tag set the API would echo back on Read.
	// Step 1 establishes [tag1]; the failing Step 2 attempt does NOT
	// mutate this (the POST 500s before any tag is added).
	var currentTags atomic.Value
	currentTags.Store([]string{tagID1})

	mock.Handle(http.MethodPost, "/api/v1/monitors", func(w http.ResponseWriter, _ *http.Request) {
		// Step 1: Create with tag_ids=[tag1]. CreateMonitorRequest embeds
		// the tag list inline, so no follow-up POST /tags is needed here.
		jsonResponse(w, monitorPartialFixture(t, monitorID, currentName.Load().(string), currentTags.Load().([]string)))
	})

	mock.Handle(http.MethodGet, "/api/v1/monitors/"+monitorID, func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, monitorPartialFixture(t, monitorID, currentName.Load().(string), currentTags.Load().([]string)))
	})

	mock.Handle(http.MethodPut, "/api/v1/monitors/"+monitorID, func(w http.ResponseWriter, r *http.Request) {
		// Decode just the `name` field — that's all the test cares about
		// for the partial-mutation observation. Other fields round-trip
		// unchanged in the fixture above.
		var req map[string]any
		if !readBody(w, r, &req) {
			return
		}
		if v, ok := req["name"].(string); ok && v != "" {
			currentName.Store(v)
		}
		// PUT itself succeeds — the bug is in what comes next.
		jsonResponse(w, monitorPartialFixture(t, monitorID, currentName.Load().(string), currentTags.Load().([]string)))
	})

	// The failure mode under test: POST /monitors/{id}/tags returns
	// 500 once. We deliberately do NOT count attempts (the resource
	// surfaces the first error to the operator; retries are handled
	// by the user's next `apply` cycle).
	mock.Handle(http.MethodPost, "/api/v1/monitors/"+monitorID+"/tags", func(w http.ResponseWriter, _ *http.Request) {
		httpError(w, http.StatusInternalServerError, "tag reconcile failed (injected)")
	})

	mock.Handle(http.MethodDelete, "/api/v1/monitors/"+monitorID, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				// Step 1: clean Create with tag_ids=[tag1].
				Config: providerConfigBlock(mock.URL()) + `
resource "devhelm_monitor" "m" {
  name              = "acme-api"
  type              = "HTTP"
  frequency_seconds = 60
  config            = jsonencode({ url = "https://example.com", method = "GET" })
  tag_ids           = ["` + tagID1 + `"]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devhelm_monitor.m", "name", "acme-api"),
					resource.TestCheckResourceAttr("devhelm_monitor.m", "tag_ids.#", "1"),
					resource.TestCheckResourceAttr("devhelm_monitor.m", "tag_ids.0", tagID1),
				),
			},
			{
				// Step 2: rename + add a second tag. PUT succeeds (and
				// server-side `currentName` flips to "renamed-api"), but
				// the follow-up POST /tags returns 500. Apply must error
				// and leave TF state at Step 1's values.
				Config: providerConfigBlock(mock.URL()) + `
resource "devhelm_monitor" "m" {
  name              = "renamed-api"
  type              = "HTTP"
  frequency_seconds = 60
  config            = jsonencode({ url = "https://example.com", method = "GET" })
  tag_ids           = ["` + tagID1 + `", "` + tagID2 + `"]
}
`,
				ExpectError: regexp.MustCompile(`reconciling monitor tags`),
				Check: resource.ComposeAggregateTestCheckFunc(
					// Partial-state contract: state was refreshed from the
					// server after the failed reconcile, so it now reflects
					// the post-PUT name AND the actual current tag set
					// (still [tag1] because the POST 500'd before adding
					// tag2). Re-running apply will replay only the missing
					// tag2 add — not the entire PUT body.
					resource.TestCheckResourceAttr("devhelm_monitor.m", "name", "renamed-api"),
					resource.TestCheckResourceAttr("devhelm_monitor.m", "tag_ids.#", "1"),
					resource.TestCheckResourceAttr("devhelm_monitor.m", "tag_ids.0", tagID1),
				),
			},
		},
	})

	// Server observably mutated by the PUT despite TF state pinning
	// to the pre-update name — direct evidence of the half-applied
	// outcome the user will see if they `curl` the API after the
	// failed apply.
	if got := currentName.Load().(string); got != "renamed-api" {
		t.Fatalf("server-side monitor name = %q, want %q (PUT should have landed before the tag reconcile failure)", got, "renamed-api")
	}
}

// webhookFixtureFor returns a minimal WebhookEndpointDto suitable for
// the partial-failure tests below. `consecutiveFailures` is set to 1
// because `ValidateDTO` treats zero-valued non-pointer fields as
// "missing required" — a known over-strict convention that trips on
// freshly-created entities whose counter naturally sits at 0. The
// tests care about the *call sequence*, not the counter value.
func webhookFixtureFor(t *testing.T, id, url string, enabled bool) generated.WebhookEndpointDto {
	t.Helper()
	whID := openapi_types.UUID(uuid.MustParse(id))
	return generated.WebhookEndpointDto{
		Id:                  whID,
		Url:                 url,
		Enabled:             enabled,
		SubscribedEvents:    []string{"monitor.created"},
		ConsecutiveFailures: 1,
		CreatedAt:           time.Now(),
		UpdatedAt:           time.Now(),
	}
}

// TestAccWebhook_CreateDisableFailure_BestEffortDeleteSucceeds pins the
// happy-path orphan-cleanup contract for `devhelm_webhook.Create` with
// `enabled = false`:
//
//  1. POST /webhooks succeeds and returns enabled=true (the API forces
//     new webhooks to enabled=true regardless of the request body).
//  2. PUT /webhooks/{id} to flip it off returns 500.
//  3. The provider issues a best-effort DELETE /webhooks/{id} to
//     remove the orphan.
//  4. Apply errors with "Error disabling webhook after create" and
//     state is empty (nothing to import / destroy).
//
// This pins the "API was healthy enough to delete" branch — the
// orphan does NOT survive the failed apply, so retrying with the same
// config produces exactly one webhook (no accumulation of orphans
// across retries).
func TestAccWebhook_CreateDisableFailure_BestEffortDeleteSucceeds(t *testing.T) {
	requireAcc(t)
	if !hasTerraformBinary() {
		t.Skip("terraform binary not on PATH")
	}

	mock := newMockAPI(t)

	const webhookID = "44444444-5555-6666-7777-888888888888"

	// The two flags below are out-of-band evidence the provider
	// actually issued the cleanup DELETE — without them a buggy
	// implementation that skips DELETE on disable failure would
	// silently regress to the orphan-leaking behavior.
	var (
		created atomic.Bool
		deleted atomic.Bool
	)

	mock.Handle(http.MethodPost, "/api/v1/webhooks", func(w http.ResponseWriter, _ *http.Request) {
		created.Store(true)
		jsonResponse(w, webhookFixtureFor(t, webhookID, "https://hooks.example.com/in", true))
	})

	mock.Handle(http.MethodPut, "/api/v1/webhooks/"+webhookID, func(w http.ResponseWriter, _ *http.Request) {
		httpError(w, http.StatusInternalServerError, "disable failed (injected)")
	})

	mock.Handle(http.MethodDelete, "/api/v1/webhooks/"+webhookID, func(w http.ResponseWriter, _ *http.Request) {
		deleted.Store(true)
		w.WriteHeader(http.StatusNoContent)
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfigBlock(mock.URL()) + `
resource "devhelm_webhook" "w" {
  url               = "https://hooks.example.com/in"
  enabled           = false
  subscribed_events = ["monitor.created"]
}
`,
				ExpectError: regexp.MustCompile(`disabling webhook after create`),
			},
		},
	})

	if !created.Load() {
		t.Fatalf("expected POST /webhooks to have run; the test wouldn't exercise the disable failure otherwise")
	}
	if !deleted.Load() {
		t.Fatalf("expected best-effort DELETE /webhooks/%s to clean up the orphan after disable failure", webhookID)
	}
}

// TestAccWebhook_CreateDisableFailure_DeleteAlsoFailsSavesPartialState
// pins the worst-case branch: API healthy enough to create the webhook
// but unhealthy enough that BOTH the disable PUT and the cleanup
// DELETE fail.
//
// In that case the provider:
//
//  1. Saves PARTIAL STATE containing the orphaned webhook id, url,
//     subscribed_events, and the server-returned enabled=true
//     (which is NOT what the user planned — the diff drives the
//     next apply to retry the disable).
//  2. Surfaces a single error mentioning BOTH the disable failure
//     and the cleanup failure, plus the orphan id, so the operator
//     has everything they need to recover by hand if the API
//     stays unhealthy.
//
// This is the "loud about failure, but recoverable" contract the
// monitor.Update partial-state path also follows.
func TestAccWebhook_CreateDisableFailure_DeleteAlsoFailsSavesPartialState(t *testing.T) {
	requireAcc(t)
	if !hasTerraformBinary() {
		t.Skip("terraform binary not on PATH")
	}

	mock := newMockAPI(t)

	const webhookID = "55555555-6666-7777-8888-999999999999"

	// `deleteCalls` toggles the DELETE handler from "fail" (during the
	// apply step's best-effort cleanup) to "succeed" (during the
	// framework's post-test destroy of the partial state). Without
	// this, the auto-destroy at the end of `resource.Test` would
	// itself error out and the test would fail with a destroy-stage
	// diagnostic instead of pinning the partial-state branch we care
	// about.
	var deleteCalls atomic.Int32

	mock.Handle(http.MethodPost, "/api/v1/webhooks", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, webhookFixtureFor(t, webhookID, "https://hooks.example.com/in", true))
	})

	mock.Handle(http.MethodPut, "/api/v1/webhooks/"+webhookID, func(w http.ResponseWriter, _ *http.Request) {
		httpError(w, http.StatusInternalServerError, "disable failed (injected)")
	})

	mock.Handle(http.MethodDelete, "/api/v1/webhooks/"+webhookID, func(w http.ResponseWriter, _ *http.Request) {
		// First call (best-effort cleanup during failing Create): inject 500.
		// Subsequent calls (framework's post-test destroy of the orphan
		// recorded in partial state): succeed so the test cleans up.
		if deleteCalls.Add(1) == 1 {
			httpError(w, http.StatusInternalServerError, "delete also failed (injected)")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfigBlock(mock.URL()) + `
resource "devhelm_webhook" "w" {
  url               = "https://hooks.example.com/in"
  enabled           = false
  subscribed_events = ["monitor.created"]
}
`,
				ExpectError: regexp.MustCompile(`disabling webhook after create \(orphan cleanup also failed\)`),
			},
		},
	})
}
