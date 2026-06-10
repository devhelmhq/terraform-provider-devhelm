// Framework-level acceptance tests for the `devhelm_service` data source.
//
// Coverage axes:
//
//   - lookup_by_slug: real terraform plan against a mock API, asserting
//     every computed attribute (including the currentStatus → overall_status
//     and uptime.month → uptime_30d derivations) lands in state.
//   - lookup_by_uuid: the same endpoint accepts a UUID in the slug slot —
//     pins that the configured value is preserved verbatim in state.
//   - not_found: a 404 from the catalog must surface the helpful
//     "no service with slug" diagnostic, not a raw API error.
//
// These run against a `httptest.Server` (no test stack required) and complete
// in <1s each. Skipped unless TF_ACC=1.
package provider

import (
	"net/http"
	"regexp"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const serviceFixtureID = "7f819203-aaaa-bbbb-cccc-121212121212"

// serviceFixture is a catalog detail DTO that satisfies ValidateDTO: every
// spec-required field (id, slug, name, adapterType, lifecycleStatus,
// dataCompleteness, the three required arrays, timestamps) is populated.
func serviceFixture(slug, name string) generated.ServiceDetailDto {
	id, _ := uuid.Parse(serviceFixtureID)
	category := "payments"
	statusURL := "https://status.stripe.com"
	month := 99.95
	return generated.ServiceDetailDto{
		Id:                 openapi_types.UUID(id),
		Slug:               slug,
		Name:               name,
		Category:           &category,
		OfficialStatusUrl:  &statusURL,
		AdapterType:        "statuspage",
		LifecycleStatus:    "ACTIVE",
		Enabled:            true,
		DataCompleteness:   "FULL",
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
		CurrentStatus:      &generated.ServiceStatusDto{OverallStatus: "operational"},
		ActiveMaintenances: []generated.ScheduledMaintenanceDto{},
		RecentIncidents:    []generated.ServiceIncidentDto{},
		Components: []generated.ServiceComponentDto{
			{Name: "API"}, {Name: "Dashboard"},
		},
		Uptime: &generated.ComponentUptimeSummaryDto{Month: &month, Source: "vendor_reported"},
	}
}

func TestAccServiceDataSource_LookupBySlug(t *testing.T) {
	requireAcc(t)
	if !hasTerraformBinary() {
		t.Skip("terraform binary not on PATH")
	}

	mock := newMockAPI(t)
	mock.Handle(http.MethodGet, "/api/v1/services/stripe", func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, serviceFixture("stripe", "Stripe"))
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfigBlock(mock.URL()) + `
data "devhelm_service" "stripe" {
  slug = "stripe"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.devhelm_service.stripe", "id", serviceFixtureID),
					resource.TestCheckResourceAttr("data.devhelm_service.stripe", "slug", "stripe"),
					resource.TestCheckResourceAttr("data.devhelm_service.stripe", "name", "Stripe"),
					resource.TestCheckResourceAttr("data.devhelm_service.stripe", "category", "payments"),
					resource.TestCheckResourceAttr("data.devhelm_service.stripe", "official_status_url", "https://status.stripe.com"),
					resource.TestCheckResourceAttr("data.devhelm_service.stripe", "overall_status", "operational"),
					resource.TestCheckResourceAttr("data.devhelm_service.stripe", "component_count", "2"),
					resource.TestCheckResourceAttr("data.devhelm_service.stripe", "uptime_30d", "99.95"),
				),
			},
		},
	})
}

func TestAccServiceDataSource_LookupByUUID(t *testing.T) {
	requireAcc(t)
	if !hasTerraformBinary() {
		t.Skip("terraform binary not on PATH")
	}

	mock := newMockAPI(t)
	mock.Handle(http.MethodGet, "/api/v1/services/"+serviceFixtureID, func(w http.ResponseWriter, _ *http.Request) {
		jsonResponse(w, serviceFixture("stripe", "Stripe"))
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfigBlock(mock.URL()) + `
data "devhelm_service" "by_id" {
  slug = "` + serviceFixtureID + `"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					// The configured UUID stays in `slug` verbatim; the
					// canonical slug is NOT swapped in (that would trip
					// Terraform's config/state consistency check).
					resource.TestCheckResourceAttr("data.devhelm_service.by_id", "slug", serviceFixtureID),
					resource.TestCheckResourceAttr("data.devhelm_service.by_id", "id", serviceFixtureID),
					resource.TestCheckResourceAttr("data.devhelm_service.by_id", "name", "Stripe"),
				),
			},
		},
	})
}

func TestAccServiceDataSource_NotFound(t *testing.T) {
	requireAcc(t)
	if !hasTerraformBinary() {
		t.Skip("terraform binary not on PATH")
	}

	mock := newMockAPI(t)
	mock.Handle(http.MethodGet, "/api/v1/services/no-such-service", func(w http.ResponseWriter, _ *http.Request) {
		httpError(w, http.StatusNotFound, "service not found")
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfigBlock(mock.URL()) + `
data "devhelm_service" "missing" {
  slug = "no-such-service"
}
`,
				ExpectError: regexp.MustCompile(`No service found in the DevHelm catalog with slug or ID\s+"no-such-service"`),
			},
		},
	})
}
