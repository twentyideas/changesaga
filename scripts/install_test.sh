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
archive="review-saga_${version}_${os}_${arch}.tar.gz"

echo "== building test release artifact"
"$repo_root/scripts/build-release.sh" "$version" "$os" "$arch" "$release" >/dev/null
(cd "$release" && "$repo_root/scripts/sha256.sh" "$archive" > SHA256SUMS)

# Stub curl: maps https://github.com/<repo>/releases/download/<tag>/<file> onto
# $SAGA_TEST_RELEASE/<file>, and resolves /releases/latest to the test tag.
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
	[ -n "$write_out" ] && printf 'https://github.com/x/y/releases/tag/%s\n' "$SAGA_TEST_TAG"
	exit 0
	;;
esac
file="${url##*/}"
src="$SAGA_TEST_RELEASE/$file"
[ -f "$src" ] || exit 22   # curl's exit code for HTTP 404 under --fail
[ -n "$out" ] && cp "$src" "$out"
exit 0
STUB
chmod +x "$stub/curl"

export SAGA_TEST_RELEASE="$release" SAGA_TEST_TAG="$tag"
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

echo "== happy path"
bindir="$work/bin"
check "install succeeds" 0 sh "$install_sh" --version "$tag" --dir "$bindir"
expect_log "reports checksum" "checksum ok"
if [ "$os" = darwin ]; then
	# Test artifacts are unsigned, so the macOS check must say so rather than
	# treating Go's default ad-hoc signature as a Developer ID signature.
	expect_log "flags the ad-hoc signature" "ad-hoc signature"
fi
assert_exec "installed binary is executable" "$bindir/review-saga"
if "$bindir/review-saga" version | grep -q "^$version"; then
	record "installed binary reports the injected version" 0
else
	record "installed binary reports the injected version" 1
fi

echo "== latest resolution"
rm -rf "$bindir"
check "resolves latest tag" 0 sh "$install_sh" --dir "$bindir"
assert_exec "latest install landed" "$bindir/review-saga"

echo "== reinstall over a running-in-place binary"
check "reinstall succeeds" 0 sh "$install_sh" --version "$tag" --dir "$bindir"

echo "== dry run installs nothing"
drydir="$work/dry"
mkdir -p "$drydir"
check "dry run succeeds" 0 sh "$install_sh" --version "$tag" --dir "$drydir" --dry-run
assert_absent "dry run left no binary" "$drydir/review-saga"

echo "== tampered archive is rejected"
tampered="$work/tampered"
cp -R "$release" "$tampered"
printf 'evil' >> "$tampered/$archive"
baddir="$work/bad"
mkdir -p "$baddir"
check "tampered archive fails" 1 \
	env SAGA_TEST_RELEASE="$tampered" sh "$install_sh" --version "$tag" --dir "$baddir"
expect_log "explains the mismatch" "checksum mismatch"
assert_absent "nothing installed after mismatch" "$baddir/review-saga"

echo "== missing SHA256SUMS is fatal"
nosums="$work/nosums"
cp -R "$release" "$nosums"
rm "$nosums/SHA256SUMS"
check "missing checksums fails" 1 \
	env SAGA_TEST_RELEASE="$nosums" sh "$install_sh" --version "$tag" --dir "$work/none1"
expect_log "refuses unverified install" "refusing to install unverified"

echo "== checksum file without our entry is fatal"
noentry="$work/noentry"
cp -R "$release" "$noentry"
printf 'deadbeef  some_other_file.tar.gz\n' > "$noentry/SHA256SUMS"
check "unknown entry fails" 1 \
	env SAGA_TEST_RELEASE="$noentry" sh "$install_sh" --version "$tag" --dir "$work/none2"
expect_log "names the missing entry" "no entry for"

echo "== missing platform archive is a clear error"
empty="$work/empty"
mkdir -p "$empty"
cp "$release/SHA256SUMS" "$empty/"
check "missing archive fails" 1 \
	env SAGA_TEST_RELEASE="$empty" sh "$install_sh" --version "$tag" --dir "$work/none3"
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

echo
printf '%d passed, %d failed\n' "$pass" "$fail"
[ "$fail" -eq 0 ]
