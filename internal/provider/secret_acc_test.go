// Framework-level acceptance tests for the `devhelm_secret` resource.
//
// Coverage axes (each `t.Run` pins one):
//
//   - lifecycle_create_read_update_delete: real terraform plan/apply against
//     a mock API, exercising every CRUD method plus state read-back.
//   - read_404_removes_from_state: simulating out-of-band deletion (the API
//     stops returning the secret in the list call) — Terraform must propose
//     a recreate, not a Read error.
//   - import_by_key: covers ImportState's "look up by key, not just UUID"
//     branch, which is otherwise only reachable from CLI `terraform import`.
//
// These run against a `httptest.Server` (no test stack required) and complete
// in <1s each. Skipped unless TF_ACC=1.
package provider

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// secretFixture is the canonical SecretDto used in CRUD tests. We keep it
// as a constructor so each test gets a fresh copy and may mutate fields
// (e.g. bumping `valueHash` on an Update step).
func secretFixture(key, valueHash string) generated.SecretDto {
	id, _ := uuid.Parse("11111111-1111-1111-1111-111111111111")
	return generated.SecretDto{
		Id:         openapi_types.UUID(id),
		Key:        key,
		ValueHash:  valueHash,
		DekVersion: int32Ptr(1),
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}
}

func TestAccSecret_FullLifecycle(t *testing.T) {
	requireAcc(t)
	if !hasTerraformBinary() {
		t.Skip("terraform binary not on PATH")
	}

	mock := newMockAPI(t)

	// Mutable cell that tracks the current value hash on the "server".
	// Update steps mutate this; Read steps observe it. Using an atomic
	// avoids racing the framework's parallel plan-vs-refresh goroutines.
	var currentHash atomic.Value
	currentHash.Store("hash-v1")

	mock.Handle(http.MethodPost, "/api/v1/secrets", func(w http.ResponseWriter, r *http.Request) {
		var req generated.CreateSecretRequest
		if !readBody(w, r, &req) {
			return
		}
		if req.Key == "" || req.Value == "" {
			httpError(w, http.StatusBadRequest, "key and value required")
			return
		}
		jsonResponse(w, secretFixture(req.Key, currentHash.Load().(string)))
	})

	mock.Handle(http.MethodGet, "/api/v1/secrets", func(w http.ResponseWriter, r *http.Request) {
		jsonListResponse(w, []generated.SecretDto{
			secretFixture("api_key", currentHash.Load().(string)),
		})
	})

	mock.Handle(http.MethodPut, "/api/v1/secrets/api_key", func(w http.ResponseWriter, r *http.Request) {
		var req generated.UpdateSecretRequest
		if !readBody(w, r, &req) {
			return
		}
		// Bump the hash so the next Read reflects the update — the
		// resource's Update method computes the new hash client-side
		// and writes it to state, so this isn't strictly required for
		// correctness, but it makes the call sequence realistic.
		currentHash.Store("hash-v2")
		jsonResponse(w, secretFixture("api_key", "hash-v2"))
	})

	mock.Handle(http.MethodDelete, "/api/v1/secrets/api_key", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfigBlock(mock.URL()) + `
resource "devhelm_secret" "k" {
  key   = "api_key"
  value = "supersecret-v1"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devhelm_secret.k", "id"),
					resource.TestCheckResourceAttr("devhelm_secret.k", "key", "api_key"),
					resource.TestCheckResourceAttrSet("devhelm_secret.k", "value_hash"),
				),
			},
			{
				Config: providerConfigBlock(mock.URL()) + `
resource "devhelm_secret" "k" {
  key   = "api_key"
  value = "supersecret-v2"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devhelm_secret.k", "key", "api_key"),
				),
			},
		},
	})
}

func TestAccSecret_Read404RemovesFromState(t *testing.T) {
	requireAcc(t)
	if !hasTerraformBinary() {
		t.Skip("terraform binary not on PATH")
	}

	mock := newMockAPI(t)

	// `dropped` flips after the apply step succeeds, simulating an
	// out-of-band deletion. The next plan should propose a recreate
	// rather than fail; this is the contract the resource's Read
	// implementation pins by calling `resp.State.RemoveResource`.
	var dropped atomic.Bool

	mock.Handle(http.MethodPost, "/api/v1/secrets", func(w http.ResponseWriter, r *http.Request) {
		var req generated.CreateSecretRequest
		if !readBody(w, r, &req) {
			return
		}
		jsonResponse(w, secretFixture(req.Key, "hash-init"))
	})

	mock.Handle(http.MethodGet, "/api/v1/secrets", func(w http.ResponseWriter, r *http.Request) {
		if dropped.Load() {
			jsonListResponse(w, []generated.SecretDto{})
			return
		}
		jsonListResponse(w, []generated.SecretDto{
			secretFixture("ephemeral", "hash-init"),
		})
	})

	mock.Handle(http.MethodDelete, "/api/v1/secrets/ephemeral", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfigBlock(mock.URL()) + `
resource "devhelm_secret" "e" {
  key   = "ephemeral"
  value = "v"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devhelm_secret.e", "id"),
				),
			},
			{
				// Flip the mock to return an empty list before the
				// next refresh+plan. PreConfig runs *before* the
				// step's refresh, so the framework's drift check
				// observes the deletion via Read → state removal →
				// non-empty (re-)create plan. That's the contract
				// we're pinning here.
				PreConfig: func() { dropped.Store(true) },
				Config: providerConfigBlock(mock.URL()) + `
resource "devhelm_secret" "e" {
  key   = "ephemeral"
  value = "v"
}
`,
				ExpectNonEmptyPlan: true,
				PlanOnly:           true,
			},
		},
	})
}

func TestAccSecret_ImportByKey(t *testing.T) {
	requireAcc(t)
	if !hasTerraformBinary() {
		t.Skip("terraform binary not on PATH")
	}

	mock := newMockAPI(t)

	mock.Handle(http.MethodPost, "/api/v1/secrets", func(w http.ResponseWriter, r *http.Request) {
		var req generated.CreateSecretRequest
		if !readBody(w, r, &req) {
			return
		}
		jsonResponse(w, secretFixture(req.Key, "hash-imp"))
	})
	mock.Handle(http.MethodGet, "/api/v1/secrets", func(w http.ResponseWriter, _ *http.Request) {
		jsonListResponse(w, []generated.SecretDto{
			secretFixture("import_me", "hash-imp"),
		})
	})
	mock.Handle(http.MethodDelete, "/api/v1/secrets/import_me", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfigBlock(mock.URL()) + `
resource "devhelm_secret" "imp" {
  key   = "import_me"
  value = "v"
}
`,
			},
			{
				ResourceName:      "devhelm_secret.imp",
				ImportState:       true,
				ImportStateId:     "import_me", // by key, not UUID
				ImportStateVerify: true,
				// `value` is write-only — the importer fills a placeholder, so it
				// won't match the original. Skip the equality check on it.
				ImportStateVerifyIgnore: []string{"value"},
			},
		},
	})
}
