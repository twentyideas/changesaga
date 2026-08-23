# Findings

Status: **experiment**. These are measurements and open questions, not a
proposal. Nothing here has been integrated into `change-saga`.

Everything below was measured on an Apple M3 Pro, darwin/arm64, Go 1.26.1,
against `daylight-codebase.saga` — a saga documenting an entire codebase — and
its source checkout. Both fixtures were read only; every generated tree was
written to a scratch directory.

Saga semantics were read through `saga.Load`, `gitdiff.Read`,
`coverage.Evaluate`, and `diffuri.Parse`/`Build`. No fixture metadata was
grepped or hand-parsed.

**Methodology.** Every wall-clock number is a warm-filesystem measurement: the
saga had been read at least once before it was timed, so what is being compared
is CPU and allocation, not disk. "Cold" and "warm" below always refer to the
derived index, never to the page cache. Process-level time and peak resident
size come from `/usr/bin/time -l`; in-process figures come from
`runtime.MemStats` and are labelled as cumulative allocation where that is what
they are. Latencies under a second are medians of repeated runs; the two
`query overview` figures are single runs, because one of them takes 17 minutes.
Byte sizes are binary — 1 MB is 2^20 bytes — which is what the tools print.

## 1. The baseline

| | |
| --- | ---: |
| Evidence files (`___diffs/*.json`) | 2,666 |
| Evidence bytes | 230.36 MB |
| Diff references | 532,290 |
| Changed atoms | 532,290 |
| Covered / uncovered / overlapping / orphaned | 532,290 / 0 / 0 / 0 |
| Narrative targets | 139, of which **118** own evidence |
| Distinct source paths | 2,666 |
| Distinct comparison identities across all references | **1** |
| Distinct notes across all references | **133** |

The saga is fully mapped and completely clean. Its size is not information; it
is 532,290 restatements of one repository URL, one base OID, one product head
identity, 2,666 paths, and 133 sentences.

`saga.Load` takes **3.7 s** and allocates **2.60 GB** cumulatively to reach a
355 MB live heap. `gitdiff.Read` takes 2.2 s and `coverage.Evaluate` 4.4 s.

## 2. Two separate wins, and they are not the same size

The compaction has two independent parts, and it matters which is which,
because only one of them needs a format change.

### 2.1 Ranges — available in v2 today

SPEC.md §5 already lets a line URI carry `start` and `end`. This saga does not
use it: every changed line is its own reference. Coalescing runs of consecutive
lines that share an owner and a note, and re-emitting ordinary v2 JSON, gives:

| | Files | Bytes | References |
| --- | ---: | ---: | ---: |
| As authored | 2,666 | 230.36 MB | 532,290 |
| Same evidence, ranged v2 JSON | 2,666 | **2.33 MB** | 5,330 |

**98.9× smaller with no format change at all.** This is the single largest
result in the experiment, and it is an authoring-tool finding rather than a
format finding: whatever wrote this saga emitted one reference per line where
the format already allowed one per run.

### 2.2 The connector encoding — the remaining factor

| | Files | Bytes | Records |
| --- | ---: | ---: | ---: |
| As authored (v2 JSON) | 2,666 | 230.36 MB | 532,290 |
| Ranged v2 JSON | 2,666 | 2.33 MB | 5,330 |
| Connector shards, ranges | 2,666 | **1.46 MB** | 5,330 |
| Connector shards, exact (one record per line) | 2,666 | 15.45 MB | 532,290 |

Against as-authored v2 the connector encoding is **158× smaller**; against
ranged v2 it is **1.6×**. At exact granularity — one record per changed line,
which preserves v2 selector identity exactly — it is still **14.9×** smaller
than as-authored v2.

**82% of the 1.46 MB is the per-shard header.** 2,666 shards each restate the
owner URN, the source path, one comparison block, and their notes: 1.19 MB of
header against 0.27 MB of records. That is the dominant remaining cost and the
subject of the first open question below.

### 2.3 Compression changes the ranking

Git stores deflated blobs, so raw bytes overstate the result. Measured per file
with `gzip -9`, on the generated fixture (`bench.TestFixtureEncodingCost`),
where each shard holds exactly one record:

| | Raw | Deflated |
| --- | ---: | ---: |
| v2 JSON, 144 files | 367,360 B | 55,863 B |
| Connector shards, 1,024 files | 380,608 B | 289,968 B |

On that shape the connector encoding is **5× larger after compression**. The
reason is structural: v2's redundancy is concentrated in a few large files that
deflate extremely well, while a 342-byte header repeated across 1,024 tiny
files compresses badly and nothing in per-file deflate can share it.

That is the encoding's worst realistic case — the generated fixture gives every
target exactly one four-line range per source file, so a header has nothing to
amortise against. The whole-codebase saga is the opposite shape, at roughly 200
references per shard, and goes the other way:

| | Raw | Deflated |
| --- | ---: | ---: |
| v2 JSON, 2,666 files | 230.36 MB | 4.15 MB |
| Connector shards, 2,666 files | 1.46 MB | 975.16 KB |

**158× raw, 4.4× deflated.** The invariant that survives both shapes is the one
the test asserts: a connector record costs about **30 bytes** against a v2
reference's **359**, and the crossover is at roughly one reference per shard.

### 2.4 What Git actually stores

`compactctl packsize` commits each evidence tree into a fresh repository and
runs `git gc --aggressive`, which is the only measurement that accounts for
both deflate and cross-blob delta compression:

| Tree | Files | Raw | Git object store |
| --- | ---: | ---: | ---: |
| As authored, v2 JSON | 2,666 | 230.36 MB | **4.38 MB** |
| Ranged v2 JSON | 2,666 | 2.33 MB | **1,002.57 KB** |
| Connector shards | 2,666 | 1.46 MB | **1.01 MB** |

Two things change once Git has packed the trees.

**The 158× becomes 4.3×.** The as-authored saga costs 4.38 MB in the object
store, not 230 MB; delta compression across 2,666 near-identical files eats
almost all of the redundancy the encoding was designed to remove. A repository
carrying this saga is not in trouble.

**Ranged v2 and connector shards are the same size.** 1,002.57 KB against
1.01 MB — ranged v2 is marginally smaller. The connector encoding's raw-byte
advantage over ranged v2 does not survive contact with Git's object store.

Raw bytes are still not nothing: they are what a checkout writes, what
`git status` walks, and what every tool that opens the saga has to parse and
hold. That is where §1's 3.7 s load and 2.60 GB of allocation, and §3.1's
1.93 GB resident, come from. But the
argument for a new encoding cannot be repository size, because repository size
was never the problem.

## 3. Query latency

### 3.1 The existing engine, as authored

`change-saga query overview` on this saga takes **1,049.7 s — 17 minutes 30
seconds** of which 1,000 s is user CPU, and peaks at **1.93 GB** resident
(`/usr/bin/time -l`, 21.9 trillion instructions retired).

That is not I/O. `reviewapp.session.build` walks every atom, and for each
assignment linearly scans every selector its target owns:

```go
for _, atom := range s.changes.Atoms {
    for _, assignment := range s.report.Ownership[atom.Key] {
        entries := s.selectors[assignment.Target]
        for i := range entries {          // linear in the target's selectors
```

**The cost is quadratic in selectors per target, not in atoms.** With one
selector per line, the scan is `sum over targets of atoms x selectors` =
`sum(atoms^2)`. Summing the measured per-target atom counts gives **1.19e10**
scan steps; one target alone (37,700 atoms across 134 files) contributes
1.4e9. With ranged selectors the same saga has 5,330 records across 118
targets, which is about **1e8** scan steps — roughly 120x fewer, against an
observed 83x end-to-end speedup once the rest of the pipeline is included.

This is why a saga that documents each line separately falls off a cliff that a
saga using ranges never reaches, and why fixing the scan is worth doing
independently of any format work.

### 3.2 The same engine, ranged v2

Reverse-migrating the connector shards to **ordinary v2 JSON with ranged
references** produces a saga any unmodified `change-saga` can read. Running the
same binary, the same command, and the same source checkout against it:

| Saga | `query overview` | Peak RSS |
| --- | ---: | ---: |
| As authored, 532,290 single-line references | 1,049.7 s | 1.93 GB |
| Same evidence, 5,330 ranged references | **12.6 s** | 1.31 GB |

**83× faster, from a change to authored granularity alone.** The two runs
report identical coverage: 532,290 current atoms at the root, 388 and 3,420 on
the two overview fragments, and 149,812 / 128,771 / 110,380 / 75,615 / 33,524 /
30,380 across the six chapters.

This is the experiment's most consequential measurement, and it is not a
measurement of the connector encoding.

### 3.3 Whole-saga load and evaluate

All four encodings, on an idle machine, with the Git comparison read once and
the same unmodified `coverage.Evaluate`. Every row reports 532,290 atoms, all
covered, no overlaps, no orphans.

| Encoding | References/records | Load | Evaluate |
| --- | ---: | ---: | ---: |
| v2 JSON, as authored | 532,290 | 3.66 s | 4.25 s |
| Ranged v2 JSON | 5,330 | **0.26 s** | **2.40 s** |
| Connector shards, ranged selectors | 5,330 | **0.27 s** | **2.28 s** |
| Connector shards, expanded to v2 selector identity | 532,290 | 1.08 s | 4.32 s |

Two things fall out of this table.

**Ranged v2 JSON and connector shards are indistinguishable** — 0.26 s against
0.27 s to load, 2.40 s against 2.28 s to evaluate. Once the selector count is
the same, the encoding stops mattering to the engine.

**Reading connectors at exact granularity is still 3.4× faster than reading the
v2 JSON** that describes the same 532,290 selectors, because it parses 1.46 MB
and synthesises the URIs instead of parsing and unescaping 230 MB of them. That
is the encoding's genuine parse advantage, and it only appears when a caller
insists on per-line selector identity.

### 3.4 The derived index

The SQLite index answers a narrower set of questions than a review session
does — counts, per-target rollups, and "who owns this line" — so these numbers
are not a like-for-like replacement for §3.1. They are what those specific
questions cost once the shards have been indexed once.

| | |
| --- | ---: |
| Cold build, 2,666 shards | 264–298 ms |
| Index size on disk | 2.54 MB |
| Warm open (stat 2,666 shards, reuse all) | 19–29 ms |
| Overview counts | 5.0 ms warm, 4.7 ms cold |
| Per-target rollup over 118 targets | 7.5 ms |
| "Which target owns this line" | 0.5–7.6 ms |

Incremental invalidation is per shard: editing one shard rebuilds one shard and
reuses the other 2,665 (`TestIndexRebuildsOnlyTheShardsThatChanged`), and
deleting the index changes only how long the next answer takes, never the
answer (`TestDiscardedIndexRebuildsToTheSameAnswers`).

The index is written to `os.UserCacheDir()/change-saga/connector-index/`, keyed
by the absolute saga path. It is never inside the saga, so it cannot be
committed by accident, and `CHANGE_SAGA_INDEX_DIR` redirects it for tests.

### 3.5 The portable fixture

`go test ./bench -bench . -benchmem -benchtime=3x`, on the generated fixture —
145 fragments, 4,096 atoms, one four-line range per (target, source path), and
therefore one record per shard:

| Benchmark | Time | Allocated |
| --- | ---: | ---: |
| `LoadAndEvaluateLegacy` | 82.3 ms | 32.3 MB |
| `LoadAndEvaluateConnectors` | 134.6 ms | 115.7 MB |
| `IndexColdBuild` | 99.0 ms | 76.1 MB |
| `IndexWarmOverview` | 1.09 ms | 1.8 KB |
| `IndexOwnersOfLine` | 0.60 ms | 1.8 KB |
| `IndexIncrementalRefresh` | 26.1 ms | 4.1 MB |

On this shape the connector encoding is **1.6× slower to load and evaluate**
than the v2 JSON it replaces, for the same reason it is larger there: 1,024
shards with 1,024 headers, each holding a single record, read at exact
granularity. It is a fair worst case and it is recorded as one.

## 4. Semantic equivalence

`compactctl verify` runs both encodings through the unmodified
`coverage.Evaluate` and compares them per atom:

```text
legacy     load 3.660s  evaluate 4.246s  atoms 532290 covered 532290 overlaps 0 orphans 0
connectors load 1.076s  evaluate 4.316s  atoms 532290 covered 532290 overlaps 0 orphans 0
equivalent: every atom has the same owners, the same notes, and the same overlap and stale verdicts
```

It passes at both granularities. At `ranges` — where 532,290 single-line
selectors have become 5,330 ranged ones — every atom still has the same owners,
the same notes, and the same overlap and stale verdicts, because the ranges are
dense.

`equiv.Compare` checks the summary, `complete`, the resolved base and head OIDs,
the repository identity, the uncovered set, the saga-change set, the per-target
rollups, and then for every atom: its owning targets, the note each owner
attached, and whether it is overlapping. Evidence file paths and selector
spellings are deliberately excluded — they must be allowed to change — but
nothing a reviewer is told is.

The same comparison passes on a generated fixture at both granularities, in both
directions (`TestConnectorRoundTripPreservesCoverageSemantics`), including the
reverse migration back to ordinary v2 JSON.

## 5. Merge behaviour

Measured with real `git merge`, in `mergetest/`:

| Scenario | v2 JSON | Connector shards |
| --- | --- | --- |
| Different source files | clean (new file each) | clean (different shards) |
| Different regions of one file | clean (new file each) | clean (line merge in one shard) |
| Two additions at the same anchor in a short shard | clean (new file each) | conflict, ≤12 lines, both claims readable |
| Same lines, different explanations | **clean — and wrong** | conflict, ≤12 lines, both explanations readable |

The fourth row is the point. `change-saga cover` names every record
`<slug>-<timestamp>.json`, so two reviewers who explain the same lines
differently never conflict: both records survive, the lines acquire two owners,
and coverage reports an overlap neither person authored
(`TestV2EvidenceMergesWithoutConflictButManufacturesAnOverlap`). v2's freedom
from conflicts is not merge friendliness — it is a merge that cannot express
disagreement, and it grows the directory without bound.

Two properties of the encoding do the work:

- **Content-addressed aliases.** A note's alias is a hash of its text, so adding
  a note never renumbers an existing record line
  (`TestAddingANoteDoesNotRewriteExistingRecordLines`). Positional aliases would
  make every concurrent addition a whole-file conflict.
- **Canonical sort.** Record order is a pure function of record content, so both
  sides of a merge put a new record in the same place.

`*.connectors merge=union` in `.gitattributes` removes every conflict and still
produces a parseable shard, but it keeps both claims — converting a
disagreement into an overlap nobody chose
(`TestUnionMergeDriverRemovesConflictsButKeepsBothClaims`). It is recorded as a
measured option, not a recommendation.

## 6. Compatibility

Shards live in the existing `___diffs/` directory. A v2 loader reads only
`*.json` there and skips everything else, so:

| Reader | Saga | Result |
| --- | --- | --- |
| v2 | v2 JSON only | works, unchanged |
| v2 | JSON **and** shards, `version: 2` | works; coverage byte-identical to before (`TestDualEncodedSagaIsUnchangedForAnUnmodifiedV2Reader`) |
| v2 | shards only, `version: 3` | loud failure: `unsupported version 3` |
| connector-aware | v2 JSON only | works |
| connector-aware | JSON and shards | works; shards win, JSON ignored, nothing double counts |
| connector-aware | shards only | works |

This was checked at full scale as well as in the tests. A dual-encoded copy of
the whole-codebase saga — 230.36 MB of JSON plus 1.46 MB of shards — loads
under the plain `saga.Load` with `valid: true`, zero issues, and exactly the
same coverage as before: 532,290 atoms, all covered, no overlaps, no orphans.
The shards are invisible to a v2 reader.

The migration is two steps and the dangerous one announces itself. Without the
version bump, a v2 reader loads a connector-only saga happily and reports every
one of its 532,290 atoms as uncovered — a fully documented change presented as
entirely undocumented. `TestConnectorOnlySagaMustAnnounceItselfWithAVersionV2Rejects`
asserts both halves: that the silent failure is real, and that the version bump
converts it into an error.

`migrate --back` returns a connector saga to ordinary v2 JSON at any time, which
is what makes the whole experiment reversible.

## 7. Tradeoffs

- **The header is now the format.** 82% of the compact bytes are per-shard
  headers. That is the price of a shard that can be read, moved, and reviewed on
  its own, and of SPEC.md's rule that evidence does not inherit revision state
  from its directory. It is a real cost.
- **Small, wide sagas gain nothing and actively lose.** When a target owns one
  range in one file, a shard is one record behind a 342-byte header. On the
  generated fixture the encoding is 1.03× larger raw, 5× larger deflated, and
  1.6× slower to load and evaluate. It is for sagas where a target owns many
  lines of a file, and it is a regression where it does not.
- **The headline number is an artefact of how the fixture was authored.** 158×
  raw compares the encoding against a saga that declined to use ranges. Against
  a saga that uses them, the encoding is 1.6× raw and 1.0× packed. Quoting the
  158× without that context would be misleading.
- **Range coalescing changes selector granularity.** Atom ownership, notes,
  overlaps, and — because ranges are dense by construction and expand exactly —
  stale detection are all preserved. What changes is that one orphan is reported
  where v2 reported forty, because forty single-line selectors became one range.
  The `exact` granularity avoids this entirely and is still 14.9× smaller.
- **Conflicts now exist.** v2 never conflicts. The encoding trades that for
  conflicts that are small, readable, and only appear when two people genuinely
  disagree or add at the same anchor.
- **SQLite is a real dependency.** `modernc.org/sqlite` pulls in nine modules.
  It is confined to this experiment module and would have to be justified
  separately — or replaced — before any of this reached the CLI, whose
  governance commits to staying dependency-light.
- **Shard count is unchanged.** 2,666 files before, 2,666 after. The encoding
  makes each file small; it does not reduce how many there are.

## 8. Open questions

1. **Should the comparison identity be hoisted saga-wide?** It would cut the
   1.46 MB to roughly 0.27 MB plus one file, and would make advancing the head
   rewrite one file instead of 2,666. It costs SPEC.md §5's guarantee that a
   link cannot silently match a different comparison, and it reintroduces
   exactly the frequently rewritten aggregate file this design set out to avoid.
   Not attempted; the arithmetic above is a projection from measured header
   bytes, not a measurement.
2. **How much of this is worth doing at all, given §2.1?** 98.9× of the 158×
   comes from using a v2 feature that already exists, and §3.3 shows that once
   both encodings use ranges their load and evaluate times are within 5% of each
   other. On the measurements taken, the connector encoding's remaining case
   rests on merge behaviour alone.
3. **Is the `reviewapp` quadratic a separate bug?** It looks like one. A map
   keyed by (evidence file, diff index) would remove the linear scan without any
   format change. That should probably be measured and fixed independently of
   this experiment, because it would change §3.1 substantially.
4. **Is there a smaller fix for the merge defect in §5?** `change-saga cover`
   names records by timestamp, which is what lets two records for the same atoms
   coexist. A content-addressed record name would make the second author's
   record land on the first one's path, so Git would have to reconcile them. If
   that works it removes the encoding's last remaining advantage. Not attempted.
5. **Does the encoding hold up under `WORKTREE` heads and multi-repository
   evidence?** The format supports several comparison blocks per shard, but
   nothing here exercises a saga with evidence from two repositories or a
   `WORKTREE` head, whose identity changes on every edit.
6. **Is one shard per (target, source path) the right shard?** It is what v2
   already produces here, so it was kept. A different sharding — per target, or
   per directory — would trade merge isolation against header amortisation, and
   was not measured.
7. **Would a saga-wide note table help more than a comparison table?** There are
   133 distinct notes across the whole saga and they are already hoisted per
   shard, but a shard that repeats a 90-byte note it shares with 200 others is
   still paying for it. Not measured separately from question 1.

## 9. Recommendation

**Keep this an experiment, and do not integrate it.** The measurements do not
justify it, and two changes that are much smaller do.

The experiment set out to show that a compact sharded format plus a disposable
index makes whole-codebase sagas small and fast. It does — but the comparison
that matters is not against the saga as authored, it is against **ranged v2
JSON**, which the format already permits today:

| | As authored | Ranged v2 | Connector shards |
| --- | ---: | ---: | ---: |
| Working-tree bytes | 230.36 MB | 2.33 MB | 1.46 MB |
| Git object store | 4.38 MB | 1,002.57 KB | 1.01 MB |
| `query overview`, unmodified CLI | 1,049.7 s | 12.6 s | — |
| `saga.Load` | 3.66 s | **0.26 s** | 0.27 s ranged / 1.08 s exact |
| `coverage.Evaluate` | 4.25 s | **2.40 s** | 2.28 s ranged / 4.32 s exact |
| Format change required | — | **none** | new encoding, migration, version bump |
| New dependencies | — | **none** | `modernc.org/sqlite` (+9 modules) |

Ranged v2 gets 99% of the size win and all of the 83× latency win for free.
Against it, the connector encoding adds 1.6× raw bytes, nothing at all in the
object store, and — at the granularity a reviewer actually needs — nothing
measurable in load or evaluate time either, in exchange for a format change, a
two-step migration, a version bump, and a SQLite dependency the project's
governance explicitly does not want.

What the encoding does have that ranged v2 does not is **merge behaviour**
(§5). v2 cannot express two reviewers disagreeing about the same lines: it
merges both records cleanly and reports an overlap neither of them authored.
That is a real defect and the connector encoding fixes it. It is also a much
narrower problem than the one this experiment was scoped around, and it may
have a much smaller fix — deterministic, content-addressed record filenames in
`change-saga cover` would remove the duplicate-record failure without a new
encoding at all.

### What to do instead

1. **Teach the authoring path to emit ranged references.** This is the whole
   result: 99% of the bytes and 83× of the latency, with no format change,
   no migration, and no compatibility question. It is also strictly better for
   readers, because the format already supports it.
2. **Fix the selector scan in `reviewapp.session.build`.** The linear scan in
   §3.1 is quadratic in selectors per target. A map keyed by (evidence file,
   diff index) removes it. That is what makes the as-authored saga survivable
   rather than merely making a well-authored one fast.
3. **Make `change-saga cover` record names deterministic.** That addresses the
   spurious-overlap merge defect in §5 directly.
4. Revisit this encoding only if, after 1–3, a whole-codebase saga is still too
   slow or still merges badly. The code, the tests, and the migration are here
   and reversible if that day comes.
