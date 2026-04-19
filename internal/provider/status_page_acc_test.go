// Framework-level acceptance tests for `devhelm_status_page`.
//
// Status page is the most state-rich resource in the provider — name,
// slug, visibility, incident_mode, enabled, and a nested branding object
// each have their own update path. These tests pin the framework-side
// CRUD wiring with a mock API; the wire-level branding semantics (omit
// vs. empty-object vs. partial) are exhaustively covered by the surface
// suite against the real API.
//
// Skipped unless TF_ACC=1.
package provider

import (
	"net/http"
	"sync"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func statusPageFixture() generated.StatusPageDto {
	id, _ := uuid.Parse("33333333-3333-3333-3333-333333333333")
	return generated.StatusPageDto{
		Id:           openapi_types.UUID(id),
		Name:         "Initial",
		Slug:         "initial-slug",
		Visibility:   generated.StatusPageDtoVisibility("PUBLIC"),
		IncidentMode: generated.StatusPageDtoIncidentMode("AUTOMATIC"),
		Enabled:      boolPtr(true),
		Branding:     generated.StatusPageBranding{HidePoweredBy: false},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
}

// TestAccStatusPage_FullLifecycle pins the basic Create → Read → Update →
// Delete sequence including in-place attribute changes (name, enabled
// toggle). The mock holds state in a struct that mutates via PUT bodies
// so subsequent Reads round-trip the new values — the same contract the
// real API provides.
func TestAccStatusPage_FullLifecycle(t *testing.T) {
	requireAcc(t)
	if !hasTerraformBinary() {
		t.Skip("terraform binary not on PATH")
	}

	mock := newMockAPI(t)

	var (
		mu    sync.Mutex
		state = statusPageFixture()
	)

	mock.Handle(http.MethodPost, "/api/v1/status-pages", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if !readBody(w, r, &req) {
			return
		}
		mu.Lock()
		if v, ok := req["name"].(string); ok {
			state.Name = v
		}
		if v, ok := req["slug"].(string); ok {
			state.Slug = v
		}
		mu.Unlock()
		jsonResponse(w, state)
	})

	mock.Handle(http.MethodGet, "/api/v1/status-pages/33333333-3333-3333-3333-333333333333", func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		jsonResponse(w, state)
	})

	mock.Handle(http.MethodPut, "/api/v1/status-pages/33333333-3333-3333-3333-333333333333", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if !readBody(w, r, &req) {
			return
		}
		mu.Lock()
		if v, ok := req["name"].(string); ok {
			state.Name = v
		}
		if v, ok := req["enabled"].(bool); ok {
			state.Enabled = &v
		}
		mu.Unlock()
		jsonResponse(w, state)
	})

	mock.Handle(http.MethodDelete, "/api/v1/status-pages/33333333-3333-3333-3333-333333333333", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfigBlock(mock.URL()) + `
resource "devhelm_status_page" "p" {
  name = "Initial"
  slug = "initial-slug"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devhelm_status_page.p", "id"),
					resource.TestCheckResourceAttr("devhelm_status_page.p", "name", "Initial"),
					resource.TestCheckResourceAttr("devhelm_status_page.p", "slug", "initial-slug"),
					// Server defaults flow through Computed attributes —
					// these are the contract for "operator didn't say,
					// trust the server".
					resource.TestCheckResourceAttr("devhelm_status_page.p", "visibility", "PUBLIC"),
					resource.TestCheckResourceAttr("devhelm_status_page.p", "incident_mode", "AUTOMATIC"),
					resource.TestCheckResourceAttr("devhelm_status_page.p", "enabled", "true"),
				),
			},
			{
				// In-place rename + disable.
				Config: providerConfigBlock(mock.URL()) + `
resource "devhelm_status_page" "p" {
  name    = "Renamed"
  slug    = "initial-slug"
  enabled = false
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devhelm_status_page.p", "name", "Renamed"),
					resource.TestCheckResourceAttr("devhelm_status_page.p", "enabled", "false"),
					// id must NOT change — that would mean a destroy+recreate.
					resource.TestCheckResourceAttr("devhelm_status_page.p", "id", "33333333-3333-3333-3333-333333333333"),
				),
			},
		},
	})
}

// TestAccStatusPage_ImportBySlug pins the import path's slug-vs-UUID
// branch. Importer first tries the input as a UUID against /pages/{id};
// on 400 it falls back to a list-and-scan. We exercise both branches.
func TestAccStatusPage_ImportBySlug(t *testing.T) {
	requireAcc(t)
	if !hasTerraformBinary() {
		t.Skip("terraform binary not on PATH")
	}

	mock := newMockAPI(t)
	state := statusPageFixture()
	state.Name = "Imp Page"
	state.Slug = "imp-page"

	mock.Handle(http.MethodPost, "/api/v1/status-pages", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if !readBody(w, r, &req) {
			return
		}
		jsonResponse(w, state)
	})
	mock.Handle(http.MethodGet, "/api/v1/status-pages/33333333-3333-3333-3333-333333333333", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, state)
	})
	// Importer's slug fallback: GET by string-that-isn't-a-UUID returns 400,
	// then importer LISTs and scans by slug.
	mock.Handle(http.MethodGet, "/api/v1/status-pages/imp-page", func(w http.ResponseWriter, _ *http.Request) {
		httpError(w, http.StatusBadRequest, "id must be a UUID")
	})
	mock.Handle(http.MethodGet, "/api/v1/status-pages", func(w http.ResponseWriter, _ *http.Request) {
		jsonListResponse(w, []generated.StatusPageDto{state})
	})
	mock.Handle(http.MethodDelete, "/api/v1/status-pages/33333333-3333-3333-3333-333333333333", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfigBlock(mock.URL()) + `
resource "devhelm_status_page" "imp" {
  name = "Imp Page"
  slug = "imp-page"
}
`,
			},
			{
				ResourceName:      "devhelm_status_page.imp",
				ImportState:       true,
				ImportStateId:     "imp-page", // by slug, not UUID
				ImportStateVerify: true,
			},
		},
	})
}
