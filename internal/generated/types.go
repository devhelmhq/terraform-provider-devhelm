// Package generated contains Go types auto-generated from the DevHelm OpenAPI spec.
//
// DO NOT EDIT — regenerate with: make typegen
//
// This file is a bootstrap placeholder that matches what oapi-codegen would
// produce from docs/openapi/monitoring-api.json. Once oapi-codegen runs
// against the real spec it replaces this file entirely.
//
// Required (always-present) response fields → value types.
// Nullable / optional fields → pointer types.
// JSON tags match the API's camelCase convention.
package generated

import "encoding/json"

// ── Tags ────────────────────────────────────────────────────────────────

type TagDto struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
}

type CreateTagRequest struct {
	Name  string  `json:"name"`
	Color *string `json:"color,omitempty"`
}

type UpdateTagRequest = CreateTagRequest

// ── Environments ────────────────────────────────────────────────────────

type EnvironmentDto struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	Slug      string            `json:"slug"`
	IsDefault bool              `json:"isDefault"`
	Variables map[string]string `json:"variables"`
}

type CreateEnvironmentRequest struct {
	Name      string            `json:"name"`
	Slug      string            `json:"slug"`
	Variables map[string]string `json:"variables,omitempty"`
	IsDefault *bool             `json:"isDefault,omitempty"`
}

type UpdateEnvironmentRequest struct {
	Name      *string            `json:"name,omitempty"`
	Variables map[string]string  `json:"variables,omitempty"`
	IsDefault *bool              `json:"isDefault,omitempty"`
}

// ── Secrets ─────────────────────────────────────────────────────────────

type SecretDto struct {
	ID        string `json:"id"`
	Key       string `json:"key"`
	ValueHash string `json:"valueHash"`
}

type CreateSecretRequest struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type UpdateSecretRequest struct {
	Value string `json:"value"`
}

// ── Alert Channels ──────────────────────────────────────────────────────

type AlertChannelDto struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ChannelType string `json:"channelType"`
	ConfigHash  string `json:"configHash"`
}

type CreateAlertChannelRequest struct {
	Name   string          `json:"name"`
	Config json.RawMessage `json:"config"`
}

type UpdateAlertChannelRequest = CreateAlertChannelRequest

// Channel config discriminated union types

type SlackChannelConfig struct {
	ChannelType string  `json:"channelType"`
	WebhookURL  string  `json:"webhookUrl"`
	MentionText *string `json:"mentionText,omitempty"`
}

type DiscordChannelConfig struct {
	ChannelType   string  `json:"channelType"`
	WebhookURL    string  `json:"webhookUrl"`
	MentionRoleID *string `json:"mentionRoleId,omitempty"`
}

type EmailChannelConfig struct {
	ChannelType string   `json:"channelType"`
	Recipients  []string `json:"recipients"`
}

type PagerDutyChannelConfig struct {
	ChannelType      string  `json:"channelType"`
	RoutingKey       string  `json:"routingKey"`
	SeverityOverride *string `json:"severityOverride,omitempty"`
}

type OpsGenieChannelConfig struct {
	ChannelType string  `json:"channelType"`
	APIKey      string  `json:"apiKey"`
	Region      *string `json:"region,omitempty"`
}

type TeamsChannelConfig struct {
	ChannelType string `json:"channelType"`
	WebhookURL  string `json:"webhookUrl"`
}

type WebhookChannelConfig struct {
	ChannelType   string            `json:"channelType"`
	URL           string            `json:"url"`
	CustomHeaders map[string]string `json:"customHeaders,omitempty"`
	SigningSecret *string           `json:"signingSecret,omitempty"`
}

// ── Notification Policies ───────────────────────────────────────────────

type NotificationPolicyDto struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Enabled    bool            `json:"enabled"`
	Priority   int             `json:"priority"`
	MatchRules []MatchRule     `json:"matchRules"`
	Escalation EscalationChain `json:"escalation"`
}

type CreateNotificationPolicyRequest struct {
	Name       string          `json:"name"`
	Enabled    *bool           `json:"enabled,omitempty"`
	Priority   *int            `json:"priority,omitempty"`
	MatchRules []MatchRule     `json:"matchRules,omitempty"`
	Escalation EscalationChain `json:"escalation"`
}

type UpdateNotificationPolicyRequest = CreateNotificationPolicyRequest

type MatchRule struct {
	Type       string   `json:"type"`
	Value      *string  `json:"value,omitempty"`
	Values     []string `json:"values,omitempty"`
	MonitorIDs []string `json:"monitorIds,omitempty"`
	Regions    []string `json:"regions,omitempty"`
}

type EscalationChain struct {
	Steps     []EscalationStep `json:"steps"`
	OnResolve *string          `json:"onResolve,omitempty"`
	OnReopen  *string          `json:"onReopen,omitempty"`
}

type EscalationStep struct {
	ChannelIDs            []string `json:"channelIds"`
	DelayMinutes          *int     `json:"delayMinutes,omitempty"`
	RequireAck            *bool    `json:"requireAck,omitempty"`
	RepeatIntervalSeconds *int     `json:"repeatIntervalSeconds,omitempty"`
}

// ── Webhooks ────────────────────────────────────────────────────────────

type WebhookEndpointDto struct {
	ID               string   `json:"id"`
	URL              string   `json:"url"`
	Description      *string  `json:"description,omitempty"`
	Enabled          bool     `json:"enabled"`
	SubscribedEvents []string `json:"subscribedEvents"`
}

type CreateWebhookEndpointRequest struct {
	URL              string   `json:"url"`
	SubscribedEvents []string `json:"subscribedEvents"`
	Description      *string  `json:"description,omitempty"`
}

type UpdateWebhookEndpointRequest struct {
	URL              *string  `json:"url,omitempty"`
	Description      *string  `json:"description,omitempty"`
	Enabled          *bool    `json:"enabled,omitempty"`
	SubscribedEvents []string `json:"subscribedEvents,omitempty"`
}

// ── Resource Groups ─────────────────────────────────────────────────────

type ResourceGroupDto struct {
	ID                       string                  `json:"id"`
	Name                     string                  `json:"name"`
	Slug                     string                  `json:"slug"`
	Description              *string                 `json:"description,omitempty"`
	AlertPolicyID            *string                 `json:"alertPolicyId,omitempty"`
	DefaultFrequency         *int                    `json:"defaultFrequency,omitempty"`
	DefaultRegions           []string                `json:"defaultRegions,omitempty"`
	DefaultRetryStrategy     *RetryStrategy          `json:"defaultRetryStrategy,omitempty"`
	DefaultAlertChannels     []string                `json:"defaultAlertChannels,omitempty"`
	DefaultEnvironmentID     *string                 `json:"defaultEnvironmentId,omitempty"`
	HealthThresholdType      *string                 `json:"healthThresholdType,omitempty"`
	HealthThresholdValue     *float64                `json:"healthThresholdValue,omitempty"`
	SuppressMemberAlerts     *bool                   `json:"suppressMemberAlerts,omitempty"`
	ConfirmationDelaySeconds *int                    `json:"confirmationDelaySeconds,omitempty"`
	RecoveryCooldownMinutes  *int                    `json:"recoveryCooldownMinutes,omitempty"`
	Members                  []ResourceGroupMemberDto `json:"members,omitempty"`
}

type ResourceGroupMemberDto struct {
	ID         string  `json:"id"`
	GroupID    string  `json:"groupId"`
	MemberType string  `json:"memberType"`
	MonitorID  *string `json:"monitorId,omitempty"`
	ServiceID  *string `json:"serviceId,omitempty"`
	Name       *string `json:"name,omitempty"`
	Slug       *string `json:"slug,omitempty"`
}

type CreateResourceGroupRequest struct {
	Name                     string         `json:"name"`
	Description              *string        `json:"description,omitempty"`
	AlertPolicyID            *string        `json:"alertPolicyId,omitempty"`
	DefaultFrequency         *int           `json:"defaultFrequency,omitempty"`
	DefaultRegions           []string       `json:"defaultRegions,omitempty"`
	DefaultRetryStrategy     *RetryStrategy `json:"defaultRetryStrategy,omitempty"`
	DefaultAlertChannels     []string       `json:"defaultAlertChannels,omitempty"`
	DefaultEnvironmentID     *string        `json:"defaultEnvironmentId,omitempty"`
	HealthThresholdType      *string        `json:"healthThresholdType,omitempty"`
	HealthThresholdValue     *float64       `json:"healthThresholdValue,omitempty"`
	SuppressMemberAlerts     *bool          `json:"suppressMemberAlerts,omitempty"`
	ConfirmationDelaySeconds *int           `json:"confirmationDelaySeconds,omitempty"`
	RecoveryCooldownMinutes  *int           `json:"recoveryCooldownMinutes,omitempty"`
}

type UpdateResourceGroupRequest = CreateResourceGroupRequest

type AddResourceGroupMemberRequest struct {
	MemberType string `json:"memberType"`
	MemberID   string `json:"memberId"`
}

type RetryStrategy struct {
	Type       string `json:"type"`
	MaxRetries *int   `json:"maxRetries,omitempty"`
	Interval   *int   `json:"interval,omitempty"`
}

// ── Monitors ────────────────────────────────────────────────────────────

type MonitorDto struct {
	ID               string               `json:"id"`
	Name             string               `json:"name"`
	Type             string               `json:"type"`
	Config           json.RawMessage      `json:"config"`
	FrequencySeconds int                  `json:"frequencySeconds"`
	Enabled          bool                 `json:"enabled"`
	Regions          []string             `json:"regions"`
	ManagedBy        *string              `json:"managedBy,omitempty"`
	Environment      *Summary             `json:"environment,omitempty"`
	Auth             *json.RawMessage     `json:"auth,omitempty"`
	IncidentPolicy   *IncidentPolicyDto   `json:"incidentPolicy,omitempty"`
	AlertChannelIds  []string             `json:"alertChannelIds,omitempty"`
	Tags             []MonitorTagDto      `json:"tags,omitempty"`
	Assertions       []MonitorAssertionDto `json:"assertions,omitempty"`
	PingUrl          *string              `json:"pingUrl,omitempty"`
}

type Summary struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Slug string `json:"slug"`
}

type MonitorTagDto struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type MonitorAssertionDto struct {
	ID       string          `json:"id"`
	Config   json.RawMessage `json:"config"`
	Severity *string         `json:"severity,omitempty"`
}

type IncidentPolicyDto struct {
	ID           string             `json:"id"`
	TriggerRules []TriggerRule      `json:"triggerRules"`
	Confirmation ConfirmationPolicy `json:"confirmation"`
	Recovery     RecoveryPolicy     `json:"recovery"`
}

type CreateMonitorRequest struct {
	Name             string                       `json:"name"`
	Type             string                       `json:"type"`
	Config           json.RawMessage              `json:"config"`
	ManagedBy        string                       `json:"managedBy"`
	FrequencySeconds *int                         `json:"frequencySeconds,omitempty"`
	Enabled          *bool                        `json:"enabled,omitempty"`
	Regions          []string                     `json:"regions,omitempty"`
	EnvironmentID    *string                      `json:"environmentId,omitempty"`
	Assertions       []CreateAssertionRequest     `json:"assertions,omitempty"`
	Auth             *json.RawMessage             `json:"auth,omitempty"`
	IncidentPolicy   *UpdateIncidentPolicyRequest `json:"incidentPolicy,omitempty"`
	AlertChannelIds  []string                     `json:"alertChannelIds,omitempty"`
	Tags             *AddMonitorTagsRequest       `json:"tags,omitempty"`
}

type UpdateMonitorRequest struct {
	Name               *string                      `json:"name,omitempty"`
	Config             *json.RawMessage             `json:"config,omitempty"`
	ManagedBy          *string                      `json:"managedBy,omitempty"`
	FrequencySeconds   *int                         `json:"frequencySeconds,omitempty"`
	Enabled            *bool                        `json:"enabled,omitempty"`
	Regions            []string                     `json:"regions,omitempty"`
	EnvironmentID      *string                      `json:"environmentId,omitempty"`
	ClearEnvironmentID *bool                        `json:"clearEnvironmentId,omitempty"`
	Assertions         []CreateAssertionRequest     `json:"assertions,omitempty"`
	Auth               *json.RawMessage             `json:"auth,omitempty"`
	ClearAuth          *bool                        `json:"clearAuth,omitempty"`
	IncidentPolicy     *UpdateIncidentPolicyRequest `json:"incidentPolicy,omitempty"`
	AlertChannelIds    []string                     `json:"alertChannelIds,omitempty"`
	Tags               *AddMonitorTagsRequest       `json:"tags,omitempty"`
}

type AddMonitorTagsRequest struct {
	TagIds  []string        `json:"tagIds,omitempty"`
	NewTags []NewTagRequest `json:"newTags,omitempty"`
}

type NewTagRequest struct {
	Name  string  `json:"name"`
	Color *string `json:"color,omitempty"`
}

type CreateAssertionRequest struct {
	Config   json.RawMessage `json:"config"`
	Severity *string         `json:"severity,omitempty"`
}

type UpdateIncidentPolicyRequest struct {
	TriggerRules []TriggerRule      `json:"triggerRules"`
	Confirmation ConfirmationPolicy `json:"confirmation"`
	Recovery     RecoveryPolicy     `json:"recovery"`
}

type TriggerRule struct {
	Type            string  `json:"type"`
	Severity        string  `json:"severity"`
	Scope           *string `json:"scope,omitempty"`
	Count           *int    `json:"count,omitempty"`
	WindowMinutes   *int    `json:"windowMinutes,omitempty"`
	ThresholdMs     *int    `json:"thresholdMs,omitempty"`
	AggregationType *string `json:"aggregationType,omitempty"`
}

type ConfirmationPolicy struct {
	Type              string `json:"type"`
	MinRegionsFailing *int   `json:"minRegionsFailing,omitempty"`
	MaxWaitSeconds    *int   `json:"maxWaitSeconds,omitempty"`
}

type RecoveryPolicy struct {
	ConsecutiveSuccesses *int `json:"consecutiveSuccesses,omitempty"`
	MinRegionsPassing    *int `json:"minRegionsPassing,omitempty"`
	CooldownMinutes      *int `json:"cooldownMinutes,omitempty"`
}

// ── Monitor Config Types ────────────────────────────────────────────────

type HttpMonitorConfig struct {
	Url           string            `json:"url"`
	Method        string            `json:"method"`
	CustomHeaders map[string]string `json:"customHeaders,omitempty"`
	RequestBody   *string           `json:"requestBody,omitempty"`
	ContentType   *string           `json:"contentType,omitempty"`
	VerifyTls     *bool             `json:"verifyTls,omitempty"`
}

type DnsMonitorConfig struct {
	Hostname       string   `json:"hostname"`
	RecordTypes    []string `json:"recordTypes,omitempty"`
	Nameservers    []string `json:"nameservers,omitempty"`
	TimeoutMs      *int     `json:"timeoutMs,omitempty"`
	TotalTimeoutMs *int     `json:"totalTimeoutMs,omitempty"`
}

type TcpMonitorConfig struct {
	Host      string `json:"host"`
	Port      *int   `json:"port,omitempty"`
	TimeoutMs *int   `json:"timeoutMs,omitempty"`
}

type IcmpMonitorConfig struct {
	Host        string `json:"host"`
	PacketCount *int   `json:"packetCount,omitempty"`
	TimeoutMs   *int   `json:"timeoutMs,omitempty"`
}

type HeartbeatMonitorConfig struct {
	ExpectedInterval int `json:"expectedInterval"`
	GracePeriod      int `json:"gracePeriod"`
}

type McpServerMonitorConfig struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

// ── Auth Config Types ───────────────────────────────────────────────────

type BearerAuthConfig struct {
	Type          string  `json:"type"`
	VaultSecretId *string `json:"vaultSecretId,omitempty"`
}

type BasicAuthConfig struct {
	Type          string  `json:"type"`
	VaultSecretId *string `json:"vaultSecretId,omitempty"`
}

type ApiKeyAuthConfig struct {
	Type          string  `json:"type"`
	HeaderName    string  `json:"headerName"`
	VaultSecretId *string `json:"vaultSecretId,omitempty"`
}

type HeaderAuthConfig struct {
	Type          string  `json:"type"`
	HeaderName    string  `json:"headerName"`
	VaultSecretId *string `json:"vaultSecretId,omitempty"`
}

// ── Service Subscriptions (Dependencies) ────────────────────────────────

type ServiceSubscriptionDto struct {
	SubscriptionID   string  `json:"subscriptionId"`
	ServiceID        *string `json:"serviceId,omitempty"`
	Slug             string  `json:"slug"`
	Name             string  `json:"name"`
	ComponentID      *string `json:"componentId,omitempty"`
	AlertSensitivity *string `json:"alertSensitivity,omitempty"`
}

type ServiceSubscribeRequest struct {
	AlertSensitivity *string `json:"alertSensitivity,omitempty"`
	ComponentID      *string `json:"componentId,omitempty"`
}

type UpdateAlertSensitivityRequest struct {
	AlertSensitivity string `json:"alertSensitivity"`
}
