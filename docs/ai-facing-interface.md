# AI-facing interface architecture

Status: **accepted for incremental implementation**

Date: 2026-08-19

Implementation: the read application core and structured `change-saga query`
CLI are available, including mapping scrutiny, claims, and verification
history. Authoring commands create claims and append verification results.
General review-overlay writes, the UI HTTP adapter, and MCP remain gated as
described below.

## Context

An AI reviewer needs to understand and update a Change Saga through a stable
application contract. Requiring it to discover `*.chapter`, `*.fragment`, or
`___review` paths with filesystem searches would couple the client to storage
layout, bypass validation, and make reverse diff ownership unnecessarily hard.

The current implementation already has most of the domain primitives, but not
one application boundary:

- `saga.Load` validates and builds the recursive saga and review model.
- `gitdiff.Read` resolves the source comparison and emits stable diff atoms.
- `coverage.Evaluate` resolves evidence selectors to atoms and produces
  uncovered, overlapping, and stale-reference results. Its reverse ownership
  map is currently internal JSON state.
- `reviewstore` performs append-friendly mutations, while the CLI and HTTP
  handlers independently resolve inputs and call it.
- The server builds additional target, thread, diff, and review indexes inside
  HTML view-model functions.

There is also a format mismatch that must be resolved before exposing new AI
writes. Version 2 records currently require `author` or `created_by`, and every
CLI and HTTP write accepts those values. The product decision in
`ux-reframe.md` is stricter: the reviewer is the **committer** of the Git commit
that first introduces the event file. Git author fields are distinct and are
not review identity. A request-supplied name is not authoritative and new
records must not ask for one.

## Decision

Create a transport-neutral Go application layer, provisionally
`internal/reviewapp`, and make it the only owner of query indexing, input
resolution, write validation, and result types. A `Session` represents one
validated saga/source snapshot:

```go
type OpenOptions struct {
    SagaRoot  string
    SourceDir string
}

type Session interface {
    Overview(context.Context, OverviewQuery) (Overview, error)
    Children(context.Context, ChildrenQuery) (ChildrenPage, error)
    ReadFragment(context.Context, FragmentQuery) (FragmentContent, error)
    FragmentDiffs(context.Context, FragmentDiffQuery) (FragmentDiffs, error)
    DiffOwners(context.Context, DiffOwnerQuery) (DiffOwnership, error)
    Reviews(context.Context, ReviewQuery) (ReviewPage, error)
    Gaps(context.Context, GapQuery) (GapPage, error)
    Mappings(context.Context, MappingQuery) (MappingPage, error)
    Claims(context.Context, ClaimQuery) (ClaimPage, error)
    Verifications(context.Context, VerificationQuery) (VerificationPage, error)

    CreateThread(context.Context, CreateThread) (MutationResult, error)
    Reply(context.Context, Reply) (MutationResult, error)
    SetThreadState(context.Context, SetThreadState) (MutationResult, error)
    RecordReview(context.Context, RecordReview) (MutationResult, error)
    RecordDiffReview(context.Context, RecordDiffReview) (MutationResult, error)
}
```

The interfaces above describe capabilities, not a requirement that callers
hold a process open. The first adapter will be structured JSON CLI commands.
Each invocation opens a session, executes one request, writes one JSON result,
and exits. The installed UI will use an HTTP adapter over the same session
methods while `change-saga open` is running. A thin stdio MCP adapter may be added as a
separate binary after the CLI contract and attribution migration prove stable.
The core package must not import HTTP, CLI flag, JSON-RPC, or MCP packages.

This makes a daemon optional. Repeated requests may pay process startup and Git
comparison costs, but correctness and deployability are more important at the
current scale. A long-lived UI or MCP process can retain a session and rebuild
it when its snapshot changes without changing the domain API.

## Shape comparison

| Shape | Strengths | Costs and risks | Decision |
| --- | --- | --- | --- |
| Structured JSON CLI | Already installable with the product; inherits local filesystem permissions; easy to invoke from any agent; one request has an obvious lifetime; no listening socket | Process and Git-diff cost per call; shell arguments are awkward for nested writes; capability discovery is weaker than MCP | Implement first. Complex bodies are JSON on stdin, never shell-expanded flags. |
| Local Go HTTP API | Natural for the installed browser UI; supports lazy page loading and retained indexes; familiar streaming and status semantics | Introduces listener authentication, CSRF/origin, lifecycle, and port concerns; a CLI-to-HTTP requirement would make the daemon mandatory | Use only inside `change-saga open` initially. The CLI calls the Go application layer directly. Documented endpoints mirror, rather than define, the contract. |
| Thin stdio MCP adapter | Tool discovery, typed schemas, resource pagination, and no network listener; a good fit for MCP-capable agents | Adds protocol/dependency surface before names and payloads stabilize; not every agent host supports MCP; easy to let tool handlers accumulate business logic | Defer the dependency. Later ship a separate `saga-mcp` stdio binary that performs a mechanical mapping to the application layer. |

## Resource and identifier model

Clients address domain resources, never metadata paths.

- Saga, chapter, section, and fragment: the existing
  `urn:change-saga:<saga-id>:...` target URNs.
- Thread, message, and event: their existing stable IDs, always accompanied by
  their kind in results. APIs do not accept a path synthesized from an ID.
- Diff atom or selector: the complete `saga-diff://v1/line?...` or
  `saga-diff://v1/event?...` URI.
- Changed file: the complete `saga-diff://v1/file?...` URI.
- Fragment asset: `(fragment target URN, normalized relative asset name)`.
  Asset names are resolved only after containment and symlink checks.

Directory paths may be returned as diagnostics for a person, but are neither
stable IDs nor accepted resource handles. Event IDs remain time-plus-random
identifiers. Each mutation also accepts an optional caller-generated
`request_id` matching the stable-ID grammar. Repeating a `request_id` with the
same canonical payload returns the first result; reusing it with a different
payload is a conflict. This prevents an agent retry from creating duplicate
comments or approvals while preserving exclusive, one-event-per-file writes.

Every session returns a `snapshot` token derived from:

- the resolved source base OID and product-patch head identity;
- the saga repository HEAD, when available; and
- a digest of relevant tracked and uncommitted saga files.

Write requests may include `if_snapshot`. A mismatch fails before any file is
created. Append-only writes do not require the token, but agents should use it
when a decision depends on a previously read target, thread state, or diff.

## Common JSON envelope and errors

All machine commands emit exactly one JSON value. Successful results use:

```json
{
  "schema": "change-saga.ai/v1",
  "ok": true,
  "snapshot": "sha256:...",
  "data": {},
  "page": {
	"total": 1373,
	"returned": 100,
	"has_more": true,
    "next_cursor": "eyJ2IjoxLCJvcCI6ImdhcHMifQ..."
  }
}
```

Errors are stable domain values rather than parsed prose:

```json
{
  "schema": "change-saga.ai/v1",
  "ok": false,
  "error": {
    "code": "stale_snapshot",
    "message": "the saga changed after it was read",
    "retryable": true,
    "details": {
      "expected": "sha256:...",
      "actual": "sha256:..."
    }
  }
}
```

Defined codes are `invalid_argument`, `not_found`, `invalid_saga`,
`source_unavailable`, `stale_snapshot`, `conflict`, `unsafe_path`,
`unsupported_media`, `too_large`, and `internal`. Validation details use the
existing `{severity,path,message}` issue shape. Error details must not contain
absolute paths unless the user explicitly enabled diagnostic paths.

CLI exit codes are 0 for success, 2 for invalid requests, 3 for an invalid or
incomplete saga result, 4 for conflict/stale snapshot, 5 for not found, 6 for a
safety rejection, 7 for unavailable source/history, and 1 for unexpected
failures. HTTP maps the same errors to conventional 400, 409, 404, 403, 422,
503, and 500 statuses. MCP returns the code and details as structured tool
error content; transport error numbers are not domain API.

## Structured CLI contract

The implemented read command group is `change-saga query`. It always emits the
envelope above and does not need a separate `--json` switch. Supported
authoring writes remain explicit top-level commands described below.

Common read form:

```text
change-saga query <operation> --saga PATH [--repo PATH] [operation flags]
```

Discover a response before parsing it:

```text
change-saga query schema <operation>
```

This requires no saga and returns the operation's stable `data_paths` plus its
pagination kind and continuation fields. Operation-specific `--help` includes
the same paths. Cursor pages expose `page.total`, `page.returned`,
`page.has_more`, and `page.next_cursor`; consumers must follow the cursor while
`has_more` is true. `total` is the size of the filtered result at the response
snapshot, not merely the current page. The schema's
`pagination.counted_path` identifies the collection that `page.total` and
`page.returned` count. Other collections in the response may be derived from
that page and must not be compared with those counts.

The operations are:

```text
change-saga query overview       --saga PATH [--repo PATH]
change-saga query children       --saga PATH --parent TARGET [--cursor TOKEN] [--limit N]
change-saga query fragment       --saga PATH --target FRAGMENT [--offset N] [--limit N]
change-saga query fragment-diffs --saga PATH --target TARGET [--cursor TOKEN] [--limit N]
change-saga query diff-owners    --saga PATH --diff URI [--cursor TOKEN] [--limit N]
change-saga query reviews        --saga PATH [--target TARGET] [--thread ID] [--state STATE]
change-saga query gaps           --saga PATH [--kind uncovered|stale|overlap] [--cursor TOKEN] [--limit N]
change-saga query mappings       --saga PATH [--target TARGET] [--sort scrutiny|target|path] [--minimum-score N]
change-saga query claims         --saga PATH [--target TARGET] [--status unverified|verified|failed|inconclusive]
change-saga query verifications  --saga PATH [--claim ID] [--status unverified|verified|failed|inconclusive]
```

`overview` returns saga identity, source snapshot, the root overview fragment
summaries, and direct chapter summaries. A chapter summary contains target,
title, order, latest review state, open thread count, child/fragment counts,
and whether it owns current or stale diffs. It does not inline chapter content.

`children` provides ordered recursive traversal one level at a time. Each node
has `kind`, `target`, `parent`, `title`, `order`, `has_children`, and compact
review/diff counts. Diff counts report `direct_current`, `direct_stale`,
`descendant_current`, and `descendant_stale`; `current` and `stale` are their
inclusive totals. Fragments are nodes with a media type and byte size.

`fragment` reads the entrypoint without exposing its storage path:

```json
{
  "target": "urn:change-saga:checkout:fragment:request-flow",
  "id": "request-flow",
  "title": "Request flow",
  "media_type": "text/markdown",
  "content": {
    "encoding": "utf-8",
    "data": "# Request flow\n...",
    "offset": 0,
    "next_offset": null,
    "bytes": 2418,
    "sha256": "..."
  },
  "assets": [
    {"name": "diagram.png", "media_type": "image/png", "bytes": 18342}
  ],
  "landmarks": [
    {
      "target": "urn:change-saga:checkout:fragment:request-flow:landmark:submit",
      "id": "submit",
      "label": "Submit request",
      "description": "The validated request crosses into persistence.",
      "selector": {"type": "element", "element_id": "submit"},
      "diffs": {"current": 12, "stale": 0}
    }
  ]
}
```

Text is UTF-8; binary data is base64. Reads are chunked and capped. An
`asset` operation can be added with the same offset contract when clients need
supporting HTML/SVG assets. The AI read path never executes active content.

`fragment-diffs` returns both the committed selectors and their resolution for
any saga, chapter, section, fragment, or landmark target. It paginates
`data.selectors`; `data.atoms` and `data.stale` are the resolution of the
selectors returned on that page:

```json
{
  "target": "urn:change-saga:checkout:fragment:request-flow",
  "selectors": [{"uri": "saga-diff://v1/line?...", "note": "...", "status": "current"}],
  "atoms": [{"uri": "saga-diff://v1/line?...", "path": "api.go", "side": "new", "line": 18, "content": "..."}],
  "stale": []
}
```

`diff-owners` accepts an atom/event URI or a file URI. An atom response contains
the atom, every owning target, evidence note and selector, and threads anchored
to that diff. A file response groups all current atoms and owners in stable
path/side/line order. Thus fragment-to-diff and diff-to-fragment navigation use
the same coverage index rather than separately reinterpreting metadata.
Each owner also carries a mapping signal with atom/file breadth, target breadth,
stale-selector count, a 0–100 scrutiny score, and stable reason codes. The score
ranks where independent inspection should begin; it is not a quality or
correctness grade.

`reviews` returns normalized threads, messages, anchors, latest state, target
review events, and file-review events. Each review event has attribution:

```json
{
  "id": "20260819T184200Z-ab12cd34",
  "kind": "approval",
  "state": "approved",
  "attribution": {
    "status": "committed",
    "commit": "9f42...",
    "committer": {"name": "Ada Lovelace", "email": "ada@example.test"},
    "committed_at": "2026-08-19T18:42:00Z"
  }
}
```

The other attribution statuses are `uncommitted` and `history_unavailable`.
Only `committed` has a committer identity. Git author fields are deliberately
absent from this schema because they are not the reviewer. A legacy payload
name may be exposed separately as `legacy_claimed_author` during migration,
never as `attribution.committer`.

Every atom carries `content`, including when the changed line is empty. `kind`
discriminates atom shape: a `line` atom always has `path`, `side`, `line`, and
`content`; an `event` atom always has `event`. `change-saga status --json`
follows the same rule for its collections — `uncovered`, `overlaps`, `orphans`,
`targets`, `saga_changes`, and `schema_issues` are always present and encode as
`[]` when empty, so a fully mapped saga reports `"uncovered": []` rather than
`null` or a missing key.

`gaps` exposes uncovered atoms, stale selectors (the current `Orphans`), and
overlaps as separate typed records. `stale` is the public term; `orphan` remains
an internal compatibility name. Results include enough selector, target, and
evidence-file diagnostic data to repair a link through a later authoring API,
without requiring metadata searches.

`mappings` groups selectors by target and independent evidence file, then
reports atom count, source-file count, target concentration, notes, stale
selectors, and explicit scrutiny reasons. The default ordering puts the
highest score first. Thresholds deliberately produce warnings rather than
validation errors because some cross-cutting mappings are legitimate.

`claims` returns falsifiable author assertions with Git attribution, exact
evidence selectors, current resolved atoms, whether those atoms are actually
mapped to the claim's target, and the latest verification result. Claims never
contribute to coverage. A claim with no result is reported as `unverified`.
`verifications` returns the full append-only history, including method,
summary, optional reproducible command, timestamp, and committer attribution.

An AI correctness review should use three passes: inspect the diff before
reading the author's conclusions; then query mappings, claims, verifications,
and narrative intent; finally reconcile contradictions and independently test
the claims. `coverage.scope` and `status.coverage_scope` are `mapping_only` to
make explicit that all-atoms-mapped detects omissions rather than correctness.

## Diff-only maintenance impact

`change-saga compare --json` is the structured read boundary for maintaining a
long-lived codebase Saga. It accepts either `--base/--head` for a direct Git
comparison or `--against-saga` for another Saga's declared comparison. The
first positional Saga is always the maintained document.

The command does not read fragment content to infer staleness. It reconstructs
the maintained Saga's evidence at the incoming comparison base and projects
incoming atoms through that ownership graph. Its `change-saga.impact/v1`
result contains baseline completeness, exact incoming identity, summary counts,
`targets` with `must_update` or `consider_update`, and ownerless `new_content`.
Each target includes its stable URN, kind, content path, evidence files, and
exact incoming atoms. A baseline that is incomplete or stale produces a
diagnostic and exit 3, because its impact list is not exhaustive.

This result is intentionally separate from `query`: it compares two source
snapshots rather than reading one immutable Saga session. It is read-only and
does not rewrite evidence or advance the maintained Saga's source declaration.

## Supported authoring mutations

`set-fragment-content` replaces a fragment entrypoint from a named file or
standard input while preserving its manifest and media type. `cover
--changed-lines` derives the exact changed line atoms for one path, optionally
filtered by side, and includes file events such as `add`. It is appropriate
only when the entire named file change belongs to the target.

`query mappings` returns `evidence_file` as the stable repair handle.
`remove-coverage --record PATH` deletes exactly that record.
`replace-coverage --record PATH --batch FILE|-` resolves every replacement
before writing, then atomically swaps the old record for one or more focused
records. This supports splitting, retargeting, and note repair without direct
metadata edits. Coverage commands accept `--json` for bounded counts and
created evidence paths. Failures remain one structured JSON result on stdout
with a non-zero exit status. `--quiet` suppresses successful output.

Unique target IDs are accepted anywhere an authoring command accepts a path or
URN. Ambiguous landmark IDs are rejected with the matching URNs.

## Proposed structured review mutation contract

The future structured review-overlay write form is:

```text
change-saga mutate <operation> --saga PATH [--repo PATH] --input -
```

`--input -` reads one bounded JSON object from stdin. A named file may be
supported, but inline JSON flags are intentionally not supported. Operations
and request bodies are:

```text
thread-create
thread-reply
thread-state
review-record
diff-review-record
```

```json
{
  "request_id": "agent-run-42-comment-1",
  "if_snapshot": "sha256:...",
  "target": "urn:change-saga:checkout:fragment:request-flow",
  "kind": "comment",
  "anchor": {"type": "text", "text": {"exact": "retry forever", "prefix": "will ", "suffix": " when"}},
  "body": {"media_type": "text/markdown", "text": "Should this be bounded?"},
  "attachments": []
}
```

`thread-create` also permits `kind: "suggestion"`, a diff anchor, and a
`replacement` string. Region and drawing anchors use the existing normalized
shape schema, so annotation creation is not a separate persistence operation.

```json
{
  "request_id": "agent-run-42-reply-1",
  "if_snapshot": "sha256:...",
  "thread": "20260819T184200Z-ab12cd34",
  "body": {"media_type": "text/markdown", "text": "Confirmed in the fallback path."},
  "attachments": []
}
```

```json
{
  "request_id": "agent-run-42-resolve-1",
  "if_snapshot": "sha256:...",
  "thread": "20260819T184200Z-ab12cd34",
  "state": "resolved"
}
```

```json
{
  "request_id": "agent-run-42-approval-1",
  "if_snapshot": "sha256:...",
  "target": "urn:change-saga:checkout:chapter:backend",
  "state": "approved",
  "body": "The retry concern is resolved."
}
```

```json
{
  "request_id": "agent-run-42-file-review-1",
  "if_snapshot": "sha256:...",
  "diff": "saga-diff://v1/file?...",
  "state": "reviewed"
}
```

There is deliberately no `author`, `created_by`, commit, or attribution input.
Writers create local event files and return attribution status `uncommitted`.
They do not run `git add` or `git commit`. After the user commits the event,
the read side derives identity from that introducing commit.

## HTTP mapping

When `change-saga open` is active, the installed UI uses the following JSON endpoints:

```text
GET  /api/v1/overview
GET  /api/v1/children?parent=...
GET  /api/v1/fragments/{escaped-target}
GET  /api/v1/fragments/{escaped-target}/diffs
GET  /api/v1/diffs/owners?uri=...
GET  /api/v1/reviews?target=...
GET  /api/v1/gaps?kind=...
POST /api/v1/threads
POST /api/v1/threads/{id}/replies
POST /api/v1/threads/{id}/events
POST /api/v1/reviews
POST /api/v1/diff-reviews
```

Handlers only decode, apply transport limits, call `reviewapp`, encode the
common envelope, and map errors. Browser-friendly redirects and multipart
forms may remain as compatibility endpoints, but new UI code uses JSON.

The server continues to bind to loopback by default. On startup it creates a
random bearer token held in the page session, checks `Origin` on mutations,
sets restrictive CORS and CSP headers, rejects non-JSON mutation content, and
limits bodies. No endpoint accepts a saga root, source directory, or arbitrary
filesystem path after startup; those are fixed when the application opens.

The standalone CLI does not start or discover this server. An explicit future
`--connect` option may target an already-running instance, but direct calls
remain the default and must have identical results.

## MCP mapping

If usage validates the need, `cmd/saga-mcp` will expose these stdio tools:

| Tool | Required arguments | Optional arguments | Result data |
| --- | --- | --- | --- |
| `change_saga_overview` | `saga_root` | `source_root` | The `overview` result |
| `change_saga_children` | `saga_root`, `parent` | `source_root`, `cursor`, `limit` | The `children` page |
| `change_saga_read_fragment` | `saga_root`, `target` | `source_root`, `offset`, `limit` | The `fragment` content result |
| `change_saga_fragment_diffs` | `saga_root`, `target` | `source_root`, `cursor`, `limit` | The `fragment-diffs` page |
| `change_saga_diff_owners` | `saga_root`, `diff` | `source_root`, `cursor`, `limit` | The `diff-owners` page |
| `change_saga_reviews` | `saga_root` | `source_root`, `target`, `thread`, `state`, `cursor`, `limit` | The `reviews` page |
| `change_saga_gaps` | `saga_root` | `source_root`, `kind`, `cursor`, `limit` | The `gaps` page |
| `change_saga_create_thread` | `saga_root`, `target`, `kind`, `anchor`, `body` | `source_root`, `request_id`, `if_snapshot`, `replacement`, `attachments` | A mutation result with thread and message IDs |
| `change_saga_reply` | `saga_root`, `thread`, `body` | `source_root`, `request_id`, `if_snapshot`, `attachments` | A mutation result with the message ID |
| `change_saga_set_thread_state` | `saga_root`, `thread`, `state` | `source_root`, `request_id`, `if_snapshot` | A mutation result with the event ID |
| `change_saga_record_review` | `saga_root`, `target`, `state` | `source_root`, `request_id`, `if_snapshot`, `body` | A mutation result with the event ID |
| `change_saga_record_diff_review` | `saga_root`, `diff`, `state` | `source_root`, `request_id`, `if_snapshot` | A mutation result with the event ID |

Each MCP input schema sets `additionalProperties: false`, applies the existing
stable-ID, target-URN, diff-URI, anchor, and state constraints, and uses integer
ranges of `offset >= 0`, `1 <= limit <= 1000`. `attachments` uses bounded
content objects (`name`, `media_type`, and exactly one of UTF-8 `text` or base64
`data`), not host filesystem paths. Mutation result IDs and attribution status
are always returned even when the result is an idempotent replay.

Arguments are the same JSON request objects described above, with `saga_root`
and optional `source_root` supplied either at process initialization or per
tool according to host capabilities. Results use the same data objects. MCP
resource links may represent large fragment assets, but the adapter must still
call the application's bounded read method.

The MCP binary owns protocol negotiation and schema descriptions only. It does
not load metadata directly, shell out to the `change-saga` CLI, or implement alternate
validation. Initially it is a separate module or build target so the core CLI
does not take a mandatory MCP dependency. Add it only when at least one target
host gains material value from tool discovery or retained sessions beyond what
request-response CLI calls provide.

## Git-derived review identity

Attribution is an application service keyed by the event's actual relative
file path, which loaders retain as non-serialized provenance. For each event
file it:

1. Locates the Git worktree containing the saga. This may differ from the
   source checkout used for diff evaluation.
2. Verifies that the canonical event path is inside that worktree and saga.
3. Checks whether the path has an introducing commit reachable from the
   selected saga revision. The implementation uses Git's path history with
   rename following and addition filtering, rather than `blame` on editable
   identity fields.
4. Reads the canonical reviewer from the commit's committer name and email,
   plus the committer timestamp and full commit OID. Git author name, email,
   and timestamp are not review identity and are not returned in the canonical
   attribution object.
5. Reports `uncommitted` when the event has no introducing commit, including
   untracked and staged-new files. It reports `history_unavailable` when the
   saga is outside Git, history is shallow/missing, or the commit cannot be
   resolved.

History rewrites intentionally recompute attribution from the current graph.
The event payload is never a fallback identity. If a prior introducing commit
is no longer reachable, clients see the attribution from rewritten history or
`history_unavailable`; they must not silently show a stale cached person.

Migration requires making legacy `author` and `created_by` fields optional in
Go models, validation, and version 2 schemas while continuing to accept old
records. New writers omit them. Existing values load into
`legacy_claimed_author` for diagnostics only. UI and AI result models display
Git attribution, or an honest local/unavailable state. This compatibility
change must land before any new mutation adapter is declared safe.

The introducing file is specific to the semantic event: `thread.json` for a
thread, `message.json` for each message, and the individual JSON file for a
thread-state, target-review, or diff-review event. Committing a thread root and
its first message separately can therefore produce distinct, truthful
attributions.

## Application indexes and performance

Opening a session performs three bounded phases:

1. Load and validate saga structure and review records.
2. Read the source comparison and evaluate coverage once.
3. Build immutable in-memory indexes used by every query:
   `target -> node`, `parent -> ordered children`, `fragment -> selectors`,
   `atom URI/key -> atom`, `target -> atoms`, `atom -> assignments`,
   `target/diff -> threads`, `thread ID -> thread`, and
   `target/file URI -> chronologically ordered events`.

This consolidates logic currently split between `coverage` and server view
helpers. Reverse ownership is promoted from `coverage.Report.Ownership`'s
non-serialized implementation detail to an application result with target
metadata and evidence notes.

Fragment contents and Git attribution are lazy. Content is read only for the
requested fragment and hashed while streaming. Attribution is batched by
passing all event paths to one Git history process or a small bounded set of
processes, then cached for the session. Lists have deterministic sorting and
opaque pagination cursors; default and maximum page sizes are 100 and 1000.
Text/binary chunks default to 256 KiB and never exceed 1 MiB.

Request-response CLI calls rebuild the session. Measure before adding durable
caching. If repeated Git evaluation becomes significant, use a process-local
cache keyed by canonical saga/source roots, base OID, product head identity,
saga snapshot, and executable format version. Never persist an index inside
the saga, where it would create review noise and merge conflicts.

Long-lived HTTP/MCP processes invalidate the entire immutable session when a
relevant saga file, saga Git HEAD, or source comparison fingerprint changes.
Rebuilding wholesale first is safer than maintaining partially coherent
indexes. Filesystem notifications may reduce polling but cannot be the source
of correctness; every mutation and periodic read verifies the fingerprint.

## Read and write safety boundaries

Read operations:

- Canonicalize roots once and reject saga metadata/asset symlink escapes.
- Never execute HTML, SVG, JavaScript, or attachments for AI reads.
- Bound JSON bodies, fragment chunks, list pages, Git output, and request time.
- Pass Git arguments as argument vectors with `--end-of-options` and pathspec
  separation; never invoke a shell with client data.
- Do not return arbitrary filesystem content or accept metadata paths as IDs.

Write operations:

- May create only validated append-only review records below the selected saga:
  thread/message directories and individual state, review, or diff-review
  event files.
- Resolve targets and diff URIs through the current application indexes.
- Use canonical containment checks and exclusive file creation. A failure must
  not overwrite an existing event.
- Validate and size-limit anchors, Markdown, replacement text, and attachments
  before creating a directory. Stage temporary content outside the saga and
  rename a complete record into place where practical, cleaning partial
  records on failure.
- Never edit source files, apply suggestions, delete/rewrite events, run Git
  staging/commit/push, start a server, or follow a URL as a side effect.
- Treat approval and reviewed state as recorded opinions, not authorization to
  merge or modify source.

HTTP additionally requires loopback/session authentication and CSRF defenses.
MCP stdio inherits the permissions of its host but receives no broader path
access from the adapter. CLI mutation payloads should be supplied over stdin to
avoid shell quoting and accidental command-history exposure.

## Incremental delivery

1. **Read application core.** Add `reviewapp.Session`, promote the shared
   indexes, and implement overview, children, fragment, bidirectional diff
   ownership, reviews, and gaps. Add table-driven service tests using a real
   temporary Git comparison. No transport changes are required to validate the
   result types.
2. **Structured read CLI.** Add `change-saga query` operations and the common envelope,
   pagination, exit codes, and golden JSON integration tests. Update the AI
   skill to use these commands instead of filesystem discovery.
3. **Git attribution and compatibility.** Retain event-file provenance in the
   loader; add batched introducing-commit lookup; make identity fields optional
   for new records while accepting old schema records; render committed,
   uncommitted, and unavailable states. This is the gate for new writes.
4. **Command service and JSON mutations.** Move target resolution and
   `reviewstore` validation behind `reviewapp`; remove author inputs from the
   new APIs; add request idempotency, snapshot preconditions, atomic cleanup,
   and concurrent append tests. Keep legacy author-taking commands temporarily
   but mark them deprecated and never expose them as AI tools.
5. **UI HTTP adapter.** Replace duplicated HTML view-model indexing and form
   handlers with application queries/commands. Add loopback session auth,
   origin checks, route tests, and lazy chapter/diff loading.
6. **MCP evidence gate.** Exercise the JSON CLI with supported AI hosts. Add the
   stdio adapter only if tool discovery, resource handling, or retained-session
   performance produces a demonstrated benefit. Keep its conformance suite
   transport-independent and run the same fixtures against CLI, HTTP, and MCP.

## Prototype decision

No vertical-slice code is included with this decision. The tempting slice—an
`inspect` command built directly on the current `saga.Saga` structs—would
serialize user-supplied author fields, omit introducing-file provenance, and
make `coverage.Report.Ownership` appear to be a stable API before its target
metadata and evidence notes are normalized. That would validate JSON encoding,
not the important boundary.

The existing `status --json` command is sufficient evidence that
request-response evaluation works without a daemon: it loads the saga, reads
the Git comparison, evaluates coverage, emits structured results, and exits.
The next useful implementation slice is therefore delivery step 1, tested at
the application layer, followed immediately by the structured read CLI.

## Consequences and open decisions

Positive consequences:

- AI clients can inspect hierarchy, content, ownership, review state, and gaps
  without knowing the storage layout.
- CLI, installed UI, and a possible MCP server share result semantics and
  mutation safety.
- Basic automation remains a single local process invocation; no daemon or MCP
  dependency is mandatory.
- Append-only persistence and Git auditability remain the source of truth.

Costs:

- Result types intentionally duplicate parts of persistence structs so private
  paths and legacy identity do not leak across the boundary.
- A session fingerprint and idempotency mechanism add work before mutations.
- Git attribution can be more expensive than reading payload fields, so it
  requires batching and honest unavailable states.

Open decisions to settle with implementation fixtures:

1. Whether `snapshot` includes ignored-but-readable fragment assets or only
   files that can affect query results and writes.
2. Whether large assets should remain chunked tool results or become MCP
   resources in hosts that support them.
3. The retention location for request-id idempotency records. A separate
   append-only operation receipt is safest but becomes saga history; deriving
   the event filename from `request_id` is simpler but exposes client naming.
4. Whether the schema compatibility change remains an additive v2 relaxation
   or warrants version 3 before the format leaves experimental status.
