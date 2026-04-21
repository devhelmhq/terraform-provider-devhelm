#!/usr/bin/env bash
#
# Regenerate Go types from an arbitrary OpenAPI spec file, then `go build`
# the provider so subsequent `terraform plan` invocations against it pick
# up the new types.
#
# Usage: scripts/regen-from.sh <path-to-spec.json>
#
# Per-artifact entry point for the spec-evolution harness
# (`mono/tests/surfaces/evolution/`). The harness handles backup/restore.
#
# Behavior:
#   - copies <path-to-spec.json> over docs/openapi/monitoring-api.json
#   - runs typegen.sh (which invokes preprocessor + oapi-codegen)
#   - runs `go build ./...` so type errors surface immediately
#   - prints absolute path to internal/generated/types.go on stdout
#
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: $0 <path-to-spec.json>" >&2
  exit 1
fi

INPUT_SPEC="$1"
if [[ ! -f "$INPUT_SPEC" ]]; then
  echo "error: spec not found at $INPUT_SPEC" >&2
  exit 1
fi

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
TARGET_SPEC="$ROOT_DIR/docs/openapi/monitoring-api.json"
OUTPUT="$ROOT_DIR/internal/generated/types.go"

# Skip the copy when the caller passes the vendored spec back in (harness
# post-session teardown re-regens from the restored baseline).
INPUT_ABS="$(cd "$(dirname "$INPUT_SPEC")" && pwd)/$(basename "$INPUT_SPEC")"
TARGET_ABS="$(cd "$(dirname "$TARGET_SPEC")" && pwd)/$(basename "$TARGET_SPEC")"
if [[ "$INPUT_ABS" != "$TARGET_ABS" ]]; then
  cp "$INPUT_SPEC" "$TARGET_SPEC"
fi

"$SCRIPT_DIR/typegen.sh" >&2

# Surface any type errors introduced by the regen — much cheaper than
# letting them pop up later in a Terraform acceptance test.
(cd "$ROOT_DIR" && go build ./... >&2)

echo "$OUTPUT"
