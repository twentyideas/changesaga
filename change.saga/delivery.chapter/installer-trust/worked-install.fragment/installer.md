# Worked install: success and fail-closed paths {#worked-install}

```text
$ install.sh --version v0.0.6
resolve linux/amd64
fetch archive + SHA256SUMS
verify sha256
inspect exactly: change-saga, LICENSE, README.md
install temporary name -> ~/.local/bin/change-saga

$ install.sh --version v0.0.6   # tampered archive
checksum mismatch
exit non-zero; existing binary remains in place
```

The POSIX installer requires HTTPS, refuses to continue without a SHA-256 tool or on a mismatch, validates archive members before extracting only the binary, and replaces the destination through a temporary sibling without invoking `sudo`.[^posix-installer]

The PowerShell path applies the same checksum and archive-shape gates for a per-user executable and does not request elevation or weaken execution policy.[^windows-installer]

macOS signing is additive evidence, not a substitute for checksum verification: available credentials route the staged binary through signing and optional notarization before archiving, while release notes disclose unsigned or signed-but-not-notarized output.[^mac-signing]

[^posix-installer]: The POSIX installer validates transport inputs, checksum, archive topology, and destination replacement before exposing a new executable.
[^windows-installer]: The Windows installer verifies SHA256SUMS and rejects unsafe ZIP entries before installing in the current user's path.
[^mac-signing]: macOS credentials are confined to the protected release job, and release metadata reports whether signing and notarization actually occurred.
