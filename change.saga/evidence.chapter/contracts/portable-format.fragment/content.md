# Portable format contract {#portable-format-contract}

## The directory tree is the exchange format {#directory-tree}

A Saga is a versioned hierarchy of root, chapter, recursive section, fragment, landmark, evidence, claim, verification, and review packages. Each independent record lives in its own file, which keeps Git merges local and makes review history append-only.[^package-hierarchy]

Fragment entrypoints are constrained to portable relative paths, structural entities reject unsafe symlink escapes, and schemas share the runtime's ID, media-type, selector, state, and URI grammars.[^portable-paths]

## Stable narrative and source identities {#stable-identities}

Narrative targets are URNs built from stable Saga, chapter, section, fragment, and landmark IDs; they survive directory moves and display-title edits. Source evidence uses absolute `saga-diff://v1` URIs that bind a canonical repository, resolved base, product-patch identity, side/range or lifecycle event, and path.[^identity-model]

Git parsing emits changed line atoms separately from file lifecycle atoms and computes a product identity that ignores Saga-only commits. That separation lets an added empty file, a rename across the Saga boundary, or a mode-only change remain coverable while author-only edits do not stale product evidence.[^git-atoms]

## Coverage is exact ownership, not correctness {#coverage-semantics}

A coverage record maps one narrative target to exact atoms. Evaluation reports uncovered atoms, overlapping owners, stale/orphan selectors, and per-target summaries; contained line ranges are indexed without treating context lines as product changes.[^coverage-evaluation]

The command supports atomic batches, dry runs, generated collision-resistant names, and record-level replacement/removal so a broad multifunction mapping can be split without hand-editing metadata.[^coverage-mutation]

## Assertions retain their verification history {#claim-history}

Claims are falsifiable assertions with supporting diff URIs but do not contribute to coverage. Verification results are separate append-only records whose status, method, summary, optional command, and Git attribution preserve what was actually checked.[^claims-verifications]

[^package-hierarchy]: The v2 model and schemas define independent hierarchy, landmark, evidence, claim, verification, and review records with append-only file granularity.
[^portable-paths]: Runtime loading and schema-contract tests enforce contained entrypoints, portable names, safe structural entities, and matching grammar constants.
[^identity-model]: Stable target constructors and the diff-URI codec define portable narrative and source identities without local filesystem paths.
[^git-atoms]: Git diff parsing distinguishes changed lines from lifecycle events and derives a product patch identity that excludes Saga-only paths.
[^coverage-evaluation]: Coverage evaluation indexes exact atoms and reports omission, overlap, and stale ownership without claiming semantic correctness.
[^coverage-mutation]: Coverage authoring resolves complete batches before writing and supports atomic replacement or removal by evidence record.
[^claims-verifications]: Claim and verification commands write independent records and validation requires valid kinds, statuses, methods, targets, and exact evidence URIs.
