// Package resources — monitor `auth` block plumbing.
//
// The Terraform-facing schema models monitor authentication as a single
// nested attribute with one sub-attribute per discriminator variant
// (bearer / basic / header / api_key). Exactly one variant must be set
// per `auth` block; this is enforced at plan time by `validateMonitorAuth`
// because Terraform's schema model has no "exactly one of" primitive that
// works for nested attributes (every sub-attribute must be Optional so the
// user can choose any single variant).
//
// Wire-format mapping is one-to-one with the OpenAPI `MonitorAuthConfig`
// discriminated union — we use the codegen `From*AuthConfig` /
// `As*AuthConfig` methods directly so the JSON roundtrip stays in lockstep
// with whatever the spec evolves to.
//
// This refactor superseded a legacy `auth = jsonencode({ ... })`
// stringly-typed shape. Because there are no production customers yet,
// no `UpgradeState` migrator is provided — existing alpha users are
// expected to remove the resource from state and re-import after the
// upgrade. See `cowork/design/040-codegen-policies.md` for the full
// rationale.
package resources

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/devhelmhq/terraform-provider-devhelm/internal/generated"
	"github.com/google/uuid"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-framework/types/basetypes"
	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ── Object types ────────────────────────────────────────────────────────

func authBearerObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"vault_secret_id": types.StringType,
	}}
}

func authBasicObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"vault_secret_id": types.StringType,
	}}
}

func authHeaderObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"header_name":     types.StringType,
		"vault_secret_id": types.StringType,
	}}
}

func authApiKeyObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"header_name":     types.StringType,
		"vault_secret_id": types.StringType,
	}}
}

func monitorAuthObjectType() types.ObjectType {
	return types.ObjectType{AttrTypes: map[string]attr.Type{
		"bearer":  authBearerObjectType(),
		"basic":   authBasicObjectType(),
		"header":  authHeaderObjectType(),
		"api_key": authApiKeyObjectType(),
	}}
}

// ── TF → API ────────────────────────────────────────────────────────────

// buildMonitorAuthConfig converts the user-facing `auth` nested object
// into the generated discriminated union. Returns (nil, nil) when the
// attribute is unset (null/unknown) — callers translate that into either
// "omit Auth on Create" or "set ClearAuth on Update".
//
// Validation (exactly-one variant, required fields per variant) happens
// in validateMonitorAuth at plan time; we re-check the invariant here as
// a defense-in-depth measure so any path that bypasses ValidateConfig
// (e.g. a future programmatic caller) still fails closed.
func buildMonitorAuthConfig(ctx context.Context, auth types.Object) (*generated.MonitorAuthConfig, error) {
	if auth.IsNull() || auth.IsUnknown() {
		return nil, nil
	}
	var m authModel
	d := auth.As(ctx, &m, basetypes.ObjectAsOptions{
		UnhandledNullAsEmpty:    true,
		UnhandledUnknownAsEmpty: true,
	})
	if d.HasError() {
		return nil, fmt.Errorf("monitor auth: %s", diagsString(d))
	}
	variants := authVariantsSet(m)
	switch len(variants) {
	case 0:
		return nil, fmt.Errorf("monitor auth: no variant set; specify one of bearer/basic/header/api_key")
	case 1:
		// fall through
	default:
		return nil, fmt.Errorf("monitor auth: %d variants set (%v); exactly one is allowed", len(variants), variants)
	}

	out := &generated.MonitorAuthConfig{}

	switch variants[0] {
	case "bearer":
		var v authSecretOnlyVariantModel
		if err := unwrapSecretOnlyVariant(ctx, m.Bearer, &v); err != nil {
			return nil, err
		}
		secret, err := parseVaultSecretIDPtr(v.VaultSecretID)
		if err != nil {
			return nil, fmt.Errorf("auth.bearer.vault_secret_id: %w", err)
		}
		if err := out.FromBearerAuthConfig(generated.BearerAuthConfig{
			Type:          generated.Bearer,
			VaultSecretId: secret,
		}); err != nil {
			return nil, fmt.Errorf("auth.bearer: %w", err)
		}
	case "basic":
		var v authSecretOnlyVariantModel
		if err := unwrapSecretOnlyVariant(ctx, m.Basic, &v); err != nil {
			return nil, err
		}
		secret, err := parseVaultSecretIDPtr(v.VaultSecretID)
		if err != nil {
			return nil, fmt.Errorf("auth.basic.vault_secret_id: %w", err)
		}
		if err := out.FromBasicAuthConfig(generated.BasicAuthConfig{
			Type:          generated.Basic,
			VaultSecretId: secret,
		}); err != nil {
			return nil, fmt.Errorf("auth.basic: %w", err)
		}
	case "header":
		var v authHeaderVariantModel
		if err := unwrapHeaderVariant(ctx, m.Header, &v); err != nil {
			return nil, err
		}
		if v.HeaderName.IsNull() || v.HeaderName.IsUnknown() || v.HeaderName.ValueString() == "" {
			return nil, fmt.Errorf("auth.header.header_name: required")
		}
		secret, err := parseVaultSecretIDPtr(v.VaultSecretID)
		if err != nil {
			return nil, fmt.Errorf("auth.header.vault_secret_id: %w", err)
		}
		if err := out.FromHeaderAuthConfig(generated.HeaderAuthConfig{
			Type:          generated.Header,
			HeaderName:    v.HeaderName.ValueString(),
			VaultSecretId: secret,
		}); err != nil {
			return nil, fmt.Errorf("auth.header: %w", err)
		}
	case "api_key":
		var v authHeaderVariantModel
		if err := unwrapHeaderVariant(ctx, m.ApiKey, &v); err != nil {
			return nil, err
		}
		if v.HeaderName.IsNull() || v.HeaderName.IsUnknown() || v.HeaderName.ValueString() == "" {
			return nil, fmt.Errorf("auth.api_key.header_name: required")
		}
		secret, err := parseVaultSecretIDPtr(v.VaultSecretID)
		if err != nil {
			return nil, fmt.Errorf("auth.api_key.vault_secret_id: %w", err)
		}
		if err := out.FromApiKeyAuthConfig(generated.ApiKeyAuthConfig{
			Type:          generated.ApiKeyAuthConfigTypeApiKey,
			HeaderName:    v.HeaderName.ValueString(),
			VaultSecretId: secret,
		}); err != nil {
			return nil, fmt.Errorf("auth.api_key: %w", err)
		}
	}
	return out, nil
}

// authVariantsSet returns the names of the variants that have a non-null,
// non-unknown nested object. Used by buildMonitorAuthConfig and
// validateMonitorAuth.
func authVariantsSet(m authModel) []string {
	var variants []string
	if !m.Bearer.IsNull() && !m.Bearer.IsUnknown() {
		variants = append(variants, "bearer")
	}
	if !m.Basic.IsNull() && !m.Basic.IsUnknown() {
		variants = append(variants, "basic")
	}
	if !m.Header.IsNull() && !m.Header.IsUnknown() {
		variants = append(variants, "header")
	}
	if !m.ApiKey.IsNull() && !m.ApiKey.IsUnknown() {
		variants = append(variants, "api_key")
	}
	return variants
}

func unwrapSecretOnlyVariant(ctx context.Context, obj types.Object, dst *authSecretOnlyVariantModel) error {
	d := obj.As(ctx, dst, basetypes.ObjectAsOptions{
		UnhandledNullAsEmpty:    true,
		UnhandledUnknownAsEmpty: true,
	})
	if d.HasError() {
		return fmt.Errorf("decoding nested object: %s", diagsString(d))
	}
	return nil
}

func unwrapHeaderVariant(ctx context.Context, obj types.Object, dst *authHeaderVariantModel) error {
	d := obj.As(ctx, dst, basetypes.ObjectAsOptions{
		UnhandledNullAsEmpty:    true,
		UnhandledUnknownAsEmpty: true,
	})
	if d.HasError() {
		return fmt.Errorf("decoding nested object: %s", diagsString(d))
	}
	return nil
}

func parseVaultSecretIDPtr(v types.String) (*openapi_types.UUID, error) {
	if v.IsNull() || v.IsUnknown() || v.ValueString() == "" {
		return nil, nil
	}
	u, err := uuid.Parse(v.ValueString())
	if err != nil {
		return nil, fmt.Errorf("invalid UUID %q: %w", v.ValueString(), err)
	}
	id := openapi_types.UUID(u)
	return &id, nil
}

func diagsString(d diag.Diagnostics) string {
	if !d.HasError() && len(d) == 0 {
		return ""
	}
	var msgs []string
	for _, x := range d {
		msgs = append(msgs, x.Summary()+": "+x.Detail())
	}
	out, _ := json.Marshal(msgs)
	return string(out)
}

// ── API → TF ────────────────────────────────────────────────────────────

// mapMonitorAuthToTF mirrors a generated MonitorAuthConfig union back into
// the typed `auth` nested object. Returns a typed-null when the API echoed
// no auth (or an empty union), preserving the "no auth attached" signal
// for the next plan.
func mapMonitorAuthToTF(ctx context.Context, dto *generated.MonitorAuthConfig) (types.Object, diag.Diagnostics) {
	if dto == nil {
		return types.ObjectNull(monitorAuthObjectType().AttrTypes), nil
	}
	raw, err := dto.MarshalJSON()
	if err != nil || !unionHasData(raw) {
		return types.ObjectNull(monitorAuthObjectType().AttrTypes), nil
	}
	disc, err := dto.Discriminator()
	if err != nil || disc == "" {
		return types.ObjectNull(monitorAuthObjectType().AttrTypes), nil
	}

	bearer := types.ObjectNull(authBearerObjectType().AttrTypes)
	basic := types.ObjectNull(authBasicObjectType().AttrTypes)
	header := types.ObjectNull(authHeaderObjectType().AttrTypes)
	apikey := types.ObjectNull(authApiKeyObjectType().AttrTypes)

	switch disc {
	case "bearer":
		v, err := dto.AsBearerAuthConfig()
		if err != nil {
			return types.ObjectNull(monitorAuthObjectType().AttrTypes),
				diag.Diagnostics{diag.NewErrorDiagnostic("Decoding monitor auth (bearer)", err.Error())}
		}
		obj, d := types.ObjectValue(authBearerObjectType().AttrTypes, map[string]attr.Value{
			"vault_secret_id": uuidPtrToString(v.VaultSecretId),
		})
		if d.HasError() {
			return types.ObjectNull(monitorAuthObjectType().AttrTypes), d
		}
		bearer = obj
	case "basic":
		v, err := dto.AsBasicAuthConfig()
		if err != nil {
			return types.ObjectNull(monitorAuthObjectType().AttrTypes),
				diag.Diagnostics{diag.NewErrorDiagnostic("Decoding monitor auth (basic)", err.Error())}
		}
		obj, d := types.ObjectValue(authBasicObjectType().AttrTypes, map[string]attr.Value{
			"vault_secret_id": uuidPtrToString(v.VaultSecretId),
		})
		if d.HasError() {
			return types.ObjectNull(monitorAuthObjectType().AttrTypes), d
		}
		basic = obj
	case "header":
		v, err := dto.AsHeaderAuthConfig()
		if err != nil {
			return types.ObjectNull(monitorAuthObjectType().AttrTypes),
				diag.Diagnostics{diag.NewErrorDiagnostic("Decoding monitor auth (header)", err.Error())}
		}
		obj, d := types.ObjectValue(authHeaderObjectType().AttrTypes, map[string]attr.Value{
			"header_name":     types.StringValue(v.HeaderName),
			"vault_secret_id": uuidPtrToString(v.VaultSecretId),
		})
		if d.HasError() {
			return types.ObjectNull(monitorAuthObjectType().AttrTypes), d
		}
		header = obj
	case "api_key":
		v, err := dto.AsApiKeyAuthConfig()
		if err != nil {
			return types.ObjectNull(monitorAuthObjectType().AttrTypes),
				diag.Diagnostics{diag.NewErrorDiagnostic("Decoding monitor auth (api_key)", err.Error())}
		}
		obj, d := types.ObjectValue(authApiKeyObjectType().AttrTypes, map[string]attr.Value{
			"header_name":     types.StringValue(v.HeaderName),
			"vault_secret_id": uuidPtrToString(v.VaultSecretId),
		})
		if d.HasError() {
			return types.ObjectNull(monitorAuthObjectType().AttrTypes), d
		}
		apikey = obj
	default:
		// Unknown discriminator (e.g. spec evolution introduced a new
		// variant). Surface as an actionable error rather than silently
		// dropping it; the user must upgrade the provider.
		return types.ObjectNull(monitorAuthObjectType().AttrTypes), diag.Diagnostics{
			diag.NewErrorDiagnostic(
				"Unknown monitor auth variant",
				fmt.Sprintf("API returned auth.type=%q which this provider does not understand. Upgrade the provider to a version that supports the new variant.", disc),
			),
		}
	}

	obj, d := types.ObjectValue(monitorAuthObjectType().AttrTypes, map[string]attr.Value{
		"bearer":  bearer,
		"basic":   basic,
		"header":  header,
		"api_key": apikey,
	})
	return obj, d
}

func uuidPtrToString(u *openapi_types.UUID) types.String {
	if u == nil {
		return types.StringNull()
	}
	return types.StringValue(u.String())
}

// ── Plan-time validation ────────────────────────────────────────────────

// validateMonitorAuth enforces the "exactly one variant" invariant and
// per-variant required fields. All diagnostics are attribute-pathed
// (`auth.<variant>[.<field>]`) so the editor underlines the right block.
func validateMonitorAuth(ctx context.Context, diags *diag.Diagnostics, auth types.Object) {
	if auth.IsNull() || auth.IsUnknown() {
		return
	}
	var m authModel
	if d := auth.As(ctx, &m, basetypes.ObjectAsOptions{
		UnhandledNullAsEmpty:    true,
		UnhandledUnknownAsEmpty: true,
	}); d.HasError() {
		diags.Append(d...)
		return
	}
	variants := authVariantsSet(m)
	switch len(variants) {
	case 0:
		diags.AddAttributeError(
			path.Root("auth"),
			"Missing auth variant",
			"`auth` is set but no variant block is populated. Specify exactly one of `bearer`, `basic`, `header`, or `api_key` (or omit `auth` entirely to remove credentials).",
		)
		return
	case 1:
		// fall through to per-variant validation
	default:
		diags.AddAttributeError(
			path.Root("auth"),
			"Multiple auth variants set",
			fmt.Sprintf("`auth` blocks must specify exactly one variant; got %d (%v). Pick one of bearer/basic/header/api_key per monitor.", len(variants), variants),
		)
		return
	}

	switch variants[0] {
	case "bearer", "basic":
		// Both variants only carry `vault_secret_id`, which is itself
		// optional in the API contract — declaring the scheme without
		// a secret is allowed (e.g. an external system populates the
		// vault entry later). Nothing else to enforce.
	case "header":
		validateHeaderVariant(ctx, diags, m.Header, "header")
	case "api_key":
		validateHeaderVariant(ctx, diags, m.ApiKey, "api_key")
	}
}

func validateHeaderVariant(ctx context.Context, diags *diag.Diagnostics, obj types.Object, name string) {
	var v authHeaderVariantModel
	if d := obj.As(ctx, &v, basetypes.ObjectAsOptions{
		UnhandledNullAsEmpty:    true,
		UnhandledUnknownAsEmpty: true,
	}); d.HasError() {
		diags.Append(d...)
		return
	}
	if (v.HeaderName.IsNull() || v.HeaderName.IsUnknown() || v.HeaderName.ValueString() == "") && !v.HeaderName.IsUnknown() {
		diags.AddAttributeError(
			path.Root("auth").AtName(name).AtName("header_name"),
			"Missing required attribute",
			fmt.Sprintf("`auth.%s.header_name` is required.", name),
		)
	}
}

// ── UUID validator ──────────────────────────────────────────────────────

// uuidStringValidator rejects strings that are not RFC-4122 UUIDs at plan
// time so users see the offending value instead of a generic 400 from the
// API. Unknown / null values are passed through (they're handled by
// Required / Optional schema markers and downstream validators).
type uuidStringValidator struct{}

func (v uuidStringValidator) Description(_ context.Context) string {
	return "value must be a UUID (e.g. 11111111-2222-3333-4444-555555555555)"
}

func (v uuidStringValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v uuidStringValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	s := req.ConfigValue.ValueString()
	if s == "" {
		return
	}
	if _, err := uuid.Parse(s); err != nil {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Invalid UUID",
			fmt.Sprintf("Expected a UUID string, got %q: %s", s, err),
		)
	}
}
