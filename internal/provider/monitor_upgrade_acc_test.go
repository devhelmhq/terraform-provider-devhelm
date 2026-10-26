package provider

// Upgrade acceptance tests: state written by the last published provider
// (v0.2.0-beta.10, downloaded from the registry) must plan no diff under
// this build, with `enabled` set or omitted.

import (
	"net/http"
	"sync"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const upgradeMonitorID = "11111111-2222-3333-4444-666666666666"

// newFakeProbeMonitor serves one HTTP monitor and applies `enabled` from
// create and update bodies.
func newFakeProbeMonitor(t *testing.T, m *mockAPI) {
	var mu sync.Mutex
	enabled := true
	exists := false
	respond := func(w http.ResponseWriter) {
		dto := monitorPartialFixture(t, upgradeMonitorID, "acme-api", nil)
		dto.Enabled = enabled
		jsonResponse(w, dto)
	}
	apply := func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if !readBody(w, r, &body) {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		exists = true
		if e, ok := body["enabled"].(bool); ok {
			enabled = e
		}
		respond(w)
	}
	m.Handle(http.MethodPost, "/api/v1/monitors", apply)
	m.Handle(http.MethodPut, "/api/v1/monitors/"+upgradeMonitorID, apply)
	m.Handle(http.MethodGet, "/api/v1/monitors/"+upgradeMonitorID, func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		if !exists {
			httpError(w, http.StatusNotFound, "gone")
			return
		}
		respond(w)
	})
	m.Handle(http.MethodDelete, "/api/v1/monitors/"+upgradeMonitorID, func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		exists = false
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
}

func probeMonitorHCL(baseURL, extra string) string {
	return providerConfigBlock(baseURL) + `
resource "devhelm_monitor" "m" {
  name              = "acme-api"
  type              = "HTTP"
  frequency_seconds = 60
  regions           = ["us-east"]
  config            = jsonencode({ url = "https://example.com", method = "GET" })
  ` + extra + `
}
`
}

var beta10 = map[string]resource.ExternalProvider{
	"devhelm": {Source: "devhelmhq/devhelm", VersionConstraint: "0.2.0-beta.10"},
}

func TestAccMonitor_UpgradeFromBeta10PlansNoDiff(t *testing.T) {
	requireAcc(t)
	// The in-process provider must answer for the registry address the
	// beta.10 state records.
	t.Setenv("TF_ACC_PROVIDER_NAMESPACE", "devhelmhq")
	for name, extra := range map[string]string{
		"enabled omitted": "",
		"enabled false":   "enabled = false",
	} {
		t.Run(name, func(t *testing.T) {
			m := newMockAPI(t)
			newFakeProbeMonitor(t, m)
			cfg := probeMonitorHCL(m.URL(), extra)
			resource.Test(t, resource.TestCase{
				Steps: []resource.TestStep{
					{ExternalProviders: beta10, Config: cfg},
					{ProtoV6ProviderFactories: newProviderFactories(), Config: cfg, PlanOnly: true},
					// Dropping `enabled` keeps the server value: no diff.
					{ProtoV6ProviderFactories: newProviderFactories(), Config: probeMonitorHCL(m.URL(), ""), PlanOnly: true},
				},
			})
		})
	}
}
