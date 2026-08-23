# Why a whole-codebase saga is 230 MB and takes 17 minutes

Status: **all three diagnosed defects are fixed and remeasured end to end.**
The original measurements remain below as the regression baseline; focused
scale benchmarks cover the authoring and selector-construction paths that
caused it.

The saga behind it documents an entire codebase: 2,666 source files, 532,290
changed atoms, 118 narrative targets that own evidence, fully mapped with no
uncovered atoms, no overlaps, and no stale references. It is, by the format's
own standards, a correct and complete saga.

`change-saga query overview` on it takes **17 minutes 30 seconds** and peaks at
**1.93 GB** resident. Its evidence is **230.36 MB**.

Neither number is a property of the format. Both come from three specific,
independent defects in this repository.

## The measurements

Apple M3 Pro, darwin/arm64, Go 1.26.1, warm filesystem. Process time and peak
resident size from `/usr/bin/time -l`.

| | |
| --- | ---: |
| Evidence files | 2,666 |
| Evidence bytes | 230.36 MB |
| Evidence bytes, after `git gc --aggressive` | 4.38 MB |
| Diff references | 532,290 |
| — of which single-line | **529,599** |
| — of which ranged | **0** |
| — of which file events | 2,691 |
| Distinct comparison identities across all references | 1 |
| Distinct notes across all references | 133 |
| `saga.Load` | 3.7 s, 2.60 GB allocated |
| `coverage.Evaluate` | 4.4 s |
| `change-saga query overview` | 1,049.7 s, 1.93 GB RSS |

After the fixes, the same machine and source comparison measure:

| Evidence representation | `query overview` | Peak RSS |
| --- | ---: | ---: |
| Original 230.36 MB / 532,290 per-line references | **10.74 s** | **1.49 GB** |
| Canonical 2.33 MB / 5,330 dense references | **3.80 s** | **0.93 GB** |

Both responses report 532,290 total and covered atoms, zero uncovered,
overlapping, or stale atoms, and are byte-identical after removing the snapshot
hash (which intentionally includes the different Saga directory contents).

Every one of those 532,290 references restates the repository URL, the base
OID, the 64-hex product head identity, and the source path. The information
content is one comparison identity, 2,666 paths, 133 sentences, and a set of
line numbers.

## Defect 1 — `cover --changed-lines` never coalesces — **fixed**

The measurements below describe the behaviour before the fix. `--changed-lines`
now coalesces as described under "What the fix costs"; see
`changedLineSelectors` in `internal/cli/cover.go`.

`internal/cli/cover.go:317`

```go
path := filepath.ToSlash(record.Path)
for _, atom := range changes.Atoms {
    matchesPath := atom.Path == path || atom.OldPath == path || atom.NewPath == path
    if !matchesPath || atom.Kind == "line" && record.Side != "" && atom.Side != record.Side {
        continue
    }
    uris = append(uris, atom.URI)
}
```

`gitdiff.Read` emits one atom per changed line, each carrying a single-line URI
(`internal/gitdiff/gitdiff.go:360`, `:364`). `--changed-lines` copies those URIs
verbatim, so covering a 110-line new file writes 110 references of about 430
bytes each plus one `add` event — where `--lines 1-110` would have written one.

The manual path does build ranges: `parseRanges` in the same function turns
`--lines "1-40,60-80"` into ranged URIs (`internal/cli/cover.go:334`). Only the
derived path forgoes them.

This is not a hypothesis about how the saga was authored. **Zero of its 529,599
line references are ranged.** A human choosing selectors by hand does not miss
`--lines` 529,599 consecutive times; that distribution only comes from the
derived path.

The authoring skill points straight at it:

> Use `--changed-lines` only when every changed atom in the named file belongs
> to the same focused target.
>
> — `skills/change-saga/references/authoring.md:266`

That advice is correct, and following it is what produces a 230 MB saga.

### What the fix costs

Coalescing runs of consecutive changed lines inside `--changed-lines` is about
twenty lines. It is not "widening a selector," which the same skill rightly
forbids: a range built from consecutive atoms the flag already selected is
*dense*, so it matches exactly the atoms that were going to be written anyway
and cannot sweep in unrelated code.

It does change one thing, and this is the decision the fix requires. Today, if
line 2 of a covered file stops being a changed line in a later comparison, the
saga reports one stale reference. With `1-40` in place of forty references, the
range still matches lines 1 and 3–40, so **nothing is reported as stale**.
Coarser selectors mean coarser stale detection. Whether that is a loss or a
simplification is a maintainer's call, not a mechanical one.

Measured effect of doing it: **230.36 MB → 2.33 MB**, 532,290 references →
5,330, with byte-identical coverage — same atoms, same owners, same notes, same
overlaps, same orphans.

That decision was taken: coarser stale detection for derived coverage is
accepted. Ranges are built per identity (repository, base, head, path, side)
and only across gapless line numbers, so a range never spans a line the flag
did not select. The loss is smaller than it reads: head identity is a digest of
the whole product patch, so an edit that stops line 2 from being a changed line
also changes the head and orphans every reference in the comparison, ranged or
not.

## Defect 2 — `session.build` recomputes a path inside its innermost loop — **fixed**

`internal/reviewapp/session.go:121`

```go
for _, atom := range s.changes.Atoms {
    for _, assignment := range s.report.Ownership[atom.Key] {
        entries := s.selectors[assignment.Target]
        for i := range entries {
            entry := &entries[i]
            if entry.selector.EvidenceFile != cleanDiagnosticPath(assignment.DiffFile) || entry.diff != assignment.Diff {
```

`cleanDiagnosticPath` is `filepath.Clean` plus `filepath.ToSlash`
(`internal/reviewapp/session.go:565`), and it is called once per *inner*
iteration although its argument only changes with the assignment. On this saga
that is roughly **1.19e10 calls**.

Hoisting it above the inner loop changes no behaviour at all. It is the single
cheapest change available and it addresses most of the ~1,840 instructions per
iteration the profile implies.

`linkOwnership` now normalizes each distinct raw evidence path once, outside
selector lookup. The lookup itself uses the normalized path as part of a
`selectorKey` rather than reparsing it for every candidate.

Later benchmarking corrects one detail of this: the call does not allocate.
`filepath.Clean` returns its argument unchanged for the already-clean relative
paths coverage produces, and `filepath.ToSlash` is a no-op on darwin and Linux,
so `testing.AllocsPerRun` measures zero allocations a call. What it costs is
**81.7 ns** of CPU to reparse the path — about 172 ms of the 218 ms of scan in
the most concentrated shape of `BenchmarkLargeSagaSelectorConstruction`
(`internal/reviewapp/largesaga_bench_test.go`). The cost is real and the fix is
unchanged; it is a CPU cost rather than a garbage one, and on Windows, where
`ToSlash` rewrites separators, it would allocate as originally stated.

## Defect 3 — the same loop is quadratic — **fixed**

The inner `for i := range entries` is a linear scan for the selector matching
`(evidence file, diff index)` among every selector its target owns. With one
selector per line, the work is `sum over targets of atoms x selectors`, which is
`sum(atoms²)`.

Summing the measured per-target atom counts gives **1.19e10** scan steps. One
target alone — 37,700 atoms across 134 files — contributes 1.4e9.

A map from `(target, evidence file, diff index)` to the selector's index,
built once, makes it O(1). The slice elements are mutated in place and the
slice is never reallocated inside the loop, so an index map is safe.

That map is now built once by `selectorIndex`. A synthetic scale benchmark over
256, 1,024, and 4,096 selectors measured 4.8×, 17.4×, and 60.4× speedups over
the former scan; growing the input 16× changed construction from 254× slower to
20× slower. A regression test exercises an eightfold input increase and rejects
quadratic growth.

### Why this matters independently of defect 1

Ranged selectors make the same saga's scan about 1e8 steps — roughly 120× fewer
— and `query overview` drops from **1,049.7 s to 12.6 s**, an 83× improvement,
with identical reported coverage. That was measured by re-emitting the same
evidence as ordinary ranged v2 JSON and running the unmodified binary against
it.

Range authoring is the normal path and the selector index independently keeps
the reader linear if explicit URI input or a malformed producer nevertheless
creates per-line evidence.

## What was ruled out

An experiment tested whether a new, compact, semantically sharded evidence
encoding plus a disposable SQLite index was the answer. It is not, and the
measurements are recorded in
[`experiments/compact-connectors/docs/findings.md`](../experiments/compact-connectors/docs/findings.md).

The short version: the encoding reaches 1.46 MB and is provably equivalent, but
against *ranged v2 JSON* — which needs no format change — it wins 1.6× on raw
bytes, **nothing** in the Git object store (1.01 MB against 1,002.57 KB), and
nothing measurable in load or evaluate time. On a sparse saga it is a
regression: 5× larger deflated and 1.6× slower. It is not worth a format
change, a migration, a version bump, and a SQLite dependency.

One thing it did surface that ranges do not fix, recorded here because it is a
separate defect and not a size problem:

The experiment also surfaced a merge defect that is now fixed. `cover` used to
name records by timestamp, so two reviewers who explained the same selectors
differently never conflicted in Git. Generated names are now a hash of the
canonical selector set and deliberately exclude the note: unrelated selectors
write unrelated files, while different explanations for the same selectors
write the same path and require explicit reconciliation.

## The query API also avoids building detail-only state

`overview` and `children` return hierarchy and aggregate counts; they cannot
return atom ownership, individual gaps, fragment bodies, or reverse diff
lookups. They now request a summary-only review session. Coverage records one
small state value per atom in a contiguous slice, puts additional target owners
in a sparse overlap map, and never constructs the large string-keyed ownership
projection those focused operations need.

The coverage index also stores atom positions rather than copied atom structs.
For `gitdiff.Read` results it constructs the match reference from the atom's
already parsed fields and the shared comparison identity, avoiding another
parse of every long atom URI. Full sessions use the same positional index and
transfer completed owner slices into the report without copying them.

This is where cache locality materially helps: the hot per-atom state is a
linear array addressed by integer, not hundreds of thousands of independently
allocated map entries. It does not require a binary on-disk format; it is an
in-memory execution detail behind an unchanged query response.

## Measured, versus inferred

Measured: every baseline number in this document, the zero-ranged-reference
count, the 83× experiment with ranged evidence, the coverage equivalence
between encodings, the focused selector-index speedups, and the final 10.74 s /
3.80 s end-to-end query results above.
