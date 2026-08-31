# Living Change Saga API

Status: implementation contract for the first living-Saga delivery.

This document resolves the exploratory requirements, work-plan, and
implementation-map drafts. A Change Saga is a living specification from the
first requirement through peer review. There is no separate "living" feature
inside a Saga and no persisted pivot resource.

## Product model

The format has four authored surfaces and one review overlay:

```text
requirements -> technical design -> work plan -> delivery evidence
                                                       |
                                                       v
                                                    review
```

- Requirements define the intended outcome and acceptance criteria.
- Technical design explains how the requirements will be satisfied.
- The work plan divides that design into parallel, mergeable workspace work.
- Commits, exact diffs, and verifications establish what was delivered.
- Review comments and approvals remain an independent overlay.

The governing invariants are:

1. Every accepted acceptance criterion is addressable by stable ID.
2. Every substantive design element identifies the criteria it addresses.
3. Every active work item identifies the design and requirements it advances.
4. Every implementation claim needs immutable evidence before peer review.
5. A recorded progress state is never proof that an implementation is correct.
6. Requirement, design, and plan updates preserve history and invalidate stale
   downstream assumptions rather than silently redirecting them.

## Format boundary

Living Sagas use format v3. New readers continue to read v2 Sagas. A v2 Saga
must be explicitly upgraded before it gains v3-only roots because v2 readers
deliberately reject unknown reserved directories.

```text
change.saga/
|-- saga.json
|-- overview.fragment/
|-- ___requirements/
|   |-- stories/
|   |-- citations/
|   `-- relations/
|-- ___design/
|   `-- <chapter, section, fragment, and landmark packages>
|-- ___workplan/
|   |-- waves/
|   |-- work-items/
|   |-- dependencies/
|   |-- contracts/
|   `-- events/
`-- ___review/
```

The three authored roots are first-class. They are not nested beneath a
generic container. Existing diff and approval packages may be attached to the
addressable narrative or design target they explain.

## Requirements

### Stories and acceptance criteria

A story has a stable identity and append-only revisions. The identity record
contains only immutable creation metadata. Each revision contains the complete
current title, user-story statement, priority, citations, and acceptance
criteria.

Acceptance criteria have IDs stable within their story. A criterion ID that is
removed is never reused for a different meaning.

```text
urn:change-saga:<saga>:story:<story>
urn:change-saga:<saga>:story:<story>:criterion:<criterion>
urn:change-saga:<saga>:story:<story>:revision:<revision>
```

A revision names its parent revision or all competing parent heads it
reconciles. The revision graph is acyclic and has one initial revision.
Concurrent child revisions remain visible as a conflict until a reconciliation
revision names every head. Timestamps never select a semantic winner.

Story lifecycle decisions are append-only events with states `proposed`,
`accepted`, `deferred`, `rejected`, or `retired`. Accepted stories must have at
least one acceptance criterion.

### Citations

Citations are immutable records that can reference an absolute URL, repository
commit, issue, document, or recorded decision. Citations establish provenance;
they do not assert implementation completion.

```text
urn:change-saga:<saga>:citation:<citation>
```

## Technical design

`___design` reuses the current chapter, section, fragment, landmark, Markdown,
SVG, HTML, image, and text packages. Design content is therefore visual and
addressable with the same tools already used for reviewer-facing explanation.

Design targets link to exact story or criterion revision URNs using typed
relations. A current criterion is considered designed only when at least one
active `addresses` relation reaches its current revision.

Design content remains editable through supported fragment mutations. Git
history records prior content. Independent fragments merge independently;
concurrent changes to the same fragment conflict normally and must be
reconciled. Relations pin both the target and a content digest so a design edit
marks dependent plan references stale until they are intentionally refreshed.

## Relations

Relations are independently stored, stable-ID records with explicit endpoint
URNs, type, rationale, and optional endpoint revisions or digests.

The initial vocabulary is deliberately small:

| Type | From | To | Meaning |
| --- | --- | --- | --- |
| `refines` | story or criterion | story or criterion | Decomposes intent |
| `addresses` | design target | story or criterion revision | Design coverage |
| `implements` | work item | design target or criterion revision | Planned delivery |
| `verifies` | claim or verification | criterion revision | Delivery evidence |
| `supersedes` | same resource kind | same resource kind | Explicit replacement |
| `conflicts_with` | compatible resources | compatible resources | Known incompatibility |

Work dependencies are first-class work-plan records rather than generic
relations because they carry readiness conditions and lifecycle state.

Changing a requirement, design target, or work item never silently retargets a
relation. Queries report the relation as stale until a writer creates a new
relation or explicitly supersedes the old one.

## Work plan

### Waves

A wave is a first-class coordination cohort for work intended to proceed in
parallel. It has a stable ID, title, objective, display order, entry conditions,
and exit conditions.

Wave order is not an implicit dependency or barrier. A later-wave item may
start whenever its explicit dependencies are satisfied. A same-wave item may
still be blocked by an explicit dependency.

```text
urn:change-saga:<saga>:wave:<wave>
```

### Work items

A work item is an independently assignable and mergeable delivery objective.
It records:

- objective and deliverables;
- current wave membership;
- requirement and design relations;
- expected touch areas;
- dependencies and shared contracts;
- workspace assignments;
- completion checks;
- progress events; and
- resulting merge units and diff evidence.

```text
urn:change-saga:<saga>:work-item:<item>
```

Definitions use stable per-resource packages. Changes to scope create a new
definition revision with explicit parents, just like requirement revisions.
This makes a work-plan change ordinary living-document maintenance rather than
a special pivot operation.

### Dependencies and contracts

Dependencies form a validated DAG. Each dependency names a prerequisite, a
dependent work item, and one satisfaction condition:

- `progress_done`
- `merge_integrated`
- `contract_fulfilled`

Contracts are stable resources describing an interface or decision on which
parallel work relies. Contract revisions behave like other authored revisions;
a changed contract makes pinned dependencies stale until reconciled.

### Workspace assignments and progress

Assignments are append-only events. A DevSwarm assignment persists the
workspace UUID, repository identity, branch, and optional source branch. It
never persists a local path or treats process liveness as authored state.

Progress events use `planned`, `ready`, `in_progress`, `blocked`, `done`, or
`cancelled`. Concurrent transitions from one parent event create an explicit
multi-head conflict. A reconciliation event names every competing head.

Completion and integration are distinct. `done` means the work item owner has
completed its declared deliverables. `merge_integrated` requires immutable Git
evidence showing that the result reached the integration target.

## Updating the living Saga

A material change is represented by ordinary revisions and relation updates:

1. Revise the affected story, criteria, design fragment, contract, wave, or
   work item.
2. Preserve stable IDs for concepts whose meaning remains continuous; create a
   replacement resource and `supersedes` relation when the meaning changes.
3. Recompute downstream references against their pinned revision or digest.
4. Report stale relations, dependencies, and delivery paths.
5. Reconcile the work plan by revising, replacing, cancelling, or adding work
   items and dependencies.
6. Commit the coherent update. Git records who changed the direction, when,
   and why.

The viewer may describe such a commit as a change of direction in its timeline,
but the persisted format has no pivot object and no second mutation language.

## Mutation API

All mutations execute under the existing Saga writer lock, validate again
inside the lock, use exclusive creation, and publish packages atomically.
Every command supports `--json`; multi-record operations accept `--request-id`
for idempotency.

```text
change-saga upgrade --to 3 SAGA

change-saga story add SAGA
change-saga story revise SAGA --story URN --parent REVISION...
change-saga story set-state SAGA --story URN --parent EVENT...
change-saga citation add SAGA

change-saga relation add SAGA --type TYPE --from URN --to URN
change-saga relation supersede SAGA --relation URN

change-saga design add-chapter SAGA NAME
change-saga design add-section SAGA PATH
change-saga design add-fragment SAGA [flags]
change-saga design set-fragment-content SAGA --target TARGET --source FILE|-

change-saga plan add-wave SAGA
change-saga plan revise-wave SAGA --wave URN --parent REVISION...
change-saga plan add-item SAGA
change-saga plan revise-item SAGA --item URN --parent REVISION...
change-saga plan add-dependency SAGA
change-saga plan add-contract SAGA
change-saga plan assign SAGA --item URN --workspace UUID
change-saga plan progress SAGA --item URN --from EVENT --to STATE
change-saga plan record-merge SAGA --item URN --commit OID
```

Initial implementation may deliver these command families incrementally, but
persisted records must conform to the frozen v3 contract from their first
release.

## Query API

Reads preserve the existing `change-saga.ai/v1` envelope, snapshot binding,
deterministic pagination, and schema-discovery behavior.

```text
change-saga query requirements
change-saga query requirement-history
change-saga query citations
change-saga query relations
change-saga query design-coverage
change-saga query waves
change-saga query work-items
change-saga query work-events
change-saga query work-conflicts
change-saga query traceability
change-saga query readiness
```

Coverage is reported on three independent axes:

- requirement coverage: accepted criteria with current design;
- plan coverage: current design and criteria with active work items; and
- delivery coverage: accepted criteria with a complete path to current,
  immutable evidence.

Only delivery coverage participates in peer-review readiness. Readiness is a
projection, never a stored approval or percentage.

## Live viewer

The viewer renders requirements, design, work-plan waves, workspace
assignments, progress, merges, evidence, and review as projections of one
snapshot. It watches for a new snapshot and swaps the complete projection; it
does not combine records from different snapshots.

The default plan view emphasizes waves and parallel workspace lanes. It also
shows dependency blockers, predicted touch-area collisions, stale references,
contract changes, current multi-head conflicts, convergence points, and merge
order.

## Implementation waves

1. **Format foundation:** v3 manifest, stable living resource IDs, schemas,
   strict loading, validation, and v2 read compatibility.
2. **Requirements and design:** story/citation/relation mutations, design root,
   revisions, and coverage queries.
3. **Parallel work plan:** waves, work items, dependencies, contracts,
   assignments, progress, and conflict queries.
4. **Traceability and readiness:** stale-reference detection and transitive
   requirement-to-evidence paths.
5. **Live experience:** wave/workspace plan UI, snapshot refresh, and history.

Each implementation wave must preserve independent package ownership and
serialize edits to the central Saga loader, CLI dispatcher, query registry, and
published specification.
