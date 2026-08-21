#!/usr/bin/env bash
# End-to-end checks for release provenance and byte-for-byte reproducibility.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

work="$(mktemp -d)"
dirty_probe="$repo_root/.release-repro-dirty-probe"
trap 'rm -rf "$work"; rm -f "$dirty_probe"' EXIT

version="9.9.9-repro-test"
goos="$(go env GOOS)"
goarch="$(go env GOARCH)"
commit="$(git rev-parse --verify 'HEAD^{commit}')"
epoch="$(git show -s --format=%ct "$commit")"
name="change-saga_${version}_${goos}_${goarch}"
if [ "$goos" = "windows" ]; then
	archive="$name.zip"
	binary="change-saga.exe"
else
	archive="$name.tar.gz"
	binary="change-saga"
fi

GITHUB_SHA=wrong-ambient-value CHANGE_SAGA_COMMIT="$commit" \
	./scripts/build-release.sh "$version" "$goos" "$goarch" "$work/first" >/dev/null
sleep 1
GITHUB_SHA=another-wrong-value CHANGE_SAGA_COMMIT="$commit" \
	./scripts/build-release.sh "$version" "$goos" "$goarch" "$work/second" >/dev/null

cmp "$work/first/$archive" "$work/second/$archive"
cmp "$work/first/$archive.sha256" "$work/second/$archive.sha256"

mkdir "$work/extracted"
if [ "$goos" = "windows" ]; then
	unzip -q "$work/first/$archive" -d "$work/extracted"
else
	tar -xzf "$work/first/$archive" -C "$work/extracted"
fi

build_date="$(date -u -r "$epoch" +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d "@$epoch" +%Y-%m-%dT%H:%M:%SZ)"
expected="$version (${commit:0:12}) built $build_date"
actual="$("$work/extracted/$binary" version)"
if [ "$actual" != "$expected" ]; then
	echo "unexpected version metadata: $actual" >&2
	exit 1
fi
if go version -m "$work/extracted/$binary" | grep -q $'\tbuild\tvcs\.'; then
	echo "automatic Go VCS metadata leaked into release binary" >&2
	exit 1
fi

if SOURCE_DATE_EPOCH=invalid CHANGE_SAGA_COMMIT="$commit" \
	./scripts/build-release.sh "$version" "$goos" "$goarch" "$work/invalid-epoch" >/dev/null 2>&1; then
	echo "invalid SOURCE_DATE_EPOCH was accepted" >&2
	exit 1
fi
if SOURCE_DATE_EPOCH="$epoch" CHANGE_SAGA_COMMIT=not-a-commit \
	./scripts/build-release.sh "$version" "$goos" "$goarch" "$work/invalid-commit" >/dev/null 2>&1; then
	echo "invalid CHANGE_SAGA_COMMIT was accepted" >&2
	exit 1
fi
other_commit="$(git rev-parse --verify 'HEAD^{commit}^')"
if SOURCE_DATE_EPOCH="$epoch" CHANGE_SAGA_COMMIT="$other_commit" \
	./scripts/build-release.sh "$version" "$goos" "$goarch" "$work/wrong-commit" >/dev/null 2>&1; then
	echo "commit that does not match the checkout was accepted" >&2
	exit 1
fi
touch "$dirty_probe"
if SOURCE_DATE_EPOCH="$epoch" CHANGE_SAGA_COMMIT="$commit" \
	./scripts/build-release.sh "$version" "$goos" "$goarch" "$work/dirty" >/dev/null 2>&1; then
	echo "dirty source checkout was accepted for explicit release provenance" >&2
	exit 1
fi
rm -f "$dirty_probe"

GOFLAGS='-tags=definitely_not_a_release_tag' GOEXPERIMENT=fieldtrack \
	CHANGE_SAGA_COMMIT="$commit" ./scripts/build-release.sh \
	"$version" "$goos" "$goarch" "$work/hostile-env" >/dev/null
cmp "$work/first/$archive" "$work/hostile-env/$archive"
cmp "$work/first/$archive.sha256" "$work/hostile-env/$archive.sha256"

echo "release reproducibility tests passed"
