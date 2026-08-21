#!/usr/bin/env bash
# Check every relative Markdown link in the repository.
#
# Verifies that each in-repository link target exists on disk and, when the link
# carries a #fragment into a Markdown file, that a heading with that anchor
# exists. External (http/https/mailto) links are listed but not fetched, so the
# check stays offline and deterministic.
#
# Usage: ./scripts/check-docs-links.sh [file...]
set -euo pipefail

cd "$(dirname "$0")/.."

if [ "$#" -gt 0 ]; then
	files=("$@")
else
	files=()
	while IFS= read -r listed; do
		files+=("$listed")
	done < <(git ls-files --cached --others --exclude-standard '*.md')
fi

failures=0
checked=0
external=0

# GitHub's heading slug: lowercase, drop anything that is not a letter, digit,
# space, hyphen or underscore, then turn spaces into hyphens.
slugify() {
	printf '%s\n' "$1" |
		tr '[:upper:]' '[:lower:]' |
		sed -e 's/[^a-z0-9 _-]//g' -e 's/ /-/g'
}

# Anchors a Markdown file offers: explicit {#id} markers win, otherwise the
# slug of the heading text. Also emits <a name="..."> and id="..." attributes.
anchors_of() {
	local file="$1" line heading explicit
	while IFS= read -r line; do
		case "$line" in
		'#'*)
			heading="$(printf '%s' "$line" | sed -e 's/^#\{1,6\}[[:space:]]*//' -e 's/[[:space:]]*#*[[:space:]]*$//')"
			explicit="$(printf '%s' "$heading" | sed -n 's/.*{#\([A-Za-z0-9_-]*\)}.*/\1/p')"
			if [ -n "$explicit" ]; then
				printf '%s\n' "$explicit"
			fi
			heading="$(printf '%s' "$heading" | sed -e 's/{#[A-Za-z0-9_-]*}//g' -e 's/[[:space:]]*$//')"
			# Strip inline code, emphasis and link syntax the way GitHub does.
			heading="$(printf '%s' "$heading" | sed -e 's/`//g' -e 's/\[\([^]]*\)\]([^)]*)/\1/g' -e 's/\*//g' -e 's/_//g')"
			slugify "$heading"
			;;
		esac
	done <"$file"
	grep -o 'name="[A-Za-z0-9_-]*"' "$file" 2>/dev/null | sed 's/name="\(.*\)"/\1/' || true
	grep -o 'id="[A-Za-z0-9_-]*"' "$file" 2>/dev/null | sed 's/id="\(.*\)"/\1/' || true
}

report() {
	printf '%s: %s\n' "$1" "$2" >&2
	failures=$((failures + 1))
}

for file in "${files[@]}"; do
	[ -f "$file" ] || continue
	dir="$(dirname "$file")"

	# Inline links plus reference definitions, one target per line. Fenced code
	# blocks are dropped first so shell examples are not mistaken for links.
	targets="$(
		awk '/^[[:space:]]*```/ {fence = !fence; next} !fence {print}' "$file" |
			{
				grep -o ']([^)]*)' - | sed -e 's/^](//' -e 's/)$//' || true
				grep -o '^\[[^]]*\]:[[:space:]]*[^[:space:]]*' - | sed 's/^\[[^]]*\]:[[:space:]]*//' || true
			}
	)"

	while IFS= read -r target; do
		[ -n "$target" ] || continue
		# Drop an optional link title: [text](path "title")
		target="${target%% *}"
		target="${target#<}"
		target="${target%>}"

		case "$target" in
		http://* | https://* | mailto:* | saga-diff://* | urn:* | data:*)
			external=$((external + 1))
			continue
			;;
		esac

		checked=$((checked + 1))
		path="${target%%#*}"
		anchor=""
		case "$target" in
		*'#'*) anchor="${target#*#}" ;;
		esac

		if [ -z "$path" ]; then
			resolved="$file"
		else
			resolved="$dir/$path"
			resolved="${resolved#./}"
			if [ ! -e "$resolved" ]; then
				report "$file" "missing link target: $target"
				continue
			fi
		fi

		if [ -n "$anchor" ] && [ -f "$resolved" ]; then
			case "$resolved" in
			*.md)
				if ! anchors_of "$resolved" | grep -qxF "$anchor"; then
					report "$file" "missing anchor: $target"
				fi
				;;
			esac
		fi
	done <<<"$targets"
done

printf 'checked %d in-repository links across %d files (%d external links skipped)\n' \
	"$checked" "${#files[@]}" "$external"

if [ "$failures" -gt 0 ]; then
	printf '%d broken link(s)\n' "$failures" >&2
	exit 1
fi
