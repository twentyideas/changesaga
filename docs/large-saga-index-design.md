# Does the large-saga server need SQLite for first-run indexing?

**No.** The server's first-run cost is not shaped like anything a query engine
makes faster, the memory it holds is not reduced by persisting it, and the two
properties a cache actually has to get right — atomic creation and exact
invalidation — are already solved in this repository by `internal/store`.

This document records what was measured, what the numbers rule out, and the
design that follows from them. It also names the defect the measurements
surfaced, which is a bigger effect than any indexing decision and is not fixed
here.

Everything below was measured on an Apple M3 Pro, darwin/arm64, Go 1.26, warm
filesystem, against this repository's own `change.saga` — 38,209 changed atoms
across 183 files and 154 targets, in 814 saga files. `docs/performance.md`
calls it the reference workload; it is roughly nine times the generated
fixture the byte budgets use.

## 1. What first run actually costs

`app.snapshot` builds the state every request needs, and `buildSnapshot` is
three steps (`internal/server/snapshot.go:106`):

| Step | Time | What it is |
| --- | ---: | --- |
| `saga.Load` | 223 ms | reading and validating 814 saga files |
| `gitdiff.Read` | 302 ms | **two `git diff` subprocesses** plus parsing their output |
| `coverage.Evaluate` | 37 ms | evaluating 38,209 atoms against the saga's selectors |
| **Total** | **562 ms** | |
| live heap held afterwards | 65 MB | |
| cumulative allocation to get there | 260 MB | |

The shape of that total is the first result. **60% of it is `git` and the
parsing of `git`'s output, and 40% is reading the saga.** Only 37 ms — 6.6% —
is the derivation step, and derivation is the only part of the pipeline a
query engine has any claim on.

An index cannot remove the other 93%. To know whether a cached answer is still
valid you must ask Git what the comparison is, and to know what the saga claims
you must read the saga. Those are the costs, and they are inputs to any cache,
not things a cache can be built out of.

Scaling is consistent with the whole-codebase saga in
[`docs/large-saga-diagnosis.md`](large-saga-diagnosis.md): 65 MB at 38K atoms
is ~1.7 KB/atom, which projects to ~900 MB at 532K atoms, against the 0.93 GB
RSS measured there. The curve is linear in atoms and the constant is the atom
itself.

## 2. What a persisted cache would buy, and what it would not

The question a cache answers is "can the 562 ms be skipped on a later run?" So
the derived state was serialized with `encoding/gob` — stdlib, no dependency —
and read back:

| | Bytes | Encode | Decode |
| --- | ---: | ---: | ---: |
| `gitdiff.ChangeSet` (38,209 atoms) | 20.21 MB | 36.5 ms | 21.2 ms |
| `coverage.Report.Ownership` | 11.99 MB | 12.7 ms | 16.3 ms |
| **Round trip** | **32.20 MB** | **49.2 ms** | **37.5 ms** |

Against a 562 ms rebuild, a 37.5 ms read is a **15× win**, and it is available
from a flat file. That is the case *for* persistence, and it is a real one.

Two things it is not:

**It is not a case for SQLite.** The win comes from not re-running `git` and
not re-parsing the saga. Nothing in it comes from a query engine, indexes, or
SQL. The consumer wants the whole set in memory, and `gob.Decode` puts it there
in 37 ms. SQLite would do the same work through a query planner, a page cache,
and a row-to-struct materialisation, and arrive at the same 65 MB.

**It does not bound memory.** Decoding rehydrates exactly the heap that was
serialized. A cache changes *when* you pay 65 MB, never *whether* you do.
Bounded memory is a property of the access pattern, not of the storage engine
— which is the next section.

Note also what the 32.20 MB is made of: atom `Content` — the actual source
lines — is 1.59 MB, **8%**. The other 92% is `Key` and `URI` strings restating
the comparison identity and path on every atom. That is the same redundancy
`docs/large-saga-diagnosis.md` found in the evidence files, appearing again in
memory. It is worth knowing before choosing a cache format, and it is worth
more than choosing a database.

## 3. Bounded memory is a partitioning decision

The server already reads its state in two very different shapes:

- **`/api/file-diff?file=X`** needs the atoms of one file. At 38,209 atoms over
  183 files that is ~209 atoms — **0.5% of the set**.
- **`GET /`** needs aggregates over everything: `changesByTarget`,
  `makeCoverageManifestView`, review-progress rollups.

Bounding the first is easy and does not need a database: write the derived
state as **one file per source path**, and a file-diff request becomes one
`os.ReadFile` of a few kilobytes. Resident memory becomes proportional to what
was asked for. This is exactly what a directory of independently readable parts
gives, and it is why the foundation in §5 stores directories.

Bounding the second is a different problem, and it is **out of scope here**:
the aggregates are what the root page renders, and the brief for this work
excludes changing root HTML rendering. The path is known and already proven on
the CLI side — `docs/large-saga-diagnosis.md` describes summary-only sessions
that keep one small state value per atom in a contiguous slice instead of a
string-keyed ownership projection. That is an in-memory execution change behind
an unchanged response, and it does not need an index either.

So on the axis the brief cares most about, the honest finding is: **SQLite
would not have helped, and the thing that does help is available without it.**

## 4. The dependency question, which is decided already

Even if the measurements were neutral, two constraints in this repository are
not.

**Cross-compilation.** `scripts/build-release.sh:167` builds every release
artifact with `CGO_ENABLED=0` for darwin, linux, and windows. `mattn/go-sqlite3`
is cgo. It is not a tradeoff for this project; it is incompatible with how the
binary ships.

**Dependency budget.** `go.mod` has exactly one requirement, `goldmark`.
This is not an accident of history — it is a governing commitment:

> **The core stays deterministic, dependency-light, and local.**
>
> — `GOVERNANCE.md:56`, repeated at `CONTRIBUTING.md:19`

`modernc.org/sqlite` is pure Go and would build, but it pulls nine modules, and
the compact-connectors experiment already recorded that as a blocking cost
rather than an open one:

> **SQLite is a real dependency.** `modernc.org/sqlite` pulls in nine modules.
> It is confined to this experiment module and would have to be justified
> separately — or replaced — before any of this reached the CLI, whose
> governance commits to staying dependency-light.
>
> — `experiments/compact-connectors/docs/findings.md` §7

That experiment measured a disposable SQLite index over saga evidence and
recommended against integrating it. Its numbers are not the server's numbers,
but its conclusion survives the change of subject, and this document adds the
server-side measurements that were missing from it.

**A third cost is specific to sagas.** SQLite is not one file. It is a database
plus `-wal`, `-shm`, and `-journal` siblings, with its own locking and its own
durability model. This repository already has one, in `internal/store`:
same-directory temp file, `fsync`, atomic rename, directory sync, and an
advisory saga lock that deliberately adds no artifact to the Git-native saga
tree. Introducing a second durability model next to that one is a cost paid
forever, in exchange for query features nothing here asked for.

## 5. What to build instead

A **disposable, content-addressed, file-partitioned generation store** outside
every saga. Four rules, each of which the brief named:

### Invalidation: the key *is* the fingerprint

There is no invalidation step. A generation is addressed by the inputs it was
derived from, so changed inputs are a different address, which misses. A miss
is always safe. A hit cannot be stale, because a hit means the inputs are
byte-identical to the ones that produced it.

The two fingerprints needed already exist and are already computed on every
request — `treeFingerprint` and `sourceFingerprint`
(`internal/server/snapshot.go:138`, `:171`). No new freshness logic is
introduced, and no new way to get freshness wrong.

Inputs that cannot be described exactly make the key **invalid**, and an
invalid key is never stored and never found. That is the existing `WORKTREE`
rule, kept.

### Atomic creation: populate a stage, publish with one rename

A generation is built in a staging directory that is a sibling of its
destination and published with a single `os.Rename`. A reader observes a
complete generation or nothing. A failed build leaves no directory; an
interrupted one leaves only a staging directory the next prune removes.

This is the discipline `store.CommitDir` already applies inside a saga, applied
outside one. `store.WriteJSON` and `store.SyncDir` are reused directly.

### Progress and "building" responses

A generation has three observable states — absent, building, ready — and
concurrent callers for the same key **build once and share the result**. That
is what lets a handler answer a request that arrives mid-build with progress
instead of either blocking for seconds or starting a second copy of the same
work. Today `app.snapshot` holds a mutex across the entire rebuild
(`internal/server/snapshot.go:60`), which is correct but cannot tell a reviewer
anything while it does so.

### Merge neutrality: nothing is ever written inside a saga

A saga is a Git-native directory that people merge. Derived bytes inside one
would be committed by accident, would conflict on every rebuild, and would put
an opaque binary in a format designed to be reviewed as text. Generations live
under `os.UserCacheDir()/change-saga/snapshot-index/`, keyed by the saga's
absolute path, following the placement `internal/cli/server_runtime.go:232`
already uses for detached server state. `CHANGE_SAGA_CACHE_DIR` redirects it.

The saga tree is untouched, so **`.gitignore` needs no entry, `git status`
sees nothing new, and merge behaviour is unchanged** — which is merge
neutrality by construction rather than by convention.

### Bounded disk

Generations accumulate as a reviewer moves between comparisons, so a bucket is
pruned to the newest few per saga, and the whole cache can be deleted at any
time. Deleting it changes only how long the next answer takes, never what the
answer is.

## 6. What was built here

`internal/snapshotcache` implements the foundation above — key, lifecycle,
atomicity, single-flight, prune, and discard — and the reviewer server now uses
it for its structural/source generation. The generation stores the parsed Git
comparison, display lines, coverage report, ownership, and target reverse index
as JSON outside the saga. Cold builds are explicit in stdout and through a
small HTTP 503/status response; a complete generation is published atomically
before the full review page can use it.

Mutable review records are a separate in-memory generation. Threads, replies,
anchor/state edits, decisions, and file-review events commit to their saga files
first; only after that succeeds does the server swap a compact overlay onto the
immutable structure. Review paths and saga-repository `HEAD` participate only
in the overlay fingerprint, so recording or committing a comment cannot rerun
Git diff or coverage work. A restart reloads those authoritative review files
while reusing the content-addressed structural/source generation.

Twelve tests pin the rules, each stated as the failure it prevents, and all
pass under `-race`:

| Test | Rule |
| --- | --- |
| `TestSecondBuildReusesTheFirst` | unchanged inputs build once |
| `TestChangedInputsMissRatherThanServeStaleState` | each fingerprint invalidates; superseded generations stay addressable |
| `TestAFailedBuildPublishesNothing` | a partial generation is never published, and failure is not sticky |
| `TestAnInterruptedBuildLeavesNoObservableGeneration` | no directory is readable mid-write; debris is pruned |
| `TestConcurrentRequestsBuildOnce` | 16 concurrent callers, one build, one shared result |
| `TestInputsThatCannotBeDescribedExactlyAreNeverCached` | `WORKTREE` and unreadable inputs are never stored |
| `TestDiscardingTheCacheChangesOnlySpeed` | disposability: rebuilt bytes equal discarded bytes |
| `TestPruneBoundsGenerationsPerSaga` | disk stays bounded; survivors stay correctly addressed |
| `TestAnotherSagaIsUnaffectedByPrune` | one saga's churn cannot evict another's |
| `TestGenerationsAreNeverWrittenInsideTheSaga` | merge neutrality, asserted against the saga tree listing |
| `TestDirEnvRedirectsTheCacheRoot` | tests and operators stay off the real cache |
| `TestADamagedGenerationRebuildsRatherThanServingItself` | damage reads as absence **and** can be rebuilt over |

The last one found a defect during development and is the reason it exists. A
damaged generation directory correctly missed on lookup, but its name was still
taken, so the rebuild's rename failed and the key was wedged permanently —
repairable only by deleting a cache directory the reviewer was never told
about. `publish` now removes damage and retries the rename once.

## 7. The finding that matters more than any of this

**A `WORKTREE` head disables the snapshot cache completely, and every request
pays a full rebuild.**

`sourceFingerprint` returns an empty string for `WORKTREE` because uncommitted
file contents are not described by any cheap probe
(`internal/server/snapshot.go:171`). That is the correct call — a stale review
is worse than a slow one. But the consequence is that on a saga being authored
against a working tree, which is the ordinary authoring configuration, the
cache never populates. At this repository's scale that is **562 ms per
request**; opening ten files in the drawer is 5.6 seconds of pure rebuild, and
the whole-codebase saga projects to roughly 3–10 s *each*.

No index fixes this, because the input genuinely changes. What fixes it is a
cheap probe that describes a working tree exactly enough to compare, and one is
available at the same standard the code already accepts elsewhere:
`treeFingerprint` hashes name, size, and modification time, and
`git status --porcelain=v2` over this repository measures **40 ms** against the
562 ms it would save — a 14× win on the path that currently has none.

That is a separate change with its own correctness argument about how exactly a
working tree must be pinned before a review may be cached, and it is
deliberately not made here. It is recorded because it is larger than the
question this document was asked, and answering only the question asked would
have buried it.

## 8. Recommendation

1. **Do not add SQLite.** 93% of first-run cost is `git` and saga reading,
   which no index touches; the derived 6.6% persists to a flat file that reads
   back in 37.5 ms; persistence does not bound memory; and `CGO_ENABLED=0`
   releases plus a one-dependency `go.mod` make the two candidate drivers
   respectively impossible and expensive.
2. **Persist the derived snapshot, partitioned per source path**, on the
   `internal/snapshotcache` foundation. That is the 15× first-run win and the
   bounded-memory win for `/api/file-diff`, together, with no new dependency.
3. **Fix the `WORKTREE` cache hole** (§7). It is worth more than items 1 and 2
   on the configuration sagas are actually authored in.
4. **Leave the root page's aggregates alone** until 1–3 are measured. If it is
   still the constraint, the summary-only projection from
   `docs/large-saga-diagnosis.md` applies, and it is an in-memory change behind
   an unchanged response — still not an index.

## Measured, versus inferred

**Measured:** every number in §1, §2, and §7's 40 ms probe, on this
repository's own saga; the twelve tests in §6.

**Inferred:** the ~900 MB projection at 532K atoms in §1, which extrapolates a
linear per-atom constant and is corroborated by, not measured as, the 0.93 GB
in `docs/large-saga-diagnosis.md`; and §7's 3–10 s per-request projection,
which scales §1 by the same factor.
