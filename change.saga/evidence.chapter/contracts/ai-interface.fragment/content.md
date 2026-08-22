# AI-facing contract {#ai-facing-contract}

## Read through queries, not storage {#query-boundary}

The `change-saga.ai/v1` query surface exposes overview, children, bounded fragment reads, evidence ownership in both directions, reviews, gaps, mappings, claims, and verifications. Every invocation returns exactly one JSON envelope and uses stable machine error codes instead of requiring an agent to parse prose or metadata files.[^query-envelope]

Fragment content is byte-bounded and UTF-8 safe; list operations are cursor-paginated, cursors are integrity-checked and bound to the operation, key, and snapshot, and callers can confirm returned and total counts.[^bounded-pagination]

## Mutate through commands with stable outputs {#mutation-boundary}

Authoring commands create hierarchy and fragments, replace entrypoint content, add semantic landmarks, attach exact coverage, and append claims or verification results. Mutation JSON keeps failures on standard output, validates all inputs before writes where atomicity matters, and resolves stable IDs or URNs across hierarchy commands.[^mutation-contract]

`validate --fix` only adds missing Markdown heading anchors and leaves already anchored content and the review overlay unchanged; structural and visual validation remains distinct from `status`, which reports source-atom omission coverage.[^validate-status]

## Maintain from source evidence {#maintenance-impact}

Comparison analysis never diffs prose to guess maintenance work. It projects destructive replacements to `must_update`, nearby additions to `consider_update`, and unowned atoms to `new_content`, while refusing to claim an exhaustive result when the maintained baseline is incomplete.[^impact-analysis]

## Safety and testability {#read-safety}

The read application builds one immutable snapshot, rejects ambiguous or invalid Sagas before serving queries, sanitizes diagnostic paths, bounds asset/content access to fragment packages, and deterministically orders hierarchy and append-only records.[^session-safety]

Shared adversarial fixtures exercise separate source and Saga repositories, large and active content, escaping symlinks, cursor tampering, filesystem side effects, and absolute-path leakage. Golden envelopes and integration tests lock the CLI and application layers to the same contract.[^query-fixtures]

[^query-envelope]: The CLI dispatcher and review application adapter implement one versioned envelope, stable operations, and stable error mapping for every query.
[^bounded-pagination]: Session reads preserve UTF-8 boundaries and snapshot-bound cursor pagination with counted page metadata.
[^mutation-contract]: CLI authoring mutations resolve stable targets, validate batches before writing, and expose structured success or failure output.
[^validate-status]: Anchor fixing is idempotent and scoped to authored Markdown, while validation and status deliberately report different contracts.
[^impact-analysis]: Diff-only impact analysis classifies replacement, adjacency, and new ownership and marks incomplete baselines explicitly.
[^session-safety]: The query session validates and indexes a bounded immutable view, contains file access, sanitizes errors, and applies deterministic ordering.
[^query-fixtures]: Reusable fixtures and CLI/application tests assert bounded inert reads, cursor integrity, no leaked host paths, and no query-side filesystem mutation.
