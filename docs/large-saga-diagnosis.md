# Why a whole-codebase saga is 230 MB and takes 17 minutes

Status: **diagnosis only. Nothing described here is fixed.** This records what
was measured, what causes it, and the decisions each fix would require. It is
not a design and it is not a plan.

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

Every one of those 532,290 references restates the repository URL, the base
OID, the 64-hex product head identity, and the source path. The information
content is one comparison identity, 2,666 paths, 133 sentences, and a set of
line numbers.

## Defect 1 — `cover --changed-lines` never coalesces

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

## Defect 2 — `session.build` recomputes a path inside its innermost loop

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

Later benchmarking corrects one detail of this: the call does not allocate.
`filepath.Clean` returns its argument unchanged for the already-clean relative
paths coverage produces, and `filepath.ToSlash` is a no-op on darwin and Linux,
so `testing.AllocsPerRun` measures zero allocations a call. What it costs is
**81.7 ns** of CPU to reparse the path — about 172 ms of the 218 ms of scan in
the most concentrated shape of `BenchmarkLargeSagaSelectorConstruction`
(`internal/reviewapp/largesaga_bench_test.go`). The cost is real and the fix is
unchanged; it is a CPU cost rather than a garbage one, and on Windows, where
`ToSlash` rewrites separators, it would allocate as originally stated.

## Defect 3 — the same loop is quadratic

The inner `for i := range entries` is a linear scan for the selector matching
`(evidence file, diff index)` among every selector its target owns. With one
selector per line, the work is `sum over targets of atoms x selectors`, which is
`sum(atoms²)`.

Summing the measured per-target atom counts gives **1.19e10** scan steps. One
target alone — 37,700 atoms across 134 files — contributes 1.4e9.

A map from `(target, evidence file, diff index)` to the selector's index,
built once, makes it O(1). The slice elements are mutated in place and the
slice is never reallocated inside the loop, so an index map is safe.

### Why this matters independently of defect 1

Ranged selectors make the same saga's scan about 1e8 steps — roughly 120× fewer
— and `query overview` drops from **1,049.7 s to 12.6 s**, an 83× improvement,
with identical reported coverage. That was measured by re-emitting the same
evidence as ordinary ranged v2 JSON and running the unmodified binary against
it.

But that only helps sagas authored *after* defect 1 is fixed. Sagas that
already exist on disk keep their per-line references, and for them the quadratic
is the whole problem. **Defect 1 protects future sagas; defects 2 and 3 rescue
the ones already written.**

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

**`cover` record filenames are timestamped** (`generatedCoverageName`,
`internal/cli/cover.go:497`), so two reviewers who explain the *same* lines
differently never conflict in Git. Both records survive the merge, the lines
acquire two owners, and coverage reports an overlap neither reviewer authored.
A content-addressed record name would put the second author's record on the
first one's path and force Git to reconcile them. Not attempted.

## Measured, versus inferred

Measured: every number in this document, the zero-ranged-references count, the
83× improvement from ranged evidence, and the coverage equivalence between the
two encodings.

Inferred: that hoisting `cleanDiagnosticPath` and replacing the linear scan with
a map will produce a specific speedup. The scan-step arithmetic and the 83×
result make the direction certain; the magnitude has not been measured, because
neither fix has been written.

Not investigated: whether anything else in a `query overview` — Git attribution,
`buildSnapshot` — becomes the bottleneck once this loop stops being one.
