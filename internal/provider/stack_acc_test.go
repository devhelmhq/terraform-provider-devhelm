package provider

// Real-API acceptance tests for code monitors (doc 42 §5.5.3, §7) against
// the mono test stack (`make test-up`, API on :8081). They run only when
// TF_ACC=1 and DEVHELM_ACC_STACK_URL are set:
//
//	TF_ACC=1 DEVHELM_ACC_STACK_URL=http://localhost:8081 \
//	DEVHELM_ACC_STACK_TOKEN=dh_live_… DEVHELM_ACC_STACK_ORG_ID=2 DEVHELM_ACC_STACK_WORKSPACE_ID=2 \
//	go test ./internal/provider -run TestAccStack -v
//
// Use an API key created with tool TERRAFORM so owner enforcement accepts
// the provider's own writes. The stack has no execution fleet, so a
// validation run never passes there: the tested-publish step asserts the
// contract (a `[platform]`, `[timeout]` or `[check_failed]` refusal that
// leaves the live code alone), and the lifecycle uses
// test_before_publish = false.

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func requireStack(t *testing.T) {
	t.Helper()
	requireAcc(t)
	if os.Getenv("DEVHELM_ACC_STACK_URL") == "" {
		t.Skip("set DEVHELM_ACC_STACK_URL (and DEVHELM_ACC_STACK_TOKEN / _ORG_ID / _WORKSPACE_ID) to run against the test stack")
	}
}

func stackProvider() string {
	return fmt.Sprintf(`
provider "devhelm" {
  base_url     = %q
  token        = %q
  org_id       = %q
  workspace_id = %q
}
`, os.Getenv("DEVHELM_ACC_STACK_URL"), os.Getenv("DEVHELM_ACC_STACK_TOKEN"),
		envDefault("DEVHELM_ACC_STACK_ORG_ID", "2"), envDefault("DEVHELM_ACC_STACK_WORKSPACE_ID", "2"))
}

func envDefault(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func stackCodeConfig(prefix, monitorType, dir, extra string) string {
	return stackProvider() + fmt.Sprintf(`
resource "devhelm_environment" "e2e" {
  name      = "%[1]s-env"
  slug      = "%[1]s-env"
  variables = { BASE_URL = "https://example.com" }
}

data "devhelm_runtime" "latest" {}

resource "devhelm_monitor" "m" {
  name              = "%[1]s"
  type              = %[2]q
  environment_id    = devhelm_environment.e2e.id
  frequency_seconds = 600
  regions           = ["us-east"]
  config            = jsonencode({ timeoutSeconds = 60, runtimeId = data.devhelm_runtime.latest.id })
  code = {
    entrypoint = "%[3]s/check.spec.ts"
    root       = %[3]q
  }
  %[4]s
}

data "devhelm_monitor_revision" "live" {
  monitor_id = devhelm_monitor.m.id
}
`, prefix, monitorType, dir, extra)
}

func writeCheck(t *testing.T, dir, body string) {
	t.Helper()
	src := "import { test, expect } from \"@playwright/test\";\nimport { label } from \"./lib/label\";\n\n" +
		"test(label(" + fmt.Sprintf("%q", body) + "), async () => {\n  expect(1).toBe(1);\n});\n"
	if err := os.MkdirAll(filepath.Join(dir, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "check.spec.ts"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "lib", "label.ts"), []byte("export const label = (s: string) => s;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAccStack_BrowserLifecycle(t *testing.T) { stackLifecycle(t, "BROWSER") }

func TestAccStack_MultiStepAPILifecycle(t *testing.T) { stackLifecycle(t, "MULTI_STEP_API") }

func stackLifecycle(t *testing.T, monitorType string) {
	requireStack(t)
	prefix := "terraform-e2e-" + uuid.NewString()[:8]
	dir := t.TempDir()
	writeCheck(t, dir, "v1")
	untested := "test_before_publish = false"
	var digestV1 string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				// Create (untested): revision 1 live, closure digest from the
				// server equals the plan's code_hash, Terraform owns it.
				Config: stackCodeConfig(prefix, monitorType, dir, untested),
				Check: resource.ComposeTestCheckFunc(
					resource.TestMatchResourceAttr("devhelm_monitor.m", "code_hash", regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)),
					resource.TestCheckResourceAttrPair("devhelm_monitor.m", "code_hash", "devhelm_monitor.m", "live_revision_digest"),
					resource.TestCheckResourceAttr("devhelm_monitor.m", "code_files.%", "2"),
					resource.TestCheckResourceAttr("devhelm_monitor.m", "owner.type", "TERRAFORM"),
					resource.TestCheckResourceAttr("devhelm_monitor.m", "owner_enforced", "true"),
					resource.TestCheckResourceAttrPair("devhelm_monitor.m", "live_revision_id", "data.devhelm_monitor_revision.live", "id"),
					resource.TestCheckResourceAttr("data.devhelm_monitor_revision.live", "number", "1"),
					func(s *terraform.State) error {
						digestV1 = s.RootModule().Resources["devhelm_monitor.m"].Primary.Attributes["code_hash"]
						return nil
					},
				),
			},
			{Config: stackCodeConfig(prefix, monitorType, dir, untested), PlanOnly: true},
			{
				// Tested publish on a stack without a fleet: the draft's
				// validation run can't pass, so the apply is refused and the
				// live code stays revision 1.
				PreConfig:   func() { writeCheck(t, dir, "v2") },
				Config:      stackCodeConfig(prefix, monitorType, dir, `timeouts = { update = "45s" }`),
				ExpectError: regexp.MustCompile(`\[(platform|timeout|check_failed)\]`),
			},
			{
				// Untested publish: one PUT with code, revision 2 live.
				Config: stackCodeConfig(prefix, monitorType, dir, untested),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("devhelm_monitor.m", plancheck.ResourceActionUpdate)},
				},
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrPair("devhelm_monitor.m", "code_hash", "devhelm_monitor.m", "live_revision_digest"),
					func(s *terraform.State) error {
						if s.RootModule().Resources["devhelm_monitor.m"].Primary.Attributes["code_hash"] == digestV1 {
							return fmt.Errorf("code_hash did not change")
						}
						return nil
					},
				),
			},
			{Config: stackCodeConfig(prefix, monitorType, dir, untested), PlanOnly: true},
			{
				// Import by id round-trips the code-monitor fields.
				Config:            stackCodeConfig(prefix, monitorType, dir, untested),
				ResourceName:      "devhelm_monitor.m",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"code", "test_before_publish", "timeouts", "owner_address", "acknowledge_rollback",
				},
			},
		},
	})
}

// Validate-then-create (doc 42 R3): with test_before_publish the provider
// runs the ad-hoc validation run before the monitor exists. Without a fleet
// the run can't pass, so the create is refused and nothing is created.
func TestAccStack_ValidateBeforeCreate(t *testing.T) {
	requireStack(t)
	prefix := "terraform-e2e-" + uuid.NewString()[:8]
	dir := t.TempDir()
	writeCheck(t, dir, "v1")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{{
			Config:      stackCodeConfig(prefix, "MULTI_STEP_API", dir, `timeouts = { create = "45s" }`),
			ExpectError: regexp.MustCompile(`\[(platform|timeout|check_failed)\]`),
		}},
	})
}

// code.include ships a fixture nothing imports (doc 42 §5.3, W0-13): the
// server keeps the include paths in the closure, so the fixture is in
// code_files and the live revision.
func TestAccStack_IncludeKeepsUnimportedFixture(t *testing.T) {
	requireStack(t)
	prefix := "terraform-e2e-" + uuid.NewString()[:8]
	dir := t.TempDir()
	writeCheck(t, dir, "v1")
	if err := os.MkdirAll(filepath.Join(dir, "fixtures"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fixtures", "order.json"), []byte("{\"id\": 1}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "unused.ts"), []byte("export const unused = 1;\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := stackProvider() + fmt.Sprintf(`
resource "devhelm_environment" "e2e" {
  name      = "%[1]s-env"
  slug      = "%[1]s-env"
  variables = { BASE_URL = "https://example.com" }
}

resource "devhelm_monitor" "m" {
  name                = "%[1]s"
  type                = "MULTI_STEP_API"
  environment_id      = devhelm_environment.e2e.id
  frequency_seconds   = 600
  regions             = ["us-east"]
  test_before_publish = false
  code = {
    entrypoint = "%[2]s/check.spec.ts"
    root       = %[2]q
    include    = ["%[2]s/fixtures/**"]
  }
}

data "devhelm_monitor_revision" "live" {
  monitor_id = devhelm_monitor.m.id
}
`, prefix, dir)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: cfg,
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttrSet("devhelm_monitor.m", "code_files.fixtures/order.json"),
					resource.TestCheckNoResourceAttr("devhelm_monitor.m", "code_files.unused.ts"),
					resource.TestCheckTypeSetElemAttr("data.devhelm_monitor_revision.live", "files.*", "fixtures/order.json"),
				),
			},
			{Config: cfg, PlanOnly: true},
		},
	})
}
