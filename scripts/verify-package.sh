#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
dist_dir=${1:-$repo_root/dist}
dist_dir=$(cd -- "$dist_dir" && pwd)
archive=myarch-buildkit-linux-x86_64.tar.gz
fail() { echo "Package verification failed: $*" >&2; exit 1; }

# Validate the manifest before sha256sum can read any paths named by it.
[[ $(wc -l < "$dist_dir/SHA256SUMS") -eq 1 ]] || fail 'expected one archive checksum'
grep -Eq '^[0-9a-f]{64}  myarch-buildkit-linux-x86_64\.tar\.gz$' "$dist_dir/SHA256SUMS" || fail 'invalid archive checksum manifest'
(cd "$dist_dir" && sha256sum --strict --check SHA256SUMS)

work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
payload=(BUILD-INFO CONTRIBUTING.md LICENSE README.md docs/user-guide.md myarch-buildkit)
printf '%s\n' "${payload[@]}" SHA256SUMS | LC_ALL=C sort > "$work/expected"
tar -tzf "$dist_dir/$archive" | LC_ALL=C sort > "$work/actual"
cmp -s "$work/expected" "$work/actual" || fail 'unexpected, missing or duplicate archive paths'
tar -tvzf "$dist_dir/$archive" > "$work/entries"
if grep -qv '^-' "$work/entries"; then
  fail 'archive must contain regular files only'
fi
mkdir "$work/unpacked"
tar --no-same-owner --no-same-permissions -xzf "$dist_dir/$archive" -C "$work/unpacked"
cd "$work/unpacked"
[[ $(wc -l < SHA256SUMS) -eq ${#payload[@]} ]] || fail 'incorrect payload checksum count'
for path in "${payload[@]}"; do
  # All payload names are fixed, contain no whitespace and need only dot escaping.
  grep -Eq "^[0-9a-f]{64}  ${path//./\\.}$" SHA256SUMS || fail "missing or invalid checksum for $path"
done
sha256sum --strict --check SHA256SUMS
[[ -x myarch-buildkit ]] || fail 'binary is not executable'
elf_header=$(od -An -tx1 -N20 myarch-buildkit | tr -d ' \n')
[[ "$elf_header" == 7f454c460201* && "${elf_header:36:4}" == 3e00 ]] || fail 'binary must be Linux x86-64 ELF'
for expected in goos=linux goarch=amd64 goamd64=v1 cgo_enabled=0; do
  grep -Fxq "$expected" BUILD-INFO || fail "incorrect build metadata: $expected"
done

if [[ $(uname -s) != Linux || $(uname -m) != x86_64 ]]; then
  echo 'Checksums and archive structure verified; CLI smoke test requires Linux x86-64.'
  exit
fi
mkdir "$work/home"
smoke=(timeout 30s env HOME="$work/home" XDG_CONFIG_HOME="$work/home/.config" XDG_STATE_HOME="$work/home/.local/state")
version=$(sed -n 's/^version=//p' BUILD-INFO)
[[ -n "$version" && "$version" != *$'\n'* ]] || fail 'missing or ambiguous version metadata'
"${smoke[@]}" ./myarch-buildkit --version > "$work/version"
grep -Fxq "myarch-buildkit $version" "$work/version" || fail 'CLI version differs from build metadata'
binary_sha=$(sha256sum myarch-buildkit)
binary_sha=${binary_sha%% *}
grep -Fxq "Executable SHA256: $binary_sha" "$work/version" || fail 'CLI executable hash differs from packaged binary'
"${smoke[@]}" ./myarch-buildkit --help > "$work/help"
grep -Fq 'Usage:' "$work/help" || fail 'CLI help is missing'
"${smoke[@]}" ./myarch-buildkit plan > "$work/plan"
[[ -s "$work/plan" ]] || fail 'CLI plan is empty'
"${smoke[@]}" ./myarch-buildkit --settings-template > "$work/settings"
[[ -s "$work/settings" ]] || fail 'settings template is empty'
if "${smoke[@]}" ./myarch-buildkit check --json --settings "$work/missing.json" > "$work/check" 2> "$work/check-error"; then
  fail 'check accepted a missing settings file'
else
  status=$?
  [[ $status -eq 2 ]] || fail "check returned $status instead of 2"
fi
python3 - "$work/settings" "$work/check" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as stream:
    settings = json.load(stream)
with open(sys.argv[2], encoding="utf-8") as stream:
    report = json.load(stream)
if not isinstance(settings, dict) or settings.get("schema_version") != 2:
    sys.exit("Package verification failed: settings template has an unsupported schema")
if not isinstance(report, dict) or report.get("status") != "incomplete" or not report.get("check_error"):
    sys.exit("Package verification failed: check did not report its initialization failure")
PY
[[ ! -e "$work/home/.local/state" ]] || fail 'read-only commands created installer state'
echo 'Package checksums, structure, target and CLI smoke tests passed.'
