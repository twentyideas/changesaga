#!/usr/bin/env bash
# Notarize a signed macOS binary with Apple and wait for the result.
#
# notarytool only accepts .zip, .dmg, and .pkg, so the binary is zipped purely
# as a submission container. The published artifact stays a .tar.gz.
#
# A bare executable cannot carry a stapled ticket (`stapler` only writes into
# bundles, disk images, and installer packages), so Gatekeeper resolves the
# ticket from Apple's servers on first launch. That is the accepted trade-off
# for a single-file CLI; see docs/releasing.md.
#
# Required environment:
#   APPLE_API_KEY_ID          App Store Connect key id
#   APPLE_API_ISSUER_ID       App Store Connect issuer id
#   APPLE_API_KEY_P8_BASE64   base64 of the AuthKey_<id>.p8 file
set -euo pipefail
umask 077

binary="${1:?usage: $0 <binary>}"
: "${APPLE_API_KEY_ID:?missing APPLE_API_KEY_ID}"
: "${APPLE_API_ISSUER_ID:?missing APPLE_API_ISSUER_ID}"
: "${APPLE_API_KEY_P8_BASE64:?missing APPLE_API_KEY_P8_BASE64}"

workdir="$(mktemp -d)"
key="$workdir/AuthKey.p8"
submission="$workdir/notarize.zip"
trap 'rm -rf "$workdir"' EXIT

printf '%s' "$APPLE_API_KEY_P8_BASE64" | /usr/bin/base64 -D > "$key"
ditto -c -k --keepParent "$binary" "$submission"

xcrun notarytool submit "$submission" \
	--key "$key" \
	--key-id "$APPLE_API_KEY_ID" \
	--issuer "$APPLE_API_ISSUER_ID" \
	--wait \
	--timeout 30m

# Gatekeeper's own assessment is the real proof the ticket is live. It is
# reported but not fatal, because propagation can lag the submit response.
if spctl --assess --type exec --verbose=4 "$binary" 2>&1; then
	echo "gatekeeper accepts $binary"
else
	echo "warning: gatekeeper assessment did not pass yet for $binary" >&2
fi
