# Living Saga implementation map

Status: **implementation planning; vocabulary is not yet normative**

This note maps a low-conflict path from the current review-oriented v2 format
to a Saga that can also carry evolving requirements and an executable work
plan. It does not freeze the format. The first implementation wave must settle
the decisions called out below before schemas, commands, or persisted examples
are merged.

The proposed architecture keeps four ideas separate:

1. **Requirements** state what the change must accomplish.
2. **Relations** connect stable entities and declare their semantics by type.
3. **Work plans** group independently addressable work items into waves and
   append progress and pivot history.
4. **Readiness** is a deterministic projection of the current graph. It is not
   persisted, and it is not approval, correctness, coverage, or mergeability.

That separation matters. If a single `plan.json` owns requirements, waves,
edges, and status, nearly every useful parallel edit conflicts in Git and every
reader must load the entire system to answer one question.

### Working assumptions

The rest of this map assumes that requirement definitions may be edited in
place under a stable ID; progress events attach to work items; pivots explain
why authored definitions or relations changed but do not apply those changes;
and readiness answers whether work can begin or continue. Requirement
acceptance, reviewer approval, coverage completeness, and merge readiness stay
independent. It also assumes that a browser UI is a later slice, not part of
the first format and API delivery. Wave 0 may change these assumptions, but it
must replace them with explicit semantics before implementation starts.

## Decisions to freeze before implementation

These choices affect both the requirements API and work-plan API. They belong
in one contract commit, not independent feature branches.

| Decision | Recommended starting point | Why it must be explicit |
| --- | --- | --- |
| Format version | Introduce a v3 Saga container; keep v2 readable | A current v2 reader rejects unknown root `___` directories. Adding living metadata while still claiming v2 would be a silent compatibility break. |
| Stable identities | Add `requirement`, `relation`, `wave`, and `work-item` Change Saga URNs | Paths can move; relations, pivots, and API cursors need durable identifiers. |
| Storage root | One `___living/` root with independent subdirectories | It requires one root-loader integration and keeps unrelated entities in unrelated files. |
| Requirement vocabulary | Freeze kinds, lifecycle states, and whether acceptance is explicit or derived | A requirement being implemented, verified, accepted, and approved are different facts. |
| Relation vocabulary | Freeze each type's direction, legal endpoint kinds, transitivity, symmetry, and readiness effect | Readiness must never guess that every edge blocks work. |
| Progress states | Freeze the state machine and terminal-state policy | `complete`, `cancelled`, and `blocked` cannot have transport-specific meanings. |
| Wave semantics | Treat order as display order unless an explicit gating relation says otherwise | Inferring dependencies from numeric order makes reordering behavior-changing. |
| Pivot semantics | Keep a pivot as append-only rationale and affected-target history; graph changes remain ordinary entity/edge edits | A prose decision record should not secretly be a second mutation language. |
| Readiness contract | Return a state plus direct blockers, transitive blocker paths, and graph diagnostics | A Boolean hides cycles, missing endpoints, explicit blocks, and why work is ready. |
| Cycles | Reject cycles in readiness-gating edges during validation | Picking an arbitrary cycle member as “the blocker” is nondeterministic and unactionable. |
| Time ordering | Use `(created_at, id)` only for event histories; use stable files for authored definitions | Clock ordering is appropriate for append-only events, but concurrent edits to the same authored requirement should conflict visibly. |

The initial relation table should be small. The following is a candidate, not a
format commitment:

| Type | From | To | Readiness-gating | Transitive | Notes |
| --- | --- | --- | --- | --- | --- |
| `depends_on` | work item or wave | work item or wave | yes | yes | The only initial edge followed by readiness. |
| `implements` | work item | requirement | no | no | Traceability, not proof that the requirement is satisfied. |
| `refines` | requirement | requirement | no | yes for navigation only | Establishes requirement decomposition without inventing progress. |
| `supersedes` | same entity kind | same entity kind | no | yes for history | A pivot may cite this edge, but does not create it implicitly. |
| `conflicts_with` | allowed same-kind pairs | allowed same-kind pairs | no | no; symmetric | A diagnostic fact, not an automatic scheduling policy. |

The registry for a relation type should own all five columns. Scattered
`switch` statements will drift when another type is introduced.

## Proposed on-disk shape

Use one file per authored entity or event, and never store membership as an
array in a shared wave or root manifest.

```text
change.saga/
├── saga.json
├── ... existing narrative and review packages ...
└── ___living/
    ├── requirements/
    │   └── checkout-idempotency.requirement.json
    ├── relations/
    │   └── implement-checkout-idempotency.relation.json
    ├── waves/
    │   └── persistence.wave.json
    ├── work-items/
    │   └── add-idempotency-key.work-item.json
    ├── progress/
    │   └── 20260831T184200.000000000Z-a1b2c3d4.json
    └── pivots/
        └── 20260831T190500.000000000Z-e5f6a7b8.json
```

Definitions use stable, semantic filenames. Concurrent edits to the *same*
requirement, relation, wave, or work item should cause a Git conflict because
they are competing edits to one fact. Edits to different facts merge cleanly.
Progress and pivots are append-only event files created exclusively with
time-plus-random IDs, so concurrent updates normally add disjoint paths.

A minimal contract candidate is:

| Record | Stable authored fields | References / event fields |
| --- | --- | --- |
| Requirement | `version`, `id`, `title`, `statement`, `kind`, acceptance criteria | Optional narrative targets; lifecycle must use the frozen policy |
| Relation | `version`, `id`, `type`, `from`, `to`, `rationale` | Both endpoints are full target URNs |
| Wave | `version`, `id`, `title`, `order`, `goal` | No embedded work-item list |
| Work item | `version`, `id`, `title`, `description`, `wave` | The wave is a URN; requirement traceability uses relations |
| Progress event | `version`, `id`, `work_item`, `state`, `summary`, `created_at` | Latest event is selected by `(created_at, id)` |
| Pivot event | `version`, `id`, `summary`, `rationale`, `targets`, `created_at` | Targets are existing requirement, relation, wave, or work-item URNs |

Fields that express the same fact must not be duplicated. In particular, do
not put `requirements` on a work item if `implements` relations are canonical,
and do not put `items` on a wave if the work item's `wave` reference is
canonical. Derived reverse indexes answer both traversal directions.

## Package boundaries

The current `internal/saga` and `internal/reviewapp` packages are already large
integration surfaces. Adding every living type, loader, validator, graph, and
query there would make `model.go`, `load.go`, `validate.go`, `types.go`, and
`session.go` permanent merge hotspots. New leaf packages should own the new
domain, with a narrow composition layer joining them to the existing Saga.

```text
internal/livingid       target URN parsing/building and relation-type policy
internal/requirements   requirement records, loading, validation, mutations
internal/workplan       waves, work items, progress, pivots, mutations
internal/readiness      pure graph projection; no filesystem or transport
internal/livingapp      aggregate load, cross-reference validation, query DTOs
internal/cli            thin command/query adapters only
internal/server         later UI/HTTP adapter; no domain rules
```

Dependency direction should remain:

```text
requirements ─┐
workplan ─────┼──> livingapp ──> readiness
livingid ─────┘         ^
                        |
                 CLI / server adapters
```

More precisely:

- `internal/livingid` has no filesystem or transport imports. It provides
  target builders/parsers, endpoint kinds, relation policies, and stable sort
  helpers.
- `internal/requirements` owns only requirement records and their directory.
  It may depend on `livingid` and `store`, but not on `workplan`, `reviewapp`,
  CLI, or server.
- `internal/workplan` owns waves, work items, progress, and pivots. It may
  depend on `livingid` and `store`, but not on requirements. Cross-domain
  references remain unresolved until composition.
- `internal/readiness` accepts an already validated graph and current progress
  projection. It must not read disk, inspect Git, or know JSON paths.
- `internal/livingapp` loads the two stores, joins endpoints, validates cross
  references, computes reverse indexes and readiness, and exposes bounded,
  deterministic request/response types.
- The CLI and HTTP layers parse flags or JSON, call `livingapp`, and translate
  errors. They do not implement relation or readiness policy.

Within a shared package, parallel branches should add separate files rather
than edit an aggregate model file. For example, `requirement.go`, `relation.go`,
`wave.go`, `progress.go`, and `pivot.go` are preferable to a new monolithic
`model.go`.

## Current code to reuse

| Existing code | Reusable pattern | Constraint / adaptation |
| --- | --- | --- |
| `internal/store/store.go` | `WriteJSON`, `WriteFile`, `CommitDir`, `EnsureDirWithin`, `WithSagaLock`, `EventID` | Reuse atomic writes, path containment, exclusive event creation, and the single Saga writer lock. Do not invent a second lock file. |
| `internal/saga/load.go` | Strict JSON decoding, real-directory checks, stable record sorting, `loadFlatRecords` shape | Extract or duplicate a small generic helper only if it avoids a package cycle. Living loaders should reject symlinks and non-regular records the same way. |
| `internal/saga/validate.go` | Runtime/schema parity and full-document cross-reference validation | Keep leaf validation local; defer endpoint existence and gating-cycle checks to `livingapp`. |
| `internal/saga/mutation.go` | Bounded mutation index and validate-again-under-lock discipline | Extend via a living-specific index instead of making review mutations load the whole work graph. |
| `internal/reviewstore/reviewstore.go` | Preflight outside and inside the writer lock; append-only event mutation | Progress and pivots need the same no-partial-write behavior, but they are authored-plan state, not review state. Keep stores separate. |
| `internal/reviewapp/types.go` and `session.go` | Transport-independent session, opaque snapshot-bound cursors, deterministic pagination | Reuse the API contract, preferably in `livingapp`; do not add graph rules to the CLI adapter. |
| `internal/cli/query.go` | One JSON envelope, operation schemas, bounded pages, normalized error codes | New operations must preserve `schema`, `ok`, `snapshot`, `data`, and `page`, including cursor snapshot checks. |
| `internal/cli/query_reviewapp.go` | Mechanical transport-to-application mapping | Add a living adapter file; avoid putting loading or joining code here. |
| `internal/gitattribution` | File-derived attribution without persisted author claims | Requirement, relation, and event queries can expose provenance using record paths. |
| `internal/server/snapshot.go` | Separate immutable structural generation from mutable append-only overlay | If the UI is added, living definitions and progress need separate fingerprints/generations so one progress event does not rebuild source coverage. |
| `internal/querytest/fixture.go` | Real-Git, separate source/Saga repositories and no-write assertions | Extend with living fixtures rather than creating transport-specific fake filesystem semantics. |
| `internal/saga/schema_contract_test.go` | Published schema versus runtime grammar checks | Add v3/living parity tables for every enum, ID, URN, and state transition. |

The source comparison identity already excludes changes beneath `.saga`
directories. Living records therefore must remain Saga metadata and must not be
allowed to alter diff-coverage identity.

## Readiness projection

Readiness is a graph query, not a stored field. Storing it would make it stale
as soon as a dependency, progress event, or pivot changes.

The projection should:

1. Build one endpoint index and one adjacency list from validated relations.
2. Reduce progress events to the latest event for each work item using
   `(created_at, id)`.
3. Follow only relation types whose registry policy says
   `readiness_gating=true`.
4. Compute states with an iterative topological pass, not recursive calls that
   can overflow on a large plan.
5. Return deterministic, deduplicated direct blockers and complete transitive
   blocker paths, sorted by stable target URN.
6. Surface missing endpoints and cycles as validation errors. A defensive API
   response may report `invalid_graph`, but it must not label affected work
   `ready`.

The public state vocabulary needs a truth table in the contract commit. At a
minimum it must distinguish:

- terminal work from work that can begin now;
- an explicit `blocked` progress state from an unmet dependency;
- a cancelled dependency from a completed dependency;
- graph invalidity from ordinary blocking; and
- work-item readiness from wave rollups.

Do not infer a gating edge from wave order, `implements`, `refines`, narrative
containment, coverage, claims, verifications, reviews, or approvals. Those may
be reported beside readiness, but they are independent axes.

A useful response returns evidence for the result rather than a percentage:

```json
{
  "target": "urn:change-saga:checkout:work-item:add-idempotency-key",
  "state": "blocked",
  "direct_blockers": [
    "urn:change-saga:checkout:work-item:add-storage-index"
  ],
  "transitive_blocker_paths": [[
    "urn:change-saga:checkout:work-item:add-idempotency-key",
    "urn:change-saga:checkout:work-item:add-storage-index",
    "urn:change-saga:checkout:work-item:choose-key-format"
  ]],
  "diagnostics": []
}
```

For large plans, the session should compute the graph once and page projections
over the immutable result. The first performance fixture should include long
chains, wide diamonds, many non-gating edges, and shared transitive blockers so
complexity regressions are visible.

## API seams

The requirements and work-plan APIs can be implemented independently if they
share only frozen identities and envelopes.

Recommended bounded read operations are:

| Operation | Returns | Natural owner |
| --- | --- | --- |
| `requirements` | Filtered requirement records plus reverse relation counts | requirements slice |
| `relations` | Typed edges filtered by endpoint or type | requirements/relations slice |
| `waves` | Ordered wave summaries, never embedded unbounded item bodies | work-plan slice |
| `work-items` | Filtered items with latest progress and direct references | work-plan slice |
| `progress` | Append-only event history, filterable by work item | work-plan slice |
| `pivots` | Append-only decision history, filterable by target | work-plan slice |
| `readiness` | Current state, direct blockers, transitive paths, diagnostics | integration slice |

Whether these are `change-saga query` operations or a new `change-saga plan`
command family is a Wave 1 decision. If they extend `query`, refactor the
dispatcher once before feature branches begin: the current operation list,
purpose map, usage map, request union, session interface, parse switch, and
dispatch switch in `internal/cli/query.go` are a seven-way hotspot. A small
operation descriptor registry can keep schema, usage, parse, and execution
together while preserving the exact v1 envelope.

Mutations should use focused commands or structured stdin requests. Each
mutation must validate the affected leaf record and the composed graph before
committing, then validate again under `WithSagaLock`. Failed cross-reference or
cycle checks must leave no new file. Batch relation creation should resolve the
entire batch before the first write, matching coverage's atomic batch behavior.

Snapshot identity must include all `___living` bytes. A cursor obtained before
a requirement edit or progress event must fail as stale rather than continue
against a mixed graph.

## Migration and compatibility

Current compatibility facts constrain the rollout:

- `saga.json` requires `version: 2` and rejects unknown fields.
- `internal/saga/load.go` rejects unknown root directories beginning `___`.
- Current runtime checks compare most record versions with one
  `CurrentVersion` constant.
- Existing canonical v2 manifests are explicitly tested to load with no
  issues.

Therefore, adding `___living` to a document that still declares v2 is not a
compatible extension. The recommended migration policy is:

1. Add a reader that accepts both v2 and v3. A v2 document projects an empty
   living model and keeps its existing behavior and zero-warning compatibility
   test.
2. Separate the Saga container version from component-record versions before
   enabling writes. Do not change one `CurrentVersion` constant and force an
   accidental all-record migration.
3. Add an explicit, atomic `upgrade --to 3` operation. It stages and validates
   the complete result before replacing the Saga. It must never partially
   rewrite a tree.
4. A v3 document may initially reuse byte-compatible v2 narrative, coverage,
   claim, verification, and review records under explicitly documented
   component versions. New living records use their published component
   version. This avoids rewriting unrelated review history merely to add a
   plan.
5. Old binaries should reject v3 cleanly as unsupported. New binaries must keep
   reading v2. Downgrade is allowed only when `___living` is empty, unless a
   separate export command intentionally discards living data.
6. Update `SPEC.md`, `change-saga spec`, schemas, compatibility tests,
   `CHANGELOG.md`, and the authoring skill in the same format release, as
   required by `CONTRIBUTING.md`.

If governance chooses an in-place v2 extension instead, the compatibility note
must explicitly accept that older v2 binaries reject the document. It should
not be described as backward compatible.

## Test seams and acceptance cases

### Leaf format and store tests

- Runtime validators and published schemas accept and reject the same IDs,
  URNs, relation types, progress states, and endpoint combinations.
- Loaders reject symlinked roots, symlinked records, non-regular JSON, unknown
  files, filename/ID disagreement, duplicates, and path traversal.
- Stable definitions write atomically; event files use exclusive creation.
- Fault injection before write, sync, commit, and directory sync leaves no
  visible partial record.
- Disjoint requirement/work-item edits and append-only events merge in Git;
  edits to the same semantic definition or edge conflict deliberately.

### Cross-domain validation tests

- Every relation, work-item wave, progress target, and pivot target exists and
  belongs to the same Saga.
- Relation endpoint kinds match the registry policy.
- Symmetric relations have one canonical orientation and cannot be duplicated
  in reverse.
- Gating self-edges and cycles are rejected with stable target diagnostics.
- Non-gating `refines`, `implements`, and `conflicts_with` cycles do not affect
  readiness unless their own policy forbids them.
- A failed composed validation performs zero writes.

### Readiness unit tests

- Isolated, complete, explicitly blocked, and cancelled work items.
- One dependency, long chain, diamond, multiple roots, and shared blockers.
- Stable deduplication and ordering of transitive paths.
- Missing endpoint and cycle defensive behavior.
- Wave rollups follow the frozen truth table and never infer dependencies from
  display order.
- Requirement traceability does not become requirement acceptance or review
  approval.
- A large deterministic graph demonstrates approximately `O(V + E)` build
  behavior and bounded page payloads.

### Application and transport tests

- The same `livingapp` fixture drives direct application and CLI integration
  tests.
- Every page reports exact `total`, `returned`, `has_more`, and `next_cursor`;
  aggregation across pages equals `total`.
- Cursors are operation-, filter-, and snapshot-bound and reject tampering.
- Read operations cause no file, Git index, worktree, or commit changes.
- Error codes distinguish invalid arguments, invalid Saga, not found, stale
  snapshot, and invalid graph without leaking absolute paths.
- Git attribution reports committed, uncommitted, rewritten, and unavailable
  record provenance consistently with claims and reviews.
- v2 fixtures remain unchanged and clean; v3 fixtures round-trip; interrupted
  upgrades leave the original v2 Saga intact.

### Server tests, only when UI work begins

- A progress event refreshes the living overlay without rebuilding the source
  comparison or coverage index.
- The initial review page stays within existing first-load budgets.
- Readiness and progress are accessible without color-only or percentage-only
  communication.
- Browser actions never convert derived readiness into an approval or merge
  verdict.

## Likely conflict points

| Hotspot | Why branches will collide | Mitigation |
| --- | --- | --- |
| `internal/saga/model.go` | Every new record is tempting to add to `Saga` | Keep living records in leaf packages and compose in `livingapp`. |
| `internal/saga/load.go` | Root directory recognition and aggregate loading | One integration owner adds `___living`/v3 dispatch after leaf loaders land. |
| `internal/saga/validate.go` | Cross-reference checks accumulate here | Keep leaf checks local and graph checks in `livingapp`. |
| `internal/saga/schema_contract_test.go` | One table currently enumerates all schemas/enums | Add living parity tests in new package files; integration owner updates only shared version assertions. |
| `internal/cli/cli.go` and `cmd/change-saga/main.go` | Central help and command switches | Land one registry/refactor commit first, or reserve these files for the integration owner. |
| `internal/cli/query.go` | Lists, help, parsing, interface, and dispatch are centralized | Extract operation descriptors before parallel query work. |
| `internal/reviewapp/types.go` and `session.go` | Shared interface and eager session build | Prefer separate `livingapp`; join sessions only in a later adapter. |
| `SPEC.md`, `CHANGELOG.md`, skills | Normative vocabulary is cross-cutting | One format owner updates them after names and semantics freeze. |
| `internal/server/server.go`, `template.go`, `appjs.go`, `styles.go` | UI features converge on large embedded files | Defer UI until the read API is stable, then split living handlers/templates/assets into new files. |

Generated files, broad gofmt rewrites, and opportunistic refactors should not
share feature commits with the format slices.

## Concrete dependency and merge plan

### Wave 0 — contract freeze

One owner lands only the normative decision record and examples:

- v3/container and component version policy;
- directory layout and target URNs;
- exact record fields and enums;
- relation policy table;
- progress state machine;
- pivot meaning;
- readiness and wave truth tables; and
- query operation names and pagination/filter shapes.

Exit gate: both API implementers can write fixtures independently without
inventing a field or state.

### Wave 1 — shared seams

Land small mechanical commits, serially:

1. Split container-version checks from component-record versions while keeping
   all existing v2 tests green.
2. Add `internal/livingid` and its schema/runtime parity tests.
3. If `query` is extended, refactor its dispatcher into operation descriptors
   with byte-for-byte golden envelope compatibility.

Exit gate: no living records exist yet, existing CLI output is unchanged, and
feature branches no longer need to edit central switches repeatedly.

### Wave 2 — parallel leaf domains

After Wave 1 merges, two branches proceed independently:

- **Requirements slice:** requirement and relation schemas; loaders;
  validators; target queries; atomic definition/edge mutations; leaf fixtures.
- **Work-plan slice:** wave, work-item, progress, and pivot schemas; loaders;
  validators; queries; atomic definition and append-only event mutations; leaf
  fixtures.

Each slice owns new package and schema files. Neither imports the other or
edits root Saga loading, `reviewapp.Session`, server assets, `SPEC.md`, or the
authoring skill. Rebase both on Wave 1 before final review. Merge either first;
their file ownership should make order irrelevant.

### Wave 3 — graph composition and transitive readiness

Merge both leaf slices, then add `internal/livingapp` and
`internal/readiness`:

- compose entity indexes;
- validate cross references and relation policies;
- reduce progress events;
- reject gating cycles;
- expose reverse relation and membership indexes;
- compute readiness and blocker paths; and
- add large-graph and cross-domain fixtures.

This wave owns the first integration with root v3 loading. Keeping it after
both leaf merges prevents either API branch from guessing the other's model.

### Wave 4 — transport integration

Add CLI reads and mutations over the stable application boundary. Requirements
and work-plan adapter files may again be developed in parallel, while one
integration owner performs the small registry/session wiring commit. Verify
query schema output, pagination, stale cursors, errors, no-write behavior, and
mutation atomicity.

Exit gate: an agent can enumerate requirements, traverse typed relations, page
waves/items/events, and explain transitive readiness without reading
`___living` directly.

### Wave 5 — migration and normative release

Add the atomic v2-to-v3 upgrade, compatibility fixtures, complete v3 schemas,
`SPEC.md`, `change-saga spec`, `CHANGELOG.md`, and skill updates. Run the full
race-enabled Go suite and cross-platform CI because paths, locks, strict JSON,
and record ordering are format behavior.

### Wave 6 — reviewer UI, if separately approved

Only after the API contract is exercised should the server add requirements,
work-plan, pivot, progress, and readiness surfaces. Give living state its own
cache fingerprint/generation. Keep it out of the source-comparison snapshot so
an append-only progress event remains a cheap refresh and does not invalidate
coverage work.

This sequencing deliberately places every broad integration edit after the
independent leaf commits. Most feature branches add files; the few changes to
central dispatch, format loading, and normative documentation have a named,
serial owner.
