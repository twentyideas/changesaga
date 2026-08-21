# Change Saga UX reframe

## Outcome

Change Saga should make a very large change feel like a sequence of small,
finishable review sessions. The interface presents the story first, keeps
mechanical coverage guarantees in the validator, and reveals review controls
only when the reviewer asks for them.

The primary experience is:

1. Read a short saga overview.
2. Choose one chapter from a collapsed chapter list.
3. Review that chapter on its own page, with linked code available in a drawer.
4. Record comments, annotations, and a chapter decision without leaving the
   chapter.
5. Return later and resume at the next unfinished chapter.

The complete code comparison remains a separate, first-class view. It uses a
real file tree and a full-width, syntax-aware diff surface. Selecting a diff can
also reveal excerpts from every related saga fragment.

## Product principles

- Content is the interface. Format details such as `text/markdown`, internal
  paths, schema versions, diff URIs, and coverage accounting are diagnostics,
  not normal reviewer UI.
- Progressive disclosure over dashboards. Show status only where it helps the
  reviewer decide what to do next.
- One review session, one scope. The overview and each chapter have distinct
  URLs and do not render the entire saga below them.
- Persistent tools, contextual actions. Annotation tools live in one available
  toolbox; fragment and chapter decisions open compact popovers/dialogs.
- Absence is silent. Do not render a diff action when a target has no linked
  diffs, and do not render empty state chrome around unavailable actions.
- Completion is durable but quiet. Chapter/file reviewed state helps with
  resumption; source coverage remains a hard validation invariant rather than
  a congratulatory percentage.
- Review data stays append-friendly. UX changes must preserve one-event-per-file
  review storage and avoid shared mutable metadata files.
- Git is the identity and audit layer. Review forms never ask for an author;
  the author of a comment, reply, approval, or other review event is the
  **committer** of the commit that introduced its file. Use Git committer name
  and email, not the distinct author fields. The renderer may show that Git
  attribution, but the saga payload must not duplicate editable identity data.

## Prior art translated into decisions

- GitHub's changed-files tree is path-filterable, hideable, and can suppress
  already-viewed files. We should use a collapsible nested tree with filtering,
  per-file counts/status, and a focused selected file instead of rendering every
  file in one page.
- GitLab supports nested tree/list modes, explicit viewed state, file-level
  comments, contextual expansion, and keyboard navigation between files and
  threads. We should reserve keyboard shortcuts and make file review state part
  of navigation, not a repeated inline form.
- Gerrit separates the change screen, file list, focused side-by-side diff, and
  a single reply/approval dialog. Draft inline comments accumulate during a
  review. We should mirror that separation for saga overview, chapter, code
  diff, and compact decision UI.
- Monaco's diff editor supports side-by-side/inline layouts, resizeable panes,
  change indicators, syntax-aware models, hidden unchanged regions, and an
  accessible diff mode. Use it (bundled locally) or match this baseline with a
  smaller renderer; do not keep the current monochrome handcrafted rows.

References:

- <https://docs.github.com/en/enterprise-server@3.21/pull-requests/collaborating-with-pull-requests/reviewing-changes-in-pull-requests/filtering-files-in-a-pull-request>
- <https://docs.github.com/en/pull-requests/get-started/reviewing-pull-requests-quickstart>
- <https://docs.gitlab.com/user/project/merge_requests/changes/>
- <https://docs.gitlab.com/user/shortcuts/>
- <https://gerrit-review.googlesource.com/Documentation/user-review-ui.html>
- <https://microsoft.github.io/monaco-editor/typedoc/functions/editor_editor_api.editor.createDiffEditor.html>
- <https://microsoft.github.io/monaco-editor/typedoc/interfaces/editor_editor_api.editor.IDiffEditorBaseOptions.html>

## Workstream A: content-first saga navigation

- Remove media-type labels, internal display paths, format/schema footer text,
  source SHAs, and the coverage percentage/bar from normal review pages.
- Keep coverage failures actionable: an invalid or incomplete saga should be a
  dedicated blocking diagnostic, not a partial-success score.
- Make the overview a standalone page with a compact chapter index.
- Start chapter entries collapsed and show title, short description/status, and
  a clear open/resume action.
- Give each chapter a stable route and render only its own recursive content.
- Remove the literal `Chapter:` label; express hierarchy through typography,
  spacing, and breadcrumbs/back navigation.
- Preserve deep links to sections and fragments within a chapter.
- Add a quiet resume signal for approved/in-progress/unreviewed chapters.

Acceptance criteria:

- Opening `/` does not render all chapter fragments.
- Opening a chapter URL does not render sibling chapters.
- Browser back/forward and direct URLs preserve the reviewer's place.
- No normal reviewer-facing text exposes media types, schema versions, diff
  URIs, internal paths, or coverage percentages.

## Workstream B: review controls and annotation toolbox

- Replace repeated approval/change forms with one icon/menu per reviewable
  target that opens a popover or modal.
- Remove author fields from all review interactions. Persist the review event,
  then derive its author from Git history after it is committed. Uncommitted
  review events should be presented as local/uncommitted rather than asking the
  reviewer to self-identify.
- Provide one always-available annotation toolbox for comment, text highlight,
  rectangle, freehand, and selection modes.
- Apply the selected annotation tool to the active fragment; do not render a
  separate tool row on every fragment.
- Keep tool state legible, keyboard accessible, dismissible with Escape, and
  inert while the reviewer is reading or interacting with sandboxed HTML.
- Render a linked-diff icon only when a section or fragment actually owns diff
  atoms.
- Maintain existing persisted review/thread/shape semantics and one-file-per-
  event conflict resistance. Evolve explicit author fields compatibly: existing
  files continue to load, while new events rely on Git attribution.
- Add a Git attribution service that resolves the introducing commit for each
  event file and exposes canonical committer name/email, commit timestamp, and
  commit ID to the renderer. Define honest states for uncommitted, rewritten,
  and unavailable history.

Acceptance criteria:

- No approval form is visible until the reviewer invokes a decision action.
- A page with ten fragments has one toolbox, not ten tool rows.
- A fragment with zero linked changes has no diff icon or empty diff drawer.
- Existing comment, reply, suggestion, approval, and annotation records remain
  loadable and writable.
- No comment, reply, suggestion, approval, or reviewed-state form asks for a
  name. After commit, attribution matches the commit that added the event file.

## Workstream C: focused code review

- Make Code Diff a focused file-at-a-time workspace with enough horizontal and
  vertical room for code.
- Build a true nested file tree with folder disclosure, changed-line counts,
  reviewed state, selection state, filtering, and show/hide behavior.
- Load one selected file diff rather than stacking every changed file.
- Use Monaco's read-only diff editor, or an explicitly justified equivalent,
  with language detection, distinct added/removed/unchanged styling, dual line
  numbers, inline/side-by-side modes, context expansion, and responsive fallback.
- Preserve line comments, multi-line comments where feasible, suggestions, and
  mark-reviewed controls as contextual actions rather than header forms.
- Add a toggleable related-saga sidebar for the selected file/line. It lists all
  linked fragments grouped by chapter and shows concise content excerpts with
  links back to the exact fragment.
- Define empty states for a diff with no related narrative and for a fragment
  whose linked diff is unavailable/stale.

Acceptance criteria:

- Additions, removals, and unchanged context are distinguishable without
  reading the `+`/`-` glyphs alone and meet contrast requirements.
- Selecting a file updates the URL and main diff without scrolling through
  unrelated files.
- The related-saga sidebar is derived from coverage ownership in both
  directions: diff to fragments and fragment to diffs.
- File tree and related-saga sidebar can be independently collapsed to maximize
  code width.
- Review comments and suggestions still persist as saga files with fully
  qualified diff URIs.

## Cross-cutting quality tasks

- Add responsive layouts for overview, chapter, and code-review routes.
- Add focus management, semantic labels, Escape behavior, and keyboard
  navigation for drawers/popovers/tree items.
- Avoid relying on color alone for diff semantics or review state.
- Add renderer tests for conditional controls and route isolation.
- Add browser-level fixtures covering overview to chapter, fragment to linked
  diff, file to related fragments, annotation creation, and incremental review
  resumption.
- Keep all JS/CSS/runtime assets local so reviewing a committed saga does not
  depend on a CDN or network connection.

## Delivery order

1. Establish routes and the content-first shell.
2. Consolidate review/annotation controls.
3. Replace the diff workspace and add reverse narrative lookup.
4. Run an integration polish pass for responsive behavior, accessibility,
   visual hierarchy, and end-to-end resumption.

The first three workstreams can be developed in parallel after agreeing on the
route and view-model contracts. Integration should prefer the simplest shared
shell and keep persistence/schema behavior unchanged unless a concrete blocker
is found.
