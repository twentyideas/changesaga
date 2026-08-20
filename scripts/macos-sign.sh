#!/usr/bin/env bash
# Sign one macOS binary with a Developer ID Application identity.
#
# Used as SAGA_SIGN_HOOK by scripts/build-release.sh so the binary is signed
# before it is archived. Hardened runtime and a secure timestamp are both
# required for notarization to succeed.
#
# Required environment:
#   MACOS_SIGNING_IDENTITY  e.g. "Developer ID Application: Acme Inc (TEAMID)"
#   SAGA_KEYCHAIN           keychain created by scripts/macos-keychain.sh
set -euo pipefail

binary="${1:?usage: $0 <binary>}"
: "${MACOS_SIGNING_IDENTITY:?missing MACOS_SIGNING_IDENTITY}"
: "${SAGA_KEYCHAIN:?missing SAGA_KEYCHAIN}"

codesign --force --timestamp --options runtime \
	--keychain "$SAGA_KEYCHAIN" \
	--sign "$MACOS_SIGNING_IDENTITY" \
	"$binary"

codesign --verify --strict --verbose=2 "$binary"
echo "signed $binary"
