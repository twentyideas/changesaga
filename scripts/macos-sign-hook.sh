#!/usr/bin/env bash
# CHANGE_SAGA_SIGN_HOOK entry point for the trusted macOS release job.
#
# scripts/build-release.sh calls this with the freshly built binary, before it
# is archived, so the artifact users download is the signed one. Notarization
# runs here too, because the ticket is bound to the signature.
set -euo pipefail

binary="${1:?usage: $0 <binary>}"
here="$(cd "$(dirname "$0")" && pwd)"

"$here/macos-sign.sh" "$binary"

notarization_values=0
for value in "${APPLE_API_KEY_ID:-}" "${APPLE_API_ISSUER_ID:-}" "${APPLE_API_KEY_P8_BASE64:-}"; do
	[ -z "$value" ] || notarization_values=$((notarization_values + 1))
done

if [ "$notarization_values" -eq 3 ]; then
	"$here/macos-notarize.sh" "$binary"
elif [ "$notarization_values" -ne 0 ]; then
	echo "notarization credentials are only partially configured; refusing to continue" >&2
	exit 1
else
	echo "notarization skipped: App Store Connect credentials are not configured" >&2
fi
