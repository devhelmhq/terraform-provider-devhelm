#!/usr/bin/env bash
# Universal release script — matches the pattern across all DevHelm surface repos.
# Usage: ./scripts/release.sh 0.1.0
set -euo pipefail

VERSION="${1:?Usage: $0 <version>}"

if [[ "$VERSION" =~ ^v ]]; then
  echo "error: version should not start with 'v' (we add it automatically)" >&2
  exit 1
fi

TAG="v$VERSION"

if git rev-parse "$TAG" >/dev/null 2>&1; then
  echo "error: tag $TAG already exists" >&2
  exit 1
fi

if [[ -n "$(git status --porcelain)" ]]; then
  echo "error: working directory is not clean — commit or stash changes first" >&2
  exit 1
fi

echo "=> Tagging $TAG on branch $(git branch --show-current)"
git tag -a "$TAG" -m "Release $TAG"
git push origin "$TAG"

echo "=> Done! Release workflow will run at:"
echo "   https://github.com/devhelmhq/terraform-provider-devhelm/actions"
