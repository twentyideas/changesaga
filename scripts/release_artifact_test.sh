#!/usr/bin/env bash
# Focused smoke tests for release-script validation, checksums, and metadata.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

failures=0

record() {
	if [ "$1" -eq 0 ]; then
		echo "ok - $2"
	else
		echo "not ok - $2" >&2
		failures=$((failures + 1))
	fi
}

expect_failure() {
	description="$1"
	expected="$2"
	shift 2
	if "$@" >"$work/output" 2>&1; then
		record 1 "$description"
	elif ! grep -F "$expected" "$work/output" >/dev/null; then
		echo "expected failure output to contain: $expected" >&2
		cat "$work/output" >&2
		record 1 "$description"
	else
		record 0 "$description"
	fi
}

echo "== validate rejected release inputs"
expect_failure "extra arguments are rejected" "usage:" \
	"$repo_root/scripts/build-release.sh" 1.2.3 linux amd64 "$work/dist" extra
expect_failure "path-like versions are rejected" "version must be" \
	"$repo_root/scripts/build-release.sh" ../../escape linux amd64 "$work/dist"
expect_failure "major versions with leading zeroes are rejected" "version must be" \
	"$repo_root/scripts/build-release.sh" 01.2.3 linux amd64 "$work/dist"
expect_failure "numeric prereleases with leading zeroes are rejected" "version must be" \
	"$repo_root/scripts/build-release.sh" 1.2.3-01 linux amd64 "$work/dist"
expect_failure "empty prerelease identifiers are rejected" "version must be" \
	"$repo_root/scripts/build-release.sh" 1.2.3-rc..1 linux amd64 "$work/dist"
expect_failure "unsupported operating systems are rejected" "unsupported GOOS" \
	"$repo_root/scripts/build-release.sh" 1.2.3 plan9 amd64 "$work/dist"
expect_failure "unsupported architectures are rejected" "unsupported GOARCH" \
	"$repo_root/scripts/build-release.sh" 1.2.3 linux 386 "$work/dist"
expect_failure "invalid commit overrides are rejected" "CHANGE_SAGA_COMMIT must be" \
	env CHANGE_SAGA_COMMIT='bad -X flag' \
	"$repo_root/scripts/build-release.sh" 1.2.3 linux amd64 "$work/dist"
expect_failure "invalid source epochs are rejected" "SOURCE_DATE_EPOCH must be" \
	env SOURCE_DATE_EPOCH=not-a-number \
	"$repo_root/scripts/build-release.sh" 1.2.3 linux amd64 "$work/dist"

echo "== checksum edge cases"
expect_failure "sha256 requires at least one file" "usage:" "$repo_root/scripts/sha256.sh"
expect_failure "sha256 rejects missing files" "not a regular file" \
	"$repo_root/scripts/sha256.sh" "$work/missing"
printf 'dash\n' >"$work/-payload"
printf 'space\n' >"$work/space payload"
newline_file="$work/line
break"
printf 'newline\n' >"$newline_file"
expect_failure "sha256 rejects newline-containing filenames" "must not contain newlines" \
	"$repo_root/scripts/sha256.sh" "$newline_file"
checksum_output="$("$repo_root/scripts/sha256.sh" "$work/-payload" "$work/space payload")"
if [ "$(printf '%s\n' "$checksum_output" | wc -l | tr -d ' ')" = 2 ] &&
	printf '%s\n' "$checksum_output" | grep -Eq '^[0-9a-fA-F]{64}  -payload$' &&
	printf '%s\n' "$checksum_output" | grep -Eq '^[0-9a-fA-F]{64}  space payload$'; then
	record 0 "sha256 handles option-like and spaced basenames"
else
	record 1 "sha256 handles option-like and spaced basenames"
fi

echo "== standalone wrapper input validation"
crafted="$work/change-saga_1.2.3'_darwin_arm64.tar.gz"
printf 'not an archive\n' >"$crafted"
expect_failure "standalone wrapper rejects shell-active archive names" "expected a darwin/arm64 release archive" \
	"$repo_root/scripts/build-macos-standalone-installer.sh" "$crafted" "$work/unsafe.command"
newline_archive="$work/change-saga_1.2.3
injected_darwin_arm64.tar.gz"
printf 'not an archive\n' >"$newline_archive"
expect_failure "standalone wrapper rejects newline-containing archive names" "expected a darwin/arm64 release archive" \
	"$repo_root/scripts/build-macos-standalone-installer.sh" "$newline_archive" "$work/unsafe-newline.command"
if [ ! -e "$work/unsafe.command" ] && [ ! -e "$work/unsafe-newline.command" ]; then
	record 0 "rejected wrapper input creates no output"
else
	record 1 "rejected wrapper input creates no output"
fi
same_path="$work/change-saga_1.2.3_darwin_arm64.tar.gz"
printf 'original archive bytes\n' >"$same_path"
expect_failure "standalone wrapper cannot overwrite its input" "must not overwrite" \
	"$repo_root/scripts/build-macos-standalone-installer.sh" "$same_path" "$same_path"
if [ "$(cat "$same_path")" = "original archive bytes" ]; then
	record 0 "same-path rejection preserves the input archive"
else
	record 1 "same-path rejection preserves the input archive"
fi
wrapper="$work/Change-Saga.command"
if "$repo_root/scripts/build-macos-standalone-installer.sh" "$same_path" "$wrapper" >/dev/null &&
	[ -x "$wrapper" ] &&
	grep -F "saga_archive_name='change-saga_1.2.3_darwin_arm64.tar.gz'" "$wrapper" >/dev/null; then
	record 0 "standalone wrapper still accepts a canonical archive name"
else
	record 1 "standalone wrapper still accepts a canonical archive name"
fi

echo "== build and inspect a host artifact"
host_os="$(go env GOOS)"
host_arch="$(go env GOARCH)"
case "$host_os/$host_arch" in
darwin/amd64 | darwin/arm64 | linux/amd64 | linux/arm64) ;;
*)
	echo "skip - unsupported smoke-test host $host_os/$host_arch"
	if [ "$failures" -ne 0 ]; then
		exit 1
	fi
	exit 0
	;;
esac

version=9.8.7-test.1
commit="$(git -C "$repo_root" rev-parse --verify 'HEAD^{commit}')"
epoch=946684800
dist="$work/dist"
env CHANGE_SAGA_COMMIT="$commit" GITHUB_SHA='wrong value that must be ignored' SOURCE_DATE_EPOCH="$epoch" \
	"$repo_root/scripts/build-release.sh" "$version" "$host_os" "$host_arch" "$dist" >/dev/null

archive="change-saga_${version}_${host_os}_${host_arch}.tar.gz"
extract="$work/extract"
mkdir "$extract"
tar -xzf "$dist/$archive" -C "$extract"

if [ "$("$extract/change-saga" version)" = \
	"$version (${commit:0:12}) built 2000-01-01T00:00:00Z" ]; then
	record 0 "artifact reports explicit version, checked commit, and reproducible date"
else
	record 1 "artifact reports explicit version, checked commit, and reproducible date"
fi

if "$extract/change-saga" help >/dev/null; then
	record 0 "artifact binary starts successfully"
else
	record 1 "artifact binary starts successfully"
fi

expected_checksum="$("$repo_root/scripts/sha256.sh" "$dist/$archive")"
if [ "$(cat "$dist/$archive.sha256")" = "$expected_checksum" ]; then
	record 0 "artifact checksum sidecar matches the archive"
else
	record 1 "artifact checksum sidecar matches the archive"
fi

if [ "$failures" -ne 0 ]; then
	echo "$failures release artifact test(s) failed" >&2
	exit 1
fi
echo "all release artifact tests passed"
