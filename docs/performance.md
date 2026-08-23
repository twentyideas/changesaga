# Large-saga performance

The reviewer server sends one page. That page describes the whole comparison —
every chapter, every changed file, and both directions of the coverage audit —
and it carries the code of only the one file the Code Diff tab has selected.
Every other diff body arrives from `/api/file-diff` when a reviewer opens that
file, and the loaded saga, Git comparison, and coverage report behind those
requests are reused until the bytes they were built from change.

That boundary is the thing the budgets below defend. A payload that grows with
the size of the comparison rather than with the size of the document means a
diff body has been inlined again.

## Fixtures

Performance work uses deterministic fixtures rather than a checked-in generated
saga. `testfixture.GenerateLargeSaga` builds a mega pull request with:

- 8 chapters, 48 sections, and 145 fragments across Markdown, SVG, and HTML;
- 4,096 changed-line atoms, 1,024 ranged references, and 4,096 exact ownership
  mappings in 144 diff files;
- 288 approval records, 48 threads with messages and events, and 32 file review
  records; and
- a real two-commit Git comparison with stable object IDs and 555,020 bytes of
  saga data.

`TestGenerateLargeSagaIsDeterministicAndValid` compares independent generated
trees byte for byte, loads the result through normal validation, and proves that
every atom is mapped with no overlaps or stale references.

That default shape is what a reviewer writes with `cover --lines`, and it is
deliberately spread: 1,024 four-line ranges over 144 fragments, so no target
owns more than eight references. `CoverageRangeWidth` and `CoverageTargets`
can instead produce deliberately fragmented per-line evidence concentrated on
a few targets. The authoring API canonicalizes that input now; the pathological
shape remains a reader regression fixture so explicit URI input or another
producer cannot restore nonlinear construction. It uses the same comparison,
narrative tree, and review overlay. `TestGenerateLargeSagaCoverageShapeIsSelectable`
proves the concentrated shape still covers every atom exactly once, and
`TestDefaultLargeSagaOptionsKeepTheirSpreadRangedShape` pins the default the
byte budgets above are measured against.

The browser suite builds its own fixed saga through the real CLI
(`largeSagaScale` in `e2e/support/fixture-builder.ts`): 6 chapters, 18
fragments, and 1,536 changed lines across 32 files, fully covered.

This repository's own `change.saga` is the reference workload for
investigation. It has 38,209 changed lines across 183 files and 154 narrative
targets, which is roughly nine times the generated fixture.

## Budgets

Budgets come in two kinds, and the difference matters when one fails.

**Hard budgets** are byte counts, element counts, and request counts over a
fixed fixture. They are deterministic, so CI asserts them and a breach always
means the payload changed shape. They live in
`internal/server/budget_test.go` and `e2e/tests/performance.spec.ts`, and each
failure prints what was measured, what the budget was, and why the budget
exists.

**Diagnostic metrics** are wall-clock times and allocation counts. A shared CI
runner varies far more than any regression worth catching, so these are
reported rather than asserted: the Go benchmarks print them, the browser suite
attaches `first-load-metrics.json` to its result, and the reference numbers are
recorded below. A repeatable slowdown or an allocation increase should be
investigated before a budget is adjusted.

### Hard budgets

| Surface | Budget | Measured | Where |
| --- | ---: | ---: | --- |
| First-load HTML, 4,096-line comparison | 6,500,000 B | 5,260,613 B | `TestLargeSagaFirstLoadStaysWithinPayloadBudgets` |
| First-load HTML elements | 200,000 | 145,103 | same |
| Diff rows in first-load HTML | 256 | 128 | same |
| Changed lines from unopened files in the page | 0 | 0 | `TestLargeSagaFirstLoadOmitsUnopenedDiffBodies` |
| One file diff response | 200,000 B | 162,399 B | `TestLargeSagaFileDiffEndpointStaysWithinBudgets` |
| One coverage diff response | 45,000 B | 32,238 B | same |
| Snapshot builds across 5 unchanged requests | 1 | 1 | `TestReviewSnapshotIsReusedUntilTheSagaChanges` |
| Snapshot builds after a review decision | 2 | 2 | same |
| First-load document, browser, 1,536-line comparison | 1,200,000 B | 555,974 B | `a large saga's first load stays within its payload budgets` |
| First-load DOM elements, browser | 20,000 | 6,315 | same |
| Diff rows in the first-load DOM | 160 | 96 | same |
| Diff-body requests before a file is opened | 0 | 0 | `loads a coverage file diff only once the reviewer opens that file` |
| Diff-body requests when reopening the same file | 1 | 1 | same |

The two request-count budgets are the boundary stated as behaviour rather than
as a size: switching to Coverage fetches nothing, opening one file fetches
exactly that file, and reopening it fetches nothing again.

`TestConcurrentRequestsShareOneSnapshotSafely` is the cost of reuse, not a size:
sixteen concurrent requests share one loaded saga, and under `-race` it fails if
any handler writes to the shared model.

### Diagnostic reference results

Recorded on Apple M3 Pro, darwin/arm64, Go 1.26, `-benchtime=5x -count=3`,
reporting medians. Run the set with:

```sh
go test ./internal/coverage ./internal/cli ./internal/gitattribution ./internal/reviewapp ./internal/server \
  -run '^$' -bench 'LargeSaga|LargeMappedDiff' -benchmem -benchtime=5x -count=3
```

| Benchmark | Time | Allocated | Output |
| --- | ---: | ---: | ---: |
| `BenchmarkLargeSagaRealisticHTTP/first_load` | 137 ms | 98.8 MB | 5,260,628 B |
| `BenchmarkLargeSagaRealisticHTTP/file_diff` | 29.0 ms | 6.38 MB | 162,399 B |
| `BenchmarkLargeSagaRealisticHTTP/coverage_diff` | 27.6 ms | 5.50 MB | 32,238 B |
| `BenchmarkLargeSagaHTTP/first_load` | 19.5 ms | 7.85 MB | 638,337 B |
| `BenchmarkLargeSagaHTTP/chapter_navigation` | 9.41 ms | 0.76 MB | — |
| `BenchmarkLargeSagaCoverageView` | 9.55 ms | 11.50 MB | — |
| `BenchmarkLargeSagaCoverageRender` | 6.82 ms | 2.80 MB | — |
| `BenchmarkLargeSagaLinkedDrawerConstruction` | 25.2 ms | 19.44 MB | — |
| `BenchmarkMakeCodeReviewViewLargeSaga` | 1.73 ms | 2.30 MB | — |
| `BenchmarkEvaluateLargeMappedDiff` | 36.3 ms | 45.2 MB | — |
| `BenchmarkValidateLargeSaga` | 61.0 ms | 15.08 MB | — |
| `BenchmarkStatusLargeSaga` | 156 ms | 50.0 MB | — |

`BenchmarkLargeSagaHTTP` uses a coverage-free saga and so exercises document
rendering only. `BenchmarkLargeSagaRealisticHTTP` uses the fully covered
fixture and is the one that reflects a real comparison. Keep the fixture and
`-benchtime` identical for before-and-after comparisons, use `benchstat` when
it is available, and never include fixture construction in the timed region.

### Selector construction over fragmented evidence

`internal/reviewapp/largesaga_bench_test.go` benchmarks `reviewapp.Open`, warm
`Session.Overview`, and isolated selector construction over one 4,096-atom
comparison written as ranged evidence and as increasingly concentrated
per-line evidence. `former-scan-steps/op` reports the deterministic work the
removed selector scan would perform—up to 2,099,200 iterations in the fixture
and 11.9 billion in the diagnosed whole-codebase saga. The current engine uses
one `selectorKey` lookup per ownership edge instead.

The focused `BenchmarkSelectorLinking` comparison on Apple M3 Pro measured:

| Selectors | Former scan | Identity index | Speedup |
| ---: | ---: | ---: | ---: |
| 256 | 1.09 ms | 0.23 ms | 4.8× |
| 1,024 | 17.26 ms | 0.99 ms | 17.4× |
| 4,096 | 277.94 ms | 4.60 ms | 60.4× |

Growing input 16× made the old construction 254× slower and the indexed
construction 20× slower. `TestSelectorLinkingScalesLinearly` guards an
eightfold increase with enough headroom for noisy shared runners while still
rejecting quadratic growth. The realistic fixture separately ensures the
stress shape remains valid, completely covered, and concentrated.

`Overview` itself is cheap once a session exists; cold `Open` also includes
Saga loading, Git diff generation, coverage evaluation, snapshot construction,
review indexing, and attribution. Those components become the next profiling
surface now that selector linking is linear.

CLI `overview` and `children` queries use an aggregate-only `Open` mode because
their response schemas expose counts and hierarchy, not atom-level details.
The mode stores per-atom assignment state in a contiguous slice, spills only
uncommon additional target owners into a sparse map, and omits ownership and
reverse-selector indexes. Generated Git atoms are indexed from their structured
fields and shared comparison identity rather than by parsing each long URI.

On the 532,290-atom whole-codebase Saga, the original per-line evidence now
opens in 10.74 s at 1.49 GB peak RSS, down from 1,049.7 s at 1.93 GB. Rewriting
the identical evidence as 5,330 canonical dense ranges reduces the Saga from
230.36 MB to 2.33 MB and the query to 3.80 s at 0.93 GB. Both responses carry
identical data after excluding the content-derived snapshot hash.

## Where the payload went

Before this work, the page carried every changed line twice: once inside a
`<template>` for each narrative target that explained it, and again inside the
coverage audit. Both copies sat behind a closed disclosure, and the linked-code
copy was discarded and refetched the moment a reviewer actually opened it.
Together they were 98% of the document.

### This repository's own saga, 38,209 changed lines

Server, measured over loopback with `change-saga serve`:

| | Before | After | Change |
| --- | ---: | ---: | ---: |
| Page bytes | 115,360,863 | 3,059,909 | 37.7× smaller |
| Page, warm | 1,072 ms | 280 ms | 3.8× faster |
| `/api/file-diff`, largest file | 505 ms, 4,620,324 B | 53 ms, 2,842,598 B | 9.5× faster, 1.6× smaller |
| `/chapters/<id>` deep link | 66 ms | 15 ms | 4.4× faster |

Browser, Chromium, 1440×1000, medians of three cold loads:

| | Before | After | Change |
| --- | ---: | ---: | ---: |
| Document transferred | 115,361,163 B | 3,060,209 B | 37.7× smaller |
| DOM elements | 2,514,437 | 29,781 | 84.4× fewer |
| First contentful paint | 1,088 ms | 340 ms | 3.2× faster |
| DOM interactive | 3,121 ms | 368 ms | 8.5× faster |
| DOMContentLoaded | 4,964 ms | 385 ms | 12.9× faster |
| Load complete | 4,977 ms | 502 ms | 9.9× faster |
| Switch to Coverage | 1,548 ms | 91 ms | 17.0× faster |
| Open one coverage file | 1,564 ms | 75 ms | 20.9× faster |

### The generated fixture, 4,096 changed lines

| | Before | After | Change |
| --- | ---: | ---: | ---: |
| First-load HTML | 45,409,451 B | 5,260,620 B | 8.6× smaller |
| HTML elements | 1,869,455 | 145,103 | 12.9× fewer |
| Diff rows in the page | 139,392 | 128 | 1,089× fewer |
| First load, warm | 752 ms | 156 ms | 4.8× faster |
| `/api/file-diff` | 148 ms, 262,039 B | 43 ms, 162,399 B | 3.4× faster, 1.6× smaller |

The old budgets could not have caught this. `BenchmarkLargeSagaHTTP` used a
coverage-free fixture, so its "1 MiB HTML" output budget was measured against a
638 KB page while the same product served 45 MB for a comparison of the same
shape with coverage attached.

### What each change contributed

- Coverage and linked-code drawers load a file's rows from `/api/file-diff`
  instead of inlining them. This is the whole of the payload reduction, and it
  took `BenchmarkLargeSagaCoverageRender` from 123.6 ms and 51.69 MB to 6.82 ms
  and 2.80 MB.
- A review snapshot — the loaded saga with Git attribution applied, the change
  set, and the coverage report — is reused while the saga tree and the resolved
  comparison identity are unchanged. Without it, every file a reviewer opened
  would pay the full ~490 ms of saga load, Git history walk, `git diff`, and
  coverage evaluation that first load pays. The freshness check that guards it
  costs about 10 ms per request on this saga: an 8 ms walk of 814 saga files and
  two short Git reads, run concurrently.
- A diff row carries its exact diff URI once, on the row, instead of once per
  row plus once on each of its two action buttons. The buttons read the row they
  sit in. This took the largest file's body from 4,620,324 B to 2,842,598 B.
- Per-atom Code Diff deep links are no longer built. All 38,209 of them were
  computed twice per page and rendered nowhere, which was 39% of the page's
  allocations; `BenchmarkMakeCodeReviewViewLargeSaga` went from 4.03 ms and
  6.33 MB to 1.73 ms and 2.30 MB.
- Authored file notes are matched against a target's changed lines bucketed by
  kind and path, rather than every note against every line. A well-covered
  target has one exact reference per changed line, so the two loops used to grow
  together.

Earlier work, unchanged by this pass and kept for context:

| Hot path | Before | After | Change |
| --- | ---: | ---: | ---: |
| Coverage matching, 10,000 atoms / 2,000 mappings | 493.8 ms, 45.22 MB | 37.1 ms, 45.23 MB | 13.3× faster |
| Git attribution, 100 committed records | 1.65 s, 17.30 MB | 175 ms, 6.31 MB | 9.4× faster |

## Remaining bottlenecks

A separate investigation into a whole-codebase saga — 532,290 changed atoms
across 2,666 files — found three defects that dominated at that scale. Dense
range authoring and direct selector identity indexing now fix them; the history
and measurements remain in [large-saga-diagnosis.md](large-saga-diagnosis.md),
and the benchmark above retains the pathological shape as a regression case.

- `attachedFileNotes` is the largest single cost left in first load: 32% of CPU
  and about 143 MB of the 313 MB a warm page allocates for this repository's
  saga. Nearly all of it is `diffuri.Parse` re-deriving references that
  `gitdiff.Read` already built from the same atom fields and then discarded.
  Carrying the parsed references on the snapshot would remove the cost outright,
  at the price of threading them through four view constructors.
- Git attribution still runs one `git log --follow` per unique committed record
  to preserve rename-aware authorship. It is now paid once per snapshot rather
  than once per request, but it remains the largest part of a cold first load.
- `gitdiff.Read` keeps separate display-context and canonical product-identity
  diffs because they require different Git options and exclude different paths.
- A `WORKTREE` head is never cached. Its diff depends on uncommitted file
  contents, which no cheap probe describes exactly, so every request rebuilds
  the snapshot. Sagas that pin a commit — the default — are unaffected.
- Responses are uncompressed. Over loopback that is close to free, and the
  remaining 3 MB page is dominated by coverage deep links rather than by
  repeated content, so compression is worth measuring before it is worth adding.
- One opened file is still rendered in full. The largest file in this
  repository's comparison is 2.8 MB of markup. Virtualizing a single file's rows
  is the next lever if that becomes the complaint; nothing measured here
  suggests it is yet.
