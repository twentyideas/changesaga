#!/bin/sh
# Change Saga installer for macOS and Linux.
#
#   curl -fsSL https://raw.githubusercontent.com/change-saga/change-saga/main/scripts/install.sh | sh
#
# With options, pass them after `-s --`:
#
#   curl -fsSL .../install.sh | sh -s -- --version v0.3.0 --dir ~/bin
#
# Security properties this script maintains, deliberately:
#   * every download is HTTPS-only with TLS 1.2 or newer, and redirects are
#     followed only to the same scheme;
#   * the SHA-256 of the archive is checked against the release SHA256SUMS file
#     and installation aborts if it does not match, or if no checksum tool is
#     available — it never degrades to an unverified install;
#   * on macOS the code signature is inspected and its signing authority (or
#     lack of one) is reported when `codesign` is present;
#   * it never removes the com.apple.quarantine attribute, never calls
#     `spctl --master-disable`, and never disables SIP or any other platform
#     protection. If Gatekeeper rejects a binary, that is a real signal and the
#     fix belongs upstream in the release, not in this installer;
#   * it never runs `sudo` on your behalf. The default target is a per-user
#     directory unless /usr/local/bin is already writable by the current,
#     non-root user; it never attempts to gain access to a system directory.
set -eu

REPO="${CHANGE_SAGA_REPO:-change-saga/change-saga}"
BIN_NAME="change-saga"
VERSION="${CHANGE_SAGA_VERSION:-latest}"
INSTALL_DIR="${CHANGE_SAGA_INSTALL_DIR:-}"
DRY_RUN=0
VERIFY_ATTESTATION=0

log() { printf '%s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

usage() {
	cat >&2 <<'USAGE'
Install the Change Saga CLI.

Options:
  --version <tag>   release tag to install (default: latest, e.g. v0.3.0)
  --dir <path>      install directory (default: ~/.local/bin, or /usr/local/bin
                    when it is already writable by you)
  --repo <owner/name> GitHub repository to install from
  --attestation     additionally verify GitHub build provenance (needs `gh`)
  --dry-run         download and verify, but do not install
  --help            show this message

Environment: CHANGE_SAGA_VERSION, CHANGE_SAGA_INSTALL_DIR, CHANGE_SAGA_REPO
USAGE
}

while [ "$#" -gt 0 ]; do
	case "$1" in
	--version) VERSION="${2:?--version needs a value}"; shift 2 ;;
	--version=*) VERSION="${1#*=}"; shift ;;
	--dir) INSTALL_DIR="${2:?--dir needs a value}"; shift 2 ;;
	--dir=*) INSTALL_DIR="${1#*=}"; shift ;;
	--repo) REPO="${2:?--repo needs a value}"; shift 2 ;;
	--repo=*) REPO="${1#*=}"; shift ;;
	--attestation) VERIFY_ATTESTATION=1; shift ;;
	--dry-run) DRY_RUN=1; shift ;;
	-h|--help) usage; exit 0 ;;
	*) usage; die "unknown option: $1" ;;
	esac
done

need() { command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"; }
need uname
need mktemp
need tar
need awk
need grep

# REPO becomes part of a URL and TAG becomes part of both a URL and local file
# names. Keep both in the same deliberately narrow grammar as the release job.
case "$REPO" in
*/*)
	repo_owner="${REPO%%/*}"
	repo_name="${REPO#*/}"
	;;
*) die "invalid GitHub repository: $REPO" ;;
esac
case "$repo_owner" in "" | "." | ".." | *[!A-Za-z0-9_.-]*) die "invalid GitHub repository: $REPO" ;; esac
case "$repo_name" in "" | "." | ".." | *[!A-Za-z0-9_.-]*) die "invalid GitHub repository: $REPO" ;; esac

# --- platform detection -----------------------------------------------------

detect_os() {
	os="$(uname -s)"
	case "$os" in
	Darwin) echo darwin ;;
	Linux) echo linux ;;
	*)
		die "unsupported operating system: $os. Windows users: download the
       .zip from https://github.com/$REPO/releases and verify it against
       SHA256SUMS."
		;;
	esac
}

detect_arch() {
	arch="$(uname -m)"
	case "$arch" in
	x86_64 | amd64) echo amd64 ;;
	arm64 | aarch64) echo arm64 ;;
	*) die "unsupported architecture: $arch. Prebuilt archives cover amd64 and arm64." ;;
	esac
}

OS="$(detect_os)"
ARCH="$(detect_arch)"

# Rosetta 2 reports x86_64 for a shell running translated on Apple silicon.
# Prefer the native arm64 build in that case.
if [ "$OS" = darwin ] && [ "$ARCH" = amd64 ] &&
	[ "$(sysctl -n sysctl.proc_translated 2>/dev/null || echo 0)" = "1" ]; then
	log "note: running under Rosetta 2; installing the native arm64 build"
	ARCH=arm64
fi

# --- download helpers -------------------------------------------------------

if command -v curl >/dev/null 2>&1; then
	DOWNLOADER=curl
elif command -v wget >/dev/null 2>&1; then
	DOWNLOADER=wget
else
	die "neither curl nor wget is available"
fi

fetch() { # fetch <url> <dest>
	case "$1" in
	https://*) ;;
	*) die "refusing to download over a non-HTTPS URL: $1" ;;
	esac
	if [ "$DOWNLOADER" = curl ]; then
		curl --fail --silent --show-error --location \
			--proto '=https' --proto-redir '=https' --tlsv1.2 \
			--retry 3 --retry-connrefused --connect-timeout 20 \
			--output "$2" "$1"
	else
		wget --https-only --secure-protocol=TLSv1_2 --tries=3 --timeout=20 \
			--quiet --output-document "$2" "$1"
	fi
}

resolve_latest() { # print the newest release tag
	# /releases/latest redirects to /releases/tag/<tag>; reading the redirect
	# target avoids the rate-limited JSON API entirely.
	url="https://github.com/$REPO/releases/latest"
	if [ "$DOWNLOADER" = curl ]; then
		effective="$(curl --fail --silent --show-error --location \
			--proto '=https' --proto-redir '=https' --tlsv1.2 \
			--retry 3 --retry-connrefused --connect-timeout 20 --output /dev/null \
			--write-out '%{url_effective}' "$url" || true)"
	else
		effective="$(wget --https-only --secure-protocol=TLSv1_2 \
			--tries=3 --timeout=20 --quiet --output-document /dev/null \
			--server-response "$url" 2>&1 |
			awk '/^  Location: /{print $2}' | tail -n 1 || true)"
	fi
	case "$effective" in
	*/releases/tag/*) printf '%s\n' "${effective##*/releases/tag/}" ;;
	*) die "could not resolve the latest release tag; pass --version <tag>" ;;
	esac
}

# --- checksum verification --------------------------------------------------

sha256_of() { # sha256_of <file> -> bare hex digest
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{print $1}'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{print $1}'
	elif command -v openssl >/dev/null 2>&1; then
		openssl dgst -sha256 "$1" | awk '{print $NF}'
	else
		# Fail closed: an unverified binary is worse than no binary.
		die "no SHA-256 tool found (need sha256sum, shasum, or openssl)"
	fi
}

# --- resolve the release ----------------------------------------------------

if [ "$VERSION" = latest ]; then
	VERSION="$(resolve_latest)"
fi
case "$VERSION" in
v*) TAG="$VERSION" ;;
*) TAG="v$VERSION" ;;
esac
case "$TAG" in
*[!0-9A-Za-z.+-]*) die "invalid release tag: $TAG" ;;
esac
SEMVER_TAG_RE='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
if ! printf '%s\n' "$TAG" |
	LC_ALL=C grep -Eq "$SEMVER_TAG_RE"; then
	die "invalid release tag: $TAG"
fi
PLAIN_VERSION="${TAG#v}"

ARCHIVE="${BIN_NAME}_${PLAIN_VERSION}_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/$REPO/releases/download/$TAG"

TMPDIR_SAGA="$(mktemp -d 2>/dev/null || mktemp -d -t saga-install)"
STAGED=""
cleanup() {
	if [ -n "$STAGED" ]; then
		rm -f "$STAGED" 2>/dev/null || true
	fi
	rm -rf "$TMPDIR_SAGA" 2>/dev/null || true
}
trap cleanup EXIT
trap 'exit 1' INT TERM HUP

log "change-saga $TAG for $OS/$ARCH"
log "downloading $ARCHIVE"
fetch "$BASE_URL/$ARCHIVE" "$TMPDIR_SAGA/$ARCHIVE" ||
	die "no release archive for $OS/$ARCH in $TAG (looked for $ARCHIVE)"
fetch "$BASE_URL/SHA256SUMS" "$TMPDIR_SAGA/SHA256SUMS" ||
	die "release $TAG has no SHA256SUMS file; refusing to install unverified"

if ! expected="$(LC_ALL=C awk -v want="$ARCHIVE" '
	{
		digest = substr($0, 1, 64)
		separator = substr($0, 65, 2)
		name = substr($0, 67)
		if (length(digest) == 64 && digest !~ /[^0-9A-Fa-f]/ &&
		    (separator == "  " || separator == " *") && name == want) {
			print tolower(digest)
			matches++
		}
	}
	END { if (matches != 1) exit 1 }
' "$TMPDIR_SAGA/SHA256SUMS")"; then
	die "SHA256SUMS must contain exactly one well-formed entry for $ARCHIVE"
fi
actual="$(sha256_of "$TMPDIR_SAGA/$ARCHIVE")"
if [ "$expected" != "$actual" ]; then
	die "checksum mismatch for $ARCHIVE
  expected $expected
  actual   $actual
Do not use the downloaded file. Report this at https://github.com/$REPO/issues"
fi
log "checksum ok: $actual"

if [ "$VERIFY_ATTESTATION" -eq 1 ]; then
	command -v gh >/dev/null 2>&1 ||
		die "--attestation needs the GitHub CLI (gh) on PATH"
	gh attestation verify "$TMPDIR_SAGA/$ARCHIVE" \
		--repo "$REPO" \
		--signer-workflow "$REPO/.github/workflows/release.yml" \
		--source-ref "refs/tags/$TAG" ||
		die "build provenance verification failed for $ARCHIVE"
	log "build provenance ok"
fi

# --- inspect and unpack -----------------------------------------------------

# A checksum proves which release asset was downloaded; it does not make an
# unsafe archive layout safe to unpack. Release archives contain exactly three
# regular files at their root. Validate that contract before extracting only
# the binary into an otherwise empty directory.
if ! tar -tzf "$TMPDIR_SAGA/$ARCHIVE" >"$TMPDIR_SAGA/members"; then
	die "could not read archive layout for $ARCHIVE"
fi
if ! LC_ALL=C awk '
	$0 == "change-saga" { binary++ ; next }
	$0 == "LICENSE" { license++ ; next }
	$0 == "README.md" { readme++ ; next }
	{ bad = 1 }
	END { if (bad || binary != 1 || license != 1 || readme != 1 || NR != 3) exit 1 }
' "$TMPDIR_SAGA/members"; then
	die "archive layout is invalid; expected only change-saga, LICENSE, and README.md at the archive root"
fi
if ! LC_ALL=C tar -tvzf "$TMPDIR_SAGA/$ARCHIVE" >"$TMPDIR_SAGA/member-details" ||
	! LC_ALL=C awk 'substr($1, 1, 1) != "-" { bad = 1 } END { if (bad || NR != 3) exit 1 }' \
		"$TMPDIR_SAGA/member-details"; then
	die "archive layout is invalid; all release entries must be regular files"
fi

UNPACK_DIR="$TMPDIR_SAGA/unpacked"
mkdir "$UNPACK_DIR"
if ! tar -xzf "$TMPDIR_SAGA/$ARCHIVE" -C "$UNPACK_DIR" "$BIN_NAME"; then
	die "could not extract $BIN_NAME from $ARCHIVE"
fi
DOWNLOADED_BINARY="$UNPACK_DIR/$BIN_NAME"
if [ ! -f "$DOWNLOADED_BINARY" ] || [ -L "$DOWNLOADED_BINARY" ]; then
	die "archive did not contain $BIN_NAME as a regular file"
fi
chmod 0700 "$DOWNLOADED_BINARY"

if [ "$OS" = darwin ] && command -v codesign >/dev/null 2>&1; then
	unsigned=""
	if codesign --verify --strict "$DOWNLOADED_BINARY" >/dev/null 2>&1; then
		# A Go binary is always ad-hoc signed, and ad-hoc passes --verify. Only
		# a real Authority line means a Developer ID certificate was used.
		authority="$(codesign --display --verbose=2 "$DOWNLOADED_BINARY" 2>&1 |
			awk -F'=' '/^Authority=/{print $2; exit}')"
		if [ -n "$authority" ]; then
			log "code signature ok: $authority"
		else
			unsigned="this build carries only an ad-hoc signature"
		fi
	else
		unsigned="this build has no valid code signature"
	fi
	if [ -n "$unsigned" ]; then
		log "warning: $unsigned."
		log "         It still runs when installed this way, because a file"
		log "         fetched by this installer carries no quarantine attribute. Gatekeeper"
		log "         is left exactly as it is; nothing here bypasses it."
	fi
fi

if ! version_output="$("$DOWNLOADED_BINARY" version 2>&1)"; then
	die "the downloaded binary did not run on this machine"
fi
case "$version_output" in
*'
'*) die "release $TAG contained a binary reporting a malformed version" ;;
"$PLAIN_VERSION" | "$PLAIN_VERSION ("* | "$PLAIN_VERSION built "*) ;;
*) die "release $TAG contained a binary reporting an unexpected version: $version_output" ;;
esac
log "verified $version_output"

if [ "$DRY_RUN" -eq 1 ]; then
	log "dry run: verified $ARCHIVE, nothing installed"
	exit 0
fi

# --- install ----------------------------------------------------------------

if [ -z "$INSTALL_DIR" ]; then
	if [ -w /usr/local/bin ] && [ "$(id -u)" -ne 0 ]; then
		INSTALL_DIR=/usr/local/bin
	else
		INSTALL_DIR="$HOME/.local/bin"
	fi
fi

case "$INSTALL_DIR" in
/*) ;;
*) INSTALL_DIR="$(pwd)/$INSTALL_DIR" ;;
esac

mkdir -p "$INSTALL_DIR" 2>/dev/null || true
[ -d "$INSTALL_DIR" ] || die "install directory does not exist: $INSTALL_DIR"
if [ ! -w "$INSTALL_DIR" ]; then
	die "$INSTALL_DIR is not writable by this user.
Choose a directory you own, for example:
  curl -fsSL https://raw.githubusercontent.com/$REPO/main/scripts/install.sh | sh -s -- --dir \"\$HOME/.local/bin\"
Or, if a system-wide install is what you want, review this script first and
then run the copy step yourself with sudo. This installer will not call sudo
for you."
fi

# Install through a temporary name in the destination directory so the swap is
# atomic and a running `change-saga` process is never overwritten in place.
destination="$INSTALL_DIR/$BIN_NAME"
[ ! -d "$destination" ] || die "install destination is a directory: $destination"
STAGED="$(mktemp "$INSTALL_DIR/.$BIN_NAME.install.XXXXXX")" ||
	die "could not create a staging file in $INSTALL_DIR"
cp "$DOWNLOADED_BINARY" "$STAGED" || die "could not stage $BIN_NAME in $INSTALL_DIR"
chmod 0755 "$STAGED" || die "could not set executable permissions on staged $BIN_NAME"
mv -f "$STAGED" "$destination" || die "could not atomically replace $destination"
STAGED=""
log "installed $destination"

case ":$PATH:" in
*":$INSTALL_DIR:"*) ;;
*)
	log ""
	log "$INSTALL_DIR is not on your PATH. Add this to your shell profile:"
	log "  export PATH=\"$INSTALL_DIR:\$PATH\""
	;;
esac

log "run '$BIN_NAME help' to get started"
