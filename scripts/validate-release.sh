#!/usr/bin/env bash
set -euo pipefail

# Only stable semantic-version tags are published automatically.
version=${1:?Usage: validate-release.sh vMAJOR.MINOR.PATCH}
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
if [[ ! "$version" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
  echo "Invalid release tag: $version (expected vMAJOR.MINOR.PATCH)" >&2
  exit 1
fi
source_version=$(sed -n 's/^var BuildID = "\([^"]*\)"$/\1/p' "$repo_root/cmd/myarch-buildkit/version.go")
if [[ "$version" != "$source_version" ]]; then
  echo "Release tag $version does not match source version $source_version" >&2
  exit 1
fi
