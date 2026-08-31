# Living Change Saga work-plan API

Status: proposed design; no production implementation is included.

Date: 2026-08-31

## Decision summary

A living Change Saga may contain an optional, Git-native work plan under
`___work_plan/`. The work plan adds first-class waves, work items, dependency
gates, handoff contracts, predicted touch areas, DevSwarm workspace
assignments, delivery records, and an append-only decision history.

A wave is a coordination cohort. It answers “which work should people discuss
and observe together?” It is not a topological layer, scheduling lock, or
implicit merge barrier. Readiness is derived only from explicit dependency
edges and their conditions. Consequently:

- work in a later wave may start before an earlier wave is complete;
- work in the same wave may still be blocked by an explicit dependency;
- dependencies may cross waves in either direction without making the plan
  invalid; and
- moving an item between waves never rewrites the dependency graph.

Every independently changing fact is stored in its own file or entity package.
Existing records are not rewritten during ordinary planning. Supported writers
use exclusive creation, the Saga writer lock, and atomic package publication.
Git can therefore merge most concurrent planning activity as disjoint adds;
semantic conflicts are reported explicitly instead of being hidden by a
last-writer-wins manifest.

The transport-neutral application layer owns this model. Structured CLI reads
extend `change-saga query`; structured writes extend the proposed
`change-saga mutate` boundary. The browser view mirrors the same snapshot and
projection semantics and is not a second source of truth.

## Goals and non-goals

The work-plan API must:

- make waves and work items durable, addressable resources;
- represent prerequisite work as a directed acyclic graph (DAG), independently
  of wave membership;
- make item scope, handoffs, likely overlap, ownership, progress, and delivery
  state inspectable without reading metadata files directly;
- retain replanning, supersession, progress, assignment, and merge history;
- remain useful when several branches or DevSwarm workspaces update the plan;
- expose deterministic, paginated JSON reads with the existing envelope,
  snapshot, cursor, error, and attribution conventions; and
- provide enough validation and derived state for a live coordination view
  without turning the view into a scheduler.

This design does not:

- create, archive, merge, or delete DevSwarm workspaces;
- run source-control operations or mark a merge complete by observation alone;
- infer dependencies from wave order, workspace ancestry, or overlapping files;
- replace review approvals, Change Saga claims, or claim verification;
- provide distributed leases, worker presence, or a task queue;
- make a work item’s `done` event a correctness verdict; or
- authorize edits to a declared touch area.

## Relationship to format v2

`___work_plan` becomes an allowed root reserved directory in Change Saga format
v2. The optional subtree has its own versioned manifest, currently
`change-saga.work-plan/v1`. A v2 Saga without the subtree behaves exactly as it
does today and queries report `present: false` rather than synthesizing a plan.

This is an additive change to the still-experimental v2 format, but older
binaries intentionally fail closed because they currently reject unknown
`___` directories. A work-plan-aware client must advertise the
`change-saga.work-plan/v1` capability. The root `saga.json` is not changed, and
work-plan state does not affect source-diff coverage or review decisions.

## Resource identities

Clients address resources by stable URN, never by a metadata path:

```text
urn:change-saga:<saga-id>:wave:<wave-id>
urn:change-saga:<saga-id>:work-item:<item-id>
urn:change-saga:<saga-id>:dependency:<dependency-id>
urn:change-saga:<saga-id>:contract:<contract-id>
urn:change-saga:<saga-id>:work-item:<item-id>:touch-area:<area-id>
urn:change-saga:<saga-id>:work-item:<item-id>:merge-unit:<unit-id>
urn:change-saga:<saga-id>:pivot:<pivot-id>
```

IDs use the existing stable-ID grammar and are unique within their resource
kind. Events use the existing UTC-time-plus-random ID. An event’s ID, its JSON
`id`, and its file or package name must agree.

Paths may be returned as human diagnostics, but mutations never accept a path
as a resource identity. No plan record contains a local checkout or worktree
path.

## Storage model

The portable layout is:

```text
___work_plan/
├── plan.json
├── waves/
│   └── foundation.wave/
│       ├── wave.json
│       └── events/
│           └── <event-id>.json
├── work-items/
│   └── query-api.work-item/
│       ├── work-item.json
│       ├── events/
│       │   ├── progress/<event-id>.json
│       │   ├── waves/<event-id>.json
│       │   └── workspaces/<event-id>.json
│       ├── merge-units/
│       │   └── primary.merge-unit/
│       │       ├── merge-unit.json
│       │       └── events/<event-id>.json
│       └── touch-areas/
│           └── reviewapp.touch-area/
│               ├── touch-area.json
│               └── events/<event-id>.json
├── dependencies/
│   └── storage-before-query.dependency/
│       ├── dependency.json
│       └── events/<event-id>.json
├── contracts/
│   └── query-envelope.contract/
│       ├── contract.json
│       └── events/<event-id>.json
└── pivots/
    └── split-renderer.pivot/
        └── pivot.json
```

`plan.json` is written once when the plan is created and is not a registry of
children:

```json
{
  "$schema": "https://changesaga.dev/schema/work-plan/v1/plan.schema.json",
  "version": 1,
  "id": "delivery",
  "title": "Living Saga delivery plan",
  "created_at": "2026-08-31T18:00:00Z",
  "request_id": "planning-run-7-plan"
}
```

Directory scans discover independently packaged resources. There are no shared
arrays of wave IDs, item IDs, edges, assignments, or events. Entity manifests
are immutable creation records. Lifecycle changes append event files; material
changes of identity, scope, or contract use supersession.

All reserved directories and entity packages must be real directories, not
symlinks. Every manifest and event must be a regular file. Unknown fields are
rejected. Unknown non-reserved files remain subject to the enclosing Saga’s
normal rules.

## Waves

A wave has stable identity, presentation order, and an objective:

```json
{
  "$schema": "https://changesaga.dev/schema/work-plan/v1/wave.schema.json",
  "version": 1,
  "id": "foundation",
  "title": "Foundation",
  "objective": "Stabilize storage and application contracts.",
  "order": 10,
  "created_at": "2026-08-31T18:01:00Z",
  "request_id": "planning-run-7-wave-foundation"
}
```

Wave lifecycle events have state `planned`, `active`, `closed`, or `cancelled`.
The first event is `planned`. Current state is the latest valid event by
`(created_at, id)`. Closing a wave with unfinished items is allowed and produces
a diagnostic in the derived view; it does not change item state.

Membership is stored on each work item as an append-only wave event. An
`assigned` event names one wave; an `unassigned` event names the previous wave.
The latest event is the item’s current cohort. Assigning a new wave implicitly
ends the previous membership in the projection without editing either wave.
Wave manifests never contain member arrays.

Wave summaries derive item counts, blockers, delivery state, and touch-area
collisions. These are observations, not conditions. In particular, neither
wave `order` nor wave `closed` participates in readiness.

## Work items

A work item is a bounded delivery objective rather than an instruction to an
agent:

```json
{
  "$schema": "https://changesaga.dev/schema/work-plan/v1/work-item.schema.json",
  "version": 1,
  "id": "query-api",
  "title": "Expose bounded work-plan queries",
  "objective": "Add snapshot-consistent reads for plan coordination.",
  "deliverables": [
    "Transport-neutral query methods",
    "CLI schema discovery and pagination tests"
  ],
  "created_at": "2026-08-31T18:02:00Z",
  "request_id": "planning-run-7-item-query-api"
}
```

Creation atomically publishes the item package and its initial `planned`
progress event. It may also include initial wave membership, touch areas, and a
workspace assignment in the staged package. Empty optional collections encode
as `[]`, not `null`.

### Progress

A progress event carries `from`, `to`, `summary`, `created_at`, and optional
structured evidence. States are:

- `planned`: accepted into the plan but not claimed ready;
- `ready`: the owner believes execution can start;
- `in_progress`: execution is underway;
- `blocked`: execution stopped on a named condition;
- `done`: the declared item deliverables are complete; and
- `cancelled`: work ended without fulfillment and should normally be replaced
  through a pivot if the objective remains necessary.

Mutations require `from` to equal the current explicit state and require a
reason for `blocked`, `cancelled`, or reopening `done`. Normal transitions are
`planned -> ready|in_progress|blocked|cancelled`,
`ready -> in_progress|blocked|cancelled`,
`in_progress -> blocked|done|cancelled`, and
`blocked -> ready|in_progress|cancelled`. `done -> in_progress` is an explicit
reopen. A cancelled or superseded item is not reopened; a replacement is
created instead.

Concurrent branches can still introduce transitions from the same prior state.
Both files remain history and deterministic ordering supplies display order,
but the current state is `conflicted`: readiness and other gates must not use a
timestamp-selected winner. Validation and `query work-conflicts` report
`concurrent_transition` until a compensating event names the selected current
state and lists every competing event ID in its `resolves` field. No event is
deleted to resolve the conflict. The same reconciliation field is available to
every work-plan state machine.

The API distinguishes:

- `explicit_progress`: the recorded progress state, or `conflicted` when no
  unique projection exists;
- `dependency_ready`: whether every active incoming dependency is satisfied;
- `effective_ready`: the item is active, not cancelled, and
  `dependency_ready`, with no unresolved state-machine conflict; and
- `blockers`: the unsatisfied dependency conditions plus any latest explicit
  blocked reason.

A mutation to `ready` or `in_progress` fails with `conflict` when dependencies
are unsatisfied unless the request contains `allow_blocked: true` and a reason.
This escape hatch records the exception and does not mark the dependencies
satisfied.

## Dependency DAG

Each dependency points from a prerequisite to a dependent work item. Active
dependencies form a DAG. A dependency has exactly one satisfaction condition:

```json
{
  "$schema": "https://changesaga.dev/schema/work-plan/v1/dependency.schema.json",
  "version": 1,
  "id": "storage-before-query",
  "prerequisite": "urn:change-saga:living:work-item:storage",
  "dependent": "urn:change-saga:living:work-item:query-api",
  "condition": {"kind": "merge_integrated"},
  "reason": "The query projection reads the committed storage contract.",
  "created_at": "2026-08-31T18:03:00Z"
}
```

Condition kinds are:

- `progress_done`: satisfied while the prerequisite’s explicit progress is
  `done`;
- `merge_integrated`: satisfied while all required merge units on the
  prerequisite are integrated and none is currently reverted; and
- `contract_fulfilled`: names a contract whose provider is the prerequisite
  and whose work-item consumer is the dependent.

Creation atomically includes an initial `active` event. A `retired` event
removes the dependency from the current graph while preserving its history. A
later `active` event may restore it. Edge state changes use `from`
preconditions like progress events.

Cycle detection ignores retired edges and reports the complete stable-URN path
of at least one cycle. Duplicate active semantic edges are invalid even when
their IDs differ. Self-edges are invalid. Dependencies involving superseded or
cancelled items remain visible in history but are invalid in the active graph
until retired or replaced.

Wave assignment has no graph meaning. The validator allows cross-wave edges,
allows a dependent’s wave to sort before its prerequisite’s wave, and never
creates an edge from wave order.

## Contracts

A contract makes a handoff or quality gate testable. It has one provider and
one consumer. The consumer is either another work item or the plan itself, so a
fan-out handoff uses one contract per consumer.

```json
{
  "$schema": "https://changesaga.dev/schema/work-plan/v1/contract.schema.json",
  "version": 1,
  "id": "query-envelope",
  "kind": "interface",
  "provider": "urn:change-saga:living:work-item:query-api",
  "consumer": {"kind": "plan"},
  "statement": "Every work-plan query emits the standard AI envelope.",
  "acceptance": [
    "Success contains schema, ok, snapshot, data, and page",
    "Failures expose a stable error.code",
    "Cursor pages identify one counted collection"
  ],
  "created_at": "2026-08-31T18:04:00Z"
}
```

Kinds are `deliverable`, `interface`, `handoff`, and `quality_gate`. Contract
events use `proposed`, `accepted`, `fulfilled`, `violated`, or `waived`.
`fulfilled`, `violated`, and `waived` require a summary; fulfillment should
include reproducible evidence URIs or a command when available. This state is a
planning assertion, not a Change Saga claim verification or review approval.

Only `fulfilled` satisfies a `contract_fulfilled` dependency. Waiving a
contract does not silently unblock work; the dependency must be retired or
superseded as part of an explicit pivot.

Material changes to a contract’s statement or acceptance checks create a new
contract and supersede the old one. This prevents an already accepted or
fulfilled contract from changing underneath its consumers.

## Touch areas

A touch area predicts where a work item expects to read or change the product.
It is coordination metadata, not code ownership, a lock, evidence coverage, or
write authorization.

```json
{
  "$schema": "https://changesaga.dev/schema/work-plan/v1/touch-area.schema.json",
  "version": 1,
  "id": "reviewapp",
  "repository": "https://github.com/twentyideas/changesaga.git",
  "selector": {"kind": "directory", "value": "internal/reviewapp"},
  "intents": ["modify", "test"],
  "reason": "The application layer owns query projections.",
  "created_at": "2026-08-31T18:05:00Z"
}
```

Selector kinds are `file`, `directory`, `glob`, and `logical`. File and
directory values are normalized repository-relative slash paths. Globs use a
documented, bounded syntax: `*`, `?`, and `**` are allowed; brace expansion,
negation, absolute paths, backslashes, empty segments, and `..` are rejected.
Logical selectors use a stable namespace and label for non-file boundaries
such as `schema:work-plan/v1`; they overlap only on exact normalized identity.
Intents are `read`, `add`, `modify`, `delete`, `rename`, `test`, or `document`.

Touch-area creation atomically includes an initial `active` event. A later
`retired` event removes it from the current projection. The read model computes
conservative pairwise overlaps among active items. Definite file and directory
containment is `overlap`; bounded glob intersection that cannot be proved is
`possible_overlap`. A collision is advisory and never creates a dependency.
Query results include both item URNs, both selectors, the reason code, and
current workspace owners so people can coordinate.

## DevSwarm workspace assignments

Assignments use the DevSwarm workspace UUID as the canonical external handle.
The event may snapshot the branch, source branch, repository ID, and display
label for human context:

```json
{
  "$schema": "https://changesaga.dev/schema/work-plan/v1/workspace-event.schema.json",
  "version": 1,
  "id": "20260831T180600.000000000Z-a1b2c3d4",
  "from": "released",
  "to": "assigned",
  "role": "owner",
  "workspace": {
    "provider": "devswarm",
    "id": "f9f72560-988a-44fd-8d06-1d020bae9854",
    "repository_id": "08fb259f-ceb1-4a3d-9121-a0b1866edd7b",
    "branch": "feature/living-saga-work-plan-api",
    "source_branch": "main",
    "label": "Living Saga: work-plan API"
  },
  "summary": "Own the work-plan contract design.",
  "created_at": "2026-08-31T18:06:00Z"
}
```

Roles are `owner`, `contributor`, and `observer`. An item has at most one
current owner but may have multiple contributors and observers. Assignment
state is reduced per `(workspace.id, role)`. A release names the same pair.
Conflicting active owners are retained and reported, not silently selected.

The portable record never stores a worktree path, terminal ID, pane ID, process
ID, credentials, or agent presence. Those are local or transient. DevSwarm
parent/child workspace relationships describe branch lineage and coordination;
they do not imply work-item dependencies or wave membership. Likewise, a
workspace’s `sourceBranch` is the expected integration direction, not proof of
a merge.

The core loader validates syntax without contacting DevSwarm. A live adapter
may resolve current external state and add an ephemeral observation such as
`active`, `archived`, `missing`, or `unavailable`, with `observed_at`. That
observation is never committed automatically. Archiving or deleting a
workspace does not erase its historical assignment; a release event or pivot
changes the plan projection.

## Merge units and delivery events

A work item may deliver through one or more merge units. A unit records the
portable integration route and whether it is required:

```json
{
  "$schema": "https://changesaga.dev/schema/work-plan/v1/merge-unit.schema.json",
  "version": 1,
  "id": "primary",
  "repository": "https://github.com/twentyideas/changesaga.git",
  "source": {
    "kind": "devswarm_workspace",
    "workspace_id": "f9f72560-988a-44fd-8d06-1d020bae9854",
    "branch": "feature/living-saga-work-plan-api"
  },
  "target_branch": "main",
  "required": true,
  "created_at": "2026-08-31T18:07:00Z"
}
```

Merge-unit events transition through `planned`, `ready`, `integrated`,
`reverted`, or `abandoned`. `ready` requires a full `head_oid`. `integrated`
requires a full `merge_oid` and may include a pull-request URL. `reverted`
requires the prior integrated OID and a full `revert_oid`. OIDs are facts
supplied by the caller and validated for shape; the core does not perform or
claim to observe a merge. An adapter may independently check reachability and
report a non-persistent diagnostic.

An item’s derived merge state is:

- `not_planned` when it has no required unit;
- `pending` when no required unit is integrated;
- `partially_integrated` when some but not all required units are integrated;
- `integrated` when every required unit is currently integrated; and
- `reverted` when a required unit’s current state is reverted.

An abandoned required unit keeps the item from `integrated` until a pivot
supersedes it or changes the delivery plan. Optional units appear in history
but do not gate `merge_integrated` dependencies.

Progress and merge state are deliberately separate. An item can be `done` but
not integrated, or integrated and later reopened. No merge event changes review
approval state, and no review approval synthesizes a merge event.

## Pivots and supersession

A pivot is an immutable explanation of a material plan change. It groups one
or more typed supersession decisions:

```json
{
  "$schema": "https://changesaga.dev/schema/work-plan/v1/pivot.schema.json",
  "version": 1,
  "id": "split-renderer",
  "title": "Split renderer delivery",
  "reason": "The UI and query projections can ship independently.",
  "decisions": [
    {
      "superseded": ["urn:change-saga:living:work-item:renderer"],
      "replacements": [
        "urn:change-saga:living:work-item:renderer-ui",
        "urn:change-saga:living:work-item:renderer-query"
      ]
    }
  ],
  "created_at": "2026-08-31T18:08:00Z"
}
```

Supersession is supported for waves, work items, dependencies, contracts,
touch areas, and merge units. Every decision contains resources of one kind;
splits and consolidations are allowed. Replacements must already exist at the
request snapshot. This keeps one pivot mutation to one atomically published
package; replacement creation cannot be partially confused with an accepted
pivot. Supersession forms an acyclic graph.

An active projection omits superseded resources by default but preserves them
in history queries. Supersession has no implicit cascading behavior:

- dependencies are not redirected to replacement items;
- contracts are not transferred;
- wave membership is not inherited;
- touch areas and workspace assignments are not copied; and
- merge evidence is not treated as evidence for a replacement.

Callers create explicit replacement and rewiring records before recording the
pivot. Validation then reports any active references to superseded resources.
Competing pivots that supersede the same resource are a semantic conflict,
unless one pivot intentionally declares all replacements in a single decision.
A later correction creates another resource version and supersedes the current
one; history is never “unsuperseded” by deleting a pivot.

## Append-only ordering, attribution, and idempotency

On a linear state history, the latest event is the greatest `(created_at, id)`
pair, matching the review overlay. File names never determine order
independently of the embedded ID. Timestamps are UTC RFC 3339 with nanoseconds.
When two events transition from the same prior state, that ordering is only for
display: the work-plan projection remains conflicted until a `resolves` event
joins the alternatives. Clock skew therefore cannot silently choose readiness,
contract fulfillment, ownership, or merge state. Mutations also carry `from`
and snapshots so supported writers reject stale decisions before writing.

Plan records contain no `author`, `created_by`, committer, or claimed workspace
agent identity. Reads derive attribution from the Git commit that first
introduced each file. New local records report `uncommitted`; unavailable
history reports `history_unavailable`; only `committed` includes committer
identity.

Every mutation accepts a caller-generated `request_id` and optional
`if_snapshot`. Request IDs are unique across the work plan. Repeating a request
ID with the same canonical operation and payload returns the original result
with `replayed: true`. Reuse with a different payload is `conflict`.
`if_snapshot` mismatch is `stale_snapshot` and creates no files. State-changing
operations should use both controls.

## Structured mutation API

Work-plan writes extend the proposed bounded mutation command:

```text
change-saga mutate <operation> --saga PATH [--repo PATH] --input FILE|-
```

The body is one bounded JSON object. Nested content is never accepted as an
inline shell flag. These operations are in scope:

| Operation | Effect |
| --- | --- |
| `plan-create` | Exclusively creates `___work_plan` and `plan.json`. |
| `wave-create` | Creates a wave package and initial `planned` event. |
| `wave-state` | Appends a wave lifecycle event. |
| `work-item-create` | Atomically creates an item, initial progress, and optional item-local wave, workspace, merge-unit, or touch-area records. |
| `work-item-wave` | Appends an assigned or unassigned wave event. |
| `progress-record` | Appends a progress transition. |
| `workspace-assign` | Appends an assignment transition. |
| `workspace-release` | Appends a release transition for the same workspace and role. |
| `merge-unit-create` | Creates a merge-unit package and initial planned event. |
| `merge-record` | Appends a merge-unit transition. |
| `dependency-create` | Creates an active dependency package after cycle evaluation. |
| `dependency-state` | Appends an active or retired transition. |
| `contract-create` | Creates a contract package and initial proposed event. |
| `contract-state` | Appends a contract lifecycle event. |
| `touch-area-create` | Creates an active item touch area. |
| `touch-area-state` | Appends an active or retired transition. |
| `pivot-record` | Creates one pivot package after replacements and rewiring records exist. |

Simple identifiers may be flags only in future human-facing aliases. The
structured mutation contract is the JSON body accepted by the application
layer. It has the same domain result whether called by CLI or loopback HTTP.

All operations support `--dry-run`. A dry run resolves references, validates
the proposed resulting graph, and returns the records and state changes it
would create, but it does not reserve IDs or return a new snapshot. A
non-dry-run mutation acquires the Saga writer lock, rebuilds the compact plan
mutation index, rechecks `if_snapshot` and `from`, resolves the entire request,
then publishes the one package or event atomically. Failure leaves no visible
partial entity.

With `--json`, the CLI writes exactly one `change-saga.ai/v1` envelope. Success
data includes `operation`, `created` resource URNs, `event_ids`, and `replayed`;
the new snapshot is the envelope’s top-level `snapshot`. Failures use the
existing stable domain codes and exit mapping. In particular, duplicate IDs or
request IDs are `conflict`, cycles and bad transitions are `invalid_argument`,
and a busy writer is a retryable `conflict`. Writers never run `git add`,
`git commit`, or a DevSwarm command.

## Query API

Reads use the existing form and always emit the common envelope:

```text
change-saga query <operation> --saga PATH [--repo PATH] [operation flags]
change-saga query schema <operation>
```

The following operations extend schema discovery:

```text
change-saga query work-plan    --saga PATH
change-saga query waves        --saga PATH [--state STATE] [--include-superseded]
                               [--cursor TOKEN] [--limit N]
change-saga query work-items   --saga PATH [--wave ID] [--status STATE]
                               [--workspace UUID] [--ready true|false]
                               [--include-superseded] [--cursor TOKEN] [--limit N]
change-saga query work-item    --saga PATH --item ID|URN
change-saga query dependencies --saga PATH [--item ID|URN]
                               [--direction incoming|outgoing|both]
                               [--wave ID] [--satisfied true|false]
                               [--include-retired] [--cursor TOKEN] [--limit N]
change-saga query contracts    --saga PATH [--item ID|URN] [--state STATE]
                               [--include-superseded] [--cursor TOKEN] [--limit N]
change-saga query touch-areas  --saga PATH [--item ID|URN] [--wave ID]
                               [--repository URI] [--collisions-only]
                               [--cursor TOKEN] [--limit N]
change-saga query work-events  --saga PATH [--item ID|URN] [--kind KIND]
                               [--since TIME] [--cursor TOKEN] [--limit N]
change-saga query pivots       --saga PATH [--resource URN]
                               [--cursor TOKEN] [--limit N]
change-saga query work-conflicts --saga PATH [--item ID|URN] [--wave ID]
                                 [--kind KIND] [--severity blocking|warning|info]
                                 [--cursor TOKEN] [--limit N]
```

`work-plan` is unpaginated and returns plan identity, `present`, active counts,
state totals, unresolved semantic-conflict counts, and compact wave summaries.
It does not inline every item or event. With no work plan it succeeds with
`present: false`, zero counts, and empty arrays.

`waves` paginates `data.waves`. Each result includes current lifecycle state,
order, active item counts, explicit progress totals, ready/blocked counts,
merge totals, open contract counts, and collision counts. None is exposed as a
wave gate.

`work-items` paginates `data.items`. Each compact item includes current wave,
explicit progress, dependency/effective readiness, blocker summaries, derived
merge state, active owner workspace, contract counts, touch-area collision
count, and supersession status. Filters apply before `page.total` is computed.

`work-item` is an unpaginated bounded summary of one item. It includes current
state, counts and stable query links for dependencies, contracts, touch areas,
and history; it does not inline unbounded collections.

`dependencies` paginates `data.dependencies`. Each edge includes compact
prerequisite and dependent summaries, its condition, active state,
`satisfied`, and a structured unsatisfied reason. With `--wave`, either endpoint
may belong to the wave; the result is still graph edges, not a fabricated wave
barrier.

`contracts`, `touch-areas`, `work-events`, `pivots`, and `work-conflicts`
paginate respectively `data.contracts`, `data.touch_areas`, `data.events`,
`data.pivots`, and `data.conflicts`.
Touch-area rows include collision records derived at the same snapshot.
`work-events` is the normalized append-only audit stream across wave,
progress, dependency, contract, touch-area, workspace, and merge events and
includes Git attribution. `work-conflicts` returns stable conflict IDs,
severity, involved resource/event IDs, candidate projections, and the mutation
or query links that can resolve or inspect the conflict. Conflict IDs are a
deterministic digest of kind and sorted involved IDs, so clients can correlate
the same unresolved condition across snapshots.

Every cursor contains its operation, normalized filters, offset, and snapshot.
A cursor used with another query or changed filters is `invalid_argument`; a
snapshot change is retryable `stale_snapshot`. Consumers follow
`page.next_cursor` while `has_more` is true, verify the collection at the
operation’s `pagination.counted_path` equals `page.returned`, aggregate to
`page.total`, and compare snapshots across related calls.

Stable empty collections encode as `[]`. Derived enum fields are always present
for active items. Query schema descriptions name every stable `data_path`, the
one counted path, and pagination kind. Plan queries use
`schema: "change-saga.ai/v1"`; the work-plan format version is returned inside
`data.plan.version` rather than creating a second envelope.

## Validation

`change-saga validate` gains a `work_plan` issue category and checks the work
plan independently from source coverage. `change-saga status` may summarize
plan diagnostics but plan incompleteness does not change mapping coverage or
its exit code. A future explicit `change-saga plan-status` may provide policy
gating; this API does not overload review or coverage status.

Structural errors include:

- unsafe, symlinked, non-regular, misplaced, or unknown reserved entries;
- schema/version violations, unknown fields, unsafe paths, malformed URNs, or
  path/ID disagreement;
- duplicate IDs or request IDs with differing canonical payloads;
- missing resource references or resource-kind mismatch;
- invalid event transitions, `from` mismatches, or malformed timestamps/OIDs;
- active dependency cycles, self-edges, and duplicate semantic edges;
- contract dependencies whose provider/consumer do not match the edge;
- supersession cycles, mixed-kind decisions, and active references to
  superseded resources.

Non-fatal diagnostics include:

- an explicit `ready` or `in_progress` state with unsatisfied dependencies;
- closed waves containing unfinished items;
- definite or possible touch-area collisions;
- done items with required delivery still pending;
- integrated items whose required merge is later reported reverted;
- archived, missing, or unavailable externally observed workspaces;
- concurrent transitions retained after a Git merge;
- competing pivots over the same resource;
- more than one active owner workspace for an item; and
- stale branch, label, or source-branch snapshots from an external workspace.

The concurrent-transition, competing-pivot, and multiple-owner diagnostics are
`blocking` coordination conflicts and remain queryable so a merged plan can be
reconciled append-only. They make the affected item or projection `conflicted`,
suppress readiness derived from that projection, and reject ordinary follow-on
state mutations until a compensating event or pivot names all alternatives.
Touch collisions and dependency exceptions are `warning`; stale external
observations are `info`. None makes unrelated Saga content unreadable.

Validation never rejects an item because another wave is incomplete, because
its dependency crosses or reverses wave order, or because its workspace is not
a child of another item’s workspace. It validates recorded facts and internal
consistency, not external scheduler policy.

## Merge-safe behavior

Supported writers follow the existing store guarantees:

1. Acquire the bounded Saga directory lock.
2. Load a compact, validated plan mutation index without loading fragment
   bodies or source diff atoms.
3. Resolve every reference, idempotency key, precondition, and resulting graph
   before writing.
4. Build complete entity packages in hidden same-parent staging directories.
5. Publish packages atomically and create event files exclusively; never
   overwrite an existing record.
6. Sync files and parent directories before reporting success.

Concurrent work on different branches commonly adds different paths and merges
without a textual conflict. Add/add collisions at the same stable ID fail
safely. Disjoint files can still express a semantic conflict—for example two
owners, two transitions from `in_progress`, a graph cycle, or competing
pivots. The loader keeps every record, emits a stable diagnostic with all
involved URNs/event IDs, and requires a compensating append-only decision.

No loader uses file modification time, directory enumeration order, Git author
identity, or DevSwarm liveness to choose current state.

## Live-view semantics

The live work-plan view is a projection of one validated snapshot, not a
mutable dashboard database.

- Initial render obtains the compact `work-plan` result and lazily queries the
  visible wave, items, and collisions.
- Every rendered row carries the snapshot that produced it. A retained server
  rebuilds its session when tracked or uncommitted relevant Saga files change.
- A refresh swaps the whole projection atomically. It never combines pages or
  derived counts from different snapshots.
- Polling, filesystem notification, or server-sent notification may trigger a
  refresh; transport choice does not change the API contract. Notifications
  are hints containing only “snapshot may have changed,” not plan events.
- Mutations send the displayed snapshot as `if_snapshot`. On
  `stale_snapshot`, the UI refreshes and asks the user to reconsider; it does
  not silently replay a state decision. An identical `request_id` may be
  safely retried after an ambiguous transport failure.
- New local files appear immediately with `uncommitted` attribution. After a
  commit, attribution may change while the semantic resource/event ID remains
  stable.
- Wave columns show cohort membership and aggregate observations. They do not
  disable later-wave controls. Unsatisfied dependency gates are shown on the
  item and its graph edges.
- Concurrent state heads and competing pivots are shown as alternatives with a
  reconcile action. The view may order them chronologically, but it must not
  present one as current or enable a gate that depends on the conflicted state.
- Definite and possible touch collisions are advisory links between items and
  their current workspace owners. They never lock an editor.
- External DevSwarm observations are visibly timestamped and marked
  unavailable when resolution fails. The last observation is not persisted or
  treated as presence.
- Superseded resources remain reachable through history and pivot links but
  are hidden from the default active board.

The UI may offer “create workspace,” “message owner,” or “merge workspace” as
explicit DevSwarm actions outside this API. Completing such an action does not
write plan state unless a separate work-plan mutation succeeds.

## Application boundary

The transport-neutral application layer should extend its session capability
approximately as follows; names are illustrative Go, while the JSON contracts
above are normative:

```go
type WorkPlanSession interface {
    WorkPlan(context.Context, WorkPlanQuery) (WorkPlanOverview, error)
    Waves(context.Context, WaveQuery) (WavePage, error)
    WorkItems(context.Context, WorkItemQuery) (WorkItemPage, error)
    WorkItem(context.Context, WorkItemDetailQuery) (WorkItemDetail, error)
    Dependencies(context.Context, DependencyQuery) (DependencyPage, error)
    Contracts(context.Context, ContractQuery) (ContractPage, error)
    TouchAreas(context.Context, TouchAreaQuery) (TouchAreaPage, error)
    WorkEvents(context.Context, WorkEventQuery) (WorkEventPage, error)
    Pivots(context.Context, PivotQuery) (PivotPage, error)
    WorkConflicts(context.Context, WorkConflictQuery) (WorkConflictPage, error)

    MutateWorkPlan(context.Context, WorkPlanMutation) (MutationResult, error)
}
```

The core package must not import CLI flags, HTTP, MCP, browser, or DevSwarm
packages. A DevSwarm resolver is an optional adapter receiving only canonical
workspace IDs and returning ephemeral observations. CLI and loopback HTTP
decode and limit requests, call the same methods, and encode the common
envelope and domain errors.

## Security and limits

The work-plan subtree is untrusted repository content. Existing containment,
symlink, bounded-read, and unknown-field protections apply. Additionally:

- titles, objectives, reasons, summaries, and acceptance checks are bounded
  UTF-8 plain text and are escaped by renderers;
- no field causes a command, URL, workspace action, or merge to execute;
- evidence commands are displayed as text unless a user explicitly runs them;
- repository identities are canonical absolute URIs without userinfo;
- branch and label snapshots are display text, never shell arguments;
- query filters and cursors are bounded, and default/max page sizes remain 100
  and 1000;
- event, item, edge, collision, and text limits fail with `too_large` rather
  than silently truncating validation; and
- loopback HTTP mutations retain bearer-token, Origin, JSON-content, and body
  limit protections and accept no filesystem root after server startup.

## Testable acceptance criteria

An implementation of this design is acceptable when all of the following are
covered by automated tests:

1. A v2 Saga without `___work_plan` validates as before, and `query work-plan`
   returns `present: false` with stable empty collections.
2. Creating a plan, wave, and item writes only the documented subtree, uses
   atomic package publication, and never modifies authored fragments, review
   records, coverage, claims, or verifications.
3. Two branches adding different waves, items, progress events, assignments,
   or merge events merge as disjoint files; identical stable-ID collisions do
   not overwrite either record.
4. Linear event reduction and display order are deterministic by
   `(created_at, id)` regardless of file name, directory enumeration order, or
   modification time. Concurrent transitions surface `conflicted` and cannot
   satisfy readiness, ownership, contract, or integration gates by timestamp.
5. `from` and `if_snapshot` reject stale progress, assignment, dependency,
   contract, touch-area, and merge transitions before any file is created.
6. Repeating the same `request_id` and canonical payload returns the original
   IDs; changing the payload returns `conflict`.
7. A dependency cycle, including one introduced only after merging two valid
   branches, reports the involved URNs. Retiring one edge restores a valid DAG
   without deleting history.
8. Same-wave work with an unsatisfied edge is not effectively ready, while a
   later-wave item with all edges satisfied is effectively ready even if an
   earlier wave remains active.
9. Cross-wave and reverse-order edges validate, and no API derives dependency
   edges from wave membership or order.
10. `progress_done`, `merge_integrated`, and `contract_fulfilled` conditions
    change satisfaction only from their named source state. A waived contract
    does not satisfy its edge.
11. Multiple required merge units derive pending, partial, integrated, and
    reverted states correctly; optional or merely `ready` units do not satisfy
    `merge_integrated`.
12. Workspace assignments persist the stable DevSwarm UUID and omit worktree,
    terminal, pane, process, and credential data. Workspace parent/child
    lineage creates no graph edge.
13. Definite path containment produces `overlap`, uncertain bounded glob
    intersection produces `possible_overlap`, and neither changes readiness or
    creates a lock.
14. A pivot preserves old resources and events, hides superseded resources
    from default active queries, exposes them with
    `--include-superseded`, and never redirects dependencies implicitly.
15. Competing pivots, active references to superseded entities, duplicate
    semantic edges, multiple active owners, and concurrent transitions produce
    stable diagnostics containing the relevant resource/event IDs. Valid
    coordination conflicts remain available through `query work-conflicts`
    and disappear only after an append-only reconciliation resolves them.
16. Every new query has schema discovery, exactly one JSON envelope, stable
    empty arrays, one declared counted path, filter-before-total behavior, and
    cursor/snapshot rejection consistent with existing queries.
17. Paging every new cursor operation yields exactly `page.total` records with
    no duplicates at one snapshot; a changed snapshot rejects the old cursor.
18. Git attribution reports new files as `uncommitted`, derives committer
    identity from the introducing commit, and never promotes payload names or
    Git authors to work-plan identity.
19. The live view never combines snapshots, refreshes after committed or
    uncommitted plan changes, keeps superseded history reachable, and never
    disables work solely because of wave order.
20. Symlink escapes, absolute/local paths, unsafe globs, URI userinfo, unknown
    fields, malformed event/package names, oversized input, and partial staged
    writes fail closed with no visible incomplete entity.

## Implementation sequence

Production work should proceed in independently reviewable increments:

1. schemas, loader, validation, target URNs, and deterministic event reduction;
2. transport-neutral projections and query schema discovery;
3. exclusive mutation store, idempotency, preconditions, and CLI mapping;
4. touch-area collision and dependency/readiness derivation;
5. live browser projection and optional DevSwarm observation adapter; and
6. merge/pivot concurrency fixtures and end-to-end acceptance tests.

This sequence is guidance for implementing the contract, not a wave barrier in
the contract itself.
