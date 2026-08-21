#!/usr/bin/env bash
# Build one release artifact for a single GOOS/GOARCH target.
#
# Usage: scripts/build-release.sh <version> <goos> <goarch> [dist-dir]
#
# Produces <dist-dir>/change-saga_<version>_<goos>_<goarch>.{tar.gz,zip} containing the
# binary plus LICENSE and README.md, and writes a matching .sha256 sidecar.
# CGO is disabled so artifacts avoid external/non-system runtime dependencies;
# the Linux targets are fully static where the release tests verify that claim.
set -euo pipefail

if [ "$#" -lt 3 ] || [ "$#" -gt 4 ]; then
	echo "usage: $0 <version> <goos> <goarch> [dist-dir]" >&2
	exit 2
fi

version="${1#v}"
goos="$2"
goarch="$3"
dist="${4:-dist}"

# Keep values used in archive paths and -ldflags deliberately narrow. Besides
# producing unusable releases, whitespace or path separators here could turn a
# caller mistake into an overwrite outside dist or another linker argument.
numeric='(0|[1-9][0-9]*)'
prerelease_id="($numeric|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)"
semver_re="^$numeric\\.$numeric\\.$numeric(-$prerelease_id(\\.$prerelease_id)*)?(\\+[0-9A-Za-z-]+(\\.[0-9A-Za-z-]+)*)?$"
if [[ ! "$version" =~ $semver_re ]]; then
	echo "error: version must be valid semantic version text (for example 1.2.3 or 1.2.3-rc.1)" >&2
	exit 2
fi
case "$goos" in
darwin | linux | windows) ;;
*)
	echo "error: unsupported GOOS $goos (expected darwin, linux, or windows)" >&2
	exit 2
	;;
esac
case "$goarch" in
amd64 | arm64) ;;
*)
	echo "error: unsupported GOARCH $goarch (expected amd64 or arm64)" >&2
	exit 2
	;;
esac

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

# Do not use GITHUB_SHA implicitly: on a manually dispatched release it can
# describe the workflow ref rather than the tag checked out above. Callers that
# need an override must opt in explicitly; otherwise checked-out HEAD wins.
checkout_commit="$(git rev-parse --verify 'HEAD^{commit}' 2>/dev/null)" || {
	echo "error: cannot determine release commit" >&2
	exit 1
}
commit_full="${CHANGE_SAGA_COMMIT:-$checkout_commit}"
if [[ ! "$commit_full" =~ ^[[:xdigit:]]{40}([[:xdigit:]]{24})?$ ]]; then
	echo "error: CHANGE_SAGA_COMMIT must be a full 40- or 64-character hexadecimal object ID" >&2
	exit 2
fi
resolved_commit="$(git rev-parse --verify "${commit_full}^{commit}" 2>/dev/null)" || {
	echo "error: release commit does not identify a commit in this checkout" >&2
	exit 2
}
if [ "$resolved_commit" != "$checkout_commit" ]; then
	echo "error: release commit does not match the checked-out source" >&2
	exit 2
fi
commit_full="$resolved_commit"
commit="${commit_full:0:12}"
dirty_status="$(git status --porcelain 2>/dev/null)"
if [ -n "${CHANGE_SAGA_COMMIT:-}" ] && [ -n "$dirty_status" ]; then
	echo "error: release builds with CHANGE_SAGA_COMMIT require a clean source checkout" >&2
	exit 1
elif [ -n "$dirty_status" ]; then
	commit="$commit-dirty"
fi

required_go="$(tr -d '\r\n' < .go-version)"
case "$required_go" in
go*) ;;
*) required_go="go$required_go" ;;
esac
go_env=(env GOENV=off GOFLAGS= GOTOOLCHAIN=local GOEXPERIMENT= GOAMD64=v1 GOARM64=v8.0)
actual_go="$("${go_env[@]}" go env GOVERSION)"
if [ "$actual_go" != "$required_go" ]; then
	echo "error: release build requires $required_go, found $actual_go" >&2
	exit 1
fi

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
# Try GNU date first. GNU -r names a reference file, while BSD -r accepts an
# epoch; the opposite order could accidentally use a numeric filename's mtime.
if ! build_date="$(date -u -d "@$source_date_epoch" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null)"; then
	if ! build_date="$(date -u -r "$source_date_epoch" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null)"; then
		echo "error: SOURCE_DATE_EPOCH is outside the supported date range" >&2
		exit 2
	fi
fi

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
"${go_env[@]}" CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
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
"${go_env[@]}" go run -mod=readonly ./internal/cmd/releasearchive \
	"$dist_abs/$archive" "$source_date_epoch" "$stage" "$binary"

"$repo_root/scripts/sha256.sh" "$dist_abs/$archive" > "$dist_abs/$archive.sha256"
echo "wrote $dist_abs/$archive"
cat "$dist_abs/$archive.sha256"
