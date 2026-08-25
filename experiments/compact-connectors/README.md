# Compact saga connectors — experiment

An experiment in encoding Change Saga coverage evidence so that a
whole-codebase saga is small and fast to query without becoming an opaque
blob that Git cannot merge.

**Nothing here is wired into `change-saga`.** It is a separate Go module with
its own `go.mod`, so `go build ./...`, `go vet ./...`, and `go test ./...` at
the repository root never see it and the reference implementation keeps its
dependency-light guarantee. The one dependency this experiment adds —
`modernc.org/sqlite`, for the disposable index — lives here and only here.

Read [`docs/format.md`](docs/format.md) for the encoding and
[`docs/findings.md`](docs/findings.md) for the measurements, the tradeoffs, and
the open questions.

## The problem, in one measurement

The saga this was built against documents an entire codebase:

| | |
| --- | ---: |
| Evidence files | 2,666 |
| Evidence bytes | 230.4 MB |
| Diff references | 532,290 |
| Distinct comparison identities in those references | **1** |
| Distinct notes in those references | **133** |

Every one of the 532,290 references restates the repository, the base OID, the
product head identity, and the source path, and each changed line is its own
reference. The information content is a few thousand facts.

`change-saga query overview` on it takes **17 minutes 30 seconds** and peaks at
**1.93 GB** resident.

**The result, up front:** most of that is not a format problem. Re-emitting the
same evidence as ordinary v2 JSON with *ranged* references — a feature the
format already has — takes it to 2.33 MB and 12.6 s, with no format change at
all. The connector encoding adds 1.6× on raw bytes over that, nothing once Git
has packed the objects, and nothing measurable in load or evaluate time. What
it does add is merge behaviour v2 cannot express. See
[`docs/findings.md`](docs/findings.md#9-recommendation) for why this stays an
experiment and what to do instead.

## What it does

- **Encoding.** One human-readable `*.connectors` file per (ownership target,
  source path), with the comparison identity and the notes hoisted into a
  header behind content-addressed aliases, and contiguous changed lines written
  as dense ranges.
- **Compatibility.** Shards live inside the existing `___diffs/` directory,
  which a v2 loader reads for `*.json` only. A dual-encoded saga is therefore
  fully readable by an unmodified `change-saga`. Dropping the JSON requires a
  manifest version a v2 reader rejects loudly.
- **Index.** A SQLite database, cached outside the saga so it cannot be
  committed, rebuilt per shard when a shard changes, and correct to delete at
  any time.
- **Equivalence.** Everything is checked against the unmodified
  `coverage.Evaluate`: same atoms, same owners per atom, same notes per atom,
  same overlaps, same orphans, same target rollups.

## Running it

```sh
cd experiments/compact-connectors
go test ./...                     # round trip, index invalidation, cold/warm, git merges
go test ./bench -bench . -benchmem
go build -o /tmp/compactctl ./cmd/compactctl
```

Against a saga of your own:

```sh
SAGA=/path/to/your.saga
REPO=/path/to/source/checkout

/tmp/compactctl stats    --saga "$SAGA" --repo "$REPO"
/tmp/compactctl migrate  --saga "$SAGA" --out /tmp/compact.saga --granularity ranges
/tmp/compactctl verify   --legacy "$SAGA" --connectors /tmp/compact.saga --repo "$REPO"
/tmp/compactctl index    --saga /tmp/compact.saga --discard
/tmp/compactctl query    --saga /tmp/compact.saga --op owners --path libs/thing.ts --side new --line 42
/tmp/compactctl bench    --legacy "$SAGA" --connectors /tmp/compact.saga --repo "$REPO"
/tmp/compactctl packsize --tree "$SAGA" --extension .json
```

`migrate` and `packsize` only ever read the saga you point them at; every
output goes to a fresh directory.

## Layout

| Path | What |
| --- | --- |
| `connector/` | the encoding: writer, strict parser, canonical ordering, coalescing, and the bridge to `saga.DiffReference` |
| `migrate/` | v2 → connectors, connectors → v2, sharding, and the owner walk |
| `reader/` | loads a connector saga into a `*saga.Saga` for the unmodified coverage engine |
| `equiv/` | per-atom semantic comparison of two coverage evaluations |
| `index/` | the disposable SQLite index |
| `bench/` | portable Go benchmarks over a generated fixture |
| `mergetest/` | real `git merge` scenarios |
| `cmd/compactctl/` | the measurement harness |
