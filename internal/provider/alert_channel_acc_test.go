// Framework-level acceptance tests for `devhelm_alert_channel`.
//
// Why a second resource here?
//
//   - alert_channel exercises the polymorphic-config code path
//     (`buildConfig` → discriminated-union JSON → `Config` field on the DTO).
//     A regression where the wrong wire field is used (e.g. `webhookUrl` vs.
//     `webhook_url`) is otherwise silent until a slack channel actually
//     fails to deliver.
//   - The provider's Read explicitly does NOT round-trip secret config back
//     into state — only the `configHash`. We pin that an Update bumping the
//     hash is observable in state without leaking the cleartext.
//   - Update preserves the resource ID (no recreate on `name` or config change).
//
// Skipped unless TF_ACC=1.
package provider

import (
	"net/http"
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func alertChannelFixture(name, hash string) generated.AlertChannelDto {
	id, _ := uuid.Parse("22222222-2222-2222-2222-222222222222")
	hashCopy := hash
	return generated.AlertChannelDto{
		Id:          openapi_types.UUID(id),
		Name:        name,
		ChannelType: generated.Slack,
		ConfigHash:  &hashCopy,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
}

func TestAccAlertChannel_LifecycleUpdatesConfigHashWithoutLeakingSecrets(t *testing.T) {
	requireAcc(t)
	if !hasTerraformBinary() {
		t.Skip("terraform binary not on PATH")
	}

	mock := newMockAPI(t)

	// Tracked server-side; bumped on each Update so the next Read
	// surfaces the new hash. Resource state must reflect this.
	currentHash := "hash-create"
	currentName := "primary-slack"

	mock.Handle(http.MethodPost, "/api/v1/alert-channels", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if !readBody(w, r, &req) {
			return
		}
		// Spot-check the discriminator survived JSON round-trip — the
		// most common bug class for polymorphic configs is "the inner
		// `channelType` field gets dropped during marshalling".
		cfg, ok := req["config"].(map[string]any)
		if !ok || cfg["channelType"] != "slack" || cfg["webhookUrl"] == "" {
			httpError(w, http.StatusBadRequest, "config missing channelType=slack and/or webhookUrl")
			return
		}
		jsonResponse(w, alertChannelFixture(currentName, currentHash))
	})

	mock.Handle(http.MethodGet, "/api/v1/alert-channels", func(w http.ResponseWriter, _ *http.Request) {
		jsonListResponse(w, []generated.AlertChannelDto{
			alertChannelFixture(currentName, currentHash),
		})
	})

	mock.Handle(http.MethodPut, "/api/v1/alert-channels/22222222-2222-2222-2222-222222222222", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if !readBody(w, r, &req) {
			return
		}
		if name, ok := req["name"].(string); ok && name != "" {
			currentName = name
		}
		currentHash = "hash-updated"
		jsonResponse(w, alertChannelFixture(currentName, currentHash))
	})

	mock.Handle(http.MethodDelete, "/api/v1/alert-channels/22222222-2222-2222-2222-222222222222", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfigBlock(mock.URL()) + `
resource "devhelm_alert_channel" "slack" {
  name         = "primary-slack"
  channel_type = "slack"
  webhook_url  = "https://hooks.slack.com/services/AAA/BBB/CCC"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("devhelm_alert_channel.slack", "id"),
					resource.TestCheckResourceAttr("devhelm_alert_channel.slack", "channel_type", "slack"),
					resource.TestCheckResourceAttr("devhelm_alert_channel.slack", "config_hash", "hash-create"),
					// `webhook_url` was supplied by config — Terraform retains
					// it in state from the plan. The interesting assertion is
					// that the *server-returned* config_hash flows in too.
					resource.TestCheckResourceAttr("devhelm_alert_channel.slack", "webhook_url", "https://hooks.slack.com/services/AAA/BBB/CCC"),
				),
			},
			{
				Config: providerConfigBlock(mock.URL()) + `
resource "devhelm_alert_channel" "slack" {
  name         = "renamed-slack"
  channel_type = "slack"
  webhook_url  = "https://hooks.slack.com/services/AAA/BBB/CCC"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("devhelm_alert_channel.slack", "name", "renamed-slack"),
					resource.TestCheckResourceAttr("devhelm_alert_channel.slack", "config_hash", "hash-updated"),
					resource.TestCheckResourceAttr("devhelm_alert_channel.slack", "id", "22222222-2222-2222-2222-222222222222"),
				),
			},
		},
	})
}

func TestAccAlertChannel_ImportByName(t *testing.T) {
	requireAcc(t)
	if !hasTerraformBinary() {
		t.Skip("terraform binary not on PATH")
	}

	mock := newMockAPI(t)

	mock.Handle(http.MethodPost, "/api/v1/alert-channels", func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if !readBody(w, r, &req) {
			return
		}
		jsonResponse(w, alertChannelFixture(req["name"].(string), "hash-imp"))
	})
	mock.Handle(http.MethodGet, "/api/v1/alert-channels", func(w http.ResponseWriter, _ *http.Request) {
		jsonListResponse(w, []generated.AlertChannelDto{
			alertChannelFixture("imp-channel", "hash-imp"),
		})
	})
	mock.Handle(http.MethodDelete, "/api/v1/alert-channels/22222222-2222-2222-2222-222222222222", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: newProviderFactories(),
		Steps: []resource.TestStep{
			{
				Config: providerConfigBlock(mock.URL()) + `
resource "devhelm_alert_channel" "imp" {
  name         = "imp-channel"
  channel_type = "slack"
  webhook_url  = "https://hooks.slack.com/services/X/Y/Z"
}
`,
			},
			{
				ResourceName:      "devhelm_alert_channel.imp",
				ImportState:       true,
				ImportStateId:     "imp-channel", // by name, not UUID
				ImportStateVerify: true,
				// Sensitive/config fields aren't round-tripped on Read (the
				// API only echoes the configHash), so they won't appear in
				// imported state — skip them in the equality check.
				ImportStateVerifyIgnore: []string{"webhook_url"},
			},
		},
	})
}
