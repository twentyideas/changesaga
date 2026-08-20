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
