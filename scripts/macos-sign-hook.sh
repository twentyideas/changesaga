#!/usr/bin/env bash
# SAGA_SIGN_HOOK entry point for the trusted macOS release job.
#
# scripts/build-release.sh calls this with the freshly built binary, before it
# is archived, so the artifact users download is the signed one. Notarization
# runs here too, because the ticket is bound to the signature.
set -euo pipefail

binary="${1:?usage: $0 <binary>}"
here="$(cd "$(dirname "$0")" && pwd)"

"$here/macos-sign.sh" "$binary"

if [ -n "${APPLE_API_KEY_P8_BASE64:-}" ]; then
	"$here/macos-notarize.sh" "$binary"
else
	echo "notarization skipped: no App Store Connect API key configured" >&2
fi
