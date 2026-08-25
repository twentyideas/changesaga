# Lazy, partitioned review generation

Status: **implementation in progress**

This design replaces the reviewer server's single eager comparison snapshot
with independently built, content-addressed parts. Its purpose is not merely to
move a long build behind a loading message. It is to make the amount of work,
memory, and response data proportional to what the reviewer actually opens.

The authored Saga remains a Git-native directory. Every derived `.data` file
described here lives in the user's Change Saga cache, never in the Saga or the
source repository.

## Decision

The server will not build the complete source comparison automatically.

- The root and narrative remain usable without any source index.
- Opening Code builds only a small changed-file catalog and the selected file.
- Opening an explanation reads its prose immediately; its linked-code summary
  is a separate lazy request.
- Opening Coverage authorizes a bounded, streaming coverage build. It may
  process the whole comparison because that view asks a whole-comparison
  question, but it must never retain the whole comparison.
- Interactive work outranks speculative or aggregate work.
- Decoded parts are retained by a byte-budgeted LRU, not forever.

SQLite is not part of the design. The access keys are already exact—source
path, target, and cursor—and independently addressable files give us atomic
publication, random access, and merge neutrality without another dependency or
durability system.

## Why the current implementation still stalls

The bounded SSR work fixed the browser payload, but `reviewSnapshot` still
joins four large structures behind one readiness gate:

1. the fully loaded Saga, including every evidence reference;
2. every Git diff atom and display line;
3. the complete atom-to-target ownership graph; and
4. file and target reverse indexes over that graph.

The managed server starts that build as soon as it starts listening. Code,
Coverage, and fragment rendering call `requestSnapshot`, so they either wait
for the same global object or retry until it exists. The root can render while
this happens, but the process is still consuming CPU, allocation bandwidth,
and garbage-collection time.

On the unreduced Daylight Saga, the current result is:

| Measurement | Current result |
| --- | ---: |
| Root response | 122,729 B in 78 ms |
| Complete background generation | 12 s |
| Cold peak server RSS | 3.45 GiB |
| Warm peak server RSS | 3.02 GiB |

This is deferred generation, not lazy generation.

The source repository demonstrates that a lazy path is practical. Against the
same Daylight comparison:

| Git operation | Wall time | Peak RSS | Output |
| --- | ---: | ---: | ---: |
| Changed-file names/status | 0.38 s | 12.7 MiB | 137,960 B |
| Per-file added/deleted counts | 1.21 s | 48.2 MiB | 146,957 B |
| Largest known file (`pnpm-lock.yaml`) | 0.01 s | 12.5 MiB | 996,488 B patch |
| Small selected file | 0.16 s | 7.0 MiB | one file |

The comparison has 2,666 files and 532,290 atoms, but no interactive request
needs all of their object representations at once.

## Required invariants

The implementation is complete only if all of these remain true:

1. **Narrative independence.** Root, chapter, fragment prose, landmarks,
   annotations, comments, and review decisions never wait for source or
   coverage generation.
2. **Immediate navigation.** Selecting a workspace tab changes the visible
   panel synchronously. Any unavailable content is represented by a visible,
   accessible loading state inside the selected panel.
3. **Request-proportional work.** Code catalog work is proportional to changed
   files; file work is proportional to one file; target work is proportional
   to one target's selector records.
4. **No global atom heap.** No steady-state object contains every atom, every
   full diff URI, or a string-keyed entry for every ownership edge.
5. **Bounded retention.** Decoded parts are evicted according to their measured
   byte cost. An adversarial browsing session cannot grow memory without bound.
6. **Exact identity.** Every generated diff URI carries the same canonical
   repository/base/head identity as the comparison. A cache hit cannot be
   stale.
7. **Disk-first reviews.** Comments, replies, annotation edits, approvals, and
   file-review decisions commit to Saga records before their in-memory overlay
   changes.
8. **Merge neutrality.** Derived parts never appear below a `.saga` directory
   or a source checkout.
9. **Disposable derivation.** Deleting any cache part changes latency only,
   never the resulting review.
10. **No guessed totals.** Until an exact coverage rollup exists, the UI says
    that it is calculating; it does not display partial numbers as final.

## Runtime architecture

```text
                         ┌───────────────────────────┐
GET /, chapters, prose ─▶│ narrative + review overlay│
                         └───────────────────────────┘
                                      │ independent
                                      │
Code tab ───────────────▶ source catalog ─────▶ selected file part
                                      │                  │
                                      │                  ├─ first 50 rows
                                      │                  └─ later cursors
                                      │
fragment linked code ───▶ target selector part ─▶ file summaries / drawers
                                      │
Coverage tab ───────────▶ mapping catalog ──────▶ streaming rollup
                                      │                  │
                                      └──────────▶ per-file / per-target pages

All derived arrows publish external cache parts atomically. Only decoded parts
inside a fixed byte budget remain resident.
```

The server owns four generations rather than one `reviewSnapshot`:

### Narrative generation

This is the existing outline/narrative path plus the review overlay. It reads
hierarchy, content, landmarks, annotations, and review records, but it does not
read `___diffs` or invoke Git.

`/api/fragment` changes in one important way: it returns prose and landmarks
without attached-code summaries. The fragment includes a placeholder whose
`/api/target-code?target=…` request loads those summaries separately. A slow
comparison can therefore never stop someone reading the report.

### Source generation

The source generation is keyed only by canonical repository identity and the
exact source comparison. It is unaffected by Saga mapping or review changes.

It contains:

- one small changed-file catalog;
- independently generated file-diff parts; and
- the shared repository/base/head identity used to reconstruct diff URIs.

For a committed head, revision resolution and the source fingerprint are cheap.
The existing product-patch identity must be hashed as a stream rather than read
with `cmd.Output`, so exact identity does not require retaining the complete
patch. A `WORKTREE` comparison remains process-local unless and until all of
its bytes can be pinned exactly.

The catalog is also the authority for path identity. For a rename or copy it
records both the old and new path, and a file-part request passes the complete
pair to Git. All catalog and file commands use one canonical set of diff flags.
This avoids a subtle failure mode where asking Git for only the displayed path
changes rename detection and produces a patch that differs from the same file
inside the complete comparison. Binary files, mode-only changes, symlinks, and
submodules are represented explicitly rather than forced through text rows.

### Mapping generation

The mapping generation is keyed by the structural evidence fingerprint plus
the source generation identity. It contains compact selector ranges indexed by
source file and narrative target. It does not contain source text or expanded
diff URIs.

Changing coverage creates a new mapping generation while reusing every source
file part. Changing a comment creates neither.

The authoring path already coalesces consecutive changed lines into ordinary
v2 ranges. That is the canonical authored representation: the compact-connector
experiment measured ranged v2 JSON at 2.33 MiB and connector shards at 1.46
MiB, with effectively identical load/evaluation time and approximately equal
Git object-store size. This design therefore does not introduce a new authored
Saga format.

The unreduced 236 MiB per-line Saga remains a stress fixture for streaming
memory behavior, not the format new authoring should produce.

### Coverage rollup generation

Coverage totals are a derived part of a mapping generation, not a prerequisite
for Code or narrative content. The rollup streams the Git comparison one file
at a time, merges that file's compact selectors, publishes any missing file
parts, updates aggregate counters, and releases the file working set before
continuing.

This is the only operation authorized to touch every changed atom. It begins
only when Coverage is opened or an explicit query asks a whole-comparison
question. It must be cooperative: after each source file it checks
cancellation and yields to queued interactive work.

## External cache layout

The existing `snapshotcache` bucket and content-addressed generation names are
retained. A generation becomes a directory of independently published parts:

```text
<user-cache>/change-saga/snapshot-index/<saga-bucket>/
  source/<source-generation>/
    ___index.json
    identity.data
    catalog.data
    files/0a/00000a31.data
    files/7f/00007f22.data

  mapping/<mapping-generation>/
    ___index.json
    dictionary.data
    by-file/0a/00000a31.data
    by-target/00/00000017.data
    orphans.data
    coverage-summary.data
```

These files are not authored artifacts. They are not committed, ignored, or
merged. The cache directory can be redirected with `CHANGE_SAGA_CACHE_DIR` and
deleted at any time.

The source and mapping directories are separate so a one-line comment or
coverage edit cannot duplicate cached source patches. The two hexadecimal path
characters shard directory entries; they are not semantic.

### Part publication

The current generation store atomically renames one complete staging directory.
A lazy generation needs atomicity at both levels:

1. Publish the small generation manifest and dictionaries with a directory
   rename.
2. Build each missing part into a same-directory temporary file.
3. `fsync` the file, rename it to its final name, then sync the directory.
4. Treat absence as not built and a malformed header/checksum as disposable
   damage to replace.

Part existence is the completion record. There is no central bitmap or lockfile
rewritten every time a file is opened. In-process single-flight is keyed by
`(generation, part kind, part ID)` so concurrent requests build a part once.

## Compact representation

Compression makes repeated strings cheap on disk but does not make decoded Go
objects cheap. The cache representation therefore stores identities once and
uses integer IDs everywhere else.

Stable dictionaries assign sorted `uint32` IDs to:

- source paths;
- narrative target URNs;
- distinct evidence notes; and
- comparison identities when more than one is present.

A line selector is represented conceptually as:

```go
type selectorRange struct {
    FileID   uint32
    TargetID uint32
    NoteID   uint32
    Start    uint32
    End      uint32
    Side     uint8
}
```

File display data uses contiguous columns and a content blob:

```go
type filePart struct {
    Kinds          []uint8
    OldLines       []uint32
    NewLines       []uint32
    ContentOffsets []uint32
    ContentLengths []uint32
    Content        []byte
}
```

Ownership uses compressed-row storage: `OwnerOffsets[n+1]` names each atom's
span inside one flat `OwnerIDs` array. There is no `map[string][]Assignment`
and no full URI per atom. Keys and `saga-diff://` URIs are reconstructed only
for rows crossing the HTTP boundary or being written as review anchors.

The `.data` encoding is private and versioned. A changed schema increments the
format in the generation key, so no migration or backward reader is required.
It uses a fixed header, little-endian integer columns, length-delimited byte
regions, and a checksum. This is not a new Saga format; it is equivalent to a
versioned object file that can always be rebuilt.

Large file parts include a row-chunk table. A cursor request can `ReadAt` the
one chunk containing its next page instead of decoding the whole file. The
initial implementation may read a whole small file part, but the format and
tests must preserve bounded chunk access for large files.

## Work scheduling and backpressure

There is no unprompted full-comparison goroutine at server startup.

Work enters one of three priorities:

1. **Interactive:** the catalog, file, target, or narrative part visible now.
2. **Visible prefetch:** the next cursor or a linked-code part likely to be
   opened from the current panel.
3. **Aggregate:** coverage rollup and optional warming.

At most one aggregate worker and one interactive Git subprocess run at once.
Aggregate loops yield between files. An HTTP request holds a waiter reference
to its job; when all waiters cancel, work that has not published anything is
cancelled unless it is within a small completion threshold.

The scheduler reports stage and progress without making the client infer them
from elapsed time:

```json
{
  "narrative": "ready",
  "catalog": "ready",
  "coverage": {"state":"building", "completed_files":418, "total_files":2666},
  "resident_bytes": 187432960,
  "queued": {"interactive":0, "prefetch":1, "aggregate":1}
}
```

## HTTP and browser behavior

Workspace tab selection is optimistic and synchronous:

1. update `aria-selected`, history, and the visible panel;
2. paint the panel's skeleton/spinner;
3. start or join the scoped request; and
4. replace only that panel when the response arrives.

The browser must be able to switch back to Saga, open a chapter, and read a
fragment while Code or Coverage is building. A late response is generation-
checked and cannot replace a newer selection.

Scoped endpoints return `202 Accepted` only for their own missing part. They
include `Retry-After` plus:

```text
X-Change-Saga-Load-Scope: catalog | file | target | coverage
X-Change-Saga-Load-State: queued | building
X-Change-Saga-Progress: 418/2666
```

The relevant panel displays those states through `role=status`, `aria-live`,
and `aria-busy`; it never leaves the previous view visible after its tab was
selected. Errors are scoped and retryable.

Endpoint responsibilities become:

| Endpoint | Required generation |
| --- | --- |
| `/`, `/api/section`, `/api/fragment`, `/api/locate` | narrative only |
| `/api/code` | source catalog only |
| `/api/file-diff` | one source file part; mapping part only when linked state is requested |
| `/api/target-code` | one target mapping part; source catalog for file summaries |
| `/api/coverage?mode=code` | mapping catalog plus requested file mapping parts |
| `/api/coverage?mode=saga` | mapping catalog plus requested target part |
| `/api/coverage-summary` | coverage rollup, progressively observable |

## In-memory retention

The runtime keeps the narrative, dictionaries, source catalog, and review
overlay resident. File, target, and coverage page parts live in a shared LRU
whose capacity is measured in decoded bytes.

Initial defaults:

- 256 MiB decoded-part budget;
- no part larger than 64 MiB retained after its request;
- oversized parts are streamed and immediately released; and
- one request pins its parts until rendering completes, after which eviction
  may proceed.

The budget is observable and configurable, but correctness cannot depend on a
larger value. Tests use a tiny budget to force eviction and prove that reloading
a part gives the same response.

## Review mutations

The existing mutation boundary remains:

1. validate through `MutationIndex`;
2. acquire the Saga writer lock;
3. revalidate and commit the review record to disk;
4. reload only the compact review overlay; and
5. acknowledge the durable write even if overlay refresh must retry.

Review records never invalidate source or mapping generations. A diff comment
stores the canonical URI reconstructed from the source identity and the row's
compact fields. File-review state is joined onto file summaries at response
time rather than copied into every cached file part.

Coverage-authoring mutations do select a new mapping generation. Existing
source catalog and file parts remain reusable because their key excludes Saga
evidence.

## Failure and invalidation behavior

- A source revision change selects a new source generation.
- A Saga evidence change selects a new mapping generation.
- Narrative edits invalidate only the affected narrative cache/fingerprint.
- Review edits invalidate only the review overlay.
- A damaged part is discarded and rebuilt; other parts remain available.
- A failed file build makes that file retryable and does not poison Code.
- A failed coverage rollup leaves narrative, Code catalog, and existing file
  parts usable.
- Server restart reopens dictionaries/catalogs and pays only for parts that are
  subsequently requested.
- Pruning removes old generations as directories. It never writes inside the
  Saga and never mutates the active generation in place.

## Acceptance budgets

The unreduced Daylight Saga remains the mandatory local scale fixture. The
compact ranged equivalent is also tested so normal authored shape does not
regress while optimizing the adversarial one.

### Interaction budgets

| Behavior | Budget |
| --- | ---: |
| Root response | ≤ 200 KiB and ≤ 250 ms loopback |
| Root comparison builds | exactly 0 |
| Tab visual activation | ≤ 100 ms; spinner/skeleton visible by next animation frame |
| Chapter/fragment prose while coverage is building | ≤ 250 ms loopback |
| Cold changed-file catalog | ≤ 2 s and ≤ 128 MiB peak RSS |
| First ordinary file page after catalog | ≤ 500 ms |
| Largest known file's first page | ≤ 2 s and ≤ 128 MiB incremental RSS |
| Code/Coverage/file response | ≤ 200 rows and ≤ 256 KiB |

### Process budgets

| State | Budget |
| --- | ---: |
| Root-only idle RSS after GC | ≤ 256 MiB |
| Catalog plus one ordinary file after GC | ≤ 512 MiB |
| Any Daylight interaction or rollup peak RSS | ≤ 1 GiB |
| Decoded-part cache | ≤ configured byte budget |
| Simultaneous aggregate workers | ≤ 1 |

The peak is a hard acceptance gate, not a projection. If the implementation
cannot meet it, it does not ship with a larger default and an explanatory note;
the representation or access pattern must change.

### Deterministic structural tests

Performance timing is supplemented with rules that fail deterministically:

- root and narrative endpoints invoke no Git commands and open no `___diffs`;
- Code catalog invokes no full patch parser;
- one file request parses no other source file;
- concatenated lazy file results are semantically identical to the existing
  full parser for additions, deletions, renames, copies, binary files,
  mode-only changes, symlinks, and submodules;
- one target request opens no unrelated target's evidence;
- coverage retains no prior file working set as it advances;
- leaving a building view does not delay Saga navigation;
- 16 concurrent requests for one part build it once;
- cursor walks return every row exactly once under forced LRU eviction;
- review mutations perform zero source or mapping rebuilds;
- cache paths are outside the Saga and source checkout; and
- a damaged or interrupted part is never observable as complete.

## Implementation sequence

Each step is independently testable and should land without maintaining two
competing global snapshot paths longer than necessary.

1. **Interaction contract.** Remove automatic comparison warming. Make view
   activation synchronous, expose scoped loading progress, and make fragment
   prose comparison-free by splitting linked-code summaries.
2. **Source catalog.** Introduce the source-generation key and build the file
   catalog from bounded Git metadata commands. Serve `/api/code` from it.
3. **Lazy file parts.** Add path-scoped Git diff reading, compact file parts,
   atomic part publication, cursor reads, and the byte-budgeted LRU. Move
   `/api/file-diff` off `reviewSnapshot`.
4. **Mapping catalog.** Load ranged evidence into file/target dictionaries and
   compact range parts without expanding URIs. Serve `/api/target-code` and
   linked evidence from those parts.
5. **Coverage streaming.** Evaluate coverage file by file, publish progressive
   summary state, and move both coverage directions off the global report.
6. **Delete the global snapshot.** Remove the eager `gitdiff.ChangeSet`,
   `coverage.Report.Ownership`, monolithic persisted JSON generation, and the
   background build state.
7. **Scale acceptance.** Run browser, race, unit, eviction, corruption, and
   real Daylight budgets. Record cold/warm results in `docs/performance.md`.

## Explicit non-goals

- No SQLite or third-party storage dependency.
- No cache artifact committed to Git.
- No migration guarantee for disposable cache generations.
- No speculative preload of every file.
- No browser-side database containing the comparison.
- No claim that compression alone solves decoded memory layout.
- No attempt to make a whole-comparison Coverage request perform less than a
  whole-comparison calculation; the requirement is that it be streaming,
  cancellable, observable, and memory-bounded.

## Completion criterion

The change is done when a reviewer can open the Daylight Saga, move through its
narrative immediately, switch into Code and see a catalog within the catalog
budget, open one file without materializing unrelated files, switch away while
Coverage is still calculating, and never drive the server above the measured
1 GiB acceptance ceiling.
