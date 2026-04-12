#!/usr/bin/env bash
#
# Regenerate Go types from the vendored OpenAPI spec.
# Equivalent to `npm run typegen` in CLI/SDK-JS and `scripts/typegen.sh` in SDK-Python.
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"

INPUT="$ROOT_DIR/docs/openapi/monitoring-api.json"
OUTPUT="$ROOT_DIR/internal/generated/types.go"

if [[ ! -f "$INPUT" ]]; then
  echo "error: OpenAPI spec not found at $INPUT" >&2
  echo "hint:  copy from monorepo: cp ../cli-artifacts/docs/openapi/monitoring-api.yaml $INPUT" >&2
  exit 1
fi

echo "=> Generating Go types from OpenAPI spec..."

go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen \
  -generate types \
  -package generated \
  -o "$OUTPUT" \
  "$INPUT"

echo "=> Generated: $OUTPUT"
