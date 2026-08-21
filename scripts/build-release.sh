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

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

commit="${GITHUB_SHA:-$(git rev-parse --short=12 HEAD 2>/dev/null || echo unknown)}"
commit="${commit:0:12}"
# SOURCE_DATE_EPOCH keeps the stamp reproducible when the caller pins it.
if [ -n "${SOURCE_DATE_EPOCH:-}" ]; then
	build_date="$(date -u -r "$SOURCE_DATE_EPOCH" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d "@$SOURCE_DATE_EPOCH" +%Y-%m-%dT%H:%M:%SZ)"
else
	build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
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
CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
	go build -trimpath -ldflags "$ldflags" -o "$stage/$binary" ./cmd/change-saga

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
	(cd "$stage" && zip -q -X "$dist_abs/$archive" "$binary" LICENSE README.md)
else
	archive="$name.tar.gz"
	tar -czf "$dist_abs/$archive" -C "$stage" "$binary" LICENSE README.md
fi

"$repo_root/scripts/sha256.sh" "$dist_abs/$archive" > "$dist_abs/$archive.sha256"
echo "wrote $dist_abs/$archive"
cat "$dist_abs/$archive.sha256"
