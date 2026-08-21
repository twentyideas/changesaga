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
output="${2:-dist/Change-Saga.command}"
archive_name="$(basename "$archive")"

numeric='(0|[1-9][0-9]*)'
prerelease_id="($numeric|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)"
semver="$numeric\\.$numeric\\.$numeric(-$prerelease_id(\\.$prerelease_id)*)?(\\+[0-9A-Za-z-]+(\\.[0-9A-Za-z-]+)*)?"
if [[ ! "$archive_name" =~ ^change-saga_${semver}_darwin_arm64\.tar\.gz$ ]]; then
	echo "error: expected a darwin/arm64 release archive, got $archive" >&2
	exit 2
fi
version="${archive_name#change-saga_}"
version="${version%_darwin_arm64.tar.gz}"

if [ ! -f "$archive" ]; then
	echo "error: archive not found: $archive" >&2
	exit 2
fi

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
archive_abs="$(cd "$(dirname "$archive")" && pwd)/$(basename "$archive")"
output_dir="$(dirname "$output")"
mkdir -p "$output_dir"
output_abs="$(cd "$output_dir" && pwd)/$(basename "$output")"

if [ "$archive_abs" = "$output_abs" ] || { [ -e "$output_abs" ] && [ "$archive_abs" -ef "$output_abs" ]; }; then
	echo "error: output must not overwrite the input archive" >&2
	exit 2
fi
if [ -d "$output_abs" ]; then
	echo "error: output is a directory: $output_abs" >&2
	exit 2
fi

build_work="$(mktemp -d)"
output_temp=""
trap 'rm -rf "$build_work"; [ -z "$output_temp" ] || rm -f "$output_temp"' EXIT
payload="$build_work/$archive_name"
cp "$archive_abs" "$payload"

# Reject surprising members before embedding the payload. The generated
# installer repeats this check before extracting only the binary.
tar -tzf "$payload" >"$build_work/members" || {
	echo "error: could not read release archive: $archive" >&2
	exit 2
}
tar -tvzf "$payload" >"$build_work/listing" || {
	echo "error: could not inspect release archive: $archive" >&2
	exit 2
}
if [ "$(wc -l <"$build_work/members" | tr -d ' ')" != 3 ]; then
	echo "error: release archive must contain exactly change-saga, LICENSE, and README.md" >&2
	exit 2
fi
for member in change-saga LICENSE README.md; do
	if [ "$(grep -Fxc "$member" "$build_work/members" || true)" != 1 ]; then
		echo "error: release archive has an invalid or duplicate $member entry" >&2
		exit 2
	fi
done
if ! awk 'substr($1, 1, 1) != "-" { exit 1 } END { if (NR != 3) exit 1 }' "$build_work/listing"; then
	echo "error: release archive members must all be regular files" >&2
	exit 2
fi

archive_sha="$("$repo_root/scripts/sha256.sh" "$payload" | awk '{print $1}')"
output_temp="$(mktemp "$output_dir/.change-saga-command.XXXXXX")"

cat >"$output_temp" <<INSTALLER
#!/bin/sh
# Self-contained Change Saga installer for Apple Silicon macOS.
set -eu

saga_archive_name='$archive_name'
saga_expected_sha='$archive_sha'
saga_expected_version='$version'

saga_fail() {
	printf 'Change Saga installation failed: %s\n' "\$*" >&2
	exit 1
}

[ "\$(uname -s)" = Darwin ] || saga_fail 'this installer requires macOS'
[ "\$(uname -m)" = arm64 ] || saga_fail 'this build requires an Apple Silicon Mac'

command -v base64 >/dev/null 2>&1 || saga_fail 'base64 is unavailable'
command -v shasum >/dev/null 2>&1 || saga_fail 'shasum is unavailable'
command -v tar >/dev/null 2>&1 || saga_fail 'tar is unavailable'
command -v chmod >/dev/null 2>&1 || saga_fail 'chmod is unavailable'
command -v mv >/dev/null 2>&1 || saga_fail 'mv is unavailable'

saga_work_dir="\$(mktemp -d -t change-saga-install)" || saga_fail 'could not create temporary directory'
saga_install_temp=''
saga_cleanup() {
	rm -rf "\$saga_work_dir"
	[ -z "\$saga_install_temp" ] || rm -f "\$saga_install_temp"
}
trap saga_cleanup EXIT INT TERM HUP

saga_payload_line="\$(awk '/^__CHANGE_SAGA_PAYLOAD_BELOW__\$/{print NR + 1; exit}' "\$0")"
[ -n "\$saga_payload_line" ] || saga_fail 'embedded payload marker is missing'

tail -n "+\$saga_payload_line" "\$0" | base64 -D >"\$saga_work_dir/\$saga_archive_name" ||
	saga_fail 'could not decode the embedded archive'

saga_actual_sha="\$(shasum -a 256 "\$saga_work_dir/\$saga_archive_name" | awk '{print \$1}')"
[ "\$saga_actual_sha" = "\$saga_expected_sha" ] || saga_fail 'embedded archive checksum mismatch'

tar -tzf "\$saga_work_dir/\$saga_archive_name" >"\$saga_work_dir/members" ||
	saga_fail 'could not inspect the embedded archive'
tar -tvzf "\$saga_work_dir/\$saga_archive_name" >"\$saga_work_dir/listing" ||
	saga_fail 'could not inspect embedded archive types'
[ "\$(wc -l <"\$saga_work_dir/members" | tr -d ' ')" = 3 ] ||
	saga_fail 'archive must contain exactly three files'
for saga_member in change-saga LICENSE README.md; do
	[ "\$(grep -Fxc "\$saga_member" "\$saga_work_dir/members" || true)" = 1 ] ||
		saga_fail "archive has an invalid or duplicate \$saga_member entry"
done
awk 'substr(\$1, 1, 1) != "-" { exit 1 } END { if (NR != 3) exit 1 }' \
	"\$saga_work_dir/listing" || saga_fail 'archive members must all be regular files'

# Extract only the validated binary. Even if a platform tar implementation has
# unusual link handling, no archive path is allowed to choose an output path.
tar -xOzf "\$saga_work_dir/\$saga_archive_name" change-saga >"\$saga_work_dir/change-saga" ||
	saga_fail 'could not extract the change-saga binary'
chmod 0755 "\$saga_work_dir/change-saga" || saga_fail 'could not make the staged binary executable'
saga_actual_version="\$("\$saga_work_dir/change-saga" version 2>/dev/null || true)"
[ "\${saga_actual_version%% *}" = "\$saga_expected_version" ] ||
	saga_fail 'archive binary version does not match the release filename'

if [ -n "\${CHANGE_SAGA_INSTALL_DIR:-}" ]; then
	saga_install_dir="\$CHANGE_SAGA_INSTALL_DIR"
elif [ -d /usr/local/bin ] && [ -w /usr/local/bin ]; then
	saga_install_dir=/usr/local/bin
else
	saga_install_dir="\$HOME/.local/bin"
fi

mkdir -p "\$saga_install_dir" || saga_fail "could not create \$saga_install_dir"
[ ! -L "\$saga_install_dir" ] && [ -d "\$saga_install_dir" ] ||
	saga_fail "install directory must be a real directory, not a symlink"
saga_install_temp="\$(mktemp "\$saga_install_dir/.change-saga.XXXXXX")" ||
	saga_fail "could not stage the binary in \$saga_install_dir"
cp "\$saga_work_dir/change-saga" "\$saga_install_temp" ||
	saga_fail "could not stage the binary in \$saga_install_dir"
chmod 0755 "\$saga_install_temp" || saga_fail 'could not set installed permissions'
mv -f "\$saga_install_temp" "\$saga_install_dir/change-saga" ||
	saga_fail "could not install to \$saga_install_dir/change-saga"
saga_install_temp=''

printf '\nChange Saga installed successfully.\n'
"\$saga_install_dir/change-saga" version
printf 'Command: %s\n' "\$saga_install_dir/change-saga"

case ":\$PATH:" in
*":\$saga_install_dir:"*) ;;
*)
	printf '\n%s is not currently on PATH. Add this line to your shell profile:\n' "\$saga_install_dir"
	printf '  export PATH="%s:\$PATH"\n' "\$saga_install_dir"
	;;
esac

exit 0
__CHANGE_SAGA_PAYLOAD_BELOW__
INSTALLER

base64 <"$payload" >>"$output_temp"
chmod 0755 "$output_temp"
mv -f "$output_temp" "$output_abs"
output_temp=""

echo "wrote $output_abs"
ls -lh "$output_abs"
