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

The Saga sidebar is a collapsed documentation tree, like a wiki's page tree. It
lists the overview and the chapters; only the open page expands, and it expands
into its own headings. It never shows how many fragments or sections a chapter
contains, never mirrors the on-disk hierarchy, and never repeats a label it is
already nested under.

The Code Diff sidebar is a different thing and should stay that way: a compact,
filterable changed-file tree with counts, status, and a selected file.

The outline is built from the saga's own manifests rather than from the rendered
page, so it names every destination in the document — including the ones inside
chapters the page has not fetched. That is what makes it navigation rather than
a view of what happens to be loaded.

## The page is a shell

`GET /` renders the saga's identity, the coverage totals, the overview's
explanations as descriptors, one summary per chapter, and the navigation
outline. It renders no explanation content at all. A chapter body arrives from
`/api/section` when the reviewer opens that chapter, and an explanation from
`/api/fragment` when it comes into view, is pointed at, or is needed to answer a
permalink. `/api/locate` says which chapter and which explanation own an anchor,
so a deep link into an unopened chapter still resolves.

Three rules follow from that, and breaking any of them is a regression:

- A descriptor is still a destination. It carries the explanation's id, title,
  target, and review controls, so permalinks, the review progress map, and
  review decisions all work before the content arrives.
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
