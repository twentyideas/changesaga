# AI query adversarial test plan

Status: **fixture-ready; application and CLI suites pending their APIs**

This plan turns the safety boundary in
[ai-facing-interface.md](ai-facing-interface.md) into transport-independent
acceptance cases. Tests should use `internal/querytest.New`, which deliberately
places the saga and compared source in separate Git repositories. That catches
accidental assumptions that `--saga` is inside `--repo`.

The application suite is authoritative. CLI tests should run the same cases
through the JSON envelope and additionally assert exit codes and stdout shape.
No test should reproduce target lookup, cursor decoding, fragment reading, or
coverage logic outside `internal/reviewapp`.

## Required contracts

These details must be stable before the pending tests can assert them.

| Case | Application result | CLI result | Invariants |
| --- | --- | --- | --- |
| Separate saga and source repositories | `OpenOptions.SagaRoot` supplies saga/review state; `OpenOptions.SourceDir` exclusively supplies the comparison checkout | `--saga` and `--repo` have identical meanings | The snapshot includes the source base/product-head identity and the saga repository state. No fallback to the saga repository occurs when `SourceDir` is present. |
| Manifest decode failure | `invalid_saga` | exit 3 and one error envelope | Unknown fields and malformed JSON never become `internal`; details contain only saga-relative issue paths. |
| Structurally invalid saga | `invalid_saga` | exit 3 and one error envelope | Opening fails before indexes are exposed. A query never returns partial data from an invalid saga. |
| Duplicate public target | `invalid_saga` | exit 3 | Duplicate fragment/section IDs are rejected during open. Lookup must never select the first traversal match. |
| Malformed or tampered cursor | `invalid_argument`, not retryable | exit 2 | The message does not reveal signing keys, serialized index state, or implementation details. No partial page is returned. |
| Cursor from another operation or filter | `invalid_argument`, not retryable | exit 2 | A cursor is bound to operation and canonical query/filter arguments, not only to a numeric offset. |
| Well-formed cursor from an old snapshot | `stale_snapshot`, retryable, with expected and actual snapshot tokens | exit 4 | Pagination never mixes nodes from two snapshots. The client can restart from the first page. |
| Escaping entrypoint symlink | open fails with `invalid_saga` | exit 3 | Validation issue paths are relative and secret bytes are never read or returned. |
| Escaping non-entrypoint asset symlink | asset is omitted from fragment metadata; a later direct asset read returns `unsafe_path` | exit 6 for direct reads | Enumeration and reads evaluate canonical containment. A symlink target outside the package is never hashed, sized, MIME-sniffed, or opened. |
| HTML or SVG with script/event content | ordinary inert content bytes | success | Reads do not render, sanitize-and-rewrite, execute, fetch referenced URLs, or create a browser/server process. Content hashes cover the original bytes. |
| Read-only request | normal result or domain error | normal envelope and documented exit | Source files, saga files, modes, worktree status, index, and HEAD are byte-for-byte unchanged. No temp or receipt file remains. |

## Content boundary cases

`fragment` uses byte offsets even for UTF-8 text. Tests require the following
rules so paging can be implemented without loss or duplication:

- The default requested chunk is 256 KiB and the maximum is 1 MiB. A larger
  limit is `invalid_argument`; it is not silently clamped.
- `bytes` and `sha256` describe the complete entrypoint, while `offset` and
  `next_offset` describe the returned chunk.
- Text chunks are valid UTF-8 and never split a code point. If a requested byte
  limit ends inside a code point, the result ends before that code point and
  `next_offset` is the first unread byte.
- Offset zero starts the file. Offset exactly equal to the complete byte length
  returns an empty final chunk. An offset beyond the length, or inside a UTF-8
  code point, is `invalid_argument`.
- Invalid UTF-8 in a declared textual fragment is `unsupported_media`; binary
  image content is base64. `image/svg+xml` needs an explicit decision: return
  UTF-8 text like HTML, or base64 like other image media. Tests must not infer
  this from filename extensions.

Run these against sizes 0, 1, 256 KiB-1, 256 KiB, 256 KiB+1, 1 MiB, and
1 MiB+1, including a multi-byte rune across each boundary. The checked-in
helper generates content at runtime so the repository does not carry megabyte
fixtures.

## Pagination and cursor cases

For every paged operation (`children`, `fragment-diffs`, `diff-owners`,
`reviews`, and `gaps`):

1. Collect an unpaged expected order, then concatenate pages at limits 1, 2,
   default, and maximum. Each record must appear exactly once.
2. Pass every token from `querytest.TamperedCursors`; expect
   `invalid_argument` without a panic.
3. Reuse a cursor with another operation, parent, target, state, or gap kind;
   expect `invalid_argument`.
4. Obtain a cursor, call `AdvanceSagaSnapshot`, and reuse the old token; expect
   `stale_snapshot`, not a continuation over changed data.
5. Reopen an unchanged session and reuse a cursor. The result must remain
   usable if opaque cursors are intended to survive CLI process boundaries,
   as the structured CLI contract requires.

The cursor encoding may be signed or authenticated, but that is an
implementation detail. Tests assert binding and behavior, not token bytes.

## Target and source confusion cases

- Query every resource by its complete URN. Paths such as
  `alpha.chapter/shared.fragment`, bare IDs such as `shared`, and URNs for a
  different saga ID are `invalid_argument` or `not_found` according to one
  consistently documented rule.
- A valid target of the wrong kind (for example, a chapter passed to
  `ReadFragment`) is `invalid_argument`; a well-formed absent fragment target is
  `not_found`.
- A missing source checkout, unverifiable repository identity, unknown base,
  unknown head, and missing merge base are `source_unavailable` (exit 7), with
  no absolute paths unless diagnostic paths were explicitly enabled.
- Adversarial revision strings beginning with `-`, containing spaces, or
  resembling pathspecs are passed as one Git argument after
  `--end-of-options`. They produce `source_unavailable` and never change Git
  configuration or execute a subprocess chosen by the input.

## No-side-effect harness

Take `fixture.State()` immediately before opening the session or invoking the
CLI and call `fixture.AssertUnchanged` after success and after every error case.
The captured state includes regular files, symlink targets, modes, Git HEAD,
the staged index view, and untracked files in both repositories. Run the
assertion for overview, every paged read, fragment content, invalid saga,
unsafe symlink, stale cursor, and source failure.

Pass each serialized error to `fixture.AssertNoAbsolutePaths`. This checks raw,
slash-normalized, and JSON-escaped spellings of both repository roots; checking
only decoded issue paths can miss a leak in an outer error message.

CLI integration tests must also assert that stdout is exactly one JSON value,
that errors do not print a second prose diagnostic to stdout, and that a read
does not start a listener. Process timeouts should be treated as test failures,
not retried indefinitely.
