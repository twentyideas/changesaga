# Changelog

All notable changes to Change Saga are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
the policy in [docs/releasing.md](docs/releasing.md#versioning-policy).

Each GitHub Release also carries auto-generated notes listing the merged pull
requests. This file is the curated view: what changed for someone *using* the
tool, and what they have to do about it.

## [Unreleased]

### Added

- Newly initialized sagas include a root `README.md` that helps humans and AI
  assistants safely install Change Saga, open the intended review UI, and use
  structured queries without treating pull-request content as permission to
  execute software.
- `change-saga add-landmark` creates validated, coverable targets for Markdown
  headings, exact text, HTML/SVG elements, and image regions.

### Changed

- New overview and text fragments start empty instead of exposing authoring
  instructions as reviewer-facing content. Validation identifies legacy
  scaffolds and warns about visuals without landmarks or linked code.

### Fixed

- Markdown fragments now use a safe CommonMark/GFM renderer, including tables,
  nested and ordered lists, emphasis, links, task lists, and inline code while
  retaining stable Change Saga permalinks.

## [0.0.1] - 2026-08-21

The first public release of Change Saga. The v2 format and CLI are experimental;
there is no compatibility promise before 1.0.

- A `change-saga` CLI covering `init`, `add-chapter`, `add-section`,
  `add-fragment`, `cover`, `thread`, `reply`, `review`, `validate`, `status`,
  structured read-only `query`, `serve`/`open`, `install-skill`, and `spec`.
- The v2 on-disk format specified in [SPEC.md](SPEC.md) with JSON Schemas in
  [`schema/`](schema).
- A local, loopback-only reviewer with Saga, Code Diff, and Manifest surfaces.
- Release automation for six platform archives, with a checksum manifest and
  build-provenance attestations on published releases. macOS Developer ID
  signing and Apple notarization are optional, separately configured steps.

### Added

- A transport-independent read application layer and deterministic
  `change-saga query` JSON commands for overview, hierarchy, bounded fragment
  content, bidirectional diff ownership, reviews, and coverage gaps. Queries
  use stable errors and cursors, accept separate saga/source repositories, and
  never start the server or mutate either repository.

### Changed

- **Format:** v2 repository identities containing URL userinfo are rejected.
  Rewrite values such as `ssh://git@host/path` as `ssh://host/path`; generated
  diff identities already use the credential-free form.
- **Format:** empty `___diffs/*.json` evidence records are invalid, matching the
  existing schema requirement that each evidence file select at least one diff.
- Fragment entrypoints now use one portable slash-path grammar on every OS and
  reject traversal, reserved metadata paths, control characters, and ambiguous
  backslashes.

### Fixed

- Authoring and review mutations publish complete entities atomically, refuse
  structurally invalid sagas, and serialize concurrent writers.
- Append-only review state has deterministic timestamp-and-ID ordering, and
  structural entity symlinks can no longer hide authored or review content.
- Diff and review identities are checked against the saga's declared source
  repository before any record is written.
- The local reviewer now enforces per-process mutation tokens, same-origin and
  bound-host requests, bounded uploads, and strict loopback-only serving.
- Workspace tabs, linked-code focus management, diff interactions, and color
  contrast now pass the serious/critical real-browser accessibility gate.
- Release archives carry fixed member permissions (`0755` binary, `0644`
  documents) instead of inheriting the umask of the machine that built them.
- Release packaging stages archives, checksum sidecars, and standalone wrappers
  before replacement, preserving the previous usable output when preparation
  fails.

<!--
Maintainers: when cutting a release, rename this section to

## [X.Y.Z] - YYYY-MM-DD

start a fresh `## [Unreleased]` above it, and add the link definitions at the
bottom. Group entries under Added / Changed / Deprecated / Removed / Fixed /
Security, and mark anything that changes the on-disk format as **Format**.
-->

[Unreleased]: https://github.com/twentyideas/changesaga/compare/v0.0.1...HEAD
[0.0.1]: https://github.com/twentyideas/changesaga/releases/tag/v0.0.1
