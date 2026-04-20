#!/usr/bin/env bash
#
# Regenerate Go types from the vendored OpenAPI spec.
#
# Uses @devhelm/openapi-tools for preprocessing (shared with all surfaces),
# then runs oapi-codegen for Go type generation.
#
# oapi-codegen version pinning:
#   The committed internal/generated/types.go was produced by oapi-codegen
#   v2.6.0 (see file header). The pinned version lives in go.mod under a
#   `tool` directive so `go tool oapi-codegen` always resolves to the
#   exact build that produced the committed file. Bumping the version
#   requires both `go get -tool ...@<new>` and re-running this script;
#   commit the resulting types.go diff in the same change so CI's
#   drift-check stays green.
#
# Preprocessing resolution order:
#   1. $OPENAPI_TOOLS env var (explicit override)
#   2. Local monorepo sibling (../mini/packages/openapi-tools)
#   3. Vendored preprocessor at scripts/preprocess.mjs (default — used in CI)
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

INPUT="$ROOT_DIR/docs/openapi/monitoring-api.json"
PREPROCESSED="$ROOT_DIR/.openapi-preprocessed.json"
OUTPUT="$ROOT_DIR/internal/generated/types.go"

if [[ ! -f "$INPUT" ]]; then
  echo "error: OpenAPI spec not found at $INPUT" >&2
  echo "hint:  copy from monorepo: cp ../mini/docs/openapi/monitoring-api.json $INPUT" >&2
  exit 1
fi

if ! command -v go >/dev/null 2>&1; then
  echo "error: go toolchain not found in PATH" >&2
  exit 1
fi

if ! command -v node >/dev/null 2>&1; then
  echo "error: node not found in PATH (required by the OpenAPI preprocessor)" >&2
  exit 1
fi

resolve_preprocessor() {
  if [[ -n "${OPENAPI_TOOLS:-}" ]]; then
    # Honour `OPENAPI_TOOLS="node /path/to/cli.js"` style overrides.
    echo "$OPENAPI_TOOLS preprocess"
    return
  fi
  local local_cli="$ROOT_DIR/../mini/packages/openapi-tools/dist/cli.js"
  if [[ -f "$local_cli" ]]; then
    echo "node $local_cli preprocess"
    return
  fi
  echo "node $SCRIPT_DIR/preprocess.mjs"
}

PREPROCESS_CMD=$(resolve_preprocessor)

echo "=> Preprocessing OpenAPI spec (via $PREPROCESS_CMD)..."
$PREPROCESS_CMD "$INPUT" "$PREPROCESSED"

echo "=> Generating Go types from preprocessed spec..."

# Resolve oapi-codegen via the `tool` directive in go.mod so the version
# is always in lockstep with the version recorded there.
(cd "$ROOT_DIR" && go tool oapi-codegen \
  -generate types \
  -package generated \
  -o "$OUTPUT" \
  "$PREPROCESSED")

rm -f "$PREPROCESSED"
echo "=> Generated: $OUTPUT"
