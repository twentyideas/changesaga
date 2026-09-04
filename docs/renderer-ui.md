# Renderer UI conventions

Status: contract, 2026-08-20
Scope: `internal/server/{template.go,styles.go,appjs.go,icons.go}`

`docs/ux-reframe.md` and `docs/review-experience-audit.md` record why the
reviewer was restructured. This file records the conventions the renderer holds
to now, so they are not undone by accident. The target is a quiet developer
tool: a reviewer should recognise it from their editor and their code host, not
from a marketing page or a report generator.

## Vocabulary boundary

Reviewer-facing chrome speaks the reviewer's language: chapters, files, changes,
comments, explanations. Storage and format vocabulary — fragment, section, media
type, schema version, diff URI, manifest, internal paths — belongs to the CLI,
the validator, and the spec.

Authored saga content is rendered verbatim and may discuss the format freely.
Never edit committed saga content to hide a word from the chrome.

## Typography and surface

- System UI at 13px for chrome; monospace only for code and code-shaped
  metadata: paths, line numbers, change counts, identifiers.
- Prose stays in the UI font even inside a code surface. The fragment excerpt in
  the explanations panel is prose; the chapter eyebrow above it is metadata.
- Hairline separators instead of cards. No decorative rounding, shadows, or
  oversized headings on ordinary content.
- Controls stay invisible until the reviewer hovers or focuses the thing they
  belong to. Content is central and stays central.

## Icons

`icons.go` ships every glyph as an inline SVG `<symbol>` sprite. The set is
original to this repository and inherits its MIT licence, so a committed saga
can be reviewed with no network access and no third-party icon licence.

Icons are always decorative (`aria-hidden`). The accessible name comes from the
owning control's `aria-label` or visible text, and every icon-only control also
carries a `title` so the pointer user gets the same word. Do not add a remote
font or icon CDN.

`fileIcon` maps a repository path to a file-type badge. Unknown types fall back
to a neutral document outline rather than guessing.

## Navigation

V4 is a native presentation. A thumbnail rail groups authored slides by deck;
Previous/Next and unmodified arrow or Page keys move through the sequence. The
current deck, slide title, and position stay visible so reviewers can orient and
resume. Fullscreen presentation hides application and review chrome without
changing the active slide.

V2/v3 remain legacy reports with their documentation tree and collapsible
chapters. The renderer must never reinterpret their fragments as slides or
manufacture deck-break slides. A report becomes a presentation only through an
explicit semantic rewrite to v4.

The Code Diff sidebar is a different thing and should stay that way: a compact,
filterable changed-file tree with counts, status, and a selected file.

Both navigation surfaces are built from manifests rather than incidental
rendered state, so every destination remains addressable before its content is
active.

## The page is a shell

For v4, `GET /` renders the deck and slide manifests, thumbnail navigator, and
one active visual review surface. Deep links select the owning slide and Item;
the visual asset is served through its stable slide target. V2/v3 keep the
bounded legacy shell: chapter bodies arrive from `/api/section`, fragments from
`/api/fragment`, and `/api/locate` resolves anchors in unopened chapters.

Three rules follow from that, and breaking any of them is a regression:

- A descriptor is still a destination. It carries the explanation's id, title,
  and target, so permalinks and the review progress map work before the content
  arrives. Its compact decision controls stay on the explanation bar; the
  chapter review directory mirrors the same target as an overview, and both
  views synchronize after one persisted event.
- Content arriving never overwrites what the reviewer has already done. A
  decision made on a descriptor moves into the rendered explanation rather than
  being replaced by the state its snapshot was built from.
- Anything that needs content must ask for it first. Arming an annotation tool
  on an explanation that has not arrived fetches it and waits, rather than
  silently disarming.
- Content is filled into the article it was described by, never swapped for a
  new one. A reviewer can be part way through clicking a descriptor's controls
  when its content lands, and replacing the element under the pointer loses
  that click.

`<body data-shell-ready>` is set once the first fill-in has finished: every
explanation on screen has arrived and any anchor in the URL has been resolved.
It is the page saying it has settled, which is what a reviewer can see and what
the browser suite waits for instead of guessing.

## Review controls

In v4 the complete slide is the approval target. Items provide precise comments,
annotations, and evidence links without becoming a second approval checklist.
Deck and Saga status are derived rollups. V2/v3 retain section/fragment controls
and the chapter review directory in their legacy report reader.

In the legacy chapter directory, each row mirrors the decision control on its target's own bar and projects
append-only approval events into exactly `Unreviewed`, `Approved`, or `Changes requested`.
Its discussion count is a separate signal: it combines non-withdrawn comment
and annotation threads, including threads on an explanation's landmarks, but
never changes the approval state. Row links use the ordinary anchor resolver,
so they fetch the chapter or explanation if needed before moving to the exact
destination.

Chapter-level approval records remain valid and appear on the chapter bar, but
container decisions still create no directory row, progress segment, or
completion credit. No data migration is required. Existing `open` and `closed`
events on approval-bearing descendants project to
`Unreviewed`; new UI decisions continue to append the storage-compatible
`approved`, `rejected`, and undo (`open`) events.

## Comments and their marks

A comment carries an anchor. When that anchor is a mark drawn on the content —
a rectangle, a freehand drawing, a highlight, or a sticky note — the comment
belongs to the mark and renders as a compact bubble pinned to it, revealed on
hover or focus of either the mark or the bubble. Every other anchor — a whole
fragment, a section, a chapter, a diff line — keeps its comment in the list
below the content. `annotationAnchor` in `server.go` is the single place that
decides which is which; adding an anchor type means answering there.

The server places a bubble from the stored anchor (`annotationBubblePoint`) and
the browser refines it against the mark as laid out (`positionAnnotationBubbles`
in `appjs.go`). The two must agree on where a shape is, so `annotationShapeBounds`
and `shapeBounds` are deliberate mirrors of each other. A highlight has no stored
geometry and is placed by the browser alone.

A revealed comment never buries the mark it describes, and arming a drawing tool
closes every open bubble so the content keeps the pointer.

## Diagnostics, not congratulation

Complete coverage is an invariant enforced by the validator, not an achievement.
The renderer shows no score, percentage, progress bar, or success banner. Only
two things are worth a reviewer's attention: changes that are still unexplained,
and committed references that have gone stale.

## Syntax highlighting

The highlighter in `appjs.go` is deliberately small and applies to code only.
Markdown, plain text, and licence files resolve to the `prose` language and are
left untouched — colouring capitalised words inside a sentence is noise, not
information. Add new code languages to `languageKeywords` and `languageForPath`
together; add new prose extensions to the `prose` branch.

## Dependencies

No frontend framework, no bundler, no CDN. All CSS, JavaScript, and assets are
served from the binary so that reviewing a committed saga works offline.
