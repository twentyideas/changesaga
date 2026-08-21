#!/usr/bin/env bash
# Validate the optional macOS signing/notarization credential sets and emit
# GitHub Actions outputs consumed by release.yml.
set -euo pipefail

: "${GITHUB_OUTPUT:?GITHUB_OUTPUT must name the workflow output file}"

signing_values=0
notarization_values=0
for value in \
	"${MACOS_CERTIFICATE_P12_BASE64:-}" \
	"${MACOS_CERTIFICATE_PASSWORD:-}" \
	"${MACOS_SIGNING_IDENTITY:-}"; do
	[ -z "$value" ] || signing_values=$((signing_values + 1))
done
for value in \
	"${APPLE_API_KEY_ID:-}" \
	"${APPLE_API_ISSUER_ID:-}" \
	"${APPLE_API_KEY_P8_BASE64:-}"; do
	[ -z "$value" ] || notarization_values=$((notarization_values + 1))
done

if [ "$signing_values" -eq 3 ]; then
	echo "available=true" >> "$GITHUB_OUTPUT"
elif [ "$signing_values" -ne 0 ]; then
	echo "::error::Developer ID credentials are only partially configured. Set all three MACOS_* secrets or remove all three."
	exit 1
elif [ "${REQUIRE_SIGNING:-}" = "true" ]; then
	echo "::error::REQUIRE_MACOS_SIGNING is set but the Developer ID secrets are missing from the release-signing environment. See docs/releasing.md."
	exit 1
else
	echo "available=false" >> "$GITHUB_OUTPUT"
	echo "::warning::Building an unsigned macOS artifact: Developer ID secrets are not configured. See docs/releasing.md."
fi

if [ "$notarization_values" -eq 3 ]; then
	if [ "$signing_values" -ne 3 ]; then
		echo "::error::Notarization credentials are configured, but Developer ID signing credentials are missing."
		exit 1
	fi
	echo "notarization_available=true" >> "$GITHUB_OUTPUT"
elif [ "$notarization_values" -ne 0 ]; then
	echo "::error::Apple notarization credentials are only partially configured. Set all three APPLE_* secrets or remove all three."
	exit 1
elif [ "${REQUIRE_NOTARIZATION:-}" = "true" ]; then
	echo "::error::REQUIRE_MACOS_NOTARIZATION is set but the App Store Connect secrets are missing from the release-signing environment. See docs/releasing.md."
	exit 1
else
	echo "notarization_available=false" >> "$GITHUB_OUTPUT"
	if [ "$signing_values" -eq 3 ]; then
		echo "::warning::Building a signed but unnotarized macOS artifact: App Store Connect secrets are not configured."
	fi
fi
