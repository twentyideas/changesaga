# Change Saga format v2

Status: experimental. Version 2 supersedes the unpublished v1 draft.

## 1. Model

A Change Saga is a Git-native review document with six layers:

1. **Overview** explains the change as a whole.
2. **Chapters** divide the change into independently reviewable units—roughly
   the PRs that might have existed if the work had been split.
3. **Sections and fragments** recursively organize each chapter; fragments are
   the smallest units of authored content.
4. **Diff links** connect a saga, chapter, section, or fragment to immutable source
   changes, including changes in another repository.
5. **Claims and verification** make falsifiable author assertions and their
   independently recorded verification state machine-readable.
6. **Review overlays** add anchored discussions without modifying the authored
   fragments.

Every persistent object is an ordinary file. Content and review data can be
branched, merged, audited, and committed independently.

## 2. Root and source

A saga root ends in `.saga` and contains `saga.json` conforming to
[`schema/v2/saga.schema.json`](schema/v2/saga.schema.json):

```json
{
  "$schema": "https://changesaga.dev/schema/v2/saga.schema.json",
  "version": 2,
  "id": "checkout-rewrite",
  "title": "Checkout rewrite",
  "source": {
    "repository": "https://github.com/acme/payments.git",
    "base": "main",
    "head": "HEAD"
  }
}
```

New sagas also contain a root `README.md` written by `change-saga init`. It is
non-normative reviewer bootstrap material: it identifies the directory as a
Change Saga, explains how to install and open the local reviewer, and directs
AI assistants to the structured `change-saga query` interface. Engines ignore
the file when loading content and calculating diff coverage, so older sagas
without it remain valid. Because a saga may arrive in an untrusted pull request,
the bootstrap tells assistants to obtain user permission before downloading or
executing software.

`source.repository` is a canonical absolute URI and identifies the source
repository, not necessarily the repository containing the saga. Repository
identities never contain URL userinfo; credentials in a remote URL are removed
before the identity is persisted, and a persisted identity that still carries
userinfo is invalid rather than silently stripped on read. An optional `pr`
records a positive `number`, an absolute `url`, or both.

`base` and `head` select the comparison to evaluate. Commit comparisons resolve
both revisions and record their actual Git merge base. `WORKTREE` is allowed as `head`; engines resolve the merge base of
the configured base and current `HEAD`, then compare that tree to the tracked
worktree. Untracked files are excluded.

`change-saga init` uses the canonical portable `origin` identity when available.
Without an origin, or when origin is itself a local path, the author must provide
a portable `--repository` URI or explicitly opt in to a local `file://` identity
with `--allow-local-repository`; a local home-directory path is never selected
silently. When a saga is stored
separately, readers provide a local source checkout with `change-saga status
--repo PATH` or `change-saga open --repo PATH`. Local checkout paths are runtime
configuration and are never committed into the portable format. Readers verify
a declared file identity or checkout origin against the declared repository; a
checkout without origin is unverifiable and fails closed. CLI authoring and
status commands expose
`--allow-repository-mismatch` for the exceptional case where a known-equivalent
checkout cannot be identified from its origin.

## 3. Chapters, sections, and fragments

The root overview is followed by direct child directories ending in `.chapter`.
Each contains `chapter.json` conforming to
[`schema/v2/chapter.schema.json`](schema/v2/chapter.schema.json):

```text
backend.chapter/
├── chapter.json
├── overview.fragment/
└── request-flow/
    ├── section.json
    └── interactive-flow.fragment/
```

```json
{ "version": 2, "id": "backend", "title": "Backend behavior", "order": 20 }
```

A chapter is a review boundary: it may own diffs, threads, and approvals, and
can be reviewed independently by a different person. Chapters are direct
children of the saga root and cannot nest. Ordinary directories inside them are
recursive sections containing `section.json`:

```json
{ "version": 2, "id": "request-flow", "title": "Request flow", "order": 20 }
```

A directory ending in `.fragment` is a fragment package rather than a section.
It contains `fragment.json` plus an entrypoint and any supporting files:

```text
interactive-flow.fragment/
├── fragment.json
├── index.html
├── app.js
├── styles.css
├── sample-data.json
├── ___landmarks/
│   └── submit-action.landmark/
│       ├── landmark.json
│       └── ___diffs/
└── ___diffs/
```

```json
{
  "version": 2,
  "id": "interactive-request-flow",
  "title": "Try the new request flow",
  "media_type": "text/html",
  "entrypoint": "index.html",
  "order": 30
}
```

The reference engine supports `text/markdown`, `text/plain`, `text/html`,
`image/svg+xml`, and raster `image/*` fragments. A fragment is directory-backed
so HTML and SVG can use JavaScript, CSS, images, data, and other relative assets.

`entrypoint` is a normalized package-relative slash path. It is never absolute,
never begins with a drive letter, never contains `\` or a control character,
never traverses with `.` or `..`, and never addresses `fragment.json` or a
reserved `___` path. Backslash is excluded because it is an ordinary filename
byte on Unix and a separator on Windows, so one saga would otherwise address two
different files. Entrypoints cannot escape their package. Engines should warn
about component names that cannot be checked out on every supported platform,
such as reserved Windows device names.

Markdown headings should declare a durable fragment-local anchor after the
visible heading text:

```markdown
## Request validation {#request-validation}
```

The anchor begins with a lowercase letter, contains only lowercase letters,
digits, and hyphens, is at most 64 characters, and is unique within its
fragment. Engines namespace it with the fragment target when producing a page
anchor. The reference validator warns about headings without explicit anchors
and rejects invalid or duplicate explicit anchors. Engines may derive fallback
anchors for older content, but authored anchors are the stable sharing contract.

Markdown footnotes are the standard prose citation form. Authors place the
citation after a focused implementation claim and make the reference definition
an exact-text landmark. When that landmark owns diff evidence, reference
renderers should let both the inline marker and the definition open the linked
code. Citation definition text should remain plain, unique within its fragment,
and focused on one claim so its exact-text selector is durable. Footnote syntax
does not itself create coverage; the landmark's independent `___diffs` records
remain the authoritative association.

Addressable subparts use one `<id>.landmark` package under `___landmarks/`.
Its `landmark.json` conforms to
[`schema/v2/landmark.schema.json`](schema/v2/landmark.schema.json) and contains a
stable fragment-local ID, a reviewer-facing label, and one selector:

- `heading` enriches an explicit Markdown heading anchor;
- `element` identifies an `id` in an HTML or SVG entrypoint;
- `text` identifies an exact quote, with optional prefix and suffix, in Markdown
  or plain text;
- `region` identifies a normalized rectangle in an image.

```json
{
  "version": 2,
  "id": "submit-action",
  "label": "Submit action",
  "description": "The validated request crosses into persistence.",
  "selector": { "type": "element", "element_id": "submit-action" }
}
```

Landmark IDs follow the Markdown-anchor grammar and are unique within the
fragment. A `heading` package intentionally shares its ID with the heading it
enriches. Engines combine the fragment target and landmark ID into a portable
page anchor and assign the landmark its own target URN. Each `___diffs/*.json`
inside the landmark package associates fully qualified diff atoms with that
exact narrative element; each association remains an independent file.
Meaningful visual landmarks should carry a concise semantic `description` that
explains their role without relying on geometry, color, or position. Query
clients return this description so non-visual consumers do not need to infer
meaning from SVG or HTML source.
Every code-bearing SVG or HTML node or edge should prefer its own stable element
ID and element landmark over evidence attached only to the enclosing fragment.

SVG element landmarks use the rendered element bounds as their on-canvas
interaction area by default, including groups, nodes, lines, paths, and graph
edges. Static SVG, HTML, and image landmarks may include a normalized `hotspot`
rectangle to override or supply that geometry. The renderer uses an external
overlay to reveal permalink and related-code controls directly on hover without
trusting or modifying the fragment document. A `region` selector is itself a
hotspot. The fragment target remains the portable fallback for media without a
native selector; authors may use an HTML wrapper with element landmarks when
inner addressability is important.

HTML and SVG fragments execute in an iframe with `sandbox="allow-scripts"` and a
network-denying Content Security Policy. They can execute bundled JavaScript but
cannot access the review application, navigate its parent, submit forms, open
popups, or make network connections. Engines must provide equivalent isolation;
they must not insert fragment HTML directly into trusted application markup.

## 4. Stable target URNs

Review and engine data use stable identifiers rather than filesystem paths:

```text
urn:change-saga:<saga-id>:saga
urn:change-saga:<saga-id>:chapter:<chapter-id>
urn:change-saga:<saga-id>:section:<section-id>
urn:change-saga:<saga-id>:fragment:<fragment-id>
urn:change-saga:<saga-id>:fragment:<fragment-id>:landmark:<landmark-id>
```

IDs are 1–128 characters from `A-Z`, `a-z`, `0-9`, `.`, `_`, and `-`, beginning
with an alphanumeric character. Chapter, section, and fragment IDs are unique
across the saga. Renaming or moving a directory does not change its target URN.

## 5. Absolute diff URIs

A diff link is a fully realized URI. It does not inherit repository or revision
state from the directory containing it.

Line range:

```text
saga-diff://v1/line
  ?repository=https%3A%2F%2Fgithub.com%2Facme%2Fpayments.git
  &base=6c5f...40-hex-oid
  &head=b127...40-hex-oid
  &path=internal%2Fcheckout%2Fhandler.go
  &side=new
  &start=18
  &end=42
```

File event:

```text
saga-diff://v1/event
  ?repository=https%3A%2F%2Fgithub.com%2Facme%2Fpayments.git
  &base=<identity>&head=<identity>
  &event=rename
  &old_path=internal%2Fold.go
  &new_path=internal%2Fnew.go
```

File review target:

```text
saga-diff://v1/file
  ?repository=https%3A%2F%2Fgithub.com%2Facme%2Fpayments.git
  &base=<identity>&head=<identity>
  &path=internal%2Fcheckout%2Fhandler.go
```

Required common parameters are `repository`, `base`, and `head`. Line URIs also
require `path`, `side` (`old` or `new`), `start`, and `end`. Event URIs use
`event=add|delete|type-change|rename|mode|binary|modify`. Rename carries exactly
`old_path` and `new_path`; every other event carries exactly `path`. `add` and
`delete` record file lifecycle independently of any changed-line atoms, so
empty-file changes remain coverable. `type-change` records transitions among
regular files, symlinks, and Gitlinks; `mode` records permission-only changes;
`binary` records content Git cannot express as lines. `modify` is the defensive
event for any other Git-reported file record that yields no more specific atom.
File URIs identify the complete changed file for review-progress events; they
are not valid coverage links.

The query parameter set is closed and every parameter occurs exactly once.
Canonical builders sort and escape the query and canonicalize the embedded
repository identity. Parsers reject duplicate or unknown parameters and reject
alternate encodings, ordering, userinfo, fragments, or other noncanonical
spellings rather than assigning them an ambiguous meaning.

The base identity is the comparison's resolved merge-base commit OID. The head identity is
`product-<sha256-of-binary-patch>` where the patch excludes paths beneath any
`.saga` directory. Product edits therefore make links stale, while committing
comments, replies, approvals, or other saga-only changes does not invalidate
otherwise identical evidence. `HEAD` and `WORKTREE` produce the same identity
when their tracked product changes are identical. Engines compare the complete
URI identity, preventing evidence from silently matching a similar path in a
different repository or product comparison.

## 6. Attaching diffs

The root, a section, or a fragment can contain `___diffs/*.json` conforming to
[`schema/v2/diff.schema.json`](schema/v2/diff.schema.json):

```json
{
  "version": 2,
  "diffs": [
    {
      "uri": "saga-diff://v1/line?repository=...&base=...&head=...&path=internal%2Fapi.go&side=new&start=18&end=42",
      "note": "The behavior demonstrated by this fragment"
    }
  ]
}
```

The containing object is the target of the evidence. One link may select a line
range; one evidence file may hold multiple links and must hold at least one. All
atoms are mapped when every changed line and file event is selected and every
committed link matches the current source comparison. This is an omission
invariant only; it does not establish explanation quality, claim truth, review
completion, or correctness. Overlap is reported but permitted.
Every Git-reported product file has at least one event or line atom, so an
unrepresentable file record cannot make a nonempty comparison appear complete.

Changes under a `.saga` path in the source repository are classified separately
as saga-only changes. When source and saga are different repositories, all saga
history is naturally outside the source comparison.

### 6.1 Diff-based maintenance impact

`change-saga compare` is a read-only evidence projection for maintaining a Saga
as its source evolves. It MUST NOT compare fragment bytes, rendered prose,
diagram geometry, review comments, or other authored content.

The first Saga is the maintained document. An incoming Git comparison may be
provided directly or through another Saga's `source` declaration. The engine
reconstructs the maintained Saga's comparison at the incoming comparison's
resolved base and evaluates its committed evidence there. It then classifies
incoming source atoms as:

- `conflicting_intersection` when a removed line or destructive file event
  intersects source evidence already owned by a Saga target;
- `additive_near_owned_code` when a new line belongs to the same replacement
  block or is immediately adjacent to owned baseline code; or
- `new_content_required` when no existing evidence owner can be derived.

The result identifies stable target URNs, target kinds, content locations,
evidence files, and exact incoming atoms. A direct intersection means the
target MUST be revisited. An adjacent addition is a prompt for consideration,
not proof that narrative content is stale. An ownerless change requires a new
or newly expanded narrative target. If the maintained Saga is incomplete or
stale at the incoming base, the result carries a `baseline_incomplete`
diagnostic and the command exits 3; an implementation MUST NOT claim exhaustive
impact in that state.

This projection does not rewrite evidence or advance the Saga's declared head.
Its purpose is to produce the formal maintenance work queue before content and
coverage are reconciled against the new source state.

## 7. Claims and verification

Author claims are independent `___claims/<id>.json` records conforming to
[`schema/v2/claim.schema.json`](schema/v2/claim.schema.json). A claim contains a
falsifiable `statement`, a `kind`, one existing narrative `target`, at least one
exact line/event diff URI in `evidence`, and `created_at`. Claim evidence never
contributes to coverage; readers independently report whether it is current and
whether every matching atom is already mapped to the claim's target.

Verification results are independent append-only
`___verifications/<id>.json` records conforming to
[`schema/v2/verification.schema.json`](schema/v2/verification.schema.json).
Each references a claim and records `unverified`, `verified`, `failed`, or
`inconclusive`, a human-readable summary, and—except for an unverified result—a
method: `test`, `command`, `measurement`, `inspection`, or `analysis`. An
optional command makes a check reproducible. The latest result is convenient
navigation state; every earlier result remains part of the audit history.

Claims and results never accept an author name. Readers derive attribution from
the Git commit that first introduced each file.

## 8. Review overlay

Review data lives separately from authored content:

```text
___review/threads/<thread-id>.thread/
├── thread.json
├── events/
│   └── <event-id>.json
└── messages/
    └── <message-id>.message/
        ├── message.json
        ├── body.fragment/
        │   ├── fragment.json
        │   └── content.md
        └── screenshot.fragment/
            ├── fragment.json
            └── screenshot.png
```

The overlay also contains append-only file-review events:

```text
___review/diffs/<event-id>-reviewed.json
```

Each event conforms to
[`schema/v2/diff-review.schema.json`](schema/v2/diff-review.schema.json), uses a
fully realized `/file` diff URI, and has state `reviewed` or `unreviewed`. The
latest event for the URI is current; when two events share a `created_at`, the
greater event `id` is the later one. Ordering never depends on file names, so
every engine resolves the same commit to the same state. A new comparison identity therefore starts
with no files implicitly reviewed.

A thread conforms to [`schema/v2/thread.schema.json`](schema/v2/thread.schema.json)
and targets a saga, chapter, section, or fragment URN. Its anchor is one of:

- `target`: the entire target.
- `region`: rectangles, ellipses, or lines in normalized coordinates.
- `drawing`: arbitrary paths represented by normalized points.
- `text`: an exact quote with optional prefix, suffix, and character positions.
- `note`: a sticky note carrying its own visible text and normalized placement.
- `diff`: one fully realized line or file-event diff URI.

Normalized coordinates are in `[0,1]` relative to the rendered fragment stage,
so drawings survive responsive resizing. Shapes support presentation hints such
as color and stroke width, and text selectors may carry a highlight color.
Engines may apply accessible defaults.

A `note` anchor is a first-class review entity rather than a new thread kind: it
is an ordinary `comment` thread whose anchor holds `text`, the normalized `x`/`y`
centre of the note on the fragment stage, and an optional `color`. Note text is
limited to 2000 characters and carries no markup; engines must render it as
plain text. Because the note lives in the anchor, moving, rewording, and
recoloring a committed note are `anchor` events rather than message rewrites,
and the note keeps the thread's replies, state, and permalink. Engines should
give each committed note its own document anchor so a sticky is directly
linkable.

Text selectors follow the resilient idea from Web Annotation selectors: `exact`
is authoritative, while positions and surrounding text help engines re-anchor
after modest content edits. An engine must report an unanchored selector rather
than attaching it to different text silently.

## 9. Thread messages and state

Each message has `message.json` and one or more fragment packages. Comments are
therefore not limited to plain text: a reply may contain Markdown, images, SVG,
or sandboxed interactive HTML using the same fragment model as the saga.

Thread lifecycle changes are append-only files in `events/` with state `open`,
`resolved`, or `withdrawn`. The latest event by `created_at` is current, with
ties broken by the greater event `id`. A
withdrawn thread is omitted from the active review surface but remains fully
auditable; a later `open` event restores it. An event may instead carry an
`anchor` replacement to move, recolor, reword, or otherwise edit committed
annotation geometry and note content. Transient undo and redo are engine-local
composition behavior and do not create files. Messages, thread roots, and thread events are history and
should not be rewritten or deleted during ordinary review.

### Append-only file granularity

Review mutations never append to a shared JSON array or rewrite a neighboring
reviewer's record:

- Each top-level comment creates its own `<id>.thread/` directory, whose name is
  exactly the `id` in its `thread.json`.
- The initial comment and every reply create separate `<id>.message/`
  directories, with independent `message.json` metadata and fragment files, and
  the directory name is exactly the `id` in its `message.json`. A record whose
  directory and identifier disagree is invalid: the two would otherwise address
  different records.
- Every resolve/reopen/remove/restore, anchor edit, approval/rejection, and
  reviewed/unreviewed change is a new event file with a unique time-plus-random
  identifier.
- Writers use exclusive creation and must fail rather than overwrite an
  existing record.

Consequently, two people commenting on the same fragment or replying to the
same thread normally add disjoint files and do not create a Git content conflict.

A thread has kind `comment` or `suggestion`. Suggestions must use a `diff`
anchor and include explicit replacement text; they remain review proposals and
are never applied to source code automatically.

Saga-, chapter-, section-, and fragment-level decisions remain separate from discussion threads.
Append-only approval events live in the target's `___approvals/` directory and
use state `approved`, `rejected`, `closed`, or `open` as defined by
[`schema/v2/review.schema.json`](schema/v2/review.schema.json). The latest event, resolving `created_at` ties by the greater `id`, is the
displayed state; repository policy, not this format, decides whether an approval
permits merging.

## 10. Reserved names

`___diffs` and `___approvals` are reserved on saga/chapter/section/fragment targets.
`___landmarks` is reserved inside fragments.
`___review`, `___claims`, and `___verifications` are reserved at the saga root. Reserved metadata directories must be
real directories, not symlinks. So must every entity package: a `.chapter`,
`.fragment`, `.landmark`, `.thread`, or `.message` entry that is a symlink or a
regular file is invalid rather than ignored, because silently skipping it would
hide authored content behind a valid-looking saga. Other names beginning with
`___` are invalid.

## 11. CLI behavior

- `change-saga install-skill` prints an agent-agnostic prompt for installing the
  project-local Change Saga authoring skill. It MUST NOT mutate the repository
  or assume an agent-specific skill path.
- `change-saga add-fragment` creates Markdown, HTML, SVG, text, or image fragments.
- `change-saga add-landmark` creates and validates a separately addressable
  Markdown heading, exact text span, HTML/SVG element, or normalized image
  region. It prints the target accepted by `change-saga cover`.
- `change-saga add-claim` creates one falsifiable assertion file without
  changing coverage. `change-saga verify-claim` appends one independent result.
- `change-saga add-chapter` creates a top-level independently reviewable chapter and
  its overview fragment.
- `change-saga cover --target ...` attaches an absolute diff URI to any target.
- `change-saga status --json` emits uncovered atoms including ready-to-use absolute
  URIs, stale links, overlap, target totals, saga-only changes, and
  `coverage_scope: "mapping_only"`.
- `change-saga compare` projects a direct Git range or another Saga's source
  comparison onto a maintained Saga's evidence owners. It compares source
  diffs only and emits stable update locations plus ownerless changes.
- `change-saga query mappings --sort scrutiny` ranks broad or thin evidence
  records without claiming that a low score proves correctness. `query claims`
  and `query verifications` expose assertions, exact evidence, attribution, and
  result history.
- `change-saga thread` and `change-saga reply` edit the review overlay without modifying
  authored fragment content.
- `change-saga review` appends a saga-, chapter-, section-, or fragment-level decision.
- `change-saga open` serves a Saga view with attached-diff drawers, a Code Diff view
  with a changed-file tree, and a bidirectional Coverage Manifest. The Manifest
  must derive both code-to-narrative and narrative-to-code projections from the
  same atom assignments. Attached code is grouped by collapsed source file, and
  evidence `note` values provide the reviewer-facing what-and-why summary before
  linked ranges are expanded. Review surfaces support diff comments and
  suggestions; the full diff view also records reviewed/unreviewed file events.
- `change-saga validate` checks structure, identifiers, URIs, anchors, entrypoints, and
  review history independently from coverage completeness.

`status` exits 0 when every atom is mapped with no stale selector and 3 when
mapping gaps remain. The status is not a correctness verdict. `validate` exits
1 for structural errors. Unknown v2 JSON fields are rejected.
