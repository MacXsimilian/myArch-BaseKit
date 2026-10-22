#!/usr/bin/env bash
set -euo pipefail

# CI supplies the exact commit tested by the preceding jobs. Never move a tag or
# replace an existing release asset when retrying publication.
: "${GH_REPO:?Set GH_REPO to OWNER/REPOSITORY}"
: "${RELEASE_TAG:?Set RELEASE_TAG to vMAJOR.MINOR.PATCH}"
: "${RELEASE_COMMIT:?Set RELEASE_COMMIT to the tested full commit SHA}"
[[ "$GH_REPO" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]] || { echo 'Invalid GH_REPO.' >&2; exit 1; }
[[ "$RELEASE_TAG" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || { echo 'Invalid release tag; expected vMAJOR.MINOR.PATCH.' >&2; exit 1; }
[[ "$RELEASE_COMMIT" =~ ^[0-9a-f]{40}$ ]] || { echo 'RELEASE_COMMIT must be a full lowercase commit SHA.' >&2; exit 1; }

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
dist_dir=$(cd -- "${DIST_DIR:-$repo_root/dist}" && pwd)
archive=myarch-buildkit-linux-x86_64.tar.gz
assets=("$archive" SHA256SUMS)
for asset in "${assets[@]}"; do
  [[ -s "$dist_dir/$asset" ]] || { echo "Missing or empty release asset: $dist_dir/$asset" >&2; exit 1; }
done
expected_checksum=$(cd -- "$dist_dir" && sha256sum "$archive")
[[ "$(cat -- "$dist_dir/SHA256SUMS")" == "$expected_checksum" ]] || {
  echo "SHA256SUMS must contain the exact checksum of $archive only." >&2
  exit 1
}

work_dir=$(mktemp -d)
trap 'rm -rf -- "$work_dir"' EXIT
api_base="repos/$GH_REPO"

# A tag can be lightweight or can point through annotated tags. Resolve it to a
# commit rather than trusting a release's target_commitish (which may be a branch).
tag_object=$(gh api "$api_base/git/ref/tags/$RELEASE_TAG" --jq '[.object.type, .object.sha] | @tsv')
for ((depth = 0; depth < 10; depth++)); do
  IFS=$'\t' read -r object_type object_sha extra <<< "$tag_object"
  [[ "$object_sha" =~ ^[0-9a-f]{40}$ && -z "$extra" ]] || { echo 'GitHub returned an invalid tag object.' >&2; exit 1; }
  if [[ "$object_type" == commit ]]; then
    break
  fi
  [[ "$object_type" == tag ]] || { echo 'Release tag does not resolve to a commit.' >&2; exit 1; }
  tag_object=$(gh api "$api_base/git/tags/$object_sha" --jq '[.object.type, .object.sha] | @tsv')
done
[[ "$object_type" == commit && "$object_sha" == "$RELEASE_COMMIT" ]] || {
  echo "Remote tag $RELEASE_TAG does not resolve to tested commit $RELEASE_COMMIT." >&2
  exit 1
}

# Only an explicit HTTP 404 means absent. Authentication, transport and server
# failures must not be treated as permission to create or alter a release.
release_exists=true
if ! gh api --include --silent "$api_base/releases/tags/$RELEASE_TAG" > "$work_dir/headers" 2> "$work_dir/api-error"; then
  http_status=$(awk '/^HTTP\// { status = $2 } END { print status }' "$work_dir/headers")
  if [[ "$http_status" == 404 ]]; then
    release_exists=false
  else
    cat -- "$work_dir/api-error" >&2
    echo "Could not inspect release $RELEASE_TAG; nothing was published." >&2
    exit 1
  fi
fi

asset_exists() {
  local expected=$1 candidate
  for candidate in "${remote_assets[@]}"; do
    [[ "$candidate" != "$expected" ]] || return 0
  done
  return 1
}

verify_remote_asset() {
  local asset=$1 download_dir
  download_dir=$(mktemp -d "$work_dir/download.XXXXXX")
  gh release download "$RELEASE_TAG" --repo "$GH_REPO" --pattern "$asset" --dir "$download_dir"
  if ! cmp -s -- "$dist_dir/$asset" "$download_dir/$asset"; then
    echo "Existing release asset $asset differs from the tested local asset; refusing to replace it." >&2
    exit 1
  fi
}

if [[ "$release_exists" == true ]]; then
  draft=$(gh release view "$RELEASE_TAG" --repo "$GH_REPO" --json isDraft --jq .isDraft)
  [[ "$draft" == true || "$draft" == false ]] || { echo 'GitHub returned an invalid draft state.' >&2; exit 1; }
  # Capture the command's status before reading its output: mapfile with process
  # substitution would hide a failed GitHub request.
  remote_names=$(gh release view "$RELEASE_TAG" --repo "$GH_REPO" --json assets --jq '.assets[].name')
  mapfile -t remote_assets <<< "$remote_names"
  missing_assets=()
  for asset in "${assets[@]}"; do
    if asset_exists "$asset"; then
      verify_remote_asset "$asset"
    else
      missing_assets+=("$dist_dir/$asset")
    fi
  done
  if [[ "$draft" == false ]]; then
    [[ ${#missing_assets[@]} == 0 ]] || { echo 'Published release is missing required assets; refusing to change it.' >&2; exit 1; }
    echo "Release $RELEASE_TAG is already published with identical assets."
    exit 0
  fi
  if (( ${#missing_assets[@]} > 0 )); then
    gh release upload "$RELEASE_TAG" "${missing_assets[@]}" --repo "$GH_REPO"
  fi
else
  gh release create "$RELEASE_TAG" "$dist_dir/$archive" "$dist_dir/SHA256SUMS" \
    --repo "$GH_REPO" --verify-tag --draft --target "$RELEASE_COMMIT" \
    --title "$RELEASE_TAG" --generate-notes
fi

# A failed upload or changed remote asset leaves a draft for inspection. Confirm
# both exact payloads before making the release visible to users.
for asset in "${assets[@]}"; do
  verify_remote_asset "$asset"
done
gh release edit "$RELEASE_TAG" --repo "$GH_REPO" --draft=false
echo "Published $RELEASE_TAG with verified assets."
