package api

import "testing"

// TestPaths_Constants pins the wire spelling of every collection endpoint. If
// the API base path or a resource name ever changes, this test (and the
// matching acceptance-test mock handlers) is the canary.
func TestPaths_Constants(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"alert-channels", PathAlertChannels, "/api/v1/alert-channels"},
		{"environments", PathEnvironments, "/api/v1/environments"},
		{"monitors", PathMonitors, "/api/v1/monitors"},
		{"notification-policies", PathNotificationPolicies, "/api/v1/notification-policies"},
		{"resource-groups", PathResourceGroups, "/api/v1/resource-groups"},
		{"secrets", PathSecrets, "/api/v1/secrets"},
		{"service-subscriptions", PathServiceSubscriptions, "/api/v1/service-subscriptions"},
		{"status-pages", PathStatusPages, "/api/v1/status-pages"},
		{"tags", PathTags, "/api/v1/tags"},
		{"webhooks", PathWebhooks, "/api/v1/webhooks"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}

// TestPaths_Item exercises the per-resource item helpers. These cover the
// happy paths the provider Create/Read/Update/Delete implementations rely on.
func TestPaths_Item(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		got  string
		want string
	}{
		{"AlertChannelPath", AlertChannelPath("ac-1"), "/api/v1/alert-channels/ac-1"},
		{"EnvironmentPath", EnvironmentPath("prod"), "/api/v1/environments/prod"},
		{"MonitorPath", MonitorPath("m-1"), "/api/v1/monitors/m-1"},
		{"MonitorTagsPath", MonitorTagsPath("m-1"), "/api/v1/monitors/m-1/tags"},
		{"NotificationPolicyPath", NotificationPolicyPath("np-1"), "/api/v1/notification-policies/np-1"},
		{"ResourceGroupPath", ResourceGroupPath("rg-1"), "/api/v1/resource-groups/rg-1"},
		{"ResourceGroupMembersPath", ResourceGroupMembersPath("rg-1"), "/api/v1/resource-groups/rg-1/members"},
		{"ResourceGroupMemberPath", ResourceGroupMemberPath("rg-1", "mem-2"), "/api/v1/resource-groups/rg-1/members/mem-2"},
		{"SecretPath", SecretPath("api_key"), "/api/v1/secrets/api_key"},
		{"ServiceSubscriptionPath", ServiceSubscriptionPath("sub-1"), "/api/v1/service-subscriptions/sub-1"},
		{"ServiceSubscriptionAlertSensitivityPath", ServiceSubscriptionAlertSensitivityPath("sub-1"), "/api/v1/service-subscriptions/sub-1/alert-sensitivity"},
		{"StatusPagePath", StatusPagePath("sp-1"), "/api/v1/status-pages/sp-1"},
		{"StatusPageComponentsPath", StatusPageComponentsPath("sp-1"), "/api/v1/status-pages/sp-1/components"},
		{"StatusPageComponentPath", StatusPageComponentPath("sp-1", "c-2"), "/api/v1/status-pages/sp-1/components/c-2"},
		{"StatusPageGroupsPath", StatusPageGroupsPath("sp-1"), "/api/v1/status-pages/sp-1/groups"},
		{"StatusPageGroupPath", StatusPageGroupPath("sp-1", "g-2"), "/api/v1/status-pages/sp-1/groups/g-2"},
		{"StatusPageDomainsPath", StatusPageDomainsPath("sp-1"), "/api/v1/status-pages/sp-1/domains"},
		{"StatusPageDomainPath", StatusPageDomainPath("sp-1", "d-2"), "/api/v1/status-pages/sp-1/domains/d-2"},
		{"StatusPageDomainVerifyPath", StatusPageDomainVerifyPath("sp-1", "d-2"), "/api/v1/status-pages/sp-1/domains/d-2/verify"},
		{"TagPath", TagPath("t-1"), "/api/v1/tags/t-1"},
		{"WebhookPath", WebhookPath("wh-1"), "/api/v1/webhooks/wh-1"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}
