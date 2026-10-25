package provider

// Framework acceptance tests for BROWSER / MULTI_STEP_API monitors against
// a stateful mock of the code-monitor routes (doc 42 §7): plan-time bundle
// upload, validate-then-create, draft → test → publish, a failed test that
// leaves state alone, rollback drift, the removed `enabled`, and import
// transferring ownership.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/bundle"
	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

const (
	codeMonitorID = "7a1b2c3d-0000-4000-8000-000000000001"
	codeEnvID     = "7a1b2c3d-0000-4000-8000-0000000000e1"
	codeDefID     = "7a1b2c3d-0000-4000-8000-0000000000d1"
)

// fakeCodeAPI holds one code monitor and its revisions.
type fakeCodeAPI struct {
	mu          sync.Mutex
	t           *testing.T
	monitor     map[string]any
	revisions   []map[string]any
	nextRunPass bool
	published   []map[string]any
	puts        []map[string]any
	transfers   []map[string]any
	rolledBack  string
}

func newFakeCodeAPI(t *testing.T, m *mockAPI) *fakeCodeAPI {
	f := &fakeCodeAPI{t: t, nextRunPass: true}
	runs := map[string]string{} // run id → outcome

	m.Handle(http.MethodPost, "/api/v1/bundles", func(w http.ResponseWriter, r *http.Request) {
		var body generated.CreateBundleRequest
		if !readBody(w, r, &body) {
			return
		}
		files := map[string][]byte{}
		var paths []string
		for p, c := range body.Files {
			files[p] = []byte(c)
			paths = append(paths, p)
		}
		d := bundle.Digest(files)
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, map[string]any{"digest": d, "uploadDigest": d, "files": paths, "keys": []string{}, "size": 10})
	})
	newRun := func(w http.ResponseWriter) {
		f.mu.Lock()
		id := uuid.NewString()
		if f.nextRunPass {
			runs[id] = "passed"
		} else {
			runs[id] = "failed"
		}
		f.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		jsonResponse(w, runFixture(id, "queued", nil))
		m.Handle(http.MethodGet, "/api/v1/runs/"+id, func(w http.ResponseWriter, _ *http.Request) {
			o := runs[id]
			jsonResponse(w, runFixture(id, "finished", &o))
		})
		m.Handle(http.MethodGet, "/api/v1/runs/"+id+"/events", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
	}
	m.Handle(http.MethodPost, "/api/v1/monitors/test", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if !readBody(w, r, &body) {
			return
		}
		if body["environmentId"] != codeEnvID || body["code"] == nil {
			t.Errorf("ad-hoc test body = %v", body)
		}
		newRun(w)
	})
	m.Handle(http.MethodPost, "/api/v1/monitors", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if !readBody(w, r, &body) {
			return
		}
		if body["managedBy"] != "TERRAFORM" || body["ownerEnforced"] != true || body["enabled"] != nil {
			t.Errorf("create body ownership = %v / %v / enabled %v", body["managedBy"], body["ownerEnforced"], body["enabled"])
		}
		code := body["code"].(map[string]any)
		f.mu.Lock()
		f.monitor = codeMonitorFixture(body["name"].(string), "TERRAFORM", true)
		f.publishDigest(code["bundle"].(string), code["entrypoint"].(string))
		mon := f.monitor
		f.mu.Unlock()
		w.WriteHeader(http.StatusCreated)
		jsonResponse(w, mon)
	})
	m.Handle(http.MethodGet, "/api/v1/monitors/"+codeMonitorID, func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.monitor == nil {
			httpError(w, http.StatusNotFound, "gone")
			return
		}
		jsonResponse(w, f.monitor)
	})
	m.Handle(http.MethodPut, "/api/v1/monitors/"+codeMonitorID, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if !readBody(w, r, &body) {
			return
		}
		if _, ok := body["managedBy"]; ok {
			t.Errorf("PUT on a code monitor must not send managedBy")
		}
		f.mu.Lock()
		f.puts = append(f.puts, body)
		if n, ok := body["name"].(string); ok {
			f.monitor["name"] = n
		}
		if code, ok := body["code"].(map[string]any); ok {
			f.publishDigest(code["bundle"].(string), code["entrypoint"].(string))
		}
		mon := f.monitor
		f.mu.Unlock()
		jsonResponse(w, mon)
	})
	m.Handle(http.MethodDelete, "/api/v1/monitors/"+codeMonitorID, func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		f.monitor = nil
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	m.Handle(http.MethodPost, "/api/v1/monitors/"+codeMonitorID+"/revisions", func(w http.ResponseWriter, r *http.Request) {
		var body generated.CreateDraftRevisionRequest
		if !readBody(w, r, &body) {
			return
		}
		f.mu.Lock()
		rev := revisionFixture(len(f.revisions)+1, body.Code.Bundle, body.Code.Entrypoint, "draft")
		f.revisions = append(f.revisions, rev)
		f.mu.Unlock()
		m.Handle(http.MethodPost, "/api/v1/monitors/"+codeMonitorID+"/revisions/"+rev["id"].(string)+"/test",
			func(w http.ResponseWriter, _ *http.Request) { newRun(w) })
		jsonResponse(w, map[string]any{"revision": rev})
	})
	m.Handle(http.MethodPost, "/api/v1/monitors/"+codeMonitorID+"/publish", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if !readBody(w, r, &body) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		f.published = append(f.published, body)
		if body["requireTested"] != true || body["expectedLiveRevisionId"] != f.monitor["liveRevisionId"] {
			t.Errorf("publish body = %v (live %v)", body, f.monitor["liveRevisionId"])
		}
		for _, rev := range f.revisions {
			if rev["id"] == body["revisionId"] {
				if rev["digest"] == f.rolledBack && body["acknowledgeRollback"] != f.rolledBack {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusConflict)
					_ = json.NewEncoder(w).Encode(map[string]any{
						"status": 409, "code": "REVISION_ROLLED_BACK", "category": "conflict", "retryable": false,
						"message": "This digest was rolled back", "timestamp": 1,
						"details": map[string]any{"rolledBackDigest": f.rolledBack},
					})
					return
				}
				rev["state"] = "live"
				f.monitor["liveRevisionId"] = rev["id"]
				f.monitor["liveRevisionDigest"] = rev["digest"]
				jsonResponse(w, map[string]any{
					"definitionId": codeDefID, "activatedAt": time.Now().Format(time.RFC3339), "revision": rev,
					"environment": map[string]any{"id": codeEnvID, "name": "Production", "slug": "production",
						"variables": map[string]string{"BASE_URL": "https://example.com"}, "orgId": 1,
						"isDefault": true, "monitorCount": 1,
						"createdAt": time.Now().Format(time.RFC3339), "updatedAt": time.Now().Format(time.RFC3339)},
				})
				return
			}
		}
		httpError(w, http.StatusNotFound, "no such revision")
	})
	m.Handle(http.MethodPost, "/api/v1/monitors/"+codeMonitorID+"/transfer", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if !readBody(w, r, &body) {
			return
		}
		f.mu.Lock()
		f.transfers = append(f.transfers, body)
		f.monitor["managedBy"] = body["to"]
		f.monitor["owner"] = map[string]any{"type": body["to"], "label": "Terraform"}
		mon := f.monitor
		f.mu.Unlock()
		jsonResponse(w, mon)
	})
	m.Handle(http.MethodGet, "/api/v1/definitions/"+codeDefID, func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, map[string]any{
			"definition": map[string]any{},
			"heads": []any{map[string]any{
				"environment": map[string]any{"id": codeEnvID},
				"revision":    map[string]any{"number": 1},
				"activatedBy": "alice@example.com", "reason": "checkout broke",
				"rolledBackDigest": f.rolledBack,
			}},
		})
	})
	return f
}

// publishDigest makes a new live revision for digest (caller holds mu).
func (f *fakeCodeAPI) publishDigest(digest, entrypoint string) {
	rev := revisionFixture(len(f.revisions)+1, digest, entrypoint, "live")
	f.revisions = append(f.revisions, rev)
	f.monitor["liveRevisionId"] = rev["id"]
	f.monitor["liveRevisionDigest"] = digest
}

// rollBack simulates a dashboard rollback to an unrelated revision.
func (f *fakeCodeAPI) rollBack() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rolledBack = f.monitor["liveRevisionDigest"].(string)
	rev := revisionFixture(len(f.revisions)+1, "sha256:"+strings.Repeat("0", 64), "checkout.spec.ts", "live")
	f.revisions = append(f.revisions, rev)
	f.monitor["liveRevisionId"] = rev["id"]
	f.monitor["liveRevisionDigest"] = rev["digest"]
}

func codeMonitorFixture(name, owner string, enforced bool) map[string]any {
	return map[string]any{
		"id": codeMonitorID, "name": name, "type": "BROWSER", "frequencySeconds": 600, "enabled": true,
		"regions": []string{"us-east"}, "config": map[string]any{"timeoutSeconds": 90, "runtimeId": "2026.11"},
		"managedBy": owner, "ownerEnforced": enforced, "muted": false, "organizationId": 1,
		"environment":  map[string]any{"id": codeEnvID, "name": "Production", "slug": "production"},
		"definitionId": codeDefID, "runParallel": true, "fastRetryMaxAttempts": 1,
		"capturePolicy": map[string]any{"screenshots": "on_failure", "trace": "on_failure", "video": "off", "network": "on_failure"},
		"owner":         map[string]any{"type": owner},
		"createdAt":     time.Now().Format(time.RFC3339), "updatedAt": time.Now().Format(time.RFC3339),
	}
}

func revisionFixture(n int, digest, entrypoint, state string) map[string]any {
	return map[string]any{
		"id": uuid.NewString(), "definitionId": codeDefID, "number": n, "digest": digest,
		"entrypoint": entrypoint, "files": []string{entrypoint}, "keys": []string{},
		"createdAt": time.Now().Format(time.RFC3339), "state": state,
	}
}

func runFixture(id, phase string, outcome *string) map[string]any {
	return map[string]any{
		"id": id, "organizationId": 1, "monitorId": codeMonitorID, "revisionId": uuid.NewString(),
		"region": "us-east", "source": "remote_validation", "countsTowardHealth": false,
		"enqueuedAt": time.Now().Format(time.RFC3339), "phase": phase, "cancelRequested": false,
		"bundleDigest": "sha256:x", "capturePolicySnapshot": map[string]any{}, "bindingVersionSnapshot": map[string]any{},
		"stream": false, "evidence": []any{}, "tabCounts": map[string]any{}, "attempt": 1, "attemptOf": 1,
		"attempts": []any{}, "executionAttempts": []any{}, "billable": true, "outcome": outcome,
		"url": "https://app.devhelm.io/runs/" + id,
	}
}

func codeMonitorHCL(baseURL, dir, extra string) string {
	return providerConfigBlock(baseURL) + fmt.Sprintf(`
resource "devhelm_monitor" "checkout" {
  name              = "Checkout"
  type              = "BROWSER"
  environment_id    = %q
  frequency_seconds = 600
  regions           = ["us-east"]
  config            = jsonencode({ timeoutSeconds = 90 })
  code = {
    entrypoint = "%s/checkout.spec.ts"
    root       = %q
  }
  %s
}
`, codeEnvID, dir, dir, extra)
}

func writeSpec(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "checkout.spec.ts"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAccCodeMonitor_CreateChangeFailRollbackDestroy(t *testing.T) {
	requireAcc(t)
	m := newMockAPI(t)
	f := newFakeCodeAPI(t, m)
	dir := t.TempDir()
	writeSpec(t, dir, "test('checkout', async () => {})\n")
	if err := os.WriteFile(filepath.Join(dir, "helper.ts"), []byte("export const x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	digestV1 := ""
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				// Create: upload at plan, validate, then create live.
				Config: codeMonitorHCL(m.URL(), dir, ""),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("devhelm_monitor.checkout", "id", codeMonitorID),
					resource.TestMatchResourceAttr("devhelm_monitor.checkout", "code_hash", regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)),
					resource.TestCheckResourceAttrPair("devhelm_monitor.checkout", "code_hash", "devhelm_monitor.checkout", "live_revision_digest"),
					resource.TestCheckResourceAttr("devhelm_monitor.checkout", "code_files.%", "2"),
					resource.TestCheckResourceAttr("devhelm_monitor.checkout", "owner.type", "TERRAFORM"),
					resource.TestCheckResourceAttr("devhelm_monitor.checkout", "owner_enforced", "true"),
					resource.TestCheckResourceAttr("devhelm_monitor.checkout", "test_before_publish", "true"),
					func(s *terraform.State) error {
						digestV1 = s.RootModule().Resources["devhelm_monitor.checkout"].Primary.Attributes["code_hash"]
						return nil
					},
				),
			},
			{
				// Same files: a stable no-change plan.
				Config:   codeMonitorHCL(m.URL(), dir, ""),
				PlanOnly: true,
			},
			{
				// Code change: draft → test → publish {requireTested, CAS}.
				PreConfig: func() { writeSpec(t, dir, "test('checkout v2', async () => {})\n") },
				Config:    codeMonitorHCL(m.URL(), dir, ""),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("devhelm_monitor.checkout", plancheck.ResourceActionUpdate)},
				},
				Check: func(s *terraform.State) error {
					attrs := s.RootModule().Resources["devhelm_monitor.checkout"].Primary.Attributes
					if attrs["code_hash"] == digestV1 || attrs["code_hash"] != attrs["live_revision_digest"] {
						return fmt.Errorf("code_hash %s / live %s after publish", attrs["code_hash"], attrs["live_revision_digest"])
					}
					if len(f.published) != 1 {
						return fmt.Errorf("publishes = %d, want 1", len(f.published))
					}
					return nil
				},
			},
			{
				// Failed validation run: [check_failed] <run url>, old code
				// stays live, state unchanged.
				PreConfig: func() {
					f.nextRunPass = false
					writeSpec(t, dir, "test('checkout v3 broken', async () => {})\n")
				},
				Config:      codeMonitorHCL(m.URL(), dir, ""),
				ExpectError: regexp.MustCompile(`\[check_failed\] https://app\.devhelm\.io/runs/`),
			},
			{
				// Back to v2 with passing runs: no change (v2 is still live).
				PreConfig: func() {
					f.nextRunPass = true
					writeSpec(t, dir, "test('checkout v2', async () => {})\n")
				},
				Config:   codeMonitorHCL(m.URL(), dir, ""),
				PlanOnly: true,
			},
			{
				// Rollback in the dashboard: plan shows drift; republishing
				// the rolled-back digest needs acknowledge_rollback.
				PreConfig:   func() { f.rollBack() },
				Config:      codeMonitorHCL(m.URL(), dir, ""),
				ExpectError: regexp.MustCompile(`acknowledge_rollback`),
			},
		},
	})
	if len(f.puts) == 0 {
		t.Error("expected a settings PUT after publishing")
	}
}

func TestAccCodeMonitor_AcknowledgeRollbackRepublishes(t *testing.T) {
	requireAcc(t)
	m := newMockAPI(t)
	f := newFakeCodeAPI(t, m)
	dir := t.TempDir()
	writeSpec(t, dir, "test('checkout', async () => {})\n")
	digest := bundle.Digest(map[string][]byte{"checkout.spec.ts": []byte("test('checkout', async () => {})\n")})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{Config: codeMonitorHCL(m.URL(), dir, "")},
			{
				PreConfig: func() { f.rollBack() },
				Config:    codeMonitorHCL(m.URL(), dir, fmt.Sprintf("acknowledge_rollback = %q", digest)),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("devhelm_monitor.checkout", "live_revision_digest", digest),
					func(*terraform.State) error {
						if got := f.published[len(f.published)-1]["acknowledgeRollback"]; got != digest {
							return fmt.Errorf("acknowledgeRollback = %v", got)
						}
						return nil
					},
				),
			},
		},
	})
}

func TestAccCodeMonitor_UntestedPublishSendsCodeOnPut(t *testing.T) {
	requireAcc(t)
	m := newMockAPI(t)
	f := newFakeCodeAPI(t, m)
	dir := t.TempDir()
	writeSpec(t, dir, "v1\n")
	m.Handle(http.MethodPost, "/api/v1/monitors/test", func(w http.ResponseWriter, _ *http.Request) {
		t.Error("test_before_publish = false must not run a validation run")
		httpError(w, 500, "no")
	})
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{Config: codeMonitorHCL(m.URL(), dir, "test_before_publish = false")},
			{
				PreConfig: func() { writeSpec(t, dir, "v2\n") },
				Config:    codeMonitorHCL(m.URL(), dir, "test_before_publish = false"),
				Check: func(*terraform.State) error {
					if len(f.published) != 0 {
						return fmt.Errorf("untested publish must go through PUT, not /publish")
					}
					if f.puts[len(f.puts)-1]["code"] == nil {
						return fmt.Errorf("PUT carried no code")
					}
					return nil
				},
			},
			{Config: codeMonitorHCL(m.URL(), dir, "test_before_publish = false"), PlanOnly: true},
		},
	})
}

func TestAccCodeMonitor_EnabledIsAnUpgradeError(t *testing.T) {
	requireAcc(t)
	m := newMockAPI(t)
	dir := t.TempDir()
	writeSpec(t, dir, "x\n")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{{
			Config:      codeMonitorHCL(m.URL(), dir, "enabled = true"),
			ExpectError: regexp.MustCompile(`devhelm\s+monitors\s+pause`),
		}},
	})
}

func TestAccCodeMonitor_ProbeRejectsCodeAndCodeNeedsEnvironment(t *testing.T) {
	requireAcc(t)
	m := newMockAPI(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfigBlock(m.URL()) + `
resource "devhelm_monitor" "p" {
  name   = "p"
  type   = "HTTP"
  config = jsonencode({ url = "https://example.com", method = "GET" })
  code   = { entrypoint = "x.spec.ts" }
}`,
				ExpectError: regexp.MustCompile(`code is only for BROWSER and MULTI_STEP_API`),
			},
			{
				Config: providerConfigBlock(m.URL()) + `
resource "devhelm_monitor" "c" {
  name = "c"
  type = "MULTI_STEP_API"
  code = { entrypoint = "x.spec.ts" }
}`,
				ExpectError: regexp.MustCompile(`Missing environment_id`),
			},
		},
	})
}

func TestAccCodeMonitor_ImportReadsThenApplyTransfersToTerraform(t *testing.T) {
	requireAcc(t)
	m := newMockAPI(t)
	f := newFakeCodeAPI(t, m)
	dir := t.TempDir()
	writeSpec(t, dir, "x")
	digest := bundle.Digest(map[string][]byte{"checkout.spec.ts": []byte("x")})
	f.monitor = codeMonitorFixture("Checkout", "DASHBOARD", false)
	f.publishDigest(digest, "checkout.spec.ts")
	liveID := f.monitor["liveRevisionId"].(string)
	m.Handle(http.MethodGet, "/api/v1/revisions/"+liveID+"/files", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, map[string]any{"entrypoint": "checkout.spec.ts", "files": map[string]string{"checkout.spec.ts": "x"}})
	})
	m.Handle(http.MethodGet, "/api/v1/monitors", func(w http.ResponseWriter, _ *http.Request) {
		jsonListResponse(w, []any{f.monitor})
	})
	cfg := codeMonitorHCL(m.URL(), dir, "")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config:        cfg,
				ResourceName:  "devhelm_monitor.checkout",
				ImportState:   true,
				ImportStateId: codeMonitorID,
				ImportStateCheck: func(states []*terraform.InstanceState) error {
					a := states[0].Attributes
					if a["owner.type"] != "DASHBOARD" || a["code_hash"] != digest || a["code_files.%"] != "1" {
						return fmt.Errorf("imported attrs = %v", a)
					}
					if len(f.transfers) != 0 {
						return fmt.Errorf("import must only read; transfers = %v", f.transfers)
					}
					return nil
				},
				ImportStatePersist: true,
			},
			{
				// First apply after import: the plan shows owner changing and
				// the apply transfers ownership; the code is already live.
				Config: cfg,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectUnknownValue("devhelm_monitor.checkout", tfjsonpath.New("owner"))},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("devhelm_monitor.checkout", "owner.type", "TERRAFORM"),
					func(*terraform.State) error {
						if len(f.transfers) != 1 || f.transfers[0]["to"] != "TERRAFORM" || f.transfers[0]["reason"] != "terraform import" {
							return fmt.Errorf("transfers = %v", f.transfers)
						}
						if len(f.published) != 0 {
							return fmt.Errorf("unchanged code must not be republished")
						}
						return nil
					},
				),
			},
			{Config: cfg, PlanOnly: true},
		},
	})
}

func TestAccCodeMonitor_PublishThenFailedPutKeepsLiveCodeInState(t *testing.T) {
	requireAcc(t)
	m := newMockAPI(t)
	f := newFakeCodeAPI(t, m)
	dir := t.TempDir()
	writeSpec(t, dir, "v1\n")
	var failPut bool
	put := m.handlers["PUT /api/v1/monitors/"+codeMonitorID]
	m.Handle(http.MethodPut, "/api/v1/monitors/"+codeMonitorID, func(w http.ResponseWriter, r *http.Request) {
		if failPut {
			httpError(w, http.StatusBadRequest, "settings refused")
			return
		}
		put(w, r)
	})
	v2 := bundle.Digest(map[string][]byte{"checkout.spec.ts": []byte("v2\n")})
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{Config: codeMonitorHCL(m.URL(), dir, "")},
			{
				PreConfig: func() {
					failPut = true
					writeSpec(t, dir, "v2\n")
				},
				Config:      codeMonitorHCL(m.URL(), dir, ""),
				ExpectError: regexp.MustCompile(`settings refused`),
			},
			{
				// The publish landed: state already records v2 live, so the
				// next plan has no code change and doesn't re-test it.
				PreConfig: func() { failPut = false },
				Config:    codeMonitorHCL(m.URL(), dir, ""),
				PlanOnly:  true,
				Check:     resource.TestCheckResourceAttr("devhelm_monitor.checkout", "live_revision_digest", v2),
			},
		},
	})
	if len(f.published) != 1 {
		t.Errorf("publishes = %d, want 1", len(f.published))
	}
}

func TestAccCodeMonitor_FailedTestAfterImportDoesNotTransfer(t *testing.T) {
	requireAcc(t)
	m := newMockAPI(t)
	f := newFakeCodeAPI(t, m)
	dir := t.TempDir()
	writeSpec(t, dir, "new code\n")
	f.monitor = codeMonitorFixture("Checkout", "DASHBOARD", false)
	f.publishDigest("sha256:"+strings.Repeat("b", 64), "checkout.spec.ts")
	f.nextRunPass = false
	m.Handle(http.MethodGet, "/api/v1/monitors", func(w http.ResponseWriter, _ *http.Request) {
		jsonListResponse(w, []any{f.monitor})
	})
	liveID := f.monitor["liveRevisionId"].(string)
	m.Handle(http.MethodGet, "/api/v1/revisions/"+liveID+"/files", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, map[string]any{"entrypoint": "checkout.spec.ts", "files": map[string]string{"checkout.spec.ts": "old"}})
	})
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: codeMonitorHCL(m.URL(), dir, ""), ResourceName: "devhelm_monitor.checkout",
				ImportState: true, ImportStateId: codeMonitorID, ImportStatePersist: true,
				ImportStateCheck: func([]*terraform.InstanceState) error { return nil },
			},
			{
				Config:      codeMonitorHCL(m.URL(), dir, ""),
				ExpectError: regexp.MustCompile(`\[check_failed\]`),
				Check: func(*terraform.State) error {
					return nil
				},
			},
		},
	})
	if len(f.transfers) != 0 {
		t.Errorf("a failed test must leave ownership alone; transfers = %v", f.transfers)
	}
}
