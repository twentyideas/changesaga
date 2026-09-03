# Slide-native Change Saga

Status: implemented contract-first preview, 2026-09-03
Scope: intentionally breaking narrative format; semantic rewrite and rendered
composition gates remain staged work

## Recommendation

Make slide-native narrative a v4 format, not a renderer option for v2/v3. A v4
Saga is explicitly `presentation.mode: "slides"`; v2/v3 remain report Sagas and
must never be silently paginated. Retire `chapter`, `section`, and narrative
`fragment` from the v4 authoring model. The new spine is:

> **Saga → Deck → Slide → Item → exact diff atoms**

The human experience is an image-first visual argument. Each slide makes one
high-level point through a diagram, screenshot, state transition, comparison,
or worked example. Short labels and sentences sit next to what they explain.
Selecting an item—including an overlaid callout—opens the exact supporting diff
without losing the slide. Every product diff atom must be owned by an item;
100% coverage remains an
omission check, not proof that the explanation is good.

“Every diff captured visually” does not mean drawing every changed line. It
means grouping exact atoms under the visual item they realize: a node, edge,
transition, risk badge, before/after region, or concise callout. Supporting
plumbing still needs a visible conceptual owner; it does not need screen space
proportional to its line count.

This is stronger than “a picture is worth a thousand words.” Relevant words and
pictures together outperform words alone, while proximity, signaling, and the
removal of extraneous material improve comprehension. Those findings support
meaningful diagrams with adjacent callouts—not decorative imagery or prose
hidden in pictures ([Mayer, 1999](https://doi.org/10.1075/dd.1.1.02may),
[Richter, Scheiter, and Eitel, 2016](https://doi.org/10.1016/j.edurev.2015.12.003)).

## What the repository and experiment show

The committed contract is a document hierarchy: root fragments, direct
`.chapter` directories, recursive sections, and `.fragment` content packages.
The renderer presents a collapsed documentation tree and lazily loads chapter
pages and explanations. This is encoded across [SPEC.md](../SPEC.md),
[the model](../internal/saga/model.go), [the renderer contract](renderer-ui.md),
the CLI authoring commands, query API, and browser tests.

Several foundations should be preserved:

- independent content packages, stable IDs, deterministic ordering, and
  append-only records are already Git- and parallel-author-friendly;
- landmarks already connect semantic SVG/HTML elements, exact text, or image
  regions to focused evidence;
- normalized annotation geometry already survives responsive resizing;
- lazy chapter/fragment loading, `/api/locate`, in-place hydration, and the code
  drawer preserve deep links and reviewer state;
- claims, verifications, reviews, and cross-Saga references are independent
  records rather than mutable arrays.

The uncommitted parent experiment proves that the existing renderer can show
one fragment at a time, lazy-load the active item, keep annotations and the code
drawer working, and add keyboard navigation. It should not become the contract:
it implicitly declares every old fragment to be a slide, every chapter to be a
deck, creates synthetic “breather” slides, hides inactive report nodes with CSS,
and primarily constrains Markdown with a 100-word count. Long HTML, crowded SVG,
tiny type, overflow, and weak visual hierarchy can still pass. That is
pagination, not visual synthesis.

The version boundary is mandatory. The v2 and v3 manifests are closed schemas
(`additionalProperties: false`), v3 deliberately changes only the container
version, and narrative components remain v2. More importantly, changing the
meaning of `fragment` would invalidate authoring, query, review, and external
reference assumptions even if the JSON bytes still loaded. An explicit mode
field inside v4 makes the new intent inspectable; the schema version makes it
enforceable.

## V4 data model

V4 has no narrative chapters or sections. A **deck** is the independently
reviewable boundary that chapters were trying to be, but it contains only a
short ordered sequence of slides. One deck has `role: overview`; the others
explain coherent behaviors, workflows, or risks. A **slide** is the atomic
review surface. An **item** is an addressable concept on that slide and the
only normal owner of implementation evidence; a callout is one item kind.

```text
checkout.saga/
  00-saga.json
  01-readme.md
  10-d-0000-b3d965f8a739.json
  20-s-b3d965f8a739-0000-e96d5ab8b8c4.json
  20-s-b3d965f8a739-0000-e96d5ab8b8c4.svg
  30-i-e96d5ab8b8c4-0000-b74ed26c821d.json
  40-e-b74ed26c821d-17ae340f8c15.json
  50-c-fc21a7e31215.json
  60-v-fc21a7e31215-a7c490f4ed82.json
  80-t-b74ed26c821d-6da710dd9134.json
  81-m-6da710dd9134-bc0a824c6081.json
```

V4 contains no directories. Category prefixes make ordinary lexical listings
group manifests, authored content, evidence, claims, and review history.
Deterministic 12-hex keys are derived from stable semantic targets; titles and
source paths never enter filenames. Fixed-width ranks sort siblings, and the
JSON repeats the semantic identity so validation can reject a renamed or
misfiled record. The CLI appends ranks in steps of ten, leaving insertion space.
A slide manifest and its single self-contained visual share one basename.
Unrelated slides, items, evidence, and review events remain independent files.

This is also the portability boundary. The earlier nested preview produced a
150-character relative evidence path (273 characters in a real checkout).
Windows still exposes a 260-character `MAX_PATH` compatibility boundary unless
both the OS and each application opt into long paths. V4 therefore caps a
basename at 64 characters and preflights a conservative 240-character absolute
path budget rather than requiring every Git, shell, archive, and editor in the
workflow to opt in ([Microsoft MAX_PATH guidance](https://learn.microsoft.com/en-us/windows/win32/fileio/maximum-file-path-limitation)).

The root manifest adds a required, closed presentation declaration:

```json
{
  "$schema": "https://changesaga.dev/schema/v4/saga.schema.json",
  "version": 4,
  "id": "checkout",
  "title": "Reject invalid checkout before persistence",
  "source": { "repository": "…", "base": "…", "head": "…" },
  "presentation": {
    "mode": "slides",
    "aspect_ratio": "16:9",
    "overview_deck": "overview"
  }
}
```

A deck manifest is small: `version`, stable `id`, `title`, `role`
(`overview|change`), `rank`, and a one-sentence `objective`. Decks own discussion
and approval; their displayed state is derived from their slides, as chapter
state is today.

A slide manifest contains:

```json
{
  "version": 4,
  "id": "reject-before-write",
  "deck": "validation",
  "title": "Invalid requests stop before persistence",
  "rank": 10,
  "intent": "explain",
  "layout": "diagram",
  "media_type": "image/svg+xml",
  "entrypoint": "20-s-b3d965f8a739-0010-e96d5ab8b8c4.svg",
  "takeaway": "Validation now guards the only path into storage.",
  "reading_order": ["request", "validate", "persist", "reject-early"]
}
```

`intent` is one of `orient`, `explain`, `compare`, `trace`, `prove`, `risk`, or
`conclude`. `layout` selects a bounded renderer template (`hero`, `diagram`,
`before-after`, `sequence`, `evidence`, or `risk`); `custom` is an explicit
escape hatch with a required exception rationale. SVG, raster image, and
sandboxed HTML remain possible entrypoints; prose formats are deliberately not
slide entrypoints.

An item names its parent with `slide` and retains the useful landmark selector
model while adding `kind`: `node`,
`edge`, `region`, `transition`, `statement`, `risk`, `metric`, `example`, or
`callout`. Every item has a concise `label` and non-visual `description`.
Selectors remain SVG/HTML element IDs, image regions, or exact text. A callout
is an evidence-bearing item subtype, not second-class decoration: it may be
free-standing or name another item in `about`, while carrying its own label,
body, placement, and leader-line presentation. A node and a callout about that
node may own different focused diffs; overlapping ownership requires explicit
justification. Each meaningful diagram node and edge has its own item. Evidence
files remain byte-compatible `saga-diff://v1` records; the compact `40-e`
filename binds each record to its Item target.
Slide-, deck-, and Saga-level mappings do not count toward v4 coverage; even a
whole-slide image or file event needs an explicit item.

Targets become `urn:change-saga:<saga>:deck:<id>`,
`…:slide:<id>`, and `…:slide:<slide-id>:item:<id>`. The versioned query API
also advances to `change-saga.ai/v2` with `overview`, `children`, `slide`,
`slide-diffs`, and the existing bidirectional evidence/review operations. V1
continues to read v2/v3; clients never have to guess which hierarchy they got.

## Enforceable visual contract

Word count stays a secondary lint, not the definition of a slide. Validation
combines schema checks, source analysis, and headless rendering. Initial numeric
limits are product defaults to test with real reviewers, not universal facts.

| Dimension | V4 gate |
| --- | --- |
| Deck scale | Overview has 2–5 slides; change decks have 3–10. Larger decks fail readiness and must split by reviewer decision or workflow. |
| One idea | Unique title and one-sentence takeaway are required; one `intent`; at most three primary regions. |
| Image-first | Every non-title slide has a primary visual occupying at least 40% of the usable stage, except `statement` and `code`; at least two thirds of a deck's non-title slides are visual; two prose-only slides may not be adjacent. |
| Items | 1–7 primary visible items; every non-decorative node/edge/callout is in `reading_order`; a callout is adjacent to or connected to the item named by `about`. |
| Density | No more than 60 explanatory words, six bullets, one nesting level, 12 table cells, or 14 visible code lines. Hitting any two maxima is a warning even when each passes. |
| Geometry | Render at 1280×720 and 1024×576; no clipping, overlap, horizontal/vertical slide scroll, or content outside a 48px safe area. No shrink-to-fit. |
| Legibility | Body/callout text ≥24 CSS px and code ≥18 CSS px at 1280×720; text lines ≤80 characters; renderer fails computed sizes, not declared CSS. |
| Evidence | Every current product diff atom is owned by at least one item; stale and broad mappings fail readiness; repeated ownership requires justification. |
| Accessibility | Unique title, explicit reading order, text equivalent for every meaningful visual, keyboard-reachable items, 4.5:1 normal-text and 3:1 large-text/non-text contrast, and pointer targets at least 24×24 CSS px. |
| Escape hatch | `layout: custom` still passes geometry, evidence, contrast, focus, and text-equivalent checks and records each waived semantic limit with a reason. |

These accessibility thresholds follow WCAG 2.2’s contrast, reflow, focus, and
minimum target requirements ([WCAG 2.2](https://www.w3.org/TR/WCAG22/)). Unique
titles, reading order, alt text, simple tables, and adequate whitespace also
match Microsoft’s presentation accessibility guidance
([PowerPoint accessibility](https://support.microsoft.com/en-us/accessibility/powerpoint/make-your-powerpoint-presentations-accessible-to-people-with-disabilities)).

A fixed canvas alone cannot satisfy zoom/reflow needs. The same semantic model
must produce a **Reading mode** that stacks title, takeaway, visual description,
ordered items, callout text, evidence links, and discussions at 320 CSS px without
two-dimensional scrolling. Presentation mode shows exactly one slide; Reading
mode is the accessible, printable, searchable linear projection. Neither mode
contains information absent from the other.

## Renderer and review workflow

Opening v4 lands on the overview deck and gives the current slide the entire
review pane below the application header. A collapsible presentation-style rail
shows a live thumbnail and title for every slide, grouped by deck. Previous/Next
and unmodified arrow/Page keys provide sequential access when focus is not
inside an interactive element. There is no autoplay. The active slide has an
accessible name and its deck and position are visible (for example,
“Validation · 2 of 5”). A presentation control enters fullscreen, hides the
application chrome and review overlays, and leaves only the maximally fitted
16:9 slide plus quiet navigation and an exit affordance. Hiding the total, as
the experiment does, weakens orientation and resumption; WAI’s carousel pattern
explicitly supports named slides and position/set-size when useful
([WAI carousel pattern](https://www.w3.org/WAI/ARIA/apg/patterns/carousel/)).

Item affordances are quiet until hover, focus, or a “show evidence” toggle.
Activating one—including a callout overlay—opens its exact diff in the existing drawer and preserves slide, focus, and
annotation state. Comments can target the whole slide or any item; freehand and
region annotations stay normalized to the slide stage. Deep links load only
the owning deck, slide, and target. Lazy shell/locate/hydration mechanics from
the committed renderer should be reused, but the DOM and API are native deck
and slide nodes rather than hidden report nodes.

There are no generated “breather” slides. A deck boundary is navigation chrome,
not authored content, evidence, or a review target. If an author wants a title
or pause slide, it is a real `statement` slide with a stable identity.

## Compatibility and rewrite

Compatibility should be deliberately asymmetric:

- v2/v3 readers and authoring commands continue unchanged; `open` renders them
  as reports and labels them legacy once v4 is default;
- the slide renderer refuses v2/v3 with `legacy_report_requires_rewrite`;
- v4 rejects `.chapter`, section manifests, and narrative `.fragment` packages;
- `add-chapter`, `add-section`, and `add-fragment` refuse v4; `add-deck`,
  `add-slide`, `set-slide-content`, and `add-item` refuse v2/v3;
- no renderer flag treats one legacy fragment as one slide, no content is
  truncated or auto-shrunk, and no approval is inherited merely because all old
  diff atoms can be mapped;
- review-message fragments may remain byte-compatible internal components;
  they are not narrative slides.

Migration is a semantic rewrite, never `upgrade --to 4`. The deterministic CLI
does not embed a model; the coding agent is the assistant and the CLI supplies
the safe protocol:

```sh
change-saga rewrite --to 4 --emit-plan rewrite.jsonl legacy.saga
# The agent storyboards decks/slides, creates visual assets, and completes mappings.
change-saga rewrite --to 4 --plan rewrite.jsonl --out review-v4.saga --dry-run legacy.saga
change-saga rewrite --to 4 --plan rewrite.jsonl --out review-v4.saga legacy.saga
```

The emitted plan inventories every legacy target, landmark, claim, thread,
approval, external reference, and diff atom through the query API. The agent
must mark each narrative target `reuse`, `rewrite`, `split`, `merge`, or
`historical-only`, provide successor targets and a reason, and assign every diff
atom to a new item. Dry-run validates the complete graph, renders a contact
sheet, and reports uncovered/stale evidence, ambiguous anchors, density failures,
and records that cannot carry. Apply writes a new Saga atomically and never
changes or deletes the source. Interrupted runs resume from content-addressed
plan steps.

Preservation rules are strict:

| Record | Rewrite behavior |
| --- | --- |
| Diff evidence | Copy canonical URIs and notes byte-for-byte when still current; author new ownership at items; never widen selectors. |
| Visual landmarks | Reuse IDs only when the asset/element is retained; otherwise create items and an explicit old→new map. |
| Claims | Create a replacement claim targeting the new item and a `supersedes` relation; do not mutate the old claim. |
| Verifications | Do not carry by default. A new analysis result may cite the old verification only when the statement and evidence are unchanged. |
| Threads/annotations | Preserve in migration history. Keep active only when content and selector/geometry digests are identical; otherwise mark `needs_reanchor` and link to candidate successors. Never guess after split/merge. |
| Approvals | Never activate on rewritten content. Preserve historical decisions and require fresh deck/slide review. |
| Deep links | Store independent alias records. One-to-one aliases redirect; one-to-many aliases open a migration landing view listing successors. |
| External Saga references | Add deck/slide/item target kinds. Revision-pinned legacy references keep resolving at their old revision; refreshed references follow explicit aliases, never silent retargeting. |

The independent alias/migration records use their own reserved flat category
and are additive files so parallel rewrites do not contend on one map. This
mirrors the repository’s strongest prior art: append-only reviews, claims, and
relations. One-record-per-slide also follows PresentationML’s proven “one part
per slide” boundary without adopting its centralized slide list
([PresentationML structure](https://learn.microsoft.com/en-us/office/open-xml/presentation/structure-of-a-presentationml-document)).

## CLI and help shape

`change-saga init --mode slides` creates v4 during opt-in and becomes the
default only after the pilot. The primary authoring loop in help and the skill
becomes:

1. inspect the diff and draft a storyboard of decks, slides, and takeaways;
2. add visual-first slides and semantic items, including evidence-bearing callouts;
3. attach exact atoms to items while draining `query gaps`;
4. render a contact sheet and run `validate --render`;
5. audit broad mappings, claims, and accessibility; then open the deck.

Help should lead with one complete SVG slide example, not a Markdown report.
`add-slide` defaults to an SVG template and requires `--intent`, `--layout`, and
`--takeaway`. `render --contact-sheet` gives authors the same high-level view a
reviewer gets. `status --json` distinguishes structural, composition,
accessibility, evidence, and migration readiness. Query help explicitly names
the API version and hierarchy.

Markdown slide separators used by reveal.js, Slidev, and Marp are useful prior
art for lightweight authoring, but they put deck boundaries inside a shared
document and make it easy to paginate prose
([reveal.js Markdown](https://revealjs.com/markdown/),
[Slidev syntax](https://sli.dev/guide/syntax.html),
[Marp Markdown](https://github.com/marp-team/marp-core/blob/main/docs/markdown.md)).
Change Saga should instead optimize for semantic evidence ownership and Git
merges.

## Staged rollout and proof

### Current branch checkpoint

This branch now implements the contract-first slice: closed v4 Saga/deck/slide/
item schemas; native runtime types and targets; explicit v4 initialization and
authoring commands; compact flat storage with path-budget preflight; Item-only
coverage; `change-saga.ai/v2` slide queries;
closed runtime validation for reading order, callout relationships, standard
Item density, and evidence placement; and a one-slide-at-a-time renderer that
reuses the evidence drawer, annotations, reviews, and deep links. V2/v3 remain
the default and their authoring paths explicitly refuse v4.

The semantic rewrite command, rendered geometry/contrast/font gates, Reading
mode, contact sheets, and external-reference alias records remain later rollout
stages. They should not be weakened into heuristics merely to call the preview
complete.

1. **Contract first.** Publish v4 schemas, URNs, query v2, rewrite-plan schema,
   renderer behavior, and fixtures before production writers. Add schema/runtime
   parity tests and prove v2/v3 bytes and rendering are unchanged.
2. **Opt-in vertical slice.** Implement `init --mode slides`, deck/slide/item
   authoring, query, validation, one standard SVG layout, Reading mode, deep
   links, annotations, evidence drawer, and contact sheet. Keep legacy as the
   default.
3. **Rewrite pilot.** Rewrite at least five real Sagas spanning prose-heavy,
   SVG, HTML, screenshots, nested sections, and external references. Do not
   accept one-fragment-per-slide output. Compare reviewer time, correct recall
   of change intent, navigation to risky code, missed defects, and requested
   clarifications against the legacy presentation.
4. **Constraint calibration.** Use pilot failures and viewport traces to tune
   numeric limits. Treat comprehension and defect discovery as the success
   criteria; raw slide count and coverage percentage are diagnostics.
5. **Default switch.** Make new `init` v4 only after rewrite success, WCAG/keyboard
   conformance, bounded-load performance, and external-reference resolution are
   proven. Keep explicit legacy read and authoring support for a published
   deprecation window; never auto-rewrite on open.

Required automated coverage includes malformed/closed schemas; concurrent
slide/item/event merges; stable ordering; split/merge alias graphs;
all-or-nothing rewrite and interruption recovery; no carried approval;
annotation reanchor refusal; bidirectional diff ownership; stale evidence;
cross-Saga pinned and refreshed resolution; unique IDs and deep links; geometry,
overflow, font size, contrast, reading order, keyboard/focus, reduced motion,
and 320px Reading mode; active-slide lazy-load budgets; and adversarial
sandboxed HTML. Golden contact sheets and browser snapshots should cover every
standard layout at both canonical sizes.

The decision gate is qualitative as well as structural: if a reviewer still
has to read paragraphs to understand the change, the rewrite has failed even
when every validator is green.
