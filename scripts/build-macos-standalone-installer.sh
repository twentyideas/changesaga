#!/usr/bin/env bash
# Wrap one macOS release archive in a self-contained, double-clickable installer.
#
# Usage: scripts/build-macos-standalone-installer.sh <archive> [output]
#
# The resulting .command file embeds the release archive, verifies it before
# installation, and installs saga without sudo. It is intended for direct
# handoff builds; tagged GitHub releases should continue to use install.sh.
set -euo pipefail

if [ "$#" -lt 1 ] || [ "$#" -gt 2 ]; then
	echo "usage: $0 <darwin-release-archive> [output.command]" >&2
	exit 2
fi

archive="$1"
output="${2:-dist/Review-Saga.command}"

case "$(basename "$archive")" in
*_darwin_arm64.tar.gz) ;;
*)
	echo "error: expected a darwin/arm64 release archive, got $archive" >&2
	exit 2
	;;
esac

if [ ! -f "$archive" ]; then
	echo "error: archive not found: $archive" >&2
	exit 2
fi

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
archive_abs="$(cd "$(dirname "$archive")" && pwd)/$(basename "$archive")"
output_dir="$(dirname "$output")"
mkdir -p "$output_dir"
output_abs="$(cd "$output_dir" && pwd)/$(basename "$output")"

archive_sha="$("$repo_root/scripts/sha256.sh" "$archive_abs" | awk '{print $1}')"
archive_name="$(basename "$archive_abs")"

cat >"$output_abs" <<INSTALLER
#!/bin/sh
# Self-contained Review Saga installer for Apple Silicon macOS.
set -eu

saga_archive_name='$archive_name'
saga_expected_sha='$archive_sha'

saga_fail() {
	printf 'Review Saga installation failed: %s\n' "\$*" >&2
	exit 1
}

[ "\$(uname -s)" = Darwin ] || saga_fail 'this installer requires macOS'
[ "\$(uname -m)" = arm64 ] || saga_fail 'this build requires an Apple Silicon Mac'

command -v base64 >/dev/null 2>&1 || saga_fail 'base64 is unavailable'
command -v shasum >/dev/null 2>&1 || saga_fail 'shasum is unavailable'
command -v tar >/dev/null 2>&1 || saga_fail 'tar is unavailable'
command -v install >/dev/null 2>&1 || saga_fail 'install is unavailable'

saga_work_dir="\$(mktemp -d -t review-saga-install)" || saga_fail 'could not create temporary directory'
saga_cleanup() { rm -rf "\$saga_work_dir"; }
trap saga_cleanup EXIT INT TERM HUP

saga_payload_line="\$(awk '/^__REVIEW_SAGA_PAYLOAD_BELOW__\$/{print NR + 1; exit}' "\$0")"
[ -n "\$saga_payload_line" ] || saga_fail 'embedded payload marker is missing'

tail -n "+\$saga_payload_line" "\$0" | base64 -D >"\$saga_work_dir/\$saga_archive_name" ||
	saga_fail 'could not decode the embedded archive'

saga_actual_sha="\$(shasum -a 256 "\$saga_work_dir/\$saga_archive_name" | awk '{print \$1}')"
[ "\$saga_actual_sha" = "\$saga_expected_sha" ] || saga_fail 'embedded archive checksum mismatch'

tar -xzf "\$saga_work_dir/\$saga_archive_name" -C "\$saga_work_dir" ||
	saga_fail 'could not unpack the embedded archive'
[ -f "\$saga_work_dir/review-saga" ] || saga_fail 'archive does not contain the review-saga binary'

if [ -n "\${SAGA_INSTALL_DIR:-}" ]; then
	saga_install_dir="\$SAGA_INSTALL_DIR"
elif [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
	saga_install_dir=/usr/local/bin
else
	saga_install_dir="\$HOME/.local/bin"
fi

mkdir -p "\$saga_install_dir" || saga_fail "could not create \$saga_install_dir"
install -m 0755 "\$saga_work_dir/review-saga" "\$saga_install_dir/review-saga" ||
	saga_fail "could not install to \$saga_install_dir/review-saga"

printf '\nReview Saga installed successfully.\n'
"\$saga_install_dir/review-saga" version
printf 'Command: %s\n' "\$saga_install_dir/review-saga"

case ":\$PATH:" in
*":\$saga_install_dir:"*) ;;
*)
	printf '\n%s is not currently on PATH. Add this line to your shell profile:\n' "\$saga_install_dir"
	printf '  export PATH="%s:\$PATH"\n' "\$saga_install_dir"
	;;
esac

exit 0
__REVIEW_SAGA_PAYLOAD_BELOW__
INSTALLER

base64 <"$archive_abs" >>"$output_abs"
chmod 0755 "$output_abs"

echo "wrote $output_abs"
ls -lh "$output_abs"
