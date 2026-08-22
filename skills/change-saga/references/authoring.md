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

Cross-check the provider's head OID/branch, title, and changed files against the
local checkout before initializing. If they do not describe the same change,
stop and resolve the mismatch. Never put an unverified PR number into
`saga.json`; a saga with no PR identity is better than one linked to the wrong
review.

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
unique within the fragment. `change-saga validate --fix` adds a stable anchor to
any heading that lacks one, leaving existing anchors, fenced code, and review
history untouched; it is a safe way to bring a draft up to this rule, not a
substitute for choosing meaningful anchors on headings you intend to link to. Treat the anchor as an identifier: preserve it when
rewriting or renaming the visible heading. The renderer namespaces it with the
fragment target, so the same anchor may be reused in another fragment. Do not
rely on an automatically generated heading slug for authored saga content.

For addressable content that needs metadata or code links, create one landmark
package per item with the CLI. For example:

```sh
change-saga add-landmark --target path/to/map.fragment \
  --element-id submit-action --label "Submit action" \
  --description "The validated request crosses into the persistence boundary." <name>.saga
```

This creates `___landmarks/<stable-id>.landmark/landmark.json` with the shape:

```json
{
  "version": 2,
  "id": "submit-action",
  "label": "Submit action",
  "description": "The validated request crosses into the persistence boundary.",
  "selector": { "type": "element", "element_id": "submit-action" }
}
```

- For a Markdown heading with related code, use a `heading` selector whose
  `heading_id` matches its explicit heading anchor:
  `change-saga add-landmark --target <fragment> --heading-id request-validation <saga>`.
- For HTML or SVG, put the same stable `id` on the meaningful element and use an
  `element` selector through `--element-id`.
- For raster images, use a `region` selector with normalized `x`, `y`, `width`,
  and `height` in `[0,1]`, passed as `--region x,y,width,height`.
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
array. Give every meaningful visual landmark a concise semantic `description`
that explains its role without referring to SVG geometry, color, or screen
position. This is the non-visual representation returned to AI clients. For
static SVGs and images, add a normalized `hotspot` rectangle when the
renderer should reveal permalink and code controls over the item on hover.
For SVG, divide the item's viewBox coordinates by the viewBox width and height;
for raster media, divide pixel coordinates by the intrinsic image dimensions.
For example: `"hotspot":{"x":0.68,"y":0.72,"width":0.2,"height":0.12}`.

When a landmark is realized by code, put each focused diff association in its
own `<landmark>/___diffs/*.json` file. Run `change-saga cover --target` with
the path or target URN printed by `add-landmark`, or use the
`<fragment-path>#<landmark-id>` shorthand,
for example `change-saga cover --target
"api.chapter/flow.fragment#submit-action"`. `change-saga query children` on the
fragment lists its landmarks and their target URNs. Do not duplicate the same
atom at fragment scope merely to make it visible. This is the literate-programming bridge: prose and diagrams
explain intent, while the landmark opens the exact implementation. The fragment
is always addressable even when it has no inner landmarks.

### Diff citations in prose

Use Markdown footnotes as diff citations when a sentence or short prose range
makes a concrete claim about the implementation. Keep the paragraph readable,
then put one focused explanation in the reference list:

```markdown
The heartbeat renews the lease before half its TTL elapses.[^lease-renewal]

[^lease-renewal]: Renewal is triggered from the heartbeat path before the lease midpoint.
```

Make the reference text an exact-text landmark and attach only the diff atoms
that substantiate that citation:

```sh
change-saga add-landmark --target path/to/lease.fragment \
  --id lease-renewal \
  --text "Renewal is triggered from the heartbeat path before the lease midpoint." \
  --label "Lease renewal evidence" <name>.saga
change-saga cover --target path/to/lease.fragment#lease-renewal \
  --uri 'saga-diff://v1/line?...' \
  --note "Schedules renewal from the heartbeat before the lease midpoint." <name>.saga
```

The renderer presents the definitions as a compact reference footer. Once its
definition owns evidence, both the inline citation marker and the linked text
open the exact code in the side drawer. Use a stable, descriptive footnote key;
keep the definition text unique within the fragment, plain text, and free of
inline Markdown so the exact selector remains durable. One citation should
support one focused idea. Cite behavioral claims, invariants, data transitions,
and non-obvious implementation facts—not every sentence and not background
context that has no realizing diff.

For SVG and HTML, prefer the more direct equivalent: give every code-bearing
node, arrow, state, or control a stable element `id`, create an element landmark
for that exact node, and attach its focused diffs there. Fragment-level evidence
is not a substitute when the visual already identifies the narrower concept.

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
- Never widen a selector solely to make every atom mapped. This applies
  unchanged to `change-saga cover --batch`: a batch delivers many exact records
  in one call and is never a reason to merge them into one broad range.
- Use `change-saga cover --dry-run` to confirm which records an invocation would
  write, and the exact selectors in each, before writing them.
- Use `--changed-lines` only when every changed atom in the named file belongs
  to the same focused target. It derives both old/new line atoms and file events
  such as `add`; it is not permission to hide unrelated concerns in one record.
- Run `change-saga query mappings --sort scrutiny` after coverage. Treat the
  score as a work queue, not a grade. Use the returned `evidence_file` with
  `change-saga replace-coverage --record PATH --batch -` to atomically split,
  retarget, or rewrite broad records, or with `remove-coverage` to delete one.
  Move broad visual ownership to semantic landmarks where that better matches
  the explanation.

## Claims and verification

Record falsifiable assertions—not design opinions—with `change-saga
add-claim`. A claim targets the narrative element making the assertion and
cites exact line or event diff URIs. Claim evidence is deliberately separate
from coverage and never makes an uncovered atom covered.

Examples include behavioral invariants, compatibility promises, measured
performance changes, security properties, and test outcomes. Avoid assertions
like “the design is clean”; prefer statements another reviewer can disprove,
such as “at most one sampler may be active for a process.”

Append a result with `change-saga verify-claim`. Use `verified`, `failed`, or
`inconclusive` only after performing the named test, command, measurement,
inspection, or analysis. Use an explicit `unverified` result when the author
has not checked the claim. Each claim and each verification result is its own
file, and later results never rewrite earlier history.

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
  image regions have valid landmarks that still resolve to their content, and
  that every code-bearing landmark opens its exact related diff.
- Confirm validation reports no untouched scaffold and no unexplained visual
  mapping warning.
- Confirm every product atom is covered, no URI is stale, and every overlap is
  defensible.
- Confirm `query mappings --sort scrutiny` has no unjustifiably broad evidence
  records and that its warnings were addressed rather than merely accepted.
- Confirm important behavioral and quantitative assertions are structured
  claims, every claim has an explicit verification state, and commands or
  measurements are reproducible. “All atoms mapped” is not verification.
- Open linked code from representative fragments and confirm every collapsed
  file has a useful what-and-why summary before its ranges are expanded.
- Confirm tests, migrations, generated artifacts, and removed behavior are not
  silently omitted.
- Report genuine uncertainty inside the saga instead of inventing intent.
