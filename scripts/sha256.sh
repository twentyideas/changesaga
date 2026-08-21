#!/usr/bin/env sh
# Print "<sha256>  <basename>" for each argument, using whichever checksum tool
# the platform provides. Output matches the format `sha256sum -c` expects.
set -eu

if [ "$#" -eq 0 ]; then
	echo "usage: $0 <file> [file ...]" >&2
	exit 2
fi

if command -v sha256sum >/dev/null 2>&1; then
	checksum_tool=sha256sum
elif command -v shasum >/dev/null 2>&1; then
	checksum_tool=shasum
else
	echo "no sha256sum or shasum available" >&2
	exit 1
fi

for target in "$@"; do
	if [ ! -f "$target" ]; then
		echo "not a regular file: $target" >&2
		exit 1
	fi
	case "$target" in
	*/*)
		dir="${target%/*}"
		base="${target##*/}"
		[ -n "$dir" ] || dir=/
		;;
	*)
		dir=.
		base="$target"
		;;
	esac
	case "$dir" in
	-*) dir="./$dir" ;;
	esac
	case "$base" in
	*'
'*)
		echo "checksum filenames must not contain newlines: $target" >&2
		exit 1
		;;
	esac
	if [ "$checksum_tool" = sha256sum ]; then
		(cd "$dir" && sha256sum -- "$base")
	else
		(cd "$dir" && shasum -a 256 -- "$base")
	fi
done
