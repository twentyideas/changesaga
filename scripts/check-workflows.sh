#!/usr/bin/env bash
# Lint GitHub Actions workflows and enforce immutable third-party references.
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

actionlint_bin="${1:-actionlint}"
if ! command -v "$actionlint_bin" >/dev/null 2>&1 && [ ! -x "$actionlint_bin" ]; then
	echo "error: actionlint not found (pass its path as the first argument)" >&2
	exit 1
fi

shopt -s nullglob
workflow_files=(.github/workflows/*.yml .github/workflows/*.yaml)
if [ "${#workflow_files[@]}" -eq 0 ]; then
	echo "error: no workflow files found" >&2
	exit 1
fi

"$actionlint_bin" "${workflow_files[@]}"

status=0
while IFS=: read -r file line declaration; do
	target="${declaration#*uses:}"
	target="${target%%#*}"
	target="$(printf '%s' "$target" | sed -E 's/^[[:space:]]+//; s/[[:space:]]+$//; s/^['\"']|['\"']$//g')"

	case "$target" in
	./*)
		# Repository-local actions and reusable workflows are bound to the
		# checked-out commit and do not have an external ref to pin.
		continue
		;;
	docker://*)
		if [[ ! "$target" =~ @sha256:[0-9a-f]{64}$ ]]; then
			echo "$file:$line: container action must use an immutable sha256 digest: $target" >&2
			status=1
		fi
		;;
	*@*)
		ref="${target##*@}"
		if [[ ! "$ref" =~ ^[0-9a-f]{40}$ ]]; then
			echo "$file:$line: external action must be pinned to a full commit SHA: $target" >&2
			status=1
		fi
		;;
	*)
		echo "$file:$line: malformed action reference: $target" >&2
		status=1
		;;
	esac
done < <(rg --line-number '^[[:space:]]*(-[[:space:]]+)?uses:' "${workflow_files[@]}")

if ! awk '
	function inspect_run() {
		if ($0 ~ /\$\{\{[^}]*github\.event\./) {
			printf "%s:%d: event data must cross into shell through env, never expression interpolation\n", FILENAME, FNR > "/dev/stderr"
			bad = 1
		}
	}
	{
		first = match($0, /[^ ]/)
		indent = first == 0 ? 9999 : first - 1
		if (in_run && $0 !~ /^[ ]*$/ && indent <= run_indent) {
			in_run = 0
		}
		if ($0 ~ /^[ ]*run:[ ]*/) {
			in_run = 1
			run_indent = indent
			inspect_run()
			next
		}
		if (in_run) {
			inspect_run()
		}
	}
	END { exit bad }
' "${workflow_files[@]}"; then
	status=1
fi

exit "$status"
