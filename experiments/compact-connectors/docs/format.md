# Connector encoding, draft 1

Status: **experiment**. Nothing in this document is part of the Change Saga
format. It describes an alternative encoding of v2 coverage evidence that was
measured, and that [`findings.md`](findings.md#9-recommendation) recommends
*against* adopting. Read that first: most of the size and latency it recovers
turns out to be recoverable with no format change at all, by using v2's
existing ranged references.

## Why

A v2 evidence record stores one fully realized `saga-diff://` URI per atom:

```json
{
  "version": 2,
  "diffs": [
    {
      "uri": "saga-diff://v1/line?base=0769f8da…&end=1&head=product-fd9ef81e…&path=libs%2Fdaylight%2Fruntime-update%2F.eslintrc.json&repository=https%3A%2F%2Fgithub.com%2Ftwentyideas%2Fdaylight.git&side=new&start=1",
      "note": "Implements signed runtime discovery, compatibility, staging, activation, and rollback."
    }
  ]
}
```

Every URI restates the repository, the base OID, the product head identity, and
the path; every reference restates the note. On the linked whole-codebase saga
that is 532,290 references carrying **one** comparison identity and **133**
distinct notes, across 230 MB.

That redundancy is deliberate and load bearing in v2: SPEC.md §5 says a diff
link "does not inherit repository or revision state from the directory
containing it", which is what stops evidence from silently matching a similar
path in a different comparison. The connector encoding keeps that guarantee at
the granularity of a file rather than of a reference.

## Shape

One file per **(ownership target, source path)**, named
`<slug>-<8 hex of sha256(source path)>.connectors`, written into the owning
package's existing `___diffs/` directory:

```text
delivery.chapter/release-path.fragment/___landmarks/runtime-channel.landmark/___diffs/
└── daylight-runtime-update-src-lib-contracts-ts-00fcc281.connectors
```

```text
saga-connectors 1
owner urn:change-saga:daylight-codebase:fragment:release-path:landmark:runtime-channel
source libs/daylight/runtime-update/src/lib/contracts.ts

comparison 73edf4
  repository https://github.com/twentyideas/daylight.git
  base 0769f8dad20076bf18029042aa537fc9877b6c6c
  head product-fd9ef81e202df5ff524a24fa3961686465fcf8bb8f56a70e72e0826603333f83

note 13692f
  Implements signed runtime discovery, compatibility, staging, activation, and rollback.

73edf4 13692f event add libs/daylight/runtime-update/src/lib/contracts.ts
73edf4 13692f lines new 1-110
```

That file replaces 110 URIs of roughly 430 bytes each.

### Grammar

```text
file        := "saga-connectors" SP version NL directive* record*
directive   := owner | source | comparison-block | note-block
owner       := "owner" SP <target URN> NL                     -- exactly one
source      := "source" SP <encoded path> NL                  -- exactly one
comparison-block := "comparison" SP alias NL
                    ("  repository" | "  base" | "  head") SP <encoded> NL
note-block  := "note" SP alias NL ("  " <encoded line> NL)+
record      := alias SP (alias | "-") SP body NL
body        := "lines" SP ("old" | "new") SP range
             | "event" SP <event> SP <encoded token>
             | "event" SP "rename" SP <encoded token> SP <encoded token>
range       := <n> | <n> "-" <m>            -- ascending, one based, m > n
```

Blank lines and `#` comment lines are ignored.

- **Aliases are content addressed**, the first three bytes of
  `sha256` over the value. Inserting a note therefore never renumbers an
  existing record line — which is exactly what would turn every concurrent
  addition into a whole-file conflict.
- **Records are canonically sorted** by comparison, then events before lines,
  then side (`new` before `old`), then range, then note. The same evidence
  always produces the same bytes, so two engines and two sides of a merge agree
  on where a record belongs.
- **A single line is written `5`, never `5-5`**, so one atom has exactly one
  spelling.
- **Escaping is minimal**: `\\`, `\n`, `\r` everywhere, plus `\_` for a space
  and `\t` for a tab in the fields that share a line with other fields. Paths
  and prose stay readable.

### Ranges are dense by construction

`Coalesce` merges only runs of *consecutive* line numbers that agree on
comparison, side, and note. Every integer inside a written range was therefore
an owned atom, which makes the transformation reversible: reading a shard at
`Exact` granularity expands `new 1-110` back into 110 single-line references
that are byte-identical to what v2 held.

This is what preserves stale-reference reporting. A range that merely *spanned*
a region would report one orphan where v2 reported forty, and a partially
matching range would report none at all.

## Compatibility

The shards live in `___diffs/` beside the JSON, and a v2 loader reads only
`*.json` from that directory. The compatibility matrix follows from that:

| Reader | Saga | Result |
| --- | --- | --- |
| v2 | v2 JSON only | works, unchanged |
| v2 | JSON **and** shards (`version: 2`) | works, reports byte-identical coverage; the shards are invisible |
| v2 | shards only, `version: 3` | **loud failure**: `unsupported version 3` |
| connector-aware | v2 JSON only | works, reads the JSON |
| connector-aware | JSON and shards | works; shards win, JSON ignored, so nothing double counts |
| connector-aware | shards only | works |

The migration is therefore two steps, and the dangerous one announces itself:

1. `migrate --dual` writes shards beside the JSON. Both readers work and report
   the same thing. This state is safe to commit and safe to revert.
2. `migrate` (drop the JSON) plus a `version: 3` manifest. A v2 reader now
   fails with an unsupported-version error rather than silently reporting a
   fully documented change as entirely undocumented — the failure mode this
   step exists to prevent, and the one
   `TestConnectorOnlySagaMustAnnounceItselfWithAVersionV2Rejects` pins down.

`migrate --back` returns a connector saga to ordinary v2 JSON at any time.

## Merge behaviour

Measured with real `git merge` in `mergetest/`:

| Scenario | Result |
| --- | --- |
| Two reviewers cover different source files | different shards, clean |
| Two reviewers cover different regions of one file, shard already has several records | same shard, clean line merge |
| Two additions land at the same anchor in a short shard | conflict, under 12 lines, both claims in plain text |
| Two reviewers claim the same range with different notes | conflict, under 12 lines, both explanations in plain text |
| `*.connectors merge=union` in `.gitattributes` | no conflict ever, but both claims survive as an overlap nobody chose |

For contrast, `TestV2EvidenceMergesWithoutConflictButManufacturesAnOverlap`
records what v2 does with the last case: because `change-saga cover` names every
record `<slug>-<timestamp>.json`, the two records merge cleanly *and both
survive*, so the same lines silently acquire two owners and coverage reports an
overlap neither reviewer authored. v2's absence of conflicts is not merge
friendliness; it is a merge that cannot express disagreement.

## What the encoding does not change

- Atom ownership, overlaps, orphans, notes, target rollups, and the comparison
  identity. `equiv.Compare` checks all of it against the unmodified
  `coverage.Evaluate`, per atom.
- The owning directory. A shard sits where its v2 records sat.
- Claims, verifications, threads, approvals, and file-review events. Only
  coverage evidence is re-encoded.
