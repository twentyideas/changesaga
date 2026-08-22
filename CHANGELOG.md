# Changelog

All notable changes to Change Saga are recorded here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow
the policy in [docs/releasing.md](docs/releasing.md#versioning-policy).

Each GitHub Release also carries auto-generated notes listing the merged pull
requests. This file is the curated view: what changed for someone *using* the
tool, and what they have to do about it.

## [Unreleased]

### Changed

- Query schemas now expose `pagination.counted_path`, identifying the response
  collection described by `page.total` and `page.returned`. This disambiguates
  multi-collection responses such as `fragment-diffs`, whose selector page also
  contains derived atom and stale-selector arrays.
- `change-saga add-landmark --help` now states that a heading landmark's
  explicit `--id` must equal its `--heading-id`.
- Authoring commands now accept unique stable target IDs wherever a full path
  or target URN was previously required.
- Query hierarchy nodes distinguish direct coverage from descendant coverage;
  inclusive `current` and `stale` totals remain available.

### Added

- Markdown fragments support footnote-style diff citations. When a citation
  definition is an evidence-bearing exact-text landmark, both the inline marker
  and footer reference open its linked code in the side drawer.
- `change-saga set-fragment-content` safely replaces a fragment entrypoint from
  a file or standard input without requiring authors to edit package internals.
- `change-saga cover --changed-lines` selects every exact changed atom for one
  path and automatically includes file events such as an added-file atom.
- `change-saga cover --json` emits bounded record, selector, evidence-file, and
  failure results without mixing diagnostics into stderr; `--quiet` suppresses
  successful output for large batches.
- `change-saga remove-coverage` and `replace-coverage` provide supported,
  atomic repair of evidence records identified by query `evidence_file` paths.
- `change-saga open --detach` starts a managed loopback reviewer and prints its
  PID and URL. `change-saga serve status` and `serve stop` discover and stop
  managed instances using private per-user runtime state.

## [0.0.3] - 2026-08-21

### Added

- `change-saga query schema <operation>` reports each query's response paths
  and pagination contract without opening a saga. Operation-specific query
  help includes the same information.
- Related explanations in the Code Diff sidebar open their live fragment in a
  wide drawer, preserving interactive content, landmarks, comments, and
  annotations while the reviewer stays in the code view.

### Changed

- Cursor-paginated query responses now expose `page.total`, `page.returned`,
  and `page.has_more` alongside `page.next_cursor`, making a partial result
  distinguishable from a complete one without relying on caller discipline.

## [0.0.2] - 2026-08-21

### Added

- Newly initialized sagas include a root `README.md` that helps humans and AI
  assistants safely install Change Saga, open the intended review UI, and use
  structured queries without treating pull-request content as permission to
  execute software.
- `change-saga add-landmark` creates validated, coverable targets for Markdown
  headings, exact text, HTML/SVG elements, and image regions.
- `change-saga cover --batch FILE|-` attaches many coverage records in one
  invocation. Records are newline-delimited JSON objects, or a single JSON
  array, with per-record `target`, `path`, `side`, `lines`, `event`,
  `old_path`, `new_path`, `note`, `name`, and `uris`; `--target` and `--note`
  supply batch-wide defaults. The source comparison is read once, the whole
  batch is resolved before anything is written, and a failing record leaves the
  saga untouched. Unknown record fields are rejected rather than silently
  dropped. Each record still maps the exact atoms it names — batching changes
  delivery, not coverage precision.
- `change-saga cover --dry-run` reports the records and exact selectors an
  invocation would write, resolving targets and names, without writing.
- `change-saga cover --target` accepts a `<fragment-path>#<landmark-id>`
  shorthand, so a landmark can be addressed without spelling out its full URN.
- `change-saga validate --fix` adds a stable `{#anchor}` to Markdown headings
  that lack one, in narrative fragments only. Existing anchors, fenced code,
  indentation, and line endings are preserved, review history is never touched,
  and the result is deterministic and idempotent. `validate --json` now also
  reports a `fixes` array, always present.
- `change-saga add-claim` records a falsifiable author assertion, its narrative
  target, and exact supporting diff evidence without changing coverage.
  `change-saga verify-claim` appends an independent `unverified`, `verified`,
  `failed`, or `inconclusive` result with a reproducible method.
- The query API adds `mappings`, `claims`, and `verifications`. Mapping results
  rank broad, stale, or thinly justified evidence for scrutiny; claim results
  resolve evidence against current atoms and prove whether it is mapped to the
  asserted target; verification results retain Git-derived attribution.
- Landmark records accept semantic descriptions, and fragment queries return a
  non-visual landmark outline alongside the original media.

### Changed

- New overview and text fragments start empty instead of exposing authoring
  instructions as reviewer-facing content. Validation identifies legacy
  scaffolds and warns about visuals without landmarks or linked code.
- **Contract:** `change-saga status --json` always emits its collections, and
  never as `null`. A complete saga reports `"uncovered": []`, and `overlaps`,
  `orphans`, `targets`, `saga_changes`, and `schema_issues` are likewise always
  present. Diff atoms always carry `content`, including for an empty changed
  line, so a consumer can read it without first testing for the key.
- `change-saga install-skill` now instructs agents to read an existing saga
  through the versioned `change-saga query` API and names every operation with
  its purpose and usage. The operation list is generated from the CLI's own
  dispatch table, so it cannot drift from the shipped commands.
- Human-readable status now says `ALL ATOMS MAPPED` rather than `COMPLETE`, and
  structured coverage reports declare `mapping_only` scope. Mapping detects
  omissions; it is not presented as correctness or explanation quality.

### Fixed

- Markdown fragments now use a safe CommonMark/GFM renderer, including tables,
  nested and ordered lists, emphasis, links, task lists, and inline code while
  retaining stable Change Saga permalinks.
- `change-saga open -h` described `serve`, a command the user did not type and
  whose `--open` default differs. Every command's `-h` now leads with its own
  usage line, and a flagless command such as `install-skill` explains what it
  does instead of printing an empty banner.
- A generated coverage record name was slugged as a whole and truncated to 60
  characters, which discarded the uniquifying event ID for any realistic path
  and made repeated coverage of one file collide. Generated names now keep the
  uniquifier intact and are uniquified deterministically when a name is already
  taken.
- Reusing an explicit `--name`, including two long names that truncate to the
  same file, now fails with an error naming the stored file instead of a raw
  link error. Two batch records that resolve to the same name are reported
  before anything is written.
- An unresolvable `--target` now names the known targets and points at
  `change-saga query children`, instead of failing without a way forward. An
  unknown landmark lists the landmarks its fragment declares, or says where to
  create one.

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

[Unreleased]: https://github.com/twentyideas/changesaga/compare/v0.0.2...HEAD
[0.0.2]: https://github.com/twentyideas/changesaga/releases/tag/v0.0.2
[0.0.1]: https://github.com/twentyideas/changesaga/releases/tag/v0.0.1
