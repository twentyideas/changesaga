# Large-saga performance

The reviewer server sends one page. That page describes the whole comparison and
the whole story — every chapter, every changed file, and both directions of the
coverage audit — and it carries almost none of either.

The saga document on that page is a shell: the saga's identity, the coverage
totals, the overview's explanations as descriptors, one summary per chapter, and
the navigation outline beside it. The code it carries is the one file the Code
Diff tab has selected. Everything else arrives on demand, from an endpoint
bounded by one node:

| Endpoint | Bounded by | Fetched when |
| --- | --- | --- |
| `/api/file-diff` | one changed file | a reviewer opens that file |
| `/api/section` | one chapter's structure | a reviewer opens that chapter |
| `/api/fragment` | one explanation | that explanation comes into view |
| `/api/locate` | one anchor | a permalink names something not on the page |

The loaded saga, Git comparison, and coverage report behind all of those are
reused until the bytes they were built from change.

That boundary is the thing the budgets below defend, and it has two halves. A
payload that grows with the size of the comparison means a diff body has been
inlined again. A payload that grows with the size of the *document* means the
narrative tree is being rendered eagerly again — one explanation's prose on the
first load is the whole of that regression, whatever the page happens to weigh.

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

The first-load budgets scale that same generator along one axis at a time —
more changed lines per file, more changed files, or the same code explained one
reference per line — and check each shape for complete coverage before measuring
it. The shapes are listed under "Scale-relative first-load budgets" below.

The browser suite builds its own fixed saga through the real CLI
(`largeSagaScale` in `e2e/support/fixture-builder.ts`): 6 chapters, 18
fragments, and 1,536 changed lines across 32 files, fully covered.

This repository's own `change.saga` is the reference workload for
investigation. It has 38,209 changed lines across 183 files and 154 narrative
targets, which is roughly nine times the generated fixture.

## Budgets

Budgets come in kinds, and the difference matters when one fails.

**Hard budgets** are byte counts, element counts, request counts, and growth
ratios over deterministic fixtures. CI asserts them and a breach always means
the payload changed shape. They live in `internal/server/budget_test.go`,
`internal/server/firstload_budget_test.go`, and
`e2e/tests/performance.spec.ts`, and each failure prints what was measured, what
the budget was, and why the budget exists.

**Diagnostic metrics** are wall-clock times. A shared CI runner varies far more
than any regression worth catching — repeated first loads of one unchanged
fixture varied by 3.9x on an idle machine — so these are reported rather than
asserted: the Go benchmarks print them, the browser suite attaches
`first-load-metrics.json` to its result, and the reference numbers are recorded
below. A repeatable slowdown should be investigated before a budget is adjusted.

Allocation sits between the two. Bytes allocated and retained heap over the same
warm request are reproducible to within 0.1% in one process, so the scale-relative
budgets do assert them; the absolute figures are skipped under `-race`, which
roughly doubles them.

### Hard budgets

| Surface | Budget | Measured | Where |
| --- | ---: | ---: | --- |
| First-load HTML, 4,096-line comparison | 3,400,000 B | 2,890,860 B | `TestLargeSagaFirstLoadStaysWithinPayloadBudgets` |
| First-load HTML elements | 90,000 | 75,127 | same |
| Diff rows in first-load HTML | 256 | 128 | same |
| Saga document bytes on first load | 60,000 B | 39,126 B | `TestLargeSagaFirstLoadShipsOnlyTheChapterShell` |
| Saga document elements on first load | 1,600 | 1,048 | same |
| Explanation content in the first-load document | 0 | 0 | same |
| Changed lines from unopened files in the page | 0 | 0 | `TestLargeSagaFirstLoadOmitsUnopenedDiffBodies` |
| One file diff response | 200,000 B | 162,399 B | `TestLargeSagaFileDiffEndpointStaysWithinBudgets` |
| One coverage diff response | 45,000 B | 32,238 B | same |
| One chapter body response | 140,000 B | 93,144 B | `TestChapterAndExplanationEndpointsStayWithinBudgets` |
| One explanation response | 40,000 B | 18,303 B | same |
| Snapshot builds across 5 unchanged requests | 1 | 1 | `TestReviewSnapshotIsReusedUntilTheSagaChanges` |
| Snapshot builds after a review decision | 2 | 2 | same |
| First-load document, browser, 1,536-line comparison | 1,200,000 B | 414,626 B | `a large saga's first load stays within its payload budgets` |
| First-load DOM elements, browser | 6,000 | 4,978 | same |
| Diff rows in the first-load DOM | 160 | 96 | same |
| Diff-body requests before a file is opened | 0 | 0 | `loads a coverage file diff only once the reviewer opens that file` |
| Diff-body requests when reopening the same file | 1 | 1 | same |
| Chapter-body requests before a chapter is opened | 0 | 0 | `ships the saga as a shell and fetches each chapter and explanation once, when it is reached` |
| Chapter-body requests when reopening the same chapter | 1 | 1 | same |

The request-count budgets are the boundary stated as behaviour rather than as a
size: switching to Coverage fetches nothing, opening one file fetches exactly
that file, opening one chapter fetches exactly that chapter, and reopening
either fetches nothing again.

`TestConcurrentRequestsShareOneSnapshotSafely` is the cost of reuse, not a size:
sixteen concurrent requests share one loaded saga, and under `-race` it fails if
any handler writes to the shared model.

### Scale-relative first-load budgets

Every budget above is a level over one fixed fixture, and the whole-codebase
failure in [large-saga-diagnosis.md](large-saga-diagnosis.md) is not a level. A
page that costs 5 MB for 4,096 atoms and 5 MB for 65,536 atoms is healthy; a
page that costs 5 MB and then 80 MB is the defect, and both pass any ceiling the
small fixture passes.

`internal/server/firstload_budget_test.go` therefore measures the same first
load over four fully covered fixtures that each move one axis, and budgets what
may and may not grow with them:

| Shape | Files | Changed lines each | Atoms | Authored ranges |
| --- | ---: | ---: | ---: | ---: |
| `base` | 32 | 64 | 4,096 | 1,024 |
| `deeper` | 32 | 256 | 16,384 | 1,024 |
| `wider` | 128 | 64 | 16,384 | 4,096 |
| `per-line` | 32 | 64 | 4,096 | 4,096 |

`deeper` quadruples the changed code and widens the range in step, so the
document and the authored evidence are unchanged and only the code moved.
`wider` quadruples the whole comparison. `per-line` explains exactly the same
code as `base` one line at a time, which is how the diagnosed saga was authored.
`TestScalingChangedLinesWithRangeWidthHoldsEvidenceConstant` pins the fixture
property `deeper` depends on, and every shape is checked for complete coverage
before it is measured — a saga that explains less would otherwise buy a smaller
page.

| Invariant | Budget | Measured | Axis |
| --- | ---: | ---: | --- |
| First-load bytes, 4x the changed code | 1.35x | 1.09x | `base` -> `deeper` |
| First-load elements, 4x the changed code | 1.25x | 1.06x | `base` -> `deeper` |
| Coverage rows rendered | = authored ranges | = authored ranges | all four |
| Coverage rows, 4x the changed code | unchanged | 1,024 -> 1,024 | `base` -> `deeper` |
| Inlined diff rows, 4x the changed files | unchanged | 128 -> 128 | `base` -> `wider` |
| Inlined diff rows, per-line evidence | unchanged | 128 -> 128 | `base` -> `per-line` |
| First-load bytes per authored range | 4,096 B | 3,357 B | `base` -> `per-line` |
| Bytes allocated, warm first load | 125,000,000 | 104,142,528 | `base` |
| Bytes allocated, 4x the changed code | 2.5x | 1.60x | `base` -> `deeper` |
| Bytes allocated, 4x the comparison | 5x | 3.54x | `base` -> `wider` |
| Retained heap after first load | 16,000,000 B | 11,233,064 B | `base` |
| Retained heap per changed atom | 4,096 B | 2,742 B | `base` |
| Retained heap per extra authored range | 5,120 B | 3,640 B | `base` -> `per-line` |
| Retained heap, 4x the comparison | 5x | 3.11x | `base` -> `wider` |
| Diff nodes materialized by first load | 2n + d | 2n + d | `base` |

Two of these are the coefficients the diagnosed saga was extreme on. Each
authored evidence range costs about 3.4 KB of page and about 3.6 KB of resident
memory; that saga authored 529,599 of them for code that needed 5,330, which is
most of both its 230 MB on disk and its 1.49 GB peak. The budgets fail with that
extrapolation printed, so a breach names the real failure rather than a number.

The coverage-row invariant is the sharpest statement of the boundary: the audit
renders one row per range a reviewer wrote and never one per atom, at every
scale. `deeper` proves it by quadrupling the atoms and leaving the row count
untouched.

Wall time is measured and never asserted, here as elsewhere. Repeated first
loads of one unchanged fixture on an idle machine varied by 3.9x, which is wider
than any regression worth catching, so `BenchmarkFirstLoadScale` reports it
instead. Allocated bytes and retained heap over the same requests varied by
under 0.1%, which is what makes them safe to assert; the absolute allocation
budget is skipped under `-race`, which roughly doubles it, while the growth
ratios hold either way because both sides pay the same overhead.

`TestFirstLoadMaterializesBoundedDiffNodes` budgets what the page cannot show.
`makeFileViews` builds one `*diffAtomView` per changed atom and one
`*DiffLineView` per display line across every changed file, and `makeSectionView`
builds another `*diffAtomView` per atom per owning target, so first load
materializes `2n + d` nodes and renders about 1% of them. That is a real cost —
532,290 atoms is over 1.5 million nodes — and removing it is listed under
remaining bottlenecks. The budget pins the accounting exactly, so a third
per-atom projection added to first load fails.

### Diagnostic reference results

Recorded on Apple M3 Pro, darwin/arm64, Go 1.26, `-benchtime=5x -count=3`,
reporting medians. Run the set with:

```sh
go test ./internal/coverage ./internal/cli ./internal/gitattribution ./internal/reviewapp ./internal/server \
  -run '^$' -bench 'LargeSaga|LargeMappedDiff|FirstLoadScale' -benchmem -benchtime=5x -count=3
```

| Benchmark | Time | Allocated | Output |
| --- | ---: | ---: | ---: |
| `BenchmarkLargeSagaRealisticHTTP/first_load` | 137 ms | 98.8 MB | 5,260,628 B |
| `BenchmarkFirstLoadScale/base` | 152 ms | 98.7 MB | 5,260,613 B |
| `BenchmarkFirstLoadScale/deeper` | 204 ms | 160.6 MB | 5,759,076 B |
| `BenchmarkFirstLoadScale/wider` | 416 ms | 352.7 MB | 15,729,946 B |
| `BenchmarkFirstLoadScale/per_line` | 251 ms | 197.1 MB | 15,573,829 B |
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
fixture and is the one that reflects a real comparison. `BenchmarkFirstLoadScale`
runs the same request over the four shapes the scale-relative budgets assert on,
and is the diagnostic companion to them: `deeper` costs 1.6x `base` for four
times the code, while `wider` costs 3.6x for four times the whole comparison and
`per_line` costs 2.0x for the same code explained one line at a time. Keep the fixture and
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

- First load materializes every changed atom twice and every display line once,
  and renders about 1% of the result. `makeFileViews` builds a `*diffAtomView`
  per atom and a `*DiffLineView` per display line for every changed file when
  only the selected file is rendered, and `makeSectionView` builds another
  `*diffAtomView` per atom per owning target although the templates only read
  `len`. On the diagnosed saga that is over 1.5 million nodes.
  `TestFirstLoadMaterializesBoundedDiffNodes` budgets the accounting exactly so
  it cannot grow, and it is the largest remaining first-load allocation.
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
