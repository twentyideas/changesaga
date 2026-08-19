# Review Saga format v2

Status: experimental. Version 2 supersedes the unpublished v1 draft.

## 1. Model

A Review Saga is a Git-native review document with five layers:

1. **Overview** explains the change as a whole.
2. **Chapters** divide the change into independently reviewable units—roughly
   the PRs that might have existed if the work had been split.
3. **Sections and fragments** recursively organize each chapter; fragments are
   the smallest units of authored content.
4. **Diff links** connect a saga, chapter, section, or fragment to immutable source
   changes, including changes in another repository.
5. **Review overlays** add anchored discussions without modifying the authored
   fragments.

Every persistent object is an ordinary file. Content and review data can be
branched, merged, audited, and committed independently.

## 2. Root and source

A saga root ends in `.saga` and contains `saga.json` conforming to
[`schema/v2/saga.schema.json`](schema/v2/saga.schema.json):

```json
{
  "$schema": "https://reviewsaga.dev/schema/v2/saga.schema.json",
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

`source.repository` is an absolute URI and identifies the source repository,
not necessarily the repository containing the saga. `base` and `head` select the
comparison to evaluate. Commit comparisons use their merge base, matching pull
request behavior. `WORKTREE` is allowed as `head`; untracked files are excluded.

When a saga is stored separately, readers provide a local source checkout with
`saga status --repo PATH` or `saga open --repo PATH`. Local checkout paths are
runtime configuration and are never committed into the portable format.

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
Entrypoints cannot escape their package.

HTML and SVG fragments execute in an iframe with `sandbox="allow-scripts"` and a
network-denying Content Security Policy. They can execute bundled JavaScript but
cannot access the review application, navigate its parent, submit forms, open
popups, or make network connections. Engines must provide equivalent isolation;
they must not insert fragment HTML directly into trusted application markup.

## 4. Stable target URNs

Review and engine data use stable identifiers rather than filesystem paths:

```text
urn:review-saga:<saga-id>:saga
urn:review-saga:<saga-id>:chapter:<chapter-id>
urn:review-saga:<saga-id>:section:<section-id>
urn:review-saga:<saga-id>:fragment:<fragment-id>
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
`event=rename|mode|binary`; rename requires both paths, while mode and binary
require `path`. File URIs identify the complete changed file for review-progress
events; they are not valid coverage links.

Commit comparisons use resolved commit OIDs in links. A `WORKTREE` head uses
`worktree-<sha256-of-patch>`, so links become stale whenever tracked worktree
content changes. Engines compare the complete URI identity, preventing evidence
from silently matching a similar path in a different repository or revision.

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
range; one evidence file may hold multiple links. Coverage is complete when
every changed line and supported file event is selected and every committed link
matches the current source comparison. Overlap is reported but permitted.

Changes under a `.saga` path in the source repository are classified separately
as saga-only changes. When source and saga are different repositories, all saga
history is naturally outside the source comparison.

## 7. Review overlay

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
latest event for the URI is current. A new comparison identity therefore starts
with no files implicitly reviewed.

A thread conforms to [`schema/v2/thread.schema.json`](schema/v2/thread.schema.json)
and targets a saga, chapter, section, or fragment URN. Its anchor is one of:

- `target`: the entire target.
- `region`: rectangles, ellipses, or lines in normalized coordinates.
- `drawing`: arbitrary paths represented by normalized points.
- `text`: an exact quote with optional prefix, suffix, and character positions.
- `diff`: one fully realized line or file-event diff URI.

Normalized coordinates are in `[0,1]` relative to the rendered fragment stage,
so drawings survive responsive resizing. Shapes support presentation hints such
as color and stroke width, but engines may apply accessible defaults.

Text selectors follow the resilient idea from Web Annotation selectors: `exact`
is authoritative, while positions and surrounding text help engines re-anchor
after modest content edits. An engine must report an unanchored selector rather
than attaching it to different text silently.

## 8. Thread messages and state

Each message has `message.json` and one or more fragment packages. Comments are
therefore not limited to plain text: a reply may contain Markdown, images, SVG,
or sandboxed interactive HTML using the same fragment model as the saga.

Thread lifecycle changes are append-only files in `events/` with state `open` or
`resolved`. The latest event by `created_at` is current. Messages, thread roots,
and state events are history and should not be rewritten or deleted during
ordinary review.

### Append-only file granularity

Review mutations never append to a shared JSON array or rewrite a neighboring
reviewer's record:

- Each top-level comment creates its own `<id>.thread/` directory.
- The initial comment and every reply create separate `<id>.message/`
  directories, with independent `message.json` metadata and fragment files.
- Every resolve/reopen, approval/rejection, and reviewed/unreviewed change is a
  new event file with a unique time-plus-random identifier.
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
[`schema/v2/review.schema.json`](schema/v2/review.schema.json). The latest event
is the displayed state; repository policy, not this format, decides whether an
approval permits merging.

## 9. Reserved names

`___diffs` and `___approvals` are reserved on saga/chapter/section/fragment targets.
`___review` is reserved at the saga root. Reserved metadata directories must be
real directories, not symlinks. Other names beginning with `___` are invalid.

## 10. CLI behavior

- `saga add-fragment` creates Markdown, HTML, SVG, text, or image fragments.
- `saga add-chapter` creates a top-level independently reviewable chapter and
  its overview fragment.
- `saga cover --target ...` attaches an absolute diff URI to any target.
- `saga status --json` emits uncovered atoms including ready-to-use absolute
  URIs, stale links, overlap, target totals, and saga-only changes.
- `saga thread` and `saga reply` edit the review overlay without modifying
  authored fragment content.
- `saga review` appends a saga-, chapter-, section-, or fragment-level decision.
- `saga open` serves a Saga view with attached-diff drawers and a Code Diff view
  with a changed-file tree. Both surfaces support diff comments and suggestions;
  the full view also records reviewed/unreviewed file events.
- `saga validate` checks structure, identifiers, URIs, anchors, entrypoints, and
  review history independently from coverage completeness.

`status` exits 0 when complete and 3 when incomplete. `validate` exits 1 for
structural errors. Unknown v2 JSON fields are rejected.
