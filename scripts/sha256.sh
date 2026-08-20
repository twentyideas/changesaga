#!/usr/bin/env sh
# Print "<sha256>  <basename>" for each argument, using whichever checksum tool
# the platform provides. Output matches the format `sha256sum -c` expects.
set -eu

for target in "$@"; do
	dir="$(dirname "$target")"
	base="$(basename "$target")"
	if command -v sha256sum >/dev/null 2>&1; then
		(cd "$dir" && sha256sum "$base")
	elif command -v shasum >/dev/null 2>&1; then
		(cd "$dir" && shasum -a 256 "$base")
	else
		echo "no sha256sum or shasum available" >&2
		exit 1
	fi
done
