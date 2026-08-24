# Large-saga performance

The reviewer server's root response is a shell. It carries saga identity,
aggregate progress and coverage totals, the navigation outline, and descriptors
for narrative content. It carries zero diff rows and zero coverage rows. Those
details arrive incrementally from cursor-bounded endpoints only after a reviewer
asks for them:

| Endpoint | Bounded by | Fetched when |
| --- | --- | --- |
| `/api/code` | one cursor-bounded code page | a reviewer opens Code Diff or selects a file |
| `/api/coverage` | one cursor-bounded audit page | a reviewer opens or advances Coverage |
| `/api/file-diff` | one cursor-bounded file page | a reviewer opens linked code |
| `/api/section` | one chapter's structure | a reviewer opens that chapter |
| `/api/fragment` | one explanation | that explanation comes into view |
| `/api/locate` | one anchor | a permalink names something not on the page |

Review pages default to 50 rows and reject limits above 200. Continuations use
`X-Change-Saga-Next-Cursor` together with `data-next-cursor` and
`data-returned` in the rendered fragment.

The detailed review model is built and retained by those review endpoints, not
by `GET /`. A small root payload is not enough: eagerly constructing the full
Git comparison and coverage projection and then discarding their markup still
violates the contract.

The budgets therefore defend both visible and hidden work. Root response bytes
and materialized nodes must stay flat as atom and authored-range counts grow;
the cold request's wall time and retained heap have bounded slopes; and a test
counter asserts that root never crosses the full-comparison build boundary. The
narrative side has the same rule: one explanation's prose on first load is the
whole eager-rendering regression, whatever the page happens to weigh.

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

The CI first-load contract uses a smaller instance of that generator and moves
one axis at a time: eight times the atoms with the authored-range count fixed,
then eight times the authored ranges with the atom count fixed. The mappings are
still exact and complete. The default benchmark fixture remains available for
profiling, while a separate opt-in harness reproduces Daylight scale.

The browser suite builds its own fixed saga through the real CLI
(`largeSagaScale` in `e2e/support/fixture-builder.ts`): 6 chapters, 18
fragments, and 1,536 changed lines across 32 files, fully covered.

This repository's own `change.saga` is the reference workload for
investigation. It has 38,209 changed lines across 183 files and 154 narrative
targets, which is roughly nine times the generated fixture.

## Budgets

Budgets come in kinds, and the difference matters when one fails.

**Hard budgets** are byte counts, materialized-node counts, request/build
counts, retained heap, smoke ceilings, and growth ratios over deterministic
fixtures. CI asserts them. They live in `internal/server/budget_test.go`,
`internal/server/firstload_budget_test.go`, and
`e2e/tests/performance.spec.ts`, and each failure prints what was measured, what
the budget was, and why the budget exists.

**Diagnostic metrics** retain precise wall-clock and allocation figures. Shared
CI runners are noisy, so the scale contract asserts the median of three cold
requests with both ratio and additive slack plus a generous smoke ceiling; it
does not pretend that a single millisecond target is portable. Go benchmarks
print the unrounded values, and the browser suite attaches
`first-load-metrics.json` to its result.

Retained heap is sampled after two collections. It is deliberately paired with
the detailed-comparison cache build counter: the heap slope catches retained
hidden work, while a nonzero root build count catches an eager comparison that
happened to be collected before the sample.

### Hard budgets

| Surface | Budget | Measured | Where |
| --- | ---: | ---: | --- |
| Diff rows in root HTML | 0 | 0 | `TestRootFirstLoadStaysBoundedAsAtomsAndRangesGrow` |
| Coverage rows in root HTML | 0 | 0 | same |
| Full comparison/coverage builds caused by root | 0 | 0 | same |
| Review-page default / hard maximum | 50 / 200 rows | 50 / 200 | `TestPaginatedReviewEndpointAllocationsStayBoundedAcrossScale` |
| Cold root wall-time smoke ceiling | 2 s | platform-dependent | `TestRootFirstLoadStaysBoundedAsAtomsAndRangesGrow` |
| Heap retained after root | 32 MiB | platform-dependent | same |
| Saga document bytes on first load | 60,000 B | 39,126 B | `TestLargeSagaFirstLoadShipsOnlyTheChapterShell` |
| Saga document elements on first load | 1,600 | 1,048 | same |
| Explanation content in the first-load document | 0 | 0 | same |
| Changed lines from unopened files in the page | 0 | 0 | `TestLargeSagaFirstLoadOmitsUnopenedDiffBodies` |
| One file diff response | 200,000 B | 162,399 B | `TestLargeSagaFileDiffEndpointStaysWithinBudgets` |
| One coverage diff response | 45,000 B | 32,238 B | same |
| One chapter body response | 140,000 B | 93,144 B | `TestChapterAndExplanationEndpointsStayWithinBudgets` |
| One explanation response | 40,000 B | 18,303 B | same |
| First-load document, browser, 1,536-line comparison | 1,200,000 B | 414,626 B | `a large saga's first load stays within its payload budgets` |
| First-load DOM elements, browser | 6,000 | 4,978 | same |
| Diff rows in the first-load DOM | 0 | 0 | same |
| Coverage rows in the first-load DOM | 0 | 0 | same |
| Diff-body requests before a file is opened | 0 | 0 | `loads a coverage file diff only once the reviewer opens that file` |
| Diff-body requests when reopening the same file | 1 | 1 | same |
| Chapter-body requests before a chapter is opened | 0 | 0 | `ships the saga as a shell and fetches each chapter and explanation once, when it is reached` |
| Chapter-body requests when reopening the same chapter | 1 | 1 | same |

The request-count budgets are the boundary stated as behaviour rather than as a
size: root fetches no review detail; opening Code Diff or Coverage fetches one
cursor-bounded page; opening one chapter fetches exactly that chapter; and a
previously fetched page is reused until its generation changes.

### Scale-relative first-load budgets

Every budget above is a level over one fixed fixture, and the whole-codebase
failure in [large-saga-diagnosis.md](large-saga-diagnosis.md) is not a level. A
root that stays small after eagerly allocating and discarding the complete
review model is also defective. `internal/server/firstload_budget_test.go`
therefore measures the same cold root over three fully mapped fixtures and
moves one axis at a time:

| Shape | Files | Changed lines each | Atoms | Authored ranges |
| --- | ---: | ---: | ---: | ---: |
| `base` | 8 | 32 | 512 | 64 |
| `atom-growth` | 8 | 256 | 4,096 | 64 |
| `range-growth` | 8 | 32 | 512 | 512 |

The story, file count, and review overlay are identical. `atom-growth` widens
each authored range with the changed code, holding evidence count fixed.
`range-growth` explains the base comparison one line at a time, holding atom
count fixed. The generated fixture reports exact mapping counts, and the test
rejects any shape that maps fewer atoms than the comparison contains.

| Invariant | Budget | Axis |
| --- | ---: | --- |
| Diff rows in root | 0 | all three |
| Coverage rows in root | 0 | all three |
| Full comparison/coverage builds caused by root | 0 | all three |
| Root response bytes | 1.10× base + 8 KiB | both growth axes |
| Materialized root nodes | 1.05× base + 16 | both growth axes |
| Median cold root wall time | 3× base + 50 ms; 2 s ceiling | both growth axes |
| Retained heap after root | 1.5× base + 2 MiB; 32 MiB ceiling | both growth axes |
| Warm 50-row review-page allocation | 1.5× base + 512 KiB | both growth axes, all three review surfaces |

Response and node budgets defend what reaches the browser. The detailed
comparison cache build count must remain zero across root requests, so the
server cannot pass by constructing a `ChangeSet` and coverage report and then
emitting a small page. The heap sample runs after two collections, so an atom-
or range-proportional model cannot remain attached to the root handler. Together
they replace the former contract that explicitly allowed `2n + d` server-side
diff nodes and one eagerly rendered row per authored range.

Pagination has its own hidden-work check. After warming one detailed comparison
generation, the test measures allocated bytes for 50-row `/api/code`,
code-first `/api/coverage`, and saga-first `/api/coverage` requests. Eight times
the atoms or ranges may cost at most 1.5× the base plus 512 KiB. This rejects an
endpoint that returns 50 rows only after eagerly projecting every row.

Wall time is the median of three independent cold root renders over the same
generated files. Ratio and additive slack make the assertion tolerant of shared
runners, while the full-build counter supplies the deterministic failure for
the regression wall time is meant to expose. `BenchmarkRootFirstLoadScale`
reports warm allocation, response bytes, and node counts for investigation.

### Daylight-scale root harness

Ordinary CI stops at 4,096 atoms. The diagnosed workload is an opt-in generated
fixture: 2,666 files with 100 replaced lines produce 533,200 old/new line atoms,
close to Daylight's 532,290, and every atom is authored as a single-line range.
Run it locally on an otherwise idle machine with a generous timeout:

```sh
CHANGE_SAGA_DAYLIGHT_SCALE=1 go test ./internal/server \
  -run '^TestDaylightRootFirstLoadScale$' -count=1 -v -timeout=30m
```

`TestDaylightRootFirstLoadScale` prints root bytes, materialized nodes, median
cold wall time, retained heap, and full-comparison build count. Fixture
generation is intentionally outside the request timing. Do not add this command
to routine CI or compare its wall time with the smaller fixture; use it for
before/after measurements on the same machine and commit.

### Diagnostic reference results

Recorded on Apple M3 Pro, darwin/arm64, Go 1.26, `-benchtime=5x -count=3`,
reporting medians. Run the set with:

```sh
go test ./internal/coverage ./internal/cli ./internal/gitattribution ./internal/reviewapp ./internal/server \
  -run '^$' -bench 'LargeSaga|LargeMappedDiff|RootFirstLoadScale' -benchmem -benchtime=5x -count=3
```

| Benchmark | Time | Allocated | Output |
| --- | ---: | ---: | ---: |
| `BenchmarkLargeSagaRealisticHTTP/file_diff` | 29.0 ms | 6.38 MB | 162,399 B |
| `BenchmarkLargeSagaRealisticHTTP/coverage_diff` | 27.6 ms | 5.50 MB | 32,238 B |
| `BenchmarkLargeSagaHTTP/chapter_navigation` | 9.41 ms | 0.76 MB | — |
| `BenchmarkLargeSagaCoverageView` | 9.55 ms | 11.50 MB | — |
| `BenchmarkLargeSagaCoverageRender` | 6.82 ms | 2.80 MB | — |
| `BenchmarkLargeSagaLinkedDrawerConstruction` | 25.2 ms | 19.44 MB | — |
| `BenchmarkMakeCodeReviewViewLargeSaga` | 1.73 ms | 2.30 MB | — |
| `BenchmarkEvaluateLargeMappedDiff` | 36.3 ms | 45.2 MB | — |
| `BenchmarkValidateLargeSaga` | 61.0 ms | 15.08 MB | — |
| `BenchmarkStatusLargeSaga` | 156 ms | 50.0 MB | — |

The table retains endpoint and component baselines measured before the bounded
root contract; obsolete eager-root results were removed. Record fresh
`BenchmarkRootFirstLoadScale` results after the incremental review surfaces are
integrated. It runs the same three shapes the asserted test uses. Keep the
fixture and `-benchtime` identical for before-and-after comparisons, use
`benchstat` when it is available, and never include fixture construction in the
timed region.

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

The measurements below record the first deferral step, when one selected file
and the coverage index still arrived at root. They are historical evidence, not
the current contract. The bounded root removes those remaining review details;
the scale suite and Daylight harness above are the source of truth for it.

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

### The saga document, deferred

The audit and the drawers were the payload's first half. The narrative tree was
the second: the page rendered every chapter, every section, and every
explanation, with each explanation's Markdown read from disk, rendered, and
inlined behind a chapter disclosure that starts closed. That work was
proportional to the size of the story and none of it was on screen.

The document is now a shell, and the same recursion that renders the page
renders one chapter body: `viewScope.shell()` renders this node's own
explanations as descriptors and any chapter beneath it as a summary, and both
the page's root render and `/api/section` use it. Navigation and the review
progress map read the saga's manifests directly rather than the rendered views,
so they still name and count every destination in the document while the
chapters holding those destinations remain unfetched.

On the 4,096-line fixture, the saga document went from roughly 2.41 MB and
71,000 elements to 39,126 bytes and 1,048 elements — 62× smaller — and the
number tracks the eight chapters rather than the 145 explanations. In the
browser the whole first-load DOM fell from 6,315 elements to 4,978, of which the
saga document is 434.

A permalink into a chapter nobody has opened is the cost of this boundary, and
`/api/locate` is what pays it: the browser asks where one anchor lives, fetches
that chapter and that explanation, and then scrolls. Shipping the same answer as
an index would put every anchor in the document back into every first load.

### What each change contributed

- Code, coverage, and linked-code detail load from `/api/code`, `/api/coverage`,
  and `/api/file-diff` in cursor-bounded pages. Root neither renders their rows
  nor builds their complete structural projections.
- Detailed review state is reused by review endpoints while its generation is
  current. Root uses only bounded summary state, so it does not pay full Git
  diff and coverage evaluation merely to make the tabs reachable.
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

- A requested review page can still spend significant time in
  `attachedFileNotes`. Nearly all of it is `diffuri.Parse` re-deriving
  references that `gitdiff.Read` already built from the same atom fields and
  then discarded. Carrying parsed references in the detailed review generation
  would remove the cost without putting it back on root.
- Git attribution still runs one `git log --follow` per unique committed record
  to preserve rename-aware authorship. It is now paid once per snapshot rather
  than once per request, but it remains the largest part of a cold first load.
- `gitdiff.Read` keeps separate display-context and canonical product-identity
  diffs because they require different Git options and exclude different paths.
- A `WORKTREE` head is never cached. Its diff depends on uncommitted file
  contents, which no cheap probe describes exactly, so every request rebuilds
  the snapshot. Sagas that pin a commit — the default — are unaffected.
- Responses are uncompressed. Over loopback that is close to free; compression
  is worth measuring against cursor-bounded pages before adding complexity.
- A single pathological file can still require many cursor pages. Endpoint
  budgets must constrain both `limit` and retained generation state so advancing
  through that file does not recreate a root-scale problem elsewhere.
