# Releasing

Review Saga ships as a single static binary for macOS, Linux, and Windows.
Everything below is driven by two workflows and four scripts, so a release is a
tag push and nothing else.

| File | Role |
| --- | --- |
| `.github/workflows/ci.yml` | Unsigned tests, lint, and cross-platform build. Runs on every pull request, including forks. |
| `.github/workflows/release.yml` | Tag-only build, macOS signing and notarization, checksums, GitHub Release. |
| `scripts/build-release.sh` | Builds and archives one `GOOS/GOARCH` target. Used by both workflows. |
| `scripts/build-macos-standalone-installer.sh` | Wraps an Apple Silicon archive in one self-contained `.command` file for direct handoff. |
| `scripts/install.sh` | The `curl \| sh` installer for macOS and Linux. |
| `scripts/macos-*.sh` | Developer ID keychain setup, signing, notarization. Only ever called from the trusted release job. |
| `scripts/install_test.sh` | End-to-end test of the installer against a locally staged release. |

## Cutting a release

1. Make sure `main` is green and the working tree is clean.
2. Tag and push:

   ```sh
   git tag -a v0.3.0 -m "v0.3.0"
   git push origin v0.3.0
   ```

3. The `Release` workflow validates the tag, re-runs the full CI matrix, builds
   six artifacts, signs and notarizes the macOS pair when Apple credentials are
   configured, generates `SHA256SUMS`, attaches a build provenance attestation,
   and publishes the GitHub Release.

Tags must be `v<major>.<minor>.<patch>` with an optional prerelease suffix. A
suffix (`v0.3.0-rc.1`) publishes as a GitHub prerelease. The workflow refuses to
overwrite an existing release; cut a new patch version instead.

To rehearse without publishing, run the workflow manually from the Actions tab
with an existing tag and `publish` left off. Every build and verification step
runs; only the release creation is skipped.

## Artifacts

```text
saga_<version>_darwin_arm64.tar.gz
saga_<version>_darwin_amd64.tar.gz
saga_<version>_linux_amd64.tar.gz
saga_<version>_linux_arm64.tar.gz
saga_<version>_windows_amd64.zip
saga_<version>_windows_arm64.zip
SHA256SUMS
```

Each archive contains the binary, `LICENSE`, and `README.md`. Builds are
`CGO_ENABLED=0 -trimpath`, so the binary has no libc or toolchain dependency and
build paths do not leak into it. `internal/cli.Version`, `.Commit`, and
`.BuildDate` are injected with `-ldflags -X`; `review-saga version` prints all three.

For a direct Apple Silicon handoff, wrap the archive in a single installer:

```sh
./scripts/build-macos-standalone-installer.sh \
  dist/saga_0.3.0_darwin_arm64.tar.gz \
  dist/Review-Saga.command
```

The resulting `.command` file contains the archive and its expected checksum.
It verifies the payload, refuses non-macOS and non-arm64 machines, and installs
without `sudo`. It installs the `review-saga` command. Zip the one file when
sending it through a service that does not preserve executable permissions.

`SHA256SUMS` is generated in the publish job from the artifacts as downloaded,
after each one is re-checked against the checksum its build job recorded. That
catches corruption between the build and the release, not just at build time.

Users can verify a download two ways:

```sh
sha256sum -c SHA256SUMS --ignore-missing
gh attestation verify saga_0.3.0_linux_amd64.tar.gz --repo review-saga/review-saga
```

The second command checks the Sigstore build provenance attestation, which ties
the artifact to this repository, this workflow, and the commit it was built
from.

## The installer

```sh
curl -fsSL https://raw.githubusercontent.com/review-saga/review-saga/main/scripts/install.sh | sh
curl -fsSL .../install.sh | sh -s -- --version v0.3.0 --dir ~/bin
```

What it does, and what it refuses to do:

- Detects `darwin`/`linux` and `amd64`/`arm64`, and prefers the native arm64
  build when the shell is running translated under Rosetta 2.
- Downloads over HTTPS only, with TLS 1.2 or newer, and follows redirects only
  to HTTPS.
- Verifies the archive against the release `SHA256SUMS` and aborts on mismatch.
  If no checksum tool is available it aborts rather than installing unverified —
  it never degrades to a weaker check.
- Verifies the macOS code signature when `codesign` is available, and reports
  the signing authority.
- Never removes `com.apple.quarantine`, never runs `spctl --master-disable`, and
  never touches SIP or any other platform protection. A binary that Gatekeeper
  rejects is a bug to fix in the release, not something for an installer to
  work around.
- Never calls `sudo`. It installs to `~/.local/bin` (or `/usr/local/bin` when
  that is already writable by the current user) and tells you what to do if the
  target is not writable.
- Installs atomically: the binary is copied into the destination directory under
  a temporary name and moved into place, so an in-flight `review-saga` process is never
  overwritten.

`scripts/install_test.sh` exercises all of this locally by staging a real
release into a temporary directory and putting a stub `curl` on `PATH`. The
installer under test is unmodified. CI runs it on Linux and macOS.

## macOS signing and notarization

The macOS build job is the only job that can read Apple secrets. Three things
enforce that:

1. `release.yml` triggers on tag pushes and manual dispatch only. It has no
   `pull_request` trigger, and deliberately no `pull_request_target` trigger.
   A pull request from a fork runs `ci.yml`, which is `permissions: contents:
   read`, references no environment, and reads no secret. GitHub does not
   expose secrets to fork pull requests in the first place; the split keeps
   that true even if the workflow triggers are changed later.
2. The signing secrets live in the `release-signing` GitHub Environment. Only a
   job that names `environment: release-signing` can read them, and the
   environment's protection rules run before the job starts.
3. The signing keychain is created fresh in `$RUNNER_TEMP` with a random
   password, and is deleted in an `if: always()` step.

Signing is optional until it is configured. With no Apple secrets present, the
release still builds and publishes, the macOS artifacts are unsigned, and the
release notes say so. Once the Apple setup below is complete, set the repository
variable `REQUIRE_MACOS_SIGNING` to `true` and a release that cannot sign will
fail instead of quietly shipping unsigned binaries.

### Secrets

Store all of these in the `release-signing` environment
(Settings → Environments → release-signing → Environment secrets).

| Secret | Contents |
| --- | --- |
| `MACOS_CERTIFICATE_P12_BASE64` | `base64` of the exported *Developer ID Application* certificate and private key as a `.p12` |
| `MACOS_CERTIFICATE_PASSWORD` | The password set when exporting that `.p12` |
| `MACOS_SIGNING_IDENTITY` | The identity string, e.g. `Developer ID Application: Acme Inc (A1B2C3D4E5)` |
| `APPLE_API_KEY_ID` | App Store Connect API key id, e.g. `A1B2C3D4E5` |
| `APPLE_API_ISSUER_ID` | App Store Connect issuer id (a UUID) |
| `APPLE_API_KEY_P8_BASE64` | `base64` of the downloaded `AuthKey_<key-id>.p8` |

Repository variable (Settings → Secrets and variables → Actions → Variables):

| Variable | Effect |
| --- | --- |
| `REQUIRE_MACOS_SIGNING` | `true` makes a release fail when signing credentials are missing. Leave unset until Apple setup is finished. |

No keychain password secret is needed: the job generates one with
`openssl rand` and throws the keychain away afterwards.

### Apple account setup

This part cannot be automated from CI; it needs a human with an Apple Developer
account (individual or organization, $99/year).

1. **Create the Developer ID Application certificate.** In Xcode
   (Settings → Accounts → Manage Certificates → + → Developer ID Application),
   or at developer.apple.com by uploading a CSR from Keychain Access. Only the
   Account Holder can create Developer ID certificates for an organization.
2. **Export it with its private key.** Keychain Access → My Certificates →
   select the *Developer ID Application* entry → right click → Export → `.p12`,
   with a strong password. Then:

   ```sh
   base64 -i DeveloperID.p12 | pbcopy   # -> MACOS_CERTIFICATE_P12_BASE64
   ```

3. **Record the exact identity string:**

   ```sh
   security find-identity -v -p codesigning
   # "Developer ID Application: Acme Inc (A1B2C3D4E5)"  -> MACOS_SIGNING_IDENTITY
   ```

4. **Create an App Store Connect API key** at
   appstoreconnect.apple.com → Users and Access → Integrations → App Store
   Connect API, with the **Developer** role, which is sufficient for
   notarization. Download `AuthKey_<key-id>.p8` — it can only be downloaded
   once. Record the key id and the issuer id shown on that page.

   ```sh
   base64 -i AuthKey_A1B2C3D4E5.p8 | pbcopy   # -> APPLE_API_KEY_P8_BASE64
   ```

   An Apple ID with an app-specific password also works with `notarytool`, but
   an API key is preferred: it is scoped, revocable, and not tied to a person.

5. **Create the `release-signing` environment**, add the six secrets, and
   restrict its deployment branches and tags to tags matching `v*` so no other
   ref can start a job that reads them. Add required reviewers if release
   approval should be a human step.
6. **Verify** by dispatching the release workflow manually against an existing
   tag with `publish` off. The job prints the signing authority and the
   Gatekeeper assessment without publishing anything.
7. **Turn on enforcement:** set `REQUIRE_MACOS_SIGNING=true`.

### What notarization does and does not do here

`scripts/macos-notarize.sh` zips the signed binary purely as a submission
container — `notarytool` accepts `.zip`, `.dmg`, and `.pkg`, not `.tar.gz` — and
waits for Apple's verdict. The published artifact remains the `.tar.gz`.

The ticket **cannot be stapled** to a bare executable; `stapler` only writes
tickets into app bundles, disk images, and installer packages. Gatekeeper
resolves the ticket online instead. In practice:

- Installed via `install.sh`, the binary runs regardless: a file fetched by
  `curl` never gets a quarantine attribute, so Gatekeeper is not consulted.
- Downloaded with a browser, the archive is quarantined. A signed and notarized
  binary passes Gatekeeper's online check; an unsigned one is blocked, which is
  the correct outcome.
- Offline first-launch of a quarantined, notarized binary can still be blocked
  because the ticket cannot be fetched. Shipping a signed, notarized, and
  stapled `.pkg` is the fix if that becomes a real complaint; it is deliberately
  not built today, since it adds an installer format that most CLI users do not
  want.

Rotate the certificate before it expires (five years) and the API key on
whatever cadence policy requires. Already-notarized releases keep working after
the certificate expires, because the notarization timestamp is what is checked.

## Windows and Linux signing

Windows artifacts are unsigned. Authenticode signing needs an OV or EV code
signing certificate; SmartScreen reputation also takes time to build. When that
is worth doing, it slots into `build-release.sh` the same way macOS does, via
`SAGA_SIGN_HOOK`, in a job that names the same protected environment.

Linux artifacts rely on `SHA256SUMS` plus the provenance attestation.

## Local rehearsal

```sh
./scripts/build-release.sh 0.3.0 darwin arm64 dist   # one target
./scripts/build-macos-standalone-installer.sh dist/saga_0.3.0_darwin_arm64.tar.gz
./scripts/install_test.sh                            # installer, end to end
shellcheck scripts/*.sh
actionlint                                           # workflow syntax
```

`build-release.sh` honours `SOURCE_DATE_EPOCH` for the build timestamp and
`SAGA_SIGN_HOOK` for signing, so a local rehearsal produces artifacts byte-close
to CI's.
