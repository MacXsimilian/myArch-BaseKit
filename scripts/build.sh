#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"
mode=${1:-build}
if [[ "$mode" != build && "$mode" != package ]]; then
  echo 'Usage: build.sh [build|package]' >&2
  exit 1
fi
: "${GO:=go}"
: "${VERSION:=}"
source_version=$(sed -n 's/^var BuildID = "\([^"]*\)"$/\1/p' cmd/myarch-buildkit/version.go)
bash scripts/validate-release.sh "${VERSION:-$source_version}"

revision=unknown
dirty=unknown
commit_epoch=0
if git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  revision=$(git rev-parse HEAD)
  commit_epoch=$(git show -s --format=%ct HEAD)
  dirty=false
  if [[ -n "$(git status --porcelain --untracked-files=normal)" ]]; then
    dirty=true
  fi
fi
build_version=$VERSION
if [[ -z "$build_version" ]]; then
  build_version="$source_version-dev.${revision:0:12}"
  if [[ "$dirty" == true ]]; then
    build_version+=.dirty
  fi
fi

if [[ "$mode" == build ]]; then
  mkdir -p bin
  "$GO" build -trimpath -buildvcs=false -ldflags "-X main.BuildID=$build_version" -o bin/myarch-buildkit ./cmd/myarch-buildkit
  exit
fi

# Release outputs have one supported target. Ignore user Go configuration that
# could silently change its architecture, linking, flags or active workspace.
export GOENV=off GOWORK=off GOFLAGS='' GOEXPERIMENT='' GOTOOLCHAIN=local
export CGO_ENABLED=0 GOOS=linux GOARCH=amd64 GOAMD64=v1
go_version=$("$GO" env GOVERSION)
pinned_go=$(cat .go-version)
if [[ -n "$VERSION" && "$go_version" != "go$pinned_go" ]]; then
  echo "Release packages require Go $pinned_go; selected compiler is $go_version (set GO=/path/to/go)." >&2
  exit 1
fi
epoch=${SOURCE_DATE_EPOCH:-$commit_epoch}
if [[ ! "$epoch" =~ ^(0|[1-9][0-9]{0,10})$ ]]; then
  echo 'SOURCE_DATE_EPOCH must be a nonnegative integer Unix timestamp (at most 11 digits).' >&2
  exit 1
fi
if ! tar --version | head -1 | grep -q 'GNU tar'; then
  echo 'Packaging requires GNU tar for deterministic archive metadata.' >&2
  exit 1
fi

mkdir -p dist
work=$(mktemp -d "$repo_root/dist/.package.XXXXXXXX")
trap 'rm -rf -- "$work"' EXIT
mkdir -p "$work/payload/docs"
"$GO" build -trimpath -buildvcs=false -ldflags "-X main.BuildID=$build_version" -o "$work/payload/myarch-buildkit" ./cmd/myarch-buildkit
cp LICENSE README.md CONTRIBUTING.md "$work/payload/"
cp docs/user-guide.md "$work/payload/docs/"
cat > "$work/payload/BUILD-INFO" <<EOF
version=$build_version
revision=$revision
dirty=$dirty
go_version=$go_version
goos=linux
goarch=amd64
goamd64=v1
cgo_enabled=0
source_date_epoch=$epoch
EOF
payload=(BUILD-INFO CONTRIBUTING.md LICENSE README.md docs/user-guide.md myarch-buildkit)
(
  cd "$work/payload"
  chmod 644 BUILD-INFO CONTRIBUTING.md LICENSE README.md docs/user-guide.md
  chmod 755 myarch-buildkit
  sha256sum "${payload[@]}" > SHA256SUMS
  chmod 644 SHA256SUMS
)
archive=myarch-buildkit-linux-x86_64.tar.gz
LC_ALL=C tar --format=gnu --sort=name --mtime="@$epoch" --owner=0 --group=0 --numeric-owner \
  --mode='u=rwX,go=rX' -cf - -C "$work/payload" "${payload[@]}" SHA256SUMS | gzip -n -9 > "$work/$archive"
(
  cd "$work"
  sha256sum "$archive" > SHA256SUMS
)
mv -f -- "$work/$archive" "dist/$archive"
mv -f -- "$work/SHA256SUMS" dist/SHA256SUMS
printf 'Packaged %s (%s, %s)\n' "dist/$archive" "$build_version" "$go_version"
