# Changelog

All notable changes to Change Saga are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
the policy in [docs/releasing.md](docs/releasing.md#versioning-policy).

Each GitHub Release also carries auto-generated notes listing the merged pull
requests. This file is the curated view: what changed for someone *using* the
tool, and what they have to do about it.

## [Unreleased]

Change Saga has not been released yet. The current source is version
`0.2.0-dev`, the v2 format is experimental, and no compatibility promise is in
effect. The first tagged release will replace this section with its own entry.

Until then, the honest summary of what exists:

- A `change-saga` CLI covering `init`, `add-chapter`, `add-section`,
  `add-fragment`, `cover`, `thread`, `reply`, `review`, `validate`, `status`,
  `serve`/`open`, `install-skill`, and `spec`.
- The v2 on-disk format specified in [SPEC.md](SPEC.md) with JSON Schemas in
  [`schema/`](schema).
- A local, loopback-only reviewer with Saga, Code Diff, and Manifest surfaces.
- Release automation for six platform targets with checksums, build provenance
  attestation, and optional macOS signing and notarization.

<!--
Maintainers: when cutting a release, rename this section to

## [X.Y.Z] - YYYY-MM-DD

start a fresh `## [Unreleased]` above it, and add the link definitions at the
bottom. Group entries under Added / Changed / Deprecated / Removed / Fixed /
Security, and mark anything that changes the on-disk format as **Format**.
-->

[Unreleased]: https://github.com/change-saga/change-saga/commits/main
