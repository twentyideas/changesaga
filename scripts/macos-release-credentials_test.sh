#!/usr/bin/env bash
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
checker="$here/macos-release-credentials.sh"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
passes=0

check_case() {
	name="$1"
	expected_exit="$2"
	expected_outputs="$3"
	shift 3
	output="$work/$name.outputs"
	log="$work/$name.log"

	set +e
	env -i PATH="$PATH" GITHUB_OUTPUT="$output" "$@" "$checker" > "$log" 2>&1
	actual_exit=$?
	set -e
	if [ "$actual_exit" -ne "$expected_exit" ]; then
		echo "not ok - $name (exit $actual_exit, wanted $expected_exit)" >&2
		cat "$log" >&2
		exit 1
	fi
	if [ -n "$expected_outputs" ] &&
		! diff -u <(printf '%s\n' "$expected_outputs") "$output"; then
		echo "not ok - $name (unexpected outputs)" >&2
		exit 1
	fi
	passes=$((passes + 1))
	echo "ok $passes - $name"
}

signing=(
	MACOS_CERTIFICATE_P12_BASE64=certificate
	MACOS_CERTIFICATE_PASSWORD=password
	MACOS_SIGNING_IDENTITY=identity
)
notarization=(
	APPLE_API_KEY_ID=key
	APPLE_API_ISSUER_ID=issuer
	APPLE_API_KEY_P8_BASE64=private-key
)

check_case none 0 $'available=false\nnotarization_available=false'
check_case signing-only 0 $'available=true\nnotarization_available=false' "${signing[@]}"
check_case signing-and-notarization 0 $'available=true\nnotarization_available=true' "${signing[@]}" "${notarization[@]}"
check_case partial-signing 1 '' MACOS_CERTIFICATE_PASSWORD=password
check_case partial-notarization 1 $'available=true' "${signing[@]}" APPLE_API_KEY_ID=key
check_case notarization-without-signing 1 $'available=false' "${notarization[@]}"
check_case require-signing 1 '' REQUIRE_SIGNING=true
check_case require-notarization 1 $'available=true' "${signing[@]}" REQUIRE_NOTARIZATION=true

echo "1..$passes"
