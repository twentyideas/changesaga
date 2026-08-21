#!/usr/bin/env bash
# Contents and permissions test for scripts/build-release.sh.
#
# Builds real archives for the host platform and for windows/amd64, then
# asserts the shape a release consumer depends on: exactly three flat entries,
# every one a regular file, an executable binary alongside world-readable docs,
# a checksum sidecar `sha256sum -c` can consume, and a version stamp that
# honours SOURCE_DATE_EPOCH.
#
# Modes are read from the archive listing rather than from an extraction.
# Extraction applies the extracting user's umask, so a wrong mode inside the
# archive would not survive the round trip to be observed.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

version="9.9.9"
# Pinned so the injected build date is predictable: 2023-11-14T22:13:20Z.
export SOURCE_DATE_EPOCH=1700000000
stamp_date="2023-11-14T22:13:20Z"

case "$(uname -s)" in
Darwin) host_os=darwin ;;
Linux) host_os=linux ;;
*) echo "unsupported test host" >&2; exit 1 ;;
esac
case "$(uname -m)" in
x86_64 | amd64) host_arch=amd64 ;;
arm64 | aarch64) host_arch=arm64 ;;
*) echo "unsupported test host arch" >&2; exit 1 ;;
esac

pass=0
fail=0

record() { # record <name> <ok?0:1>
	if [ "$2" -eq 0 ]; then
		printf 'ok   %s\n' "$1"
		pass=$((pass + 1))
	else
		printf 'FAIL %s\n' "$1"
		fail=$((fail + 1))
	fi
}

assert_eq() { # assert_eq <name> <want> <got>
	if [ "$2" = "$3" ]; then
		record "$1" 0
	else
		printf 'FAIL %s\n       want: %s\n       got:  %s\n' "$1" "$2" "$3"
		fail=$((fail + 1))
	fi
}

# octal_mode <ls-style mode string> -> three octal digits, e.g. -rwxr-xr-x -> 755
octal_mode() {
	local m="${1: -9}" i digit out=""
	for i in 0 3 6; do
		digit=0
		if [ "${m:i:1}" = r ]; then digit=$((digit + 4)); fi
		if [ "${m:i+1:1}" = w ]; then digit=$((digit + 2)); fi
		case "${m:i+2:1}" in
		x | s | t) digit=$((digit + 1)) ;;
		esac
		out="$out$digit"
	done
	printf '%s\n' "$out"
}

# mode_for <listing> <entry name> -> the ls-style mode string, empty if absent.
mode_for() {
	awk -v want="$2" '$NF == want { print $1; exit }' "$1"
}

# assert_entry <label> <listing> <name> <want-octal>
assert_entry() {
	local mode
	mode="$(mode_for "$2" "$3")"
	if [ -z "$mode" ]; then
		record "$1 $3 is present" 1
		return
	fi
	record "$1 $3 is present" 0
	assert_eq "$1 $3 is a regular file" "-" "${mode:0:1}"
	assert_eq "$1 $3 is mode 0$4" "$4" "$(octal_mode "$mode")"
}

# assert_no_special_bits <label> <listing>
assert_no_special_bits() {
	local offenders
	offenders="$(awk '$1 ~ /[sStT]/ { print $NF }' "$2" | tr '\n' ' ')"
	if [ -n "${offenders// /}" ]; then
		printf 'FAIL %s carries no setuid/setgid/sticky bit\n       offenders: %s\n' "$1" "$offenders"
		fail=$((fail + 1))
	else
		record "$1 carries no setuid/setgid/sticky bit" 0
	fi
}

# assert_flat <label> <names file>
# Every member must be a bare name in the archive root: no directory entries,
# no absolute paths, and nothing that unpacks outside the extraction directory.
assert_flat() {
	local offenders
	offenders="$(grep -v '^[A-Za-z0-9][A-Za-z0-9._-]*$' "$2" | tr '\n' ' ' || true)"
	if [ -n "${offenders// /}" ]; then
		printf 'FAIL %s unpacks flat into the extraction directory\n       offenders: %s\n' "$1" "$offenders"
		fail=$((fail + 1))
	else
		record "$1 unpacks flat into the extraction directory" 0
	fi
}

# assert_sidecar <label> <dist dir> <archive name>
assert_sidecar() {
	local dir="$2" archive="$3" sidecar want got
	sidecar="$dir/$archive.sha256"
	if [ ! -f "$sidecar" ]; then
		record "$1 wrote a checksum sidecar" 1
		return
	fi
	record "$1 wrote a checksum sidecar" 0
	assert_eq "$1 sidecar holds one entry" "1" "$(grep -c . "$sidecar")"
	# sha256sum -c resolves the recorded name relative to its own directory, so
	# a path here would make the published checksum unverifiable.
	assert_eq "$1 sidecar names the archive by basename" "$archive" "$(awk '{print $2}' "$sidecar")"
	want="$(awk '{print $1}' "$sidecar")"
	got="$("$repo_root/scripts/sha256.sh" "$dir/$archive" | awk '{print $1}')"
	assert_eq "$1 sidecar checksum matches the archive" "$want" "$got"
}

echo "== host archive ($host_os/$host_arch)"
host_dist="$work/dist-host"
host_archive="change-saga_${version}_${host_os}_${host_arch}.tar.gz"
"$repo_root/scripts/build-release.sh" "$version" "$host_os" "$host_arch" "$host_dist" >/dev/null

tar -tzf "$host_dist/$host_archive" > "$work/tar-names"
tar -tvzf "$host_dist/$host_archive" > "$work/tar-listing"

assert_eq "host archive holds exactly the documented members" \
	"LICENSE README.md change-saga" \
	"$(LC_ALL=C sort "$work/tar-names" | tr '\n' ' ' | sed 's/ $//')"
assert_flat "host archive" "$work/tar-names"
assert_entry "host archive" "$work/tar-listing" change-saga 755
assert_entry "host archive" "$work/tar-listing" LICENSE 644
assert_entry "host archive" "$work/tar-listing" README.md 644
assert_no_special_bits "host archive" "$work/tar-listing"

assert_eq "a single-target build leaves only the archive and its sidecar" \
	"$host_archive $host_archive.sha256" \
	"$(find "$host_dist" -type f -exec basename {} \; | sort | tr '\n' ' ' | sed 's/ $//')"
assert_sidecar "host archive" "$host_dist" "$host_archive"

unpack="$work/unpack"
mkdir -p "$unpack"
tar -xzf "$host_dist/$host_archive" -C "$unpack"
if [ -x "$unpack/change-saga" ]; then record "unpacked binary is executable" 0; else record "unpacked binary is executable" 1; fi
if [ "$host_os" = linux ]; then
	if ldd "$unpack/change-saga" >"$work/ldd-output" 2>&1; then
		record "Linux binary has no dynamic runtime dependencies" 1
	elif grep -Eq 'not a dynamic executable|statically linked' "$work/ldd-output"; then
		record "Linux binary has no dynamic runtime dependencies" 0
	else
		cat "$work/ldd-output" >&2
		record "Linux binary has no dynamic runtime dependencies" 1
	fi
fi
version_line="$("$unpack/change-saga" version)"
assert_eq "unpacked binary reports the injected version" \
	"$version" "$(printf '%s' "$version_line" | awk '{print $1}')"
if printf '%s' "$version_line" | grep -q "built $stamp_date"; then
	record "build date honours SOURCE_DATE_EPOCH" 0
else
	printf 'FAIL build date honours SOURCE_DATE_EPOCH\n       got: %s\n' "$version_line"
	fail=$((fail + 1))
fi

echo
echo "== windows archive (windows/amd64)"
win_dist="$work/dist-windows"
win_archive="change-saga_${version}_windows_amd64.zip"
"$repo_root/scripts/build-release.sh" "$version" windows amd64 "$win_dist" >/dev/null

unzip -Z1 "$win_dist/$win_archive" > "$work/zip-names"
unzip -Z "$win_dist/$win_archive" > "$work/zip-listing"

assert_eq "windows archive holds exactly the documented members" \
	"LICENSE README.md change-saga.exe" \
	"$(LC_ALL=C sort "$work/zip-names" | tr '\n' ' ' | sed 's/ $//')"
assert_flat "windows archive" "$work/zip-names"
# The zip records unix modes, so an archive unpacked on WSL or macOS still
# yields a runnable binary rather than one the user has to chmod.
assert_entry "windows archive" "$work/zip-listing" change-saga.exe 755
assert_entry "windows archive" "$work/zip-listing" LICENSE 644
assert_entry "windows archive" "$work/zip-listing" README.md 644
assert_no_special_bits "windows archive" "$work/zip-listing"
assert_sidecar "windows archive" "$win_dist" "$win_archive"

win_unpack="$work/unpack-windows"
mkdir -p "$win_unpack"
unzip -q "$win_dist/$win_archive" -d "$win_unpack"
assert_eq "windows payload is a PE image" "MZ" "$(head -c 2 "$win_unpack/change-saga.exe")"

echo
echo "== permissions do not depend on the builder's umask"
# A maintainer building a handoff artifact under a restrictive umask must not
# ship a binary only they can execute.
umask_dist="$work/dist-umask"
(umask 077 && "$repo_root/scripts/build-release.sh" "$version" "$host_os" "$host_arch" "$umask_dist" >/dev/null)
tar -tvzf "$umask_dist/$host_archive" > "$work/umask-listing"
assert_entry "umask 077 archive" "$work/umask-listing" change-saga 755
assert_entry "umask 077 archive" "$work/umask-listing" LICENSE 644
assert_entry "umask 077 archive" "$work/umask-listing" README.md 644

echo
printf '%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
