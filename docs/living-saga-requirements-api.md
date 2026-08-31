# Living Change Saga requirements and traceability API

Status: exploratory Wave 1 input. The converged implementation contract is
[`living-saga-api.md`](living-saga-api.md), which resolves this draft's format,
storage-root, vocabulary, and update-semantics decisions.

## 1. Decision summary

Requirements are first-class, stable saga resources rather than prose inferred
from fragments or a new meaning assigned to author claims. Their definitions,
lifecycle decisions, citations, and trace relations are independent Git-native
records. Definition and lifecycle history is append-only. A current value is a
unique head in an explicit parent graph, not whichever file happens to have the
latest wall-clock timestamp.

The capability has five persistent record types:

1. a requirement identity;
2. append-only requirement revisions;
3. append-only requirement lifecycle events;
4. immutable citations; and
5. a trace identity with append-only trace-state events.

It adds `requirements`, `requirement-history`, `traces`, `citations`, and
`readiness` operations to the existing `change-saga query` API. Existing query
operations, diff coverage, claims, verification, reviews, and target URNs keep
their present meanings.

Three distinctions are normative:

- Diff coverage answers whether every changed source atom is mapped to authored
  narrative. It does not answer whether requirements are satisfied.
- Trace completeness answers whether an accepted requirement has the required
  current narrative, implementation, and verification links. It does not prove
  that any linked claim is true.
- Readiness is a reproducible projection over lifecycle state, current traces,
  diff currency, and claim verification. It is not approval to merge.

## 2. Compatibility boundary and storage

The new records use format version `2`, matching the current saga model. They
do not add fields to `saga.json`, whose v2 schema is closed. A capable loader
recognizes these additional root metadata directories:

```text
example.saga/
├── ___requirements/
│   └── refund-window.requirement/
│       ├── requirement.json
│       ├── revisions/
│       │   └── refund-window-r1.json
│       └── states/
│           └── refund-window-proposed.json
├── ___citations/
│   └── policy-2026.json
└── ___traces/
    └── refund-handler.trace/
        ├── trace.json
        └── states/
            └── refund-handler-withdrawn.json
```

All listed directories are real directories, not symlinks. Record names match
the ID in the record. Writers use exclusive creation and never rewrite an
existing revision, state event, citation, or trace. Creating a requirement or
trace stages its complete package and atomically renames it into place.

The absence of all three directories means "requirements not adopted". It is
valid and preserves the behavior of every existing v2 saga. An empty
`___requirements` directory means the capability is adopted but has no recorded
requirements; readiness is `blocked`, not `not_applicable`.

Updated readers MUST load existing v2 sagas unchanged. Older binaries currently
reject unknown reserved directories and therefore cannot read a saga after it
adopts this capability. That forward-compatibility limit is unavoidable without
weakening the existing closed-directory rule. Implementations must publish the
minimum supporting CLI version and return a clear unsupported-format error;
they must not disguise the records in ordinary narrative directories.

## 3. Identifiers

Persistent IDs use the existing stable-ID grammar:
`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`. Requirement criterion IDs use the same
grammar and are unique within one requirement. IDs are never recycled.

The following URNs are canonical:

```text
urn:change-saga:<saga-id>:requirement:<requirement-id>
urn:change-saga:<saga-id>:requirement:<requirement-id>:criterion:<criterion-id>
urn:change-saga:<saga-id>:requirement:<requirement-id>:revision:<revision-id>
urn:change-saga:<saga-id>:requirement:<requirement-id>:state:<event-id>
urn:change-saga:<saga-id>:citation:<citation-id>
urn:change-saga:<saga-id>:trace:<trace-id>
urn:change-saga:<saga-id>:trace:<trace-id>:state:<event-id>
urn:change-saga:<saga-id>:claim:<claim-id>
urn:change-saga:<saga-id>:verification:<verification-id>
```

The last two forms are additive canonical names for existing claim and
verification records; their current on-disk IDs and schemas do not change.
Requirement, criterion, citation, and trace URNs survive directory moves.
Revision and event URNs identify immutable historical records.

APIs accept only canonical URNs, canonical `saga-diff://v1/line` or
`saga-diff://v1/event` URIs, and existing narrative target URNs. Filesystem
paths are diagnostics or mutation repair handles, never resource identities.

## 4. Closed record schemas

Every schema uses JSON Schema draft 2020-12, has
`additionalProperties: false` at every object boundary, and is published below
`https://changesaga.dev/schema/v2/`. The examples in this section show all
fields; fields described as optional may be omitted rather than set to `null`.

### 4.1 Requirement identity

`___requirements/<id>.requirement/requirement.json` conforms to
`requirement.schema.json`:

```json
{
  "$schema": "https://changesaga.dev/schema/v2/requirement.schema.json",
  "version": 2,
  "id": "refund-window",
  "created_at": "2026-08-31T18:20:00Z"
}
```

Required fields are `$schema`, `version`, `id`, and `created_at`. `version` is
the constant `2`; `created_at` is an RFC 3339 timestamp. The package MUST have
at least one valid revision and one valid lifecycle event. The identity record
contains no mutable title, state, author, or aggregate arrays.

### 4.2 Requirement revision

`revisions/<revision-id>.json` conforms to
`requirement-revision.schema.json`:

```json
{
  "$schema": "https://changesaga.dev/schema/v2/requirement-revision.schema.json",
  "version": 2,
  "id": "refund-window-r2",
  "requirement": "urn:change-saga:checkout-rewrite:requirement:refund-window",
  "parents": [
    "urn:change-saga:checkout-rewrite:requirement:refund-window:revision:refund-window-r1"
  ],
  "title": "Refunds remain available for 30 days",
  "statement": "An authenticated buyer can request a refund until 30 days after settlement.",
  "kind": "functional",
  "priority": "must",
  "criteria": [
    {
      "id": "before-deadline",
      "statement": "At 29 days 23:59:59 after settlement, an eligible request is accepted.",
      "implementation": { "required": true }
    },
    {
      "id": "after-deadline",
      "statement": "At 30 days or later, the request is rejected with the documented reason.",
      "implementation": { "required": true }
    },
    {
      "id": "support-playbook",
      "statement": "Support can identify the deadline rule in the operational playbook.",
      "implementation": {
        "required": false,
        "rationale": "This criterion is fulfilled by an operational artifact rather than product code.",
        "citations": ["urn:change-saga:checkout-rewrite:citation:support-policy"]
      }
    }
  ],
  "citations": [
    "urn:change-saga:checkout-rewrite:citation:policy-2026"
  ],
  "created_at": "2026-08-31T18:35:00Z"
}
```

`parents` is a unique array of revision URNs from the same requirement. It is
empty only for the initial revision. A normal revision has one parent. A
reconciliation revision names every current head it reconciles. The revision
graph must be acyclic, all nodes must reach the initial revision, and there can
be only one initial revision.

`kind` is one of `functional`, `quality`, `constraint`, `compatibility`,
`security`, `operational`, or `documentation`. `priority` is one of `must`,
`should`, or `may`. `title` and `statement` are non-blank strings. `criteria`
is a unique-by-ID array and is the full current criterion set, not a patch.
Criterion IDs that remain semantically the same persist across revisions.
Removing a criterion does not reuse its ID later for a different meaning.

Each criterion has `id`, `statement`, and `implementation`. When
`implementation.required` is `false`, a non-blank `rationale` and at least one
citation URN are required. When it is `true`, `rationale` and criterion-level
`citations` are absent. Accepted requirements must have at least one criterion;
proposed requirements may temporarily have none.

`citations` is a unique array of citation URNs. A revision is a full snapshot:
omitting a citation or criterion from a child revision removes it from the
current definition but does not erase history.

### 4.3 Requirement lifecycle event

`states/<event-id>.json` conforms to `requirement-state.schema.json`:

```json
{
  "$schema": "https://changesaga.dev/schema/v2/requirement-state.schema.json",
  "version": 2,
  "id": "refund-window-accepted",
  "requirement": "urn:change-saga:checkout-rewrite:requirement:refund-window",
  "parents": [
    "urn:change-saga:checkout-rewrite:requirement:refund-window:state:refund-window-proposed"
  ],
  "state": "accepted",
  "reason": "Approved for the checkout rewrite scope.",
  "citations": ["urn:change-saga:checkout-rewrite:citation:decision-42"],
  "created_at": "2026-08-31T19:00:00Z"
}
```

Required fields are `$schema`, `version`, `id`, `requirement`, `parents`,
`state`, `reason`, `citations`, and `created_at`. `state` is `proposed`,
`accepted`, `deferred`, `rejected`, or `retired`. The initial event is
`proposed` with no parents. Every later event names one current parent, or all
current parents when reconciling a branch conflict. `reason` is always required;
`citations` may be empty only for `proposed`.

The event graph follows the same root, reachability, and acyclicity rules as
the revision graph. Parent relationships, not timestamps, determine the
current head. Timestamps are display metadata and deterministic secondary sort
keys only.

### 4.4 Citation

`___citations/<id>.json` conforms to `citation.schema.json`:

```json
{
  "$schema": "https://changesaga.dev/schema/v2/citation.schema.json",
  "version": 2,
  "id": "policy-2026",
  "source": {
    "type": "uri",
    "uri": "https://example.com/policies/refunds/2026",
    "title": "Refund policy 2026",
    "locator": "Section 4.2",
    "retrieved_at": "2026-08-30T12:00:00Z",
    "content_sha256": "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
    "excerpt": "Refund requests remain available through the thirtieth day."
  },
  "note": "Product policy source for the cutoff requirement.",
  "created_at": "2026-08-31T18:10:00Z"
}
```

`source` is exactly one of:

- `uri`: an absolute `https` or `http` URI without userinfo, plus required
  `title`; `locator`, `retrieved_at`, `content_sha256`, and an excerpt of at
  most 2,000 Unicode scalar values are optional;
- `target`: an existing saga target URN plus a required label;
- `diff`: a canonical current-or-historical line/event diff URI plus a required
  label; or
- `recorded_input`: a required label and excerpt, used when a meeting, user
  request, or policy decision has no durable URI.

`note` and `created_at` are required. Citation records are immutable. A
correction creates a new citation ID and a new requirement revision or state
event that cites it. Readers never fetch a citation URI as a side effect.
A digest pins retrieved bytes when available; an excerpt is context, not proof
that the source remains reachable or authoritative.

### 4.5 Trace identity and state

`___traces/<id>.trace/trace.json` conforms to `trace.schema.json`:

```json
{
  "$schema": "https://changesaga.dev/schema/v2/trace.schema.json",
  "version": 2,
  "id": "refund-handler",
  "type": "implements",
  "from": {
    "uri": "saga-diff://v1/line?base=...&end=91&head=...&path=internal%2Frefund.go&repository=...&side=new&start=64"
  },
  "to": {
    "uri": "urn:change-saga:checkout-rewrite:requirement:refund-window:criterion:before-deadline",
    "requirement_revision": "urn:change-saga:checkout-rewrite:requirement:refund-window:revision:refund-window-r2"
  },
  "rationale": "The selected branch enforces the inclusive deadline boundary.",
  "citations": [],
  "created_at": "2026-08-31T20:00:00Z"
}
```

Required fields are `$schema`, `version`, `id`, `type`, `from`, `to`,
`rationale`, `citations`, and `created_at`. An endpoint has `uri` and has
`requirement_revision` exactly when its URI is a requirement or criterion URN.
That revision must belong to the named requirement and contain the criterion
when a criterion is named. Pinning fulfillment traces to a revision makes an
accepted definition change visibly stale instead of silently inheriting old
evidence.

The closed relation vocabulary and endpoint matrix is:

| Type | From | To | Meaning |
| --- | --- | --- | --- |
| `refines` | requirement | requirement | The source is a more specific obligation. |
| `depends_on` | requirement | requirement | The source cannot be ready until the target is ready. |
| `conflicts_with` | requirement | requirement | Both accepted obligations cannot currently be satisfied together. |
| `supersedes` | requirement | requirement | The source replaces the target; lifecycle changes remain explicit. |
| `addresses` | saga/chapter/section/fragment/landmark target | requirement | Authored narrative explains the current requirement. |
| `implements` | canonical line/event diff URI | criterion | Current source change realizes the criterion. |
| `verifies` | claim URN | criterion | A falsifiable claim evaluates the criterion. |

Self-relations are invalid. `refines`, `depends_on`, and `supersedes` graphs
must be acyclic. `conflicts_with` is symmetric: writers canonicalize the two
endpoint URIs lexicographically and reject a duplicate active pair. No relation
implies another relation, lifecycle transition, diff coverage assignment, or
claim result.

A trace is initially active. Later state records in `states/<event-id>.json`
conform to `trace-state.schema.json`:

```json
{
  "$schema": "https://changesaga.dev/schema/v2/trace-state.schema.json",
  "version": 2,
  "id": "refund-handler-withdrawn",
  "trace": "urn:change-saga:checkout-rewrite:trace:refund-handler",
  "parents": ["urn:change-saga:checkout-rewrite:trace:refund-handler"],
  "state": "withdrawn",
  "reason": "The implementation moved to a different selector.",
  "created_at": "2026-09-02T10:00:00Z"
}
```

`state` is `active` or `withdrawn`. For the first state event, `parents`
contains the trace URN (the implicit active root); later events name current
trace-state URNs. Multiple heads are a visible conflict. Restoring a withdrawn
trace appends `active`; it never deletes the withdrawal.

## 5. Current-value and lifecycle semantics

The current requirement revision, requirement state, and trace state are the
unique heads of their respective parent graphs. A graph with multiple heads is
structurally readable but conflicted. Queries return every head, omit a
singular `current_*` value, and readiness reports `history_conflict`. A new
record naming all heads resolves the conflict without erasing either branch.

Allowed lifecycle transitions are:

| From | To |
| --- | --- |
| `proposed` | `accepted`, `deferred`, `rejected` |
| `accepted` | `deferred`, `retired` |
| `deferred` | `proposed`, `accepted`, `rejected`, `retired` |
| `rejected` | `proposed` |
| `retired` | `proposed` |

An event may not repeat its parent state. A reconciliation event must be a
valid transition from every parent state. Revising a requirement does not
change lifecycle state. Accepting a requirement does not claim it is ready.
`deferred`, `rejected`, and `retired` requirements remain queryable and retain
all traces, but they are out of delivery scope.

An active trace has a separate currency value:

- `current`: every endpoint exists, every pinned requirement revision is the
  unique current revision, and every diff endpoint matches the current source
  comparison;
- `stale_revision`: a pinned requirement revision is historical;
- `stale_diff`: a diff URI no longer matches the source comparison;
- `dangling`: an endpoint no longer resolves, including a criterion removed by
  the pinned revision; or
- `conflicted`: the trace state has multiple heads.

Only traces whose logical state is `active` and currency is `current` satisfy
readiness. Historical and withdrawn traces remain in `requirement-history` and
`traces` results.

## 6. Provenance and citations

Records do not accept `author`, `created_by`, reviewer identity, or an asserted
source-control commit. Readers derive provenance from the Git commit that first
introduced each exact record file, using the same model as claims,
verifications, and review events:

```json
{
  "status": "committed",
  "commit": "40-hex-oid",
  "committer": {"name": "A. Reviewer", "email": "a@example.com"},
  "committed_at": "2026-08-31T20:03:00Z"
}
```

The other honest states are `uncommitted` and `history_unavailable`. The
payload's `created_at` orders and describes the event but is not identity.
History rewrites recompute attribution. Absolute local paths are not returned
unless diagnostic paths were explicitly enabled.

Citations establish where a requirement, exception, or decision came from.
They do not count as implementation or verification evidence. A requirement
revision cites its definition sources; lifecycle events cite scope decisions;
implementation exemptions cite their justification; and traces may cite a
design decision that explains the relation. Exact source evidence continues to
use canonical diff URIs and claim evidence.

## 7. Mutation CLI

New commands follow the existing flat CLI vocabulary and append-only/exclusive
write behavior:

```text
change-saga add-citation --input FILE|- [--id ID] [--if-snapshot TOKEN] [--json|--quiet] <saga>
change-saga add-requirement --input FILE|- [--id ID] [--if-snapshot TOKEN] [--json|--quiet] <saga>
change-saga revise-requirement --requirement URN --input FILE|- --parent REVISION... [--if-snapshot TOKEN] [--json|--quiet] <saga>
change-saga set-requirement-state --requirement URN --state STATE --reason TEXT --parent EVENT... [--citation URN...] [--if-snapshot TOKEN] [--json|--quiet] <saga>
change-saga add-trace --input FILE|- [--id ID] [--repo PATH] [--if-snapshot TOKEN] [--dry-run] [--json|--quiet] <saga>
change-saga set-trace-state --trace URN --state active|withdrawn --reason TEXT --parent TRACE-OR-EVENT... [--if-snapshot TOKEN] [--json|--quiet] <saga>
```

`--input` is one JSON object with the semantic fields from the corresponding
record, excluding `$schema`, `version`, `created_at`, and fields supplied as
flags. Unknown input fields are rejected. Complex criterion and endpoint
objects are never encoded through shell-expanded mini-languages. `--repo` is
accepted where diff currency must be resolved.

`add-requirement` creates the identity, initial revision, and initial proposed
event as one atomic package. `revise-requirement` and both state commands fail
with `conflict` unless `--parent` names exactly the current head set.
`add-trace --dry-run` resolves endpoint kinds, canonicalizes a symmetric pair,
checks revision and diff currency, and returns the record it would create.

`--if-snapshot` is optional but recommended whenever a mutation follows reads.
A mismatch is `stale_snapshot` before any file is created. User-supplied IDs
make retries safe: if an existing ID has the same canonical semantic payload,
the command returns the original resource with `replayed: true`; a different
payload is `conflict`. Generated IDs remain cryptographically random stable
IDs. A failed multi-file mutation leaves no partial package.

Without `--json`, commands print a short human-readable created resource and
its URN. With `--json`, they emit exactly one `change-saga.ai/v1` envelope. The
success data is:

```json
{
  "resource": "urn:change-saga:checkout-rewrite:requirement:refund-window",
  "records": [
    "urn:change-saga:checkout-rewrite:requirement:refund-window:revision:refund-window-r1",
    "urn:change-saga:checkout-rewrite:requirement:refund-window:state:refund-window-proposed"
  ],
  "replayed": false
}
```

The common error codes and exit mapping remain those of the query API.
Mutations never edit source files, apply suggestions, fetch citations, run Git
staging/commit/push, or start a server.

## 8. Query API

The envelope remains `change-saga.ai/v1`:

```json
{
  "schema": "change-saga.ai/v1",
  "ok": true,
  "snapshot": "sha256:...",
  "data": {},
  "page": {
    "total": 42,
    "returned": 100,
    "has_more": false,
    "next_cursor": null
  }
}
```

Every invocation writes exactly one envelope. Failures set `ok: false` and use
the existing stable codes, including `invalid_argument`, `not_found`,
`invalid_saga`, `source_unavailable`, `stale_snapshot`, `conflict`,
`unsafe_path`, `too_large`, and `internal`. Clients branch on codes, never
message text.

### 8.1 Operations

```text
change-saga query requirements --saga PATH [--requirement URN] [--state STATE] [--kind KIND] [--cursor TOKEN] [--limit N] [--repo PATH]
change-saga query requirement-history --saga PATH --requirement URN [--cursor TOKEN] [--limit N] [--repo PATH]
change-saga query traces --saga PATH [--from URI] [--to URI] [--type TYPE] [--state active|withdrawn] [--currency CURRENCY] [--cursor TOKEN] [--limit N] [--repo PATH]
change-saga query citations --saga PATH [--citation URN] [--requirement URN] [--cursor TOKEN] [--limit N] [--repo PATH]
change-saga query readiness --saga PATH [--requirement URN] [--status ready|blocked] [--cursor TOKEN] [--limit N] [--repo PATH]
```

`requirements` returns `data.requirements`. Each item contains the requirement
URN and ID, revision and state head URNs, nullable singular current revision
and state, the current definition when unambiguous, trace counts by type and
currency, and per-record Git attribution.

`requirement-history` returns `data.events`, a discriminated union of
`revision`, `lifecycle`, `trace`, and `trace_state` records relevant to the
requirement. It includes historical records and deterministic graph-depth,
timestamp, then ID ordering. It never fabricates a single linear history from
concurrent heads.

`traces` returns `data.traces`, including relation endpoints, pinned revisions,
logical state, currency, head URNs, rationale, citations, and provenance. A
filter is part of cursor identity. `--from` and `--to` accept resource or diff
URIs, not paths.

`citations` returns `data.citations`, including source metadata and provenance.
`--requirement` includes citations reachable from the current revision,
current lifecycle state, implementation exemptions, and current traces. It
does not download or embed remote content beyond the recorded excerpt.

`readiness` returns `data.summary` and paginated
`data.requirements`. Summary counts are over the full filtered result, not the
current page:

```json
{
  "summary": {
    "status": "blocked",
    "adoption": "adopted",
    "accepted": 4,
    "ready": 3,
    "blocked": 1,
    "unresolved_proposed": 1,
    "diff_coverage_complete": true
  },
  "requirements": [
    {
      "requirement": "urn:change-saga:checkout-rewrite:requirement:refund-window",
      "status": "blocked",
      "checks": {
        "definition": "pass",
        "narrative": "pass",
        "implementation": "pass",
        "verification": "fail",
        "dependencies": "pass",
        "conflicts": "pass"
      },
      "reasons": [
        {
          "code": "criterion_unverified",
          "criterion": "urn:change-saga:checkout-rewrite:requirement:refund-window:criterion:after-deadline",
          "message": "No current verified claim traces to this criterion."
        }
      ]
    }
  ]
}
```

Reason codes are stable data; messages are explanatory prose. Initial codes
include `history_conflict`, `definition_incomplete`, `citation_missing`,
`narrative_missing`, `criterion_unimplemented`, `criterion_unverified`,
`claim_unverified`, `claim_evidence_stale`, `claim_evidence_unmapped`,
`dependency_not_ready`, `accepted_conflict`, `trace_stale`, and
`scope_unresolved`.

### 8.2 Schema discovery and pagination

`change-saga query schema <operation>` advertises these counted paths:

| Operation | `pagination.kind` | `pagination.counted_path` |
| --- | --- | --- |
| `requirements` | `cursor` | `data.requirements` |
| `requirement-history` | `cursor` | `data.events` |
| `traces` | `cursor` | `data.traces` |
| `citations` | `cursor` | `data.citations` |
| `readiness` | `cursor` | `data.requirements` |

For every page, the array length at `counted_path` equals `page.returned`.
`page.total` is the filtered collection size, is constant across a cursor walk,
and is independent of the page limit. Default and maximum limits remain 100
and 1,000. Cursors are opaque, integrity checked, and bound to operation,
normalized filters, sort order, and snapshot. They survive an unchanged
process restart. Reusing one with another operation or filter is
`invalid_argument`; using it after relevant saga or source changes is the
retryable `stale_snapshot` error.

The snapshot includes the existing source comparison and saga inputs plus all
requirement, citation, and trace records. Clients preserve one snapshot across
related query walks and restart if it changes.

Default ordering is canonical requirement URN, citation URN, or trace URN.
History ordering is graph depth, `created_at`, then record ID. Readiness uses
requirement URN. Adding an unrelated record does not change the order of
existing records, though it changes the snapshot and invalidates a cursor as
required.

## 9. Coverage, trace completeness, and readiness

### 9.1 Existing diff coverage is unchanged

`overview.data.coverage`, `status --json`, `query gaps`, `query mappings`,
`query fragment-diffs`, and `query diff-owners` retain `mapping_only` scope.
Requirement traces never add a diff owner and cannot make an uncovered atom
covered. Coverage records never implicitly satisfy a requirement.

This avoids two unsafe shortcuts: citing a broad file cannot satisfy a
criterion, and linking a requirement cannot conceal unmapped changed lines.

### 9.2 Requirement readiness

A requirement is in delivery scope only when its unique current lifecycle
state is `accepted`. Its readiness is `ready` exactly when all checks below
pass for its unique current revision:

1. **Definition:** title and statement are non-blank, at least one criterion is
   present, all referenced citations resolve, and revision/state histories have
   one head each.
2. **Narrative:** at least one active, current `addresses` trace points from an
   existing narrative target to the requirement at its current revision.
3. **Implementation:** every criterion whose `implementation.required` is true
   has at least one active, current `implements` trace from a current line/event
   diff URI. An explicit non-code criterion passes only with its required
   rationale and citations.
4. **Verification:** every criterion has an active, current `verifies` trace
   from an existing claim. The claim's latest verification is `verified`, all
   claim evidence is current, and each evidence atom is already mapped to the
   claim's narrative target. A later `failed`, `inconclusive`, or `unverified`
   result supersedes an earlier verified result for readiness.
5. **Dependencies:** every target of an active, current `depends_on` relation is
   itself accepted and ready. A dependency cycle is invalid rather than
   recursively considered ready.
6. **Conflicts:** there is no active, current `conflicts_with` relation to
   another accepted requirement.

`refines` and `supersedes` are explanatory only. They do not inherit traces or
change lifecycle state. A requirement revision immediately makes fulfillment
traces pinned to its parent revision stale. This is intentional: the author
must confirm or replace the relations for the new wording.

### 9.3 Saga readiness

The requirements readiness summary is:

- `not_applicable` when the capability has not been adopted;
- `blocked` when it is adopted and there is any proposed requirement, any
  conflicted history, or any accepted requirement that is not ready; and
- `ready` when it is adopted, has at least one accepted requirement, has no
  proposed requirements, and every accepted requirement is ready.

An adopted saga with zero accepted requirements is `blocked`. Deferred,
rejected, and retired requirements are reported but do not otherwise block.

`data.summary.diff_coverage_complete` reports the existing mapping-only result
alongside requirement readiness. It does not alter the readiness checks above.
A consumer may define a delivery gate as both diff coverage complete and
requirements readiness ready, but the format does not equate that gate with
correctness or approval.

The existing `change-saga status` exit semantics remain unchanged for backward
compatibility. A new `change-saga requirements-status --json <saga>` may return
the readiness projection and exit 0 for `ready` or `not_applicable`, and 3 for
`blocked`; it must not relabel `status` coverage as requirements readiness.

## 10. Validation rules

`change-saga validate` remains structural and referential rather than a merge
gate. It adds these errors:

- malformed or unknown fields, unsupported record versions, unsafe/symlinked
  packages, filename/ID disagreement, or duplicate IDs;
- a missing initial revision or proposed event, multiple roots, a cycle,
  unreachable graph node, cross-requirement parent, or missing parent;
- a lifecycle transition not in the table above;
- a citation/endpoint with invalid syntax, userinfo, a cross-saga URN, or a diff
  URI for another source repository;
- a trace type whose endpoints violate the closed matrix, a self-relation, a
  cyclic `refines`/`depends_on`/`supersedes` graph, or a noncanonical duplicate
  symmetric conflict; and
- an implementation exemption without its rationale and citation.

Multiple history heads, stale traces, removed-criterion endpoints, missing
fulfillment traces, unverified claims, and unresolved proposed requirements
are validation warnings plus readiness failures. They remain queryable so an
author can discover and reconcile them through the public API. Coverage gaps
remain status/query results, not validation errors.

Unknown fields are never silently retained and then discarded on rewrite.
Validators report paths only as safe saga-relative diagnostics. `validate
--fix` does not invent requirements, lifecycle decisions, citations, traces,
or graph reconciliations.

## 11. Backward and transport compatibility

- Existing v2 sagas with no requirements directories load with zero new
  issues. Existing data-operation payloads, paths, page totals, sort orders,
  and exit statuses remain unchanged. Query discovery/help add the five new
  operation names as intended.
- Existing target and diff URI grammar is unchanged. New URNs are additive and
  cannot be mistaken for narrative targets by old mutation commands.
- Existing claims and verifications are not rewritten. Their additive URNs are
  projections in the requirements API.
- The envelope stays `change-saga.ai/v1`. Adding named operations and fields in
  their new `data` objects is compatible with v1 clients; removing or changing
  an existing field requires `change-saga.ai/v2`.
- Existing `overview.data.coverage.complete` and `status` retain mapping-only
  meaning. Requirements summaries are not inserted into their page counts.
- New schemas are closed and versioned independently under format v2. A future
  incompatible record shape uses format v3 or a new record version; readers do
  not guess.
- CLI, a future local HTTP adapter, and a future stdio MCP adapter call the same
  application service and return the same domain objects. No transport may
  inspect metadata paths directly or implement a second readiness algorithm.

## 12. Testable acceptance criteria

An implementation of this proposal is complete only when all of the following
are automated tests:

1. **Schema contract:** valid examples for all six schemas pass draft 2020-12
   validation; every missing required field, extra field, bad enum, bad ID,
   malformed URN, and invalid conditional exemption fails both schema and Go
   validation.
2. **Legacy floor:** the existing canonical-v2 and noncanonical-repository
   compatibility fixtures load exactly as before with no new requirement
   warning. Golden files for existing data operations remain byte-for-byte
   unchanged; only discovery/help goldens add the new operation names.
3. **Stable identity:** moving a requirement package without changing its IDs
   does not change any URN. Filename/ID mismatch and ID reuse are rejected.
4. **Atomic creation:** fault injection at every write in `add-requirement` and
   `add-trace` leaves either the complete package or no package. Concurrent
   writers cannot overwrite an existing record.
5. **Optimistic concurrency:** stale `if_snapshot` and incomplete `--parent`
   head sets create no files and return `stale_snapshot` or `conflict` with the
   documented exit code.
6. **Branch reconciliation:** merging two independently appended revisions or
   lifecycle events exposes two heads and `history_conflict`; appending one
   record whose parents are both heads restores a unique current value while
   preserving both branches in history.
7. **Lifecycle table:** every allowed transition succeeds and every other
   transition fails. Revising an accepted requirement leaves it accepted but
   makes revision-pinned fulfillment traces stale.
8. **Typed relations:** every valid endpoint combination in the relation table
   loads, every other combination fails, dependency/refinement/supersession
   cycles fail, and symmetric conflict duplicates normalize deterministically.
9. **Provenance:** committed, uncommitted, and history-unavailable records
   return truthful attribution. Payload timestamps and any recorded-input text
   are never presented as author identity.
10. **Citation safety:** query never fetches a remote URI, rejects URL userinfo
    and local file URIs, bounds excerpts, and reports a missing or historical
    diff citation without reading arbitrary filesystem content.
11. **Query discovery:** `query schema` lists every new operation, exact data
    path, and pagination contract. Help, invalid input, success, and domain
    failure each emit exactly one JSON envelope.
12. **Pagination:** for fixtures larger than 1,000 records, every page length
    equals `page.returned`, a full walk equals `page.total`, cursors survive an
    unchanged reopen, and changed inputs or cross-operation/filter cursor reuse
    fail with the specified codes.
13. **Readiness truth table:** fixtures cover every readiness reason, a later
    failed verification overriding an earlier verified result, dependencies,
    accepted conflicts, non-code exemptions, proposed scope, no adoption, and
    adopted-but-empty state.
14. **Coverage separation:** adding or withdrawing any requirement trace never
    changes `query gaps`, `query mappings`, `overview.data.coverage`, or legacy
    `status` output and exit code. Adding coverage never makes a criterion pass
    without its typed current traces and verified claim.
15. **Bounded behavior:** a 10,000-requirement fixture opens and serves a
    one-page query without materializing fragment bodies or fetching citation
    content; list responses remain capped at 1,000 records.
16. **Transport conformance:** when another transport is added, the same
    fixtures produce equivalent domain data, ordering, cursor behavior,
    readiness reasons, and errors as the CLI adapter.

These criteria deliberately test observable contracts rather than storage
layout lookups by clients. The directories in this proposal are an engine
implementation detail; stable URNs and the versioned query API are the public
interface.
