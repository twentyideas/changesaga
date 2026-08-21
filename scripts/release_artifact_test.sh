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
leading_zero="$work/change-saga_01.2.3_darwin_arm64.tar.gz"
printf 'not an archive\n' >"$leading_zero"
expect_failure "standalone wrapper enforces strict SemVer" "expected a darwin/arm64 release archive" \
	"$repo_root/scripts/build-macos-standalone-installer.sh" "$leading_zero" "$work/leading-zero.command"

wrapper_stage="$work/wrapper-stage"
mkdir "$wrapper_stage"
printf '#!/bin/sh\necho "1.2.3 (test) built test"\n' >"$wrapper_stage/change-saga"
printf 'license\n' >"$wrapper_stage/LICENSE"
printf 'readme\n' >"$wrapper_stage/README.md"
chmod 0755 "$wrapper_stage/change-saga"
valid_archive="$work/change-saga_1.2.3_darwin_arm64.tar.gz"
tar -czf "$valid_archive" -C "$wrapper_stage" change-saga LICENSE README.md
wrapper="$work/Change-Saga.command"
if "$repo_root/scripts/build-macos-standalone-installer.sh" "$valid_archive" "$wrapper" >/dev/null &&
	[ -x "$wrapper" ] &&
	grep -F "saga_expected_version='1.2.3'" "$wrapper" >/dev/null; then
	record 0 "standalone wrapper accepts a canonical, exact-layout archive"
else
	record 1 "standalone wrapper accepts a canonical, exact-layout archive"
fi

link_stage="$work/link-stage"
mkdir "$link_stage"
ln -s /tmp/not-the-release "$link_stage/change-saga"
cp "$wrapper_stage/LICENSE" "$wrapper_stage/README.md" "$link_stage/"
link_archive="$work/change-saga_1.2.4_darwin_arm64.tar.gz"
tar -czf "$link_archive" -C "$link_stage" change-saga LICENSE README.md
printf 'preserve this output\n' >"$work/preserved.command"
expect_failure "standalone wrapper rejects link members" "must all be regular files" \
	"$repo_root/scripts/build-macos-standalone-installer.sh" "$link_archive" "$work/preserved.command"
if [ "$(cat "$work/preserved.command")" = "preserve this output" ]; then
	record 0 "failed wrapper build preserves the prior output"
else
	record 1 "failed wrapper build preserves the prior output"
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
dist="$work/caller/relative-dist"
mkdir -p "$work/caller"
(cd "$work/caller" && \
	env CHANGE_SAGA_COMMIT="$commit" GITHUB_SHA='wrong value that must be ignored' SOURCE_DATE_EPOCH="$epoch" \
	"$repo_root/scripts/build-release.sh" "$version" "$host_os" "$host_arch" relative-dist >/dev/null)
if [ -f "$dist/change-saga_${version}_${host_os}_${host_arch}.tar.gz" ]; then
	record 0 "relative output directories resolve from the caller's working directory"
else
	record 1 "relative output directories resolve from the caller's working directory"
fi

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

if [ "$host_os/$host_arch" = darwin/arm64 ]; then
	echo "== run the standalone macOS installer end to end"
	standalone="$work/real.command"
	standalone_install="$work/standalone-bin"
	"$repo_root/scripts/build-macos-standalone-installer.sh" "$dist/$archive" "$standalone" >/dev/null
	if CHANGE_SAGA_INSTALL_DIR="$standalone_install" "$standalone" >"$work/standalone-output" 2>&1 &&
		[ "$("$standalone_install/change-saga" version)" = "$version (${commit:0:12}) built 2000-01-01T00:00:00Z" ]; then
		record 0 "standalone installer verifies and atomically installs a real artifact"
	else
		cat "$work/standalone-output" >&2
		record 1 "standalone installer verifies and atomically installs a real artifact"
	fi

	wrong_stage="$work/wrong-version-stage"
	mkdir "$wrong_stage"
	printf '#!/bin/sh\necho "0.0.0-wrong (test) built test"\n' >"$wrong_stage/change-saga"
	printf 'license\n' >"$wrong_stage/LICENSE"
	printf 'readme\n' >"$wrong_stage/README.md"
	chmod 0755 "$wrong_stage/change-saga"
	wrong_archive="$work/change-saga_1.2.5_darwin_arm64.tar.gz"
	tar -czf "$wrong_archive" -C "$wrong_stage" change-saga LICENSE README.md
	wrong_wrapper="$work/wrong-version.command"
	"$repo_root/scripts/build-macos-standalone-installer.sh" "$wrong_archive" "$wrong_wrapper" >/dev/null
	printf 'existing installation\n' >"$standalone_install/change-saga"
	chmod 0755 "$standalone_install/change-saga"
	if CHANGE_SAGA_INSTALL_DIR="$standalone_install" "$wrong_wrapper" >"$work/wrong-output" 2>&1; then
		record 1 "standalone installer rejects a mismatched embedded version"
	elif [ "$(cat "$standalone_install/change-saga")" = "existing installation" ]; then
		record 0 "version rejection preserves the existing installation"
	else
		record 1 "version rejection preserves the existing installation"
	fi
fi

if [ "$failures" -ne 0 ]; then
	echo "$failures release artifact test(s) failed" >&2
	exit 1
fi
echo "all release artifact tests passed"
