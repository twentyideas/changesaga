# Releasing

Change Saga ships self-contained command-line executables for macOS, Linux, and
Windows. Everything below is driven by repository workflows and scripts, so
publishing a release is a tag push and nothing else.

| File | Role |
| --- | --- |
| `.github/workflows/ci.yml` | Unsigned tests, lint, workflow-policy checks, and cross-platform build. Runs on every pull request, including forks. |
| `.github/workflows/release.yml` | Tag-push publication or manual rehearsal, optional macOS signing/notarization, checksums, provenance, and the GitHub Release. |
| `scripts/build-release.sh` | Builds and archives one `GOOS/GOARCH` target. Used by both workflows. |
| `scripts/build-macos-standalone-installer.sh` | Wraps an Apple Silicon archive in one self-contained `.command` file for direct handoff. |
| `scripts/install.sh` | The `curl \| sh` installer for macOS and Linux. |
| `scripts/install.ps1` | The `irm \| iex` installer for Windows PowerShell. |
| `scripts/macos-*.sh` | Developer ID keychain setup, signing, notarization. Only ever called from the trusted release job. |
| `scripts/install_test.sh` | End-to-end test of the installer against a locally staged release. |
| `scripts/artifact_test.sh` | Checks a built archive's contents, layout, and permissions. |
| `scripts/install_windows_test.ps1` | End-to-end Windows installer test with locally mocked release downloads. |

## Cutting a release

1. Make sure `main` is green and the working tree is clean.
2. Tag and push:

   ```sh
   git tag -a v0.3.0 -m "v0.3.0"
   git push origin v0.3.0
   ```

3. The `Release` workflow validates the tag, re-runs the full CI matrix, builds
   six artifacts, signs and notarizes the macOS pair when the corresponding
   Apple credentials are configured, generates `SHA256SUMS`, attaches build
   provenance to every published asset, and publishes the GitHub Release.

Tags must be `v<major>.<minor>.<patch>` with an optional prerelease suffix and
must point to a commit on the default branch. A suffix (`v0.3.0-rc.1`) publishes
as a GitHub prerelease. The workflow refuses to overwrite an existing release;
cut a new patch version instead.

Configure a repository tag ruleset for `v*` that restricts creation to release
maintainers and blocks updates and deletion. The workflow also peels the tag to
one commit before any build, checks that commit against the tag-push event, uses
the commit SHA (not the mutable tag name) for every checkout, and re-fetches the
tag immediately before publication. A moved tag therefore fails even if the
repository ruleset is accidentally weakened.

To rehearse without publishing, run the workflow manually from the Actions tab
with an existing tag. Every build and verification step runs, but the privileged
provenance and GitHub Release job is skipped before it receives a runner or
token. A manual run can never be promoted; push the tag to start a separate,
tag-bound publication run.

## Repository configuration

The repository owner must configure these controls before treating releases as
production-ready:

- Keep the default `GITHUB_TOKEN` permission read-only. Repository or
  organization policy must still permit the release workflow's explicit
  `contents: write`, `id-token: write`, and `attestations: write` grants.
- Protect `main` and require the CI and Browser E2E checks before merge.
- Add a tag ruleset for `v*` that restricts tag creation to release maintainers
  and blocks tag updates and deletion.
- Create the `release-signing` environment. Allow the default branch (manual
  rehearsals) and `v*` tags (publication), and add required reviewers if a human
  release gate is desired. Add the six Apple secrets documented below.
- After one signed rehearsal succeeds, set `REQUIRE_MACOS_SIGNING=true`; after
  notarization succeeds, also set `REQUIRE_MACOS_NOTARIZATION=true`. Until
  those variables are enabled, unsigned or signed-but-not-notarized macOS
  releases are intentionally allowed and disclosed in their release notes.
- Enable immutable releases so a published release's tag and assets cannot be
  changed. The workflow already refuses to replace an existing release; this
  repository setting makes that invariant server-side.

GitHub Release assets are the distribution source of truth. The installers
resolve a requested tag (or GitHub's latest stable release), then download the
named archive and `SHA256SUMS` from that release. Workflow-run artifacts are
temporary transport between jobs and are never an installer source.

## Versioning policy

Tags are `v<major>.<minor>.<patch>`, semantic versioning as interpreted below.
`internal/cli.Version` in the source tree is a development placeholder; the
released version comes from the tag via `-ldflags`, so cutting a release never
requires a source bump.

**While the project is `0.y.z`** the format and the CLI are experimental and
there is no compatibility promise. Breaking changes may land in a minor bump.
Each one is called out in [CHANGELOG.md](../CHANGELOG.md) with the migration
step, and anything that changes the on-disk format is marked **Format**.

**From `1.0.0` onward** the contract is:

| Change | Bump |
| --- | --- |
| A saga written by an older version stops loading | major |
| A CLI flag or exit code is removed or changes meaning | major |
| A `schema/` document rejects what it used to accept | major |
| New command, flag, fragment type, or optional field | minor |
| A saga written by the new version loads on the previous minor | minor |
| Bug fix, renderer change, docs, dependency update | patch |

Exit codes are part of the contract: `0` success, `1` error (including schema
errors from `validate`), `2` usage error, and `3` incomplete coverage from
`status`. The structured `query` boundary additionally uses `3` for an invalid
saga, `4` for stale snapshots or conflicts, `5` for missing resources, `6` for
unsafe paths, unsupported media, or oversized content, and `7` when the source
comparison is unavailable; its JSON error code is the authoritative detail.

Prerelease suffixes (`v1.0.0-rc.1`) publish as GitHub prereleases with
`--latest=false`, so they can never replace the stable release used by the
installers. Stable releases leave latest selection to GitHub's automatic
semantic-version ordering, so publishing an older stable tag does not demote a
newer one.

**Release checklist**

1. `main` is green and the working tree is clean.
2. [CHANGELOG.md](../CHANGELOG.md) has an entry for the version, dated, with the
   `[Unreleased]` section reset above it.
3. Any format change carries its [SPEC.md](../SPEC.md) and `schema/` updates in
   the same history.
4. Tag and push, as above.

Security fixes ship in the next release from `main`. Older tags are not
backported; see [SECURITY.md](../SECURITY.md).

## Artifacts

```text
change-saga_<version>_darwin_arm64.tar.gz
change-saga_<version>_darwin_amd64.tar.gz
change-saga_<version>_linux_amd64.tar.gz
change-saga_<version>_linux_arm64.tar.gz
change-saga_<version>_windows_amd64.zip
change-saga_<version>_windows_arm64.zip
SHA256SUMS
```

Each archive is flat and contains only the platform binary, `LICENSE`, and
`README.md`; extract it into an empty directory when unpacking by hand. The
binary is mode `0755` and both documents are `0644`, regardless of the builder's
umask. Builds use `CGO_ENABLED=0` and `-trimpath`, so users do not need a
separately installed Go runtime or third-party dynamic library and absolute
source paths are omitted. The executables still use platform system libraries
where the operating system requires them. `Version`, `Commit`, and `BuildDate`
in `internal/cli` are injected with `-ldflags -X`;
`change-saga version` prints all three. `Commit` is the first 12 characters of
the tagged revision's object ID, and `BuildDate` is UTC.

For a direct Apple Silicon handoff, wrap the archive in a single installer:

```sh
./scripts/build-macos-standalone-installer.sh \
  dist/change-saga_0.3.0_darwin_arm64.tar.gz \
  dist/Change-Saga.command
```

The resulting `.command` file contains the archive and its expected checksum.
It verifies the payload, requires exactly the documented regular archive
members, checks the embedded binary's version, refuses non-macOS and non-arm64
machines, and installs atomically without `sudo`. It installs the `change-saga`
command. Zip the one file when sending it through a service that does not
preserve executable permissions.

`SHA256SUMS` is generated in an unprivileged preparation job from the artifacts
as downloaded, after each one is re-checked against the checksum its build job
recorded. That catches corruption between the build and the release, not just at
build time. Only a tag-push run starts the separate job with `contents`,
`id-token`, and `attestations` write permissions; it attests the six archives and
`SHA256SUMS` before publishing them.

Users can verify a download two ways:

```sh
sha256sum -c SHA256SUMS --ignore-missing
gh attestation verify change-saga_0.3.0_linux_amd64.tar.gz --repo change-saga/change-saga
```

The second command checks the Sigstore build provenance attestation, which ties
the artifact to this repository, this workflow, and the commit it was built
from.

## The installer

macOS and Linux:

```sh
curl -fsSL https://raw.githubusercontent.com/change-saga/change-saga/main/scripts/install.sh | sh
curl -fsSL .../install.sh | sh -s -- --version v0.3.0 --dir ~/bin
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/change-saga/change-saga/main/scripts/install.ps1 | iex
```

The PowerShell installer downloads `change-saga_<version>_windows_<arch>.zip`,
verifies it against `SHA256SUMS`, installs to the current user's local
application directory, and adds that directory to the user `PATH`. It never
requests elevation or changes execution policy. CI exercises the complete
download, checksum, archive-layout validation, atomic installation,
latest-release, dry-run, and tamper-rejection paths on Windows.

What the macOS/Linux shell installer does, and what it refuses to do:

- Detects `darwin`/`linux` and `amd64`/`arm64`, and prefers the native arm64
  build when the shell is running translated under Rosetta 2.
- Downloads over HTTPS only, with TLS 1.2 or newer, and follows redirects only
  to HTTPS.
- Verifies the archive against the release `SHA256SUMS` and aborts on mismatch.
  If no checksum tool is available it aborts rather than installing unverified —
  it never degrades to a weaker check.
- Requires exactly one well-formed checksum entry for the selected GitHub
  Release asset, rejects unexpected or non-regular archive members before
  extraction, and confirms that the binary reports the requested release
  version before installing it.
- Inspects the macOS code signature when `codesign` is available. It reports a
  Developer ID authority when present and warns when the release is unsigned or
  carries only an ad-hoc signature.
- Never removes `com.apple.quarantine`, never runs `spctl --master-disable`, and
  never touches SIP or any other platform protection. A binary that Gatekeeper
  rejects is a bug to fix in the release, not something for an installer to
  work around.
- Never calls `sudo`. It installs to `~/.local/bin` (or `/usr/local/bin` when
  that is already writable by the current user) and tells you what to do if the
  target is not writable.
- Installs atomically: the binary is copied into the destination directory under
  a temporary name and moved into place, so an in-flight `change-saga` process is never
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
   environment's protection rules run before the job starts. Its allowed refs
   are the default branch (for manual rehearsals) and `v*` tags (for releases),
   so dispatching a modified workflow from another branch cannot read them.
3. The signing keychain is created fresh in `$RUNNER_TEMP` with a random
   password, and is deleted in an `if: always()` step.

Signing is optional until configured, and notarization is an additional optional
layer. With no Apple secrets present, the release still builds and publishes,
the macOS artifacts are unsigned, and the release notes say so. A complete
Developer ID credential set produces signed artifacts; adding the complete App
Store Connect set notarizes them too. Any partial set fails closed, and
notarization is refused without signing. Once setup is complete, the enforcement
variables below prevent a release from quietly dropping either guarantee.

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
| `REQUIRE_MACOS_NOTARIZATION` | `true` makes a release fail when notarization credentials are missing. Enable after the App Store Connect setup is verified. |

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
   restrict its deployment branches and tags to the default branch plus tags
   matching `v*`. That permits default-branch manual rehearsals and tag releases
   without exposing secrets to workflows dispatched from feature branches. Add
   required reviewers if release approval should be a human step.
6. **Verify** by dispatching the release workflow manually against an existing
   tag. The job prints the signing authority and the Gatekeeper assessment
   without granting publish or provenance permissions.
7. **Turn on enforcement:** set `REQUIRE_MACOS_SIGNING=true` and
   `REQUIRE_MACOS_NOTARIZATION=true`.

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
`CHANGE_SAGA_SIGN_HOOK`, in a job that names the same protected environment.

Linux artifacts rely on `SHA256SUMS` plus the provenance attestation.

## Local rehearsal

```sh
./scripts/build-release.sh 0.3.0 darwin arm64 dist   # one target
./scripts/build-macos-standalone-installer.sh dist/change-saga_0.3.0_darwin_arm64.tar.gz
./scripts/install_test.sh                            # installer, end to end
shellcheck scripts/*.sh
./scripts/check-workflows.sh                         # workflow syntax + immutable action refs
```

`build-release.sh` requires the exact Go patch release in `.go-version`, honours
`SOURCE_DATE_EPOCH` for the build timestamp, and uses `CHANGE_SAGA_COMMIT` to
bind explicit release provenance to a clean checkout. Unsigned builds with the
same source, toolchain, target, and epoch are byte-for-byte reproducible.
`CHANGE_SAGA_SIGN_HOOK` adds platform signing; signatures and Apple's
notarization service are intentionally outside that reproducibility claim.
