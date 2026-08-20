# Authoring a change for review

The saga is the authored proposal that accompanies the code—the successor to a
flat PR title and description. Build it from the change author's point of view
so another person can review it. Do not populate the review overlay, invent
review feedback, or make approval judgments while authoring.

## Resolve the comparison

For a PR number or URL, use repository-host metadata rather than guessing:

- PR number, URL, title, and stated motivation
- base branch/OID and head branch/OID
- commit and changed-file summary
- local checkout containing the head

When GitHub CLI is available, an appropriate discovery call is:

```sh
gh pr view <number> --json number,title,url,body,baseRefName,headRefName,headRefOid,files,commits
```

Use an available provider integration instead when it has better access. For a
local change without a PR, identify the intended merge base and choose `HEAD` or
`WORKTREE` explicitly.

## Overview contract

The root overview should let a reviewer answer these before opening code:

1. What user or system problem is being solved?
2. What changes in behavior, and what deliberately does not change?
3. What is the end-to-end flow or architecture?
4. Why is this implementation shaped this way?
5. What are the major risks, rollout/compatibility concerns, and verification
   signals?
6. What chapters follow, in what order, and which can be reviewed independently?

Lead with a system/change map before the detailed prose. For a large change, the
overview must visually establish the affected components, primary workflows,
boundaries, and chapter structure. Add a before/after view when the behavioral
shift is otherwise easy to miss.

## Visual-first contract

Treat visuals and examples as the primary explanation, not decoration added
after the prose:

- Start every substantial chapter with at least one diagram, interactive
  walkthrough, or worked example. A short mechanical chapter may use a compact
  before/after example instead of a diagram.
- Use SVG for architecture, ownership boundaries, data models, stable request
  flows, and migration topology.
- Use self-contained HTML and JavaScript when a reviewer benefits from switching
  between paths, stepping through states, changing an input, inspecting a
  payload, or comparing old and new behavior. Make the useful default state
  understandable without interaction and do not load network dependencies.
- Show by example: include concrete inputs, important intermediate state, output
  or side effects, and at least one consequential failure or edge path. Prefer a
  realistic payload, schema row, event, command, or UI action over an abstract
  paragraph.
- For data flows, show producers, transformations, persistence, consumers,
  trust/process boundaries, and failure or retry paths.
- For data models, show entities, important fields, relationships, ownership,
  lifecycle changes, migration/compatibility behavior, and old-to-new shape.
- For workflows, show the entry point, happy path, permissions or validation,
  side effects, failure/recovery path, and observable result.
- Give meaningful SVG/HTML nodes stable IDs and landmark packages. Attach the
  exact realizing diff to the node, state, arrow, example step, or control—not
  only to the enclosing fragment.

Keep prose around a visual short: state what question it answers, call out the
non-obvious invariant or tradeoff, and tell the reviewer where to look next.
Split or replace long prose fragments that repeat relationships already visible
in the artifact.

## Chapter contract

A chapter is approximately a PR-sized review boundary. It should be coherent
enough that another reviewer could take responsibility for it. Its overview
should state:

- purpose and boundary;
- dependencies on earlier/later chapters;
- behavioral walkthrough and important invariants;
- notable implementation decisions and rejected alternatives when relevant;
- failure modes, security/data/operational risk, and compatibility concerns;
- tests and concrete reviewer checks;
- which code is intentionally covered at chapter scope rather than by a more
  focused fragment.

Organize the chapter around the key workflow it changes. If it changes stored
or exchanged data, include the relevant data model and how information moves
through it. If it changes behavior, demonstrate the old and new outcomes with a
concrete example. Prose-only chapters require a clear reason.

Split a chapter when it contains independently understandable behavior with a
different risk profile or reviewer specialty. Do not create a chapter merely
for each directory or language.

## Landmark and hyperlink contract

End every Markdown heading with a stable, fragment-local anchor:

```markdown
## Request validation {#request-validation}
```

Use lowercase letters, digits, and hyphens; begin with a letter; keep the value
unique within the fragment. Treat the anchor as an identifier: preserve it when
rewriting or renaming the visible heading. The renderer namespaces it with the
fragment target, so the same anchor may be reused in another fragment. Do not
rely on an automatically generated heading slug for authored saga content.

For addressable content that needs metadata or code links, create one
`___landmarks/<stable-id>.landmark/` package per item. Put this in
`landmark.json`:

```json
{
  "version": 2,
  "id": "submit-action",
  "label": "Submit action",
  "selector": { "type": "element", "element_id": "submit-action" }
}
```

- For a Markdown heading with related code, use a `heading` selector whose
  `heading_id` matches its explicit heading anchor.
- For HTML or SVG, put the same stable `id` on the meaningful element and use an
  `element` selector.
- For raster images, use a `region` selector with normalized `x`, `y`, `width`,
  and `height` in `[0,1]`.
- For plain text, use a `text` selector with an exact quote and optional prefix
  and suffix. In Markdown, choose literal source text that also renders as the
  same visible text; do not span inline markup. Prefer heading anchors when a
  heading is the intended target.
- For a format without a native selector, make the fragment itself the link
  target or place the media in a small HTML wrapper whose meaningful controls
  and regions have stable element IDs. Do not invent an unrecognized selector.

Create landmarks for independently discussable concepts, states, controls, and
diagram nodes—not decorative shapes or every sentence. Keep IDs stable when
labels or visuals change, keep them unique within the fragment, and use a
separate package for each landmark so parallel edits do not touch a shared
array. For static SVGs and images, add a normalized `hotspot` rectangle when the
renderer should reveal permalink and code controls over the item on hover.
For SVG, divide the item's viewBox coordinates by the viewBox width and height;
for raster media, divide pixel coordinates by the intrinsic image dimensions.
For example: `"hotspot":{"x":0.68,"y":0.72,"width":0.2,"height":0.12}`.

When a landmark is realized by code, put each focused diff association in its
own `<landmark>/___diffs/*.json` file. Run `review-saga cover --target` with the
landmark target URN; do not duplicate the same atom at fragment scope merely to
make it visible. This is the literate-programming bridge: prose and diagrams
explain intent, while the landmark opens the exact implementation. The fragment
is always addressable even when it has no inner landmarks.

## Evidence discipline

- Attach a changed atom to the most focused fragment that actually explains it.
- Give every evidence record a concise `--note` that answers both “what changed
  in this file?” and “why does this target own it?” Write for the collapsed file
  row a reviewer sees before opening code. Prefer one concrete sentence, such as
  “Parses and validates absolute diff URIs so evidence remains unambiguous across
  repositories.” Do not use path-only labels or generic notes such as
  “implementation,” “supporting changes,” or “tests.”
- Keep one file and one coherent reason per evidence record. When separate
  ranges in the same file serve different reviewer ideas, attach them to their
  most focused targets with distinct notes; the renderer will preserve both
  explanations under that file.
- Use chapter-level evidence only for truly cross-cutting code discussed by the
  chapter overview.
- Read generated, vendored, lockfile, migration, and snapshot changes; group
  them explicitly rather than hiding them in a broad range.
- Keep deletions visible and explain behavior that disappeared.
- Treat overlaps as intentional only when the same code is necessary in two
  distinct reviewer journeys.
- Never widen a selector solely to achieve complete coverage.

## Reviewer-readiness check

Before handing off:

- Read the saga in rendered order without relying on prior author knowledge.
- Confirm the root overview explains goals and supplies a useful chapter map.
- Confirm the overview leads with a system/change map rather than a wall of
  introductory text.
- Confirm every chapter can be reviewed and approved independently.
- Confirm every substantial chapter leads with a useful visual or worked
  example and makes its affected workflow explicit.
- Confirm relevant data flows identify boundaries and failure paths, and
  relevant data models show relationships plus migration/compatibility impact.
- Confirm interactive fragments teach something through their default state and
  controls, remain self-contained, and are not static prose placed in HTML.
- Confirm diagrams and examples agree with current code.
- Confirm meaningful diagram nodes, interactive controls, text regions, and
  image regions have valid landmarks that still resolve to their content.
- Confirm every product atom is covered, no URI is stale, and every overlap is
  defensible.
- Open linked code from representative fragments and confirm every collapsed
  file has a useful what-and-why summary before its ranges are expanded.
- Confirm tests, migrations, generated artifacts, and removed behavior are not
  silently omitted.
- Report genuine uncertainty inside the saga instead of inventing intent.
