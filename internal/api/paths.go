package api

// Centralized REST endpoint constants for every resource the provider manages.
//
// These exist so that:
//   - Path typos surface as compile errors instead of confusing runtime 404s.
//   - A future API base-path change ("/api/v1/" → "/api/v2/", or a versioned
//     prefix per surface) is a single-file edit.
//   - The provider, datasources, and acceptance tests all reference the same
//     spelling, eliminating the historical drift between resource code and
//     mock-server fixtures (END-1143 / END-1134).
//
// Each *Path helper takes URL segments (IDs, slugs, etc.) as already-escaped
// strings — callers that accept user input should run `PathEscape` on the
// segment first (see `dependency.go`, `environment.go`).
//
// Conventions:
//   - List/collection endpoints are exposed as constants (e.g. PathMonitors).
//   - Item endpoints are exposed as helpers (e.g. MonitorPath(id)) so the
//     "ID slot" cannot be accidentally swapped with a sub-resource name.
//   - Sub-resource helpers compose on top of the parent helpers so a single
//     change to the parent path propagates through.

const (
	// Top-level collections.
	PathAlertChannels        = "/api/v1/alert-channels"
	PathEnvironments         = "/api/v1/environments"
	PathMonitors             = "/api/v1/monitors"
	PathNotificationPolicies = "/api/v1/notification-policies"
	PathResourceGroups       = "/api/v1/resource-groups"
	PathSecrets              = "/api/v1/secrets"
	PathServiceSubscriptions = "/api/v1/service-subscriptions"
	PathStatusPages          = "/api/v1/status-pages"
	PathTags                 = "/api/v1/tags"
	PathWebhooks             = "/api/v1/webhooks"
)

// AlertChannelPath returns /api/v1/alert-channels/{id}.
func AlertChannelPath(id string) string { return PathAlertChannels + "/" + id }

// EnvironmentPath returns /api/v1/environments/{slug}.
func EnvironmentPath(slug string) string { return PathEnvironments + "/" + slug }

// MonitorPath returns /api/v1/monitors/{id}.
func MonitorPath(id string) string { return PathMonitors + "/" + id }

// MonitorTagsPath returns /api/v1/monitors/{id}/tags.
func MonitorTagsPath(monitorID string) string { return MonitorPath(monitorID) + "/tags" }

// NotificationPolicyPath returns /api/v1/notification-policies/{id}.
func NotificationPolicyPath(id string) string { return PathNotificationPolicies + "/" + id }

// ResourceGroupPath returns /api/v1/resource-groups/{id}.
func ResourceGroupPath(id string) string { return PathResourceGroups + "/" + id }

// ResourceGroupMembersPath returns /api/v1/resource-groups/{id}/members.
func ResourceGroupMembersPath(groupID string) string {
	return ResourceGroupPath(groupID) + "/members"
}

// ResourceGroupMemberPath returns /api/v1/resource-groups/{groupId}/members/{memberId}.
func ResourceGroupMemberPath(groupID, memberID string) string {
	return ResourceGroupMembersPath(groupID) + "/" + memberID
}

// SecretPath returns /api/v1/secrets/{key}.
func SecretPath(key string) string { return PathSecrets + "/" + key }

// ServiceSubscriptionPath returns /api/v1/service-subscriptions/{idOrSlug}.
// Callers that pass user-supplied input should pre-escape the segment.
func ServiceSubscriptionPath(idOrSlug string) string {
	return PathServiceSubscriptions + "/" + idOrSlug
}

// ServiceSubscriptionAlertSensitivityPath returns
// /api/v1/service-subscriptions/{id}/alert-sensitivity.
func ServiceSubscriptionAlertSensitivityPath(id string) string {
	return ServiceSubscriptionPath(id) + "/alert-sensitivity"
}

// StatusPagePath returns /api/v1/status-pages/{id}.
func StatusPagePath(id string) string { return PathStatusPages + "/" + id }

// StatusPageComponentsPath returns /api/v1/status-pages/{id}/components.
func StatusPageComponentsPath(pageID string) string {
	return StatusPagePath(pageID) + "/components"
}

// StatusPageComponentPath returns
// /api/v1/status-pages/{pageId}/components/{componentId}.
func StatusPageComponentPath(pageID, componentID string) string {
	return StatusPageComponentsPath(pageID) + "/" + componentID
}

// StatusPageGroupsPath returns /api/v1/status-pages/{id}/groups.
func StatusPageGroupsPath(pageID string) string {
	return StatusPagePath(pageID) + "/groups"
}

// StatusPageGroupPath returns /api/v1/status-pages/{pageId}/groups/{groupId}.
func StatusPageGroupPath(pageID, groupID string) string {
	return StatusPageGroupsPath(pageID) + "/" + groupID
}

// StatusPageDomainsPath returns /api/v1/status-pages/{id}/domains.
func StatusPageDomainsPath(pageID string) string {
	return StatusPagePath(pageID) + "/domains"
}

// StatusPageDomainPath returns
// /api/v1/status-pages/{pageId}/domains/{domainId}.
func StatusPageDomainPath(pageID, domainID string) string {
	return StatusPageDomainsPath(pageID) + "/" + domainID
}

// StatusPageDomainVerifyPath returns
// /api/v1/status-pages/{pageId}/domains/{domainId}/verify.
func StatusPageDomainVerifyPath(pageID, domainID string) string {
	return StatusPageDomainPath(pageID, domainID) + "/verify"
}

// TagPath returns /api/v1/tags/{id}.
func TagPath(id string) string { return PathTags + "/" + id }

// WebhookPath returns /api/v1/webhooks/{id}.
func WebhookPath(id string) string { return PathWebhooks + "/" + id }
