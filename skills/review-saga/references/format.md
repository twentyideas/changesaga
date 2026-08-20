# Review Saga v2 quick reference

## Layout

```text
<name>.saga/
  saga.json
  ___diffs/
  ___review/
    diffs/
    threads/
  overview.fragment/
    fragment.json
    content.md
    ___diffs/
  <chapter>.chapter/
    chapter.json
    overview.fragment/
    <section>/
      section.json
      <demo>.fragment/
        fragment.json
        index.html
        app.js
        ___landmarks/
          submit-action.landmark/
            landmark.json
            ___diffs/
```

Direct root `.chapter` directories are independently reviewable chapters.
Ordinary directories inside them are recursive sections. `.fragment` directories are atomic content
packages. A fragment manifest declares `version`, stable `id`, `media_type`,
`entrypoint`, and optional `title`/`order`. Supported content includes Markdown,
plain text, HTML, SVG, and raster images. Bundle HTML dependencies inside its
fragment directory; do not rely on network access in the sandboxed viewer.

Give every Markdown heading an explicit stable anchor:

```markdown
## Request validation {#request-validation}
```

Anchors begin with a lowercase letter and contain only lowercase letters,
digits, and hyphens. They are unique within a fragment and remain unchanged when
the visible heading is edited. The renderer combines the fragment target with
the authored anchor to create a collision-free permalink.

Addressable subparts use independent landmark packages:

```json
{
  "version": 2,
  "id": "submit-action",
  "label": "Submit action",
  "selector": { "type": "element", "element_id": "submit-action" }
}
```

Store the record at `___landmarks/<id>.landmark/landmark.json`. Selector types
are `heading` for an explicit Markdown anchor, `element` for an HTML/SVG element
ID, `text` for an exact Markdown/plain-text quote, and `region` for normalized
image coordinates. Put each code association in its own `___diffs/*.json`
inside the package. Static SVG/image records may include a normalized `hotspot`
that places hover controls over the illustrated item.

## Commands

```sh
saga init --repo <source-checkout> --base <rev> --head <rev-or-WORKTREE> --title "Title" <name>.saga
saga add-chapter --title "Title" <name>.saga backend
saga add-section --title "Title" <name>.saga backend.chapter/path/to/section
saga add-fragment --section path/to/section --type markdown --title "Context" <name>.saga
saga add-fragment --section path/to/section --type html --source ./demo-package --entrypoint index.html <name>.saga
saga cover --repo <source-checkout> --target path/to/demo.fragment --path file.go --side new --lines 4-9,12 <name>.saga
saga cover --target path/to/demo.fragment --uri 'saga-diff://v1/line?...' <name>.saga
saga cover --target path/to/demo.fragment/___landmarks/submit-action.landmark --uri 'saga-diff://v1/line?...' <name>.saga
saga validate --json <name>.saga
saga status --json --repo <source-checkout> <name>.saga
saga open --repo <source-checkout> <name>.saga
saga review --target path/to/demo.fragment --state approved <name>.saga
```

`--repo` may be omitted when the saga is inside the source checkout. Flags
precede positional arguments. `status` exits 0 when complete and 3 when
incomplete.

## Portable identities

Targets are stable URNs:

```text
urn:review-saga:<saga-id>:saga
urn:review-saga:<saga-id>:chapter:<chapter-id>
urn:review-saga:<saga-id>:section:<section-id>
urn:review-saga:<saga-id>:fragment:<fragment-id>
urn:review-saga:<saga-id>:fragment:<fragment-id>:landmark:<landmark-id>
```

Evidence contains absolute `saga-diff://v1/line?...` or
`saga-diff://v1/event?...` URIs. Each URI includes the absolute repository URI,
resolved base identity, stable product-patch identity, and line range or event. Saga-only
commits preserve the product identity; product changes do not. Do not hand-edit a URI
to make stale evidence pass; regenerate it against the intended source state.
`saga-diff://v1/file?...` identifies a whole changed file only for review
progress; it is not coverage evidence.

## Review overlay

Threads live under `___review/threads/<id>.thread/`. Anchors use `target`,
`region`, `drawing`, `text`, or `diff`. Region/drawing coordinates are normalized to
`[0,1]`; shapes may carry color and stroke hints. Text anchors retain an exact
quote and may carry a highlight color. Messages contain `.fragment`
packages, enabling Markdown, image, SVG, or sandboxed HTML replies. Treat thread
roots, messages, reviewed-file records, approvals, and state events as
append-only history. Diff threads may be comments or suggestions; suggestions
include explicit replacement text.

Every top-level comment has its own `.thread` directory; every initial comment
or reply has its own `.message` directory; and each state or approval transition
is a new JSON file. Never consolidate these records into shared arrays.

Do not supply or persist reviewer names. Canonical identity is the committer
name and email, commit OID, and committer timestamp of the commit that first
introduced each individual event file. Legacy `author` and `created_by` fields
remain loadable but are not authoritative.
