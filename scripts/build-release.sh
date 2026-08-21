#!/usr/bin/env bash
# Build one release artifact for a single GOOS/GOARCH target.
#
# Usage: scripts/build-release.sh <version> <goos> <goarch> [dist-dir]
#
# Produces <dist-dir>/change-saga_<version>_<goos>_<goarch>.{tar.gz,zip} containing the
# binary plus LICENSE and README.md, and writes a matching .sha256 sidecar.
# The build is static (CGO disabled) so a release artifact has no runtime
# dependency on the toolchain or libc of the machine that built it.
set -euo pipefail

if [ "$#" -lt 3 ]; then
	echo "usage: $0 <version> <goos> <goarch> [dist-dir]" >&2
	exit 2
fi

version="${1#v}"
goos="$2"
goarch="$3"
dist="${4:-dist}"

for value_name in version goos goarch; do
	value="${!value_name}"
	if [[ ! "$value" =~ ^[[:alnum:]][[:alnum:]._+-]*$ ]]; then
		echo "error: invalid $value_name: $value" >&2
		exit 2
	fi
done

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

commit_full="${GITHUB_SHA:-}"
if [ -z "$commit_full" ]; then
	commit_full="$(git rev-parse --verify 'HEAD^{commit}' 2>/dev/null)" || {
		echo "error: cannot determine release commit" >&2
		exit 1
	}
fi
if [[ ! "$commit_full" =~ ^[[:xdigit:]]{40}([[:xdigit:]]{24})?$ ]]; then
	echo "error: release commit must be a 40- or 64-character hexadecimal object ID" >&2
	exit 2
fi
checkout_commit="$(git rev-parse --verify 'HEAD^{commit}' 2>/dev/null || true)"
if [ -n "$checkout_commit" ]; then
	resolved_commit="$(git rev-parse --verify "${commit_full}^{commit}" 2>/dev/null)" || {
		echo "error: release commit does not identify a commit in this checkout" >&2
		exit 2
	}
	if [ "$resolved_commit" != "$checkout_commit" ]; then
		echo "error: release commit does not match the checked-out source" >&2
		exit 2
	fi
	if [ -n "$(git status --porcelain=v1 --untracked-files=normal)" ]; then
		echo "error: release builds require a clean source checkout" >&2
		exit 1
	fi
	commit_full="$resolved_commit"
fi
commit="${commit_full:0:12}"

# Use the commit timestamp by default so identical source inputs produce the
# same metadata. Callers may override it with the standard reproducible-build
# input when reconstructing an artifact outside the original checkout.
source_date_epoch="${SOURCE_DATE_EPOCH:-}"
if [ -z "$source_date_epoch" ]; then
	source_date_epoch="$(git show -s --format=%ct "$commit_full" 2>/dev/null)" || {
		echo "error: cannot determine commit timestamp; set SOURCE_DATE_EPOCH" >&2
		exit 1
	}
fi
if [[ ! "$source_date_epoch" =~ ^[0-9]+$ ]]; then
	echo "error: SOURCE_DATE_EPOCH must be a non-negative integer" >&2
	exit 2
fi
build_date="$(date -u -r "$source_date_epoch" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d "@$source_date_epoch" +%Y-%m-%dT%H:%M:%SZ)" || {
	echo "error: SOURCE_DATE_EPOCH is outside the supported date range" >&2
	exit 2
}

name="change-saga_${version}_${goos}_${goarch}"
stage="$(mktemp -d)"
trap 'rm -rf "$stage"' EXIT

binary="change-saga"
if [ "$goos" = "windows" ]; then
	binary="change-saga.exe"
fi

ldflags="-s -w"
ldflags="$ldflags -X github.com/change-saga/change-saga/internal/cli.Version=${version}"
ldflags="$ldflags -X github.com/change-saga/change-saga/internal/cli.Commit=${commit}"
ldflags="$ldflags -X github.com/change-saga/change-saga/internal/cli.BuildDate=${build_date}"

echo "building ${name}"
CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
	go build -mod=readonly -trimpath -buildvcs=false -ldflags "$ldflags" -o "$stage/$binary" ./cmd/change-saga

# Signing runs against the staged binary before it is archived. CHANGE_SAGA_SIGN_HOOK
# is set by the trusted macOS release job; it is empty everywhere else.
if [ -n "${CHANGE_SAGA_SIGN_HOOK:-}" ]; then
	echo "running sign hook for ${name}"
	"$CHANGE_SAGA_SIGN_HOOK" "$stage/$binary"
fi

cp LICENSE README.md "$stage/"

mkdir -p "$dist"
dist_abs="$(cd "$dist" && pwd)"
rm -f "$dist_abs/$name.tar.gz" "$dist_abs/$name.zip"

if [ "$goos" = "windows" ]; then
	archive="$name.zip"
else
	archive="$name.tar.gz"
fi
go run -mod=readonly ./internal/cmd/releasearchive \
	"$dist_abs/$archive" "$source_date_epoch" "$stage" "$binary"

"$repo_root/scripts/sha256.sh" "$dist_abs/$archive" > "$dist_abs/$archive.sha256"
echo "wrote $dist_abs/$archive"
cat "$dist_abs/$archive.sha256"
