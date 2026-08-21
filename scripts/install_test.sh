#!/usr/bin/env bash
# End-to-end test for scripts/install.sh.
#
# Builds a real archive for the host platform, publishes it into a local
# directory that stands in for the GitHub release, and puts a stub `curl` on
# PATH that serves from that directory. The installer itself is unmodified, so
# this exercises the real download, checksum, unpack, and install path.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

release="$work/release"
stub="$work/stub"
mkdir -p "$release" "$stub"

version="9.9.9"
tag="v$version"
case "$(uname -s)" in
Darwin) os=darwin ;;
Linux) os=linux ;;
*) echo "unsupported test host" >&2; exit 1 ;;
esac
case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
arm64 | aarch64) arch=arm64 ;;
*) echo "unsupported test host arch" >&2; exit 1 ;;
esac
archive="change-saga_${version}_${os}_${arch}.tar.gz"

echo "== building test release artifact"
"$repo_root/scripts/build-release.sh" "$version" "$os" "$arch" "$release" >/dev/null
(cd "$release" && "$repo_root/scripts/sha256.sh" "$archive" > SHA256SUMS)

# Stub curl: maps https://github.com/<repo>/releases/download/<tag>/<file> onto
# $CHANGE_SAGA_TEST_RELEASE/<file>, and resolves /releases/latest to the test tag.
cat > "$stub/curl" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
out=""
url=""
write_out=""
while [ "$#" -gt 0 ]; do
	case "$1" in
	--output) out="$2"; shift 2 ;;
	--write-out) write_out="$2"; shift 2 ;;
	https://*) url="$1"; shift ;;
	*) shift ;;
	esac
done
case "$url" in
*/releases/latest)
	[ -n "$write_out" ] && printf 'https://github.com/x/y/releases/tag/%s\n' "$CHANGE_SAGA_TEST_TAG"
	exit 0
	;;
esac
file="${url##*/}"
src="$CHANGE_SAGA_TEST_RELEASE/$file"
[ -f "$src" ] || exit 22   # curl's exit code for HTTP 404 under --fail
[ -n "$out" ] && cp "$src" "$out"
exit 0
STUB
chmod +x "$stub/curl"

# Stub gh while preserving the exact attestation policy arguments for checks.
cat > "$stub/gh" <<'STUB'
#!/usr/bin/env sh
set -eu
printf '%s\n' "$@" > "$CHANGE_SAGA_TEST_GH_LOG"
STUB
chmod +x "$stub/gh"

export CHANGE_SAGA_TEST_RELEASE="$release" CHANGE_SAGA_TEST_TAG="$tag"
export CHANGE_SAGA_TEST_GH_LOG="$work/gh-arguments"
export PATH="$stub:$PATH"
install_sh="$repo_root/scripts/install.sh"
pass=0
fail=0

check() { # check <name> <expected-status> <command...>
	local name="$1" want="$2"
	shift 2
	local log status
	log="$work/log"
	set +e
	"$@" >"$log" 2>&1
	status=$?
	set -e
	if [ "$status" -eq "$want" ]; then
		printf 'ok   %s\n' "$name"
		pass=$((pass + 1))
	else
		printf 'FAIL %s (exit %d, want %d)\n' "$name" "$status" "$want"
		sed 's/^/       /' "$log"
		fail=$((fail + 1))
	fi
}

expect_log() { # expect_log <name> <pattern>
	if grep -q "$2" "$work/log"; then
		printf 'ok   %s\n' "$1"
		pass=$((pass + 1))
	else
		printf 'FAIL %s: no match for %s\n' "$1" "$2"
		sed 's/^/       /' "$work/log"
		fail=$((fail + 1))
	fi
}

record() { # record <name> <ok?0:1>
	if [ "$2" -eq 0 ]; then
		printf 'ok   %s\n' "$1"
		pass=$((pass + 1))
	else
		printf 'FAIL %s\n' "$1"
		fail=$((fail + 1))
	fi
}

assert_exec() { # assert_exec <name> <path>
	if [ -x "$2" ]; then record "$1" 0; else record "$1" 1; fi
}

assert_absent() { # assert_absent <name> <path>
	if [ -e "$2" ]; then record "$1" 1; else record "$1" 0; fi
}

assert_contains() { # assert_contains <name> <path> <pattern>
	if grep -q "$3" "$2"; then record "$1" 0; else record "$1" 1; fi
}

refresh_checksum() { # refresh_checksum <release-dir> <archive>
	(cd "$1" && "$repo_root/scripts/sha256.sh" "$2" >SHA256SUMS)
}

installed_mode() { # installed_mode <path>
	stat -f '%Lp' "$1" 2>/dev/null || stat -c '%a' "$1"
}

echo "== happy path"
bindir="$work/bin"
check "install succeeds" 0 sh "$install_sh" --version "$tag" --dir "$bindir"
expect_log "reports checksum" "checksum ok"
if [ "$os" = darwin ]; then
	# Test artifacts are unsigned, so the macOS check must say so rather than
	# treating Go's default ad-hoc signature as a Developer ID signature.
	expect_log "flags the ad-hoc signature" "ad-hoc signature"
fi
assert_exec "installed binary is executable" "$bindir/change-saga"
if [ "$(installed_mode "$bindir/change-saga")" = 755 ]; then
	record "installed binary permissions are 0755" 0
else
	record "installed binary permissions are 0755" 1
fi
if "$bindir/change-saga" version | grep -q "^$version"; then
	record "installed binary reports the injected version" 0
else
	record "installed binary reports the injected version" 1
fi

echo "== provenance policy"
attested="$work/attested"
check "attestation verification succeeds" 0 sh "$install_sh" \
	--version "$tag" --dir "$attested" --attestation --dry-run
expect_log "reports attestation verification" "build provenance ok"
if grep -Fxq -- '--repo' "$CHANGE_SAGA_TEST_GH_LOG" &&
	grep -Fxq -- 'twentyideas/changesaga' "$CHANGE_SAGA_TEST_GH_LOG" &&
	grep -Fxq -- '--signer-workflow' "$CHANGE_SAGA_TEST_GH_LOG" &&
	grep -Fxq -- 'twentyideas/changesaga/.github/workflows/release.yml' "$CHANGE_SAGA_TEST_GH_LOG" &&
	grep -Fxq -- '--source-ref' "$CHANGE_SAGA_TEST_GH_LOG" &&
	grep -Fxq -- "refs/tags/$tag" "$CHANGE_SAGA_TEST_GH_LOG"; then
	record "pins attestation to the release workflow and tag" 0
else
	record "pins attestation to the release workflow and tag" 1
fi
assert_absent "attestation dry run installs nothing" "$attested/change-saga"

echo "== latest resolution"
rm -rf "$bindir"
check "resolves latest tag" 0 sh "$install_sh" --dir "$bindir"
assert_exec "latest install landed" "$bindir/change-saga"

echo "== reinstall over a running-in-place binary"
check "reinstall succeeds" 0 sh "$install_sh" --version "$tag" --dir "$bindir"

echo "== destination symlink safety"
victim="$work/symlink-victim"
printf 'do not overwrite\n' >"$victim"
rm "$bindir/change-saga"
ln -s "$victim" "$bindir/change-saga"
check "install replaces a destination symlink" 0 sh "$install_sh" --version "$tag" --dir "$bindir"
if [ ! -L "$bindir/change-saga" ]; then record "installed binary is not a symlink" 0; else record "installed binary is not a symlink" 1; fi
assert_contains "destination symlink target was untouched" "$victim" "do not overwrite"

echo "== dry run installs nothing"
drydir="$work/dry"
mkdir -p "$drydir"
check "dry run succeeds" 0 sh "$install_sh" --version "$tag" --dir "$drydir" --dry-run
assert_absent "dry run left no binary" "$drydir/change-saga"

echo "== tampered archive is rejected"
tampered="$work/tampered"
cp -R "$release" "$tampered"
printf 'evil' >> "$tampered/$archive"
baddir="$work/bad"
mkdir -p "$baddir"
printf 'existing install\n' >"$baddir/change-saga"
check "tampered archive fails" 1 \
	env CHANGE_SAGA_TEST_RELEASE="$tampered" sh "$install_sh" --version "$tag" --dir "$baddir"
expect_log "explains the mismatch" "checksum mismatch"
assert_contains "existing install survives checksum failure" "$baddir/change-saga" "existing install"

echo "== missing SHA256SUMS is fatal"
nosums="$work/nosums"
cp -R "$release" "$nosums"
rm "$nosums/SHA256SUMS"
check "missing checksums fails" 1 \
	env CHANGE_SAGA_TEST_RELEASE="$nosums" sh "$install_sh" --version "$tag" --dir "$work/none1"
expect_log "refuses unverified install" "refusing to install unverified"

echo "== checksum file without our entry is fatal"
noentry="$work/noentry"
cp -R "$release" "$noentry"
printf 'deadbeef  some_other_file.tar.gz\n' > "$noentry/SHA256SUMS"
check "unknown entry fails" 1 \
	env CHANGE_SAGA_TEST_RELEASE="$noentry" sh "$install_sh" --version "$tag" --dir "$work/none2"
expect_log "names the missing entry" "exactly one well-formed entry"

echo "== duplicate checksum entry is fatal"
duplicates="$work/duplicates"
cp -R "$release" "$duplicates"
cat "$release/SHA256SUMS" >>"$duplicates/SHA256SUMS"
check "duplicate checksum fails" 1 \
	env CHANGE_SAGA_TEST_RELEASE="$duplicates" sh "$install_sh" --version "$tag" --dir "$work/none-duplicate"
expect_log "duplicate checksum is explained" "exactly one well-formed entry"

echo "== archive layout is validated before extraction"
layout="$work/layout"
layout_stage="$work/layout-stage"
cp -R "$release" "$layout"
mkdir -p "$layout_stage/extra"
tar -xzf "$release/$archive" -C "$layout_stage"
printf 'must not be extracted\n' >"$layout_stage/extra/escape"
tar -czf "$layout/$archive" -C "$layout_stage" change-saga LICENSE README.md extra
refresh_checksum "$layout" "$archive"
layout_dir="$work/layout-install"
mkdir -p "$layout_dir"
printf 'existing install\n' >"$layout_dir/change-saga"
check "unexpected archive member fails" 1 \
	env CHANGE_SAGA_TEST_RELEASE="$layout" sh "$install_sh" --version "$tag" --dir "$layout_dir"
expect_log "unexpected member is explained" "archive layout is invalid"
assert_contains "existing install survives invalid layout" "$layout_dir/change-saga" "existing install"
assert_absent "unexpected archive member was never extracted" "$work/escape"

echo "== link entries cannot escape the unpack directory"
link_release="$work/link-release"
link_stage="$work/link-stage"
escape_target="$work/escape-target"
mkdir -p "$link_release" "$link_stage"
printf 'do not chmod or execute\n' >"$escape_target"
escape_mode="$(installed_mode "$escape_target")"
cp "$repo_root/LICENSE" "$repo_root/README.md" "$link_stage/"
ln -s "$escape_target" "$link_stage/change-saga"
tar -czf "$link_release/$archive" -C "$link_stage" change-saga LICENSE README.md
refresh_checksum "$link_release" "$archive"
check "symlink binary entry fails" 1 \
	env CHANGE_SAGA_TEST_RELEASE="$link_release" sh "$install_sh" --version "$tag" --dir "$work/none-link"
expect_log "symlink entry is explained" "regular files"
assert_contains "symlink target was untouched" "$escape_target" "do not chmod or execute"
if [ "$(installed_mode "$escape_target")" = "$escape_mode" ]; then
	record "symlink target permissions were untouched" 0
else
	record "symlink target permissions were untouched" 1
fi

echo "== release binary must match its tag"
wrong_version="9.9.8"
wrong_archive="change-saga_${wrong_version}_${os}_${arch}.tar.gz"
wrong_release="$work/wrong-version"
wrong_stage="$work/wrong-version-stage"
mkdir -p "$wrong_release" "$wrong_stage"
tar -xzf "$release/$archive" -C "$wrong_stage"
tar -czf "$wrong_release/$wrong_archive" -C "$wrong_stage" change-saga LICENSE README.md
refresh_checksum "$wrong_release" "$wrong_archive"
check "mismatched binary version fails" 1 \
	env CHANGE_SAGA_TEST_RELEASE="$wrong_release" sh "$install_sh" --version "$wrong_version" --dir "$work/none-version"
expect_log "mismatched version is explained" "unexpected version"

echo "== missing platform archive is a clear error"
empty="$work/empty"
mkdir -p "$empty"
cp "$release/SHA256SUMS" "$empty/"
check "missing archive fails" 1 \
	env CHANGE_SAGA_TEST_RELEASE="$empty" sh "$install_sh" --version "$tag" --dir "$work/none3"
expect_log "names the platform" "no release archive"

echo "== unwritable install directory is refused without sudo"
rodir="$work/readonly"
mkdir -p "$rodir"
chmod 0555 "$rodir"
check "unwritable dir fails" 1 sh "$install_sh" --version "$tag" --dir "$rodir"
expect_log "does not offer to sudo for you" "will not call sudo"
chmod 0755 "$rodir"

echo "== argument handling"
check "help exits zero" 0 sh "$install_sh" --help
check "unknown option fails" 1 sh "$install_sh" --nope
check "path-traversing version fails before download" 1 \
	sh "$install_sh" --version '9.9.9/../../escape' --dir "$work/none-version-path"
expect_log "invalid version is explained" "invalid release tag"
check "leading-zero SemVer core fails" 1 \
	sh "$install_sh" --version 'v09.9.9' --dir "$work/none-leading-zero"
expect_log "leading-zero version is explained" "invalid release tag"
check "empty prerelease identifier fails" 1 \
	sh "$install_sh" --version 'v9.9.9-rc..1' --dir "$work/none-empty-identifier"
expect_log "empty prerelease identifier is explained" "invalid release tag"
check "repository metacharacters fail before download" 1 \
	sh "$install_sh" --repo 'owner/repo;touch-pwned' --version "$tag" --dir "$work/none-repo"
expect_log "invalid repository is explained" "invalid GitHub repository"
check "dot repository component fails before download" 1 \
	sh "$install_sh" --repo '../repo' --version "$tag" --dir "$work/none-dot-repo"
expect_log "dot repository component is explained" "invalid GitHub repository"
check "untrusted latest redirect tag is rejected" 1 \
	env CHANGE_SAGA_TEST_TAG='v09.9.9' sh "$install_sh" --dir "$work/none-latest-tag"
expect_log "invalid latest tag is explained" "invalid release tag"

echo "== failed atomic swap cleans its staging file"
mvstub="$work/mvstub"
swapdir="$work/swap-failure"
mkdir -p "$mvstub" "$swapdir"
cat >"$mvstub/mv" <<'STUB'
#!/bin/sh
exit 73
STUB
chmod +x "$mvstub/mv"
printf 'existing install\n' >"$swapdir/change-saga"
check "failed destination swap propagates" 1 \
	env PATH="$mvstub:$PATH" sh "$install_sh" --version "$tag" --dir "$swapdir"
expect_log "failed swap is explained" "could not atomically replace"
assert_contains "existing install survives failed swap" "$swapdir/change-saga" "existing install"
if find "$swapdir" -name '.change-saga.install.*' -print | grep -q .; then
	record "failed swap leaves no staging file" 1
else
	record "failed swap leaves no staging file" 0
fi

echo
printf '%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
