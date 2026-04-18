#!/usr/bin/env bash
#
# Generate the docs/ tree from provider schema descriptions and the
# examples/ directory.
#
# Why a wrapper instead of calling `tfplugindocs generate` directly:
#
#   tfplugindocs builds the provider as `hashicorp/<name>` (the only
#   convention it knows) and then runs `terraform providers schema` to
#   discover types. Our provider lives at `devhelmhq/devhelm`, so the
#   default invocation hits a permission-denied / missing-provider error.
#
# The fix is to:
#
#   1. Build the provider ourselves and stash it under a temp plugins dir
#      structured as `plugins/registry.terraform.io/devhelmhq/devhelm/...`.
#   2. Point `terraform providers schema -json` at a tiny project that
#      requires `devhelmhq/devhelm` and resolves it via dev_overrides.
#   3. Pass the resulting schema JSON to `tfplugindocs generate
#      --providers-schema`, which causes it to skip the build/exec step
#      entirely and consume the schema as-is.
#
# Net effect: a vanilla `make docs` regenerates the entire docs/ tree
# from schema + examples/ in ~5 seconds, and the same path runs in CI.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

PLUGIN_DIR="$TMP_DIR/plugins/registry.terraform.io/devhelmhq/devhelm/0.1.0-dev/$(go env GOOS)_$(go env GOARCH)"
mkdir -p "$PLUGIN_DIR"

echo "==> building provider into $PLUGIN_DIR"
(cd "$REPO_ROOT" && go build -o "$PLUGIN_DIR/terraform-provider-devhelm" .)

# A throwaway project that just declares the provider so we can ask
# Terraform for its schema.
SCHEMA_DIR="$TMP_DIR/schema-project"
mkdir -p "$SCHEMA_DIR"
cat >"$SCHEMA_DIR/main.tf" <<'EOF'
terraform {
  required_providers {
    devhelm = {
      source  = "devhelmhq/devhelm"
      version = "0.1.0-dev"
    }
  }
}
EOF

# tfplugindocs uses the same dev_overrides mechanism Terraform uses for
# local provider development. We point it at the binary we just built.
TFRC="$TMP_DIR/terraformrc"
cat >"$TFRC" <<EOF
provider_installation {
  dev_overrides {
    "devhelmhq/devhelm" = "$PLUGIN_DIR"
  }
  direct {}
}
EOF

echo "==> exporting provider schema"
TF_CLI_CONFIG_FILE="$TFRC" terraform -chdir="$SCHEMA_DIR" providers schema -json \
  >"$TMP_DIR/schema.raw.json" 2>/dev/null

# tfplugindocs only looks up the provider schema under the keys
# `<short>` (e.g. "devhelm") or `registry.terraform.io/hashicorp/<short>`.
# Our real key is `registry.terraform.io/devhelmhq/devhelm`, so we
# duplicate it under both lookup keys before handing the JSON over.
python3 - "$TMP_DIR/schema.raw.json" "$TMP_DIR/schema.json" <<'PY'
import json, sys
src, dst = sys.argv[1], sys.argv[2]
with open(src) as f:
    data = json.load(f)
schemas = data.get("provider_schemas", {})
real_key = "registry.terraform.io/devhelmhq/devhelm"
if real_key not in schemas:
    raise SystemExit(f"expected key {real_key} not found in {list(schemas)}")
schemas["devhelm"] = schemas[real_key]
schemas["registry.terraform.io/hashicorp/devhelm"] = schemas[real_key]
data["provider_schemas"] = schemas
with open(dst, "w") as f:
    json.dump(data, f)
PY

echo "==> running tfplugindocs generate"
(cd "$REPO_ROOT" && go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate \
  --provider-name devhelm \
  --rendered-provider-name DevHelm \
  --providers-schema "$TMP_DIR/schema.json")

echo "==> docs regenerated under docs/"
