#!/usr/bin/env bash
# Import the Developer ID Application certificate into a throwaway keychain.
#
# Runs only in the trusted, tag-only macOS release job. Forked pull requests
# never receive these secrets, so this script is never reached from fork CI.
#
# Required environment:
#   MACOS_CERTIFICATE_P12_BASE64  base64 of the exported .p12 (cert + key)
#   MACOS_CERTIFICATE_PASSWORD    password used when exporting the .p12
#
# Writes the keychain path to $GITHUB_ENV as SAGA_KEYCHAIN when running in
# Actions so later steps and the cleanup step can find it.
set -euo pipefail

: "${MACOS_CERTIFICATE_P12_BASE64:?missing MACOS_CERTIFICATE_P12_BASE64}"
: "${MACOS_CERTIFICATE_PASSWORD:?missing MACOS_CERTIFICATE_PASSWORD}"

keychain="${SAGA_KEYCHAIN:-$RUNNER_TEMP/saga-signing.keychain-db}"
# The keychain password is ephemeral and never leaves this runner.
keychain_password="$(openssl rand -base64 24)"
cert="$RUNNER_TEMP/saga-signing.p12"

cleanup_cert() { rm -f "$cert"; }
trap cleanup_cert EXIT

printf '%s' "$MACOS_CERTIFICATE_P12_BASE64" | base64 --decode > "$cert"

security create-keychain -p "$keychain_password" "$keychain"
# Auto-lock disabled for the life of the job; the keychain is deleted in cleanup.
security set-keychain-settings -lut 21600 "$keychain"
security unlock-keychain -p "$keychain_password" "$keychain"
security import "$cert" -k "$keychain" -P "$MACOS_CERTIFICATE_PASSWORD" \
	-T /usr/bin/codesign -T /usr/bin/security
# Allow codesign to use the key without an interactive prompt.
security set-key-partition-list -S apple-tool:,apple:,codesign: \
	-s -k "$keychain_password" "$keychain" >/dev/null
# Prepend rather than replace, so the runner's default keychains stay searchable.
# Word splitting of the existing list is intentional here.
# shellcheck disable=SC2046
security list-keychains -d user -s "$keychain" $(security list-keychains -d user | tr -d '"')

echo "imported signing identities:"
security find-identity -v -p codesigning "$keychain"

if [ -n "${GITHUB_ENV:-}" ]; then
	echo "SAGA_KEYCHAIN=$keychain" >> "$GITHUB_ENV"
fi
