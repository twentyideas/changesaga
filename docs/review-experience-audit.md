# Large-change review experience audit

Status: implementation brief, 2026-08-19
Scope: `internal/server/{template.go,appjs.go,server.go}` and
`docs/ux-reframe.md`; no renderer changes are included here.

## Recommendation

Adopt the direction in `ux-reframe.md`, but make the session boundary and
resumption contract explicit. The overview, one chapter, and one code file are
separate pages. Each page has one primary task, one quiet status/action cluster,
and stable URLs. Annotation tools persist once per chapter page; decisions move
to one compact dialog; code review uses a nested file tree, a single-file diff,
and a related-fragments sidebar. Review interactions never request identity;
the introducing commit’s committer name and email are the identity record.

This turns “review the PR” into “finish the next chapter,” while preserving the
append-only review records and diff URIs already used by the server.

## Audit findings

| Priority | Existing behavior | Effect on a large review | Required correction |
| --- | --- | --- | --- |
| P0 | `template.go` recursively renders the root, every chapter, and every fragment on `/`. | There is no finishable session or trustworthy return point. | Give overview and each chapter an isolated route; render only the requested scope. |
| P0 | Code Diff stacks every file and uses anchors in a flat path list. | Review cost grows with the whole PR and selected-file state is not durable. | Render one URL-addressable file and navigate with a nested, filterable tree. |
| P0 | Author fields, approval forms, and annotation buttons repeat on sections and fragments. | Controls visually outweigh the narrative and ask users to self-report identity that Git should supply. | Remove identity fields, keep one persistent annotation toolbar, and use one decision dialog per invoked target. |
| P0 | Coverage ownership is only projected from narrative target to diff rows. | A reviewer in code cannot recover the author’s explanation. | Build the reverse index and show all owning fragments/sections for the selected file or line. |
| P1 | Every fragment shows a linked-diff button, including `0`, and opens an empty drawer. | Empty controls create false affordances and noise. | Omit the action when no current diff atom is linked. |
| P1 | The custom diff has one line number, handcrafted rows, no context expansion, and no accessible review mode. | It is harder to compare and navigate than established review tools. | Use a local Monaco read-only diff editor or meet the same explicit baseline. |
| P1 | Drawers only toggle `aria-hidden`; they do not trap/restore focus or make the background inert. Escape does not cancel an active drawing preview. | Keyboard and screen-reader users can lose context or leave hidden UI active. | Implement the focus behavior below and cancel transient annotation state first. |
| P1 | At narrow widths saga navigation is hidden; line actions become another row. | Mobile loses orientation while retaining crowded controls. | Replace sidebars with labeled drawers and force an inline diff. |
| P2 | Normal pages expose repository refs, internal paths, media type, coverage percentage, generation time, and schema version. | Implementation diagnostics compete with review content. | Remove them from the review surface; show a blocking diagnostics page only when invalid. |

The current reframe identifies most structural problems. It does not yet define
route/state ownership, exact focus behavior, reverse-ownership presentation, or
what “resume” means. The contracts below close those gaps.

## Page-level information architecture

### Route and state contract

| Route | Primary job | Server-rendered scope | URL-owned state |
| --- | --- | --- | --- |
| `/` | Understand the change and choose/resume a chapter. | Saga overview fragment, ordered chapter summaries, latest chapter states. | Optional `#chapter-id` for a shared chapter reference. |
| `/chapters/{chapter-id}` | Review one coherent narrative unit. | This chapter, its recursive sections/fragments, local threads/reviews, linked-diff counts. Never sibling chapters. | `#fragment-id` or `#thread-id`; browser history preserves position. |
| `/diff?file={path}` | Review one changed file. | Nested file-tree model, selected file diff, file review state, related ownership. Never sibling file diffs. | `file`, optional fully qualified `line` diff URI, `layout=split|inline`; tree/sidebar visibility is a user preference, not link state. |
| `/diagnostics` | Explain why review cannot safely start. | Validation, uncovered, orphaned, and source-comparison failures. | Filters may use query parameters. |

On each successful navigation, store only the last valid route and anchor in
browser-local state keyed by saga ID and exact head OID. The overview’s primary
action is:

1. **Resume** the saved location when it still belongs to the current head.
2. Otherwise **Continue** the first chapter whose latest state is not `approved`
   or `closed`.
3. When all chapters are complete, **Review code** at the first unreviewed file.

Do not encode review truth in local state. Chapter/file states continue to come
from append-only review records. A file review is already revision-specific
because its URI contains base/head; chapter and fragment reviews are not. Before
resumption can be trusted across a new head, bind those reviews to a source OID
or render them as **possibly stale** after source movement. This is the largest
data-model risk.

Git is the identity and audit layer. No comment, reply, suggestion, decision,
thread-state, or viewed-state interaction asks for a name. For a committed event,
display the committer name and email of the commit that introduced its file—not
the distinct Git author fields. Display an event not yet committed as **Local ·
uncommitted**. Keep legacy files with explicit author fields readable; where
introducing-commit attribution is unavailable, label the fallback honestly
rather than presenting editable payload identity as verified.

### Global shell

1. Skip link: “Skip to review content.”
2. A 48 px sticky top bar: product mark, breadcrumb/back, page switcher
   (“Overview” / “Code”), then one local status/action cluster.
3. Page content. Overview and chapter use a readable centered column. Diff uses
   the full viewport below the top bar.
4. A nonvisual polite live region for saves, selection changes, and errors.

Do not show global completion percentages. Use exact, quiet labels only where
they answer “what is next,” such as “4 of 7 chapters complete” or “Viewed.”

## Text wireframes

### Overview

```text
┌ Change Saga ───────────────────────── Overview | Code ─────────────── ⋯ ┐
│                                                                        │
│  Safer attachment handling                                            │
│  Short overview narrative: intent, boundaries, and review order.      │
│                                                                        │
│  [Resume · Reviewer experience]        4 of 7 chapters complete       │
│                                                                        │
│  Chapters                                                              │
│  ●  Format history             Approved                  [Open]        │
│  ◐  Reviewer experience        In progress               [Resume]      │
│  ○  Diff authoring             Not started               [Open]        │
│      Remaining chapter rows are collapsed summaries, not content.     │
└────────────────────────────────────────────────────────────────────────┘
```

The overview fragment is content, not a card dashboard. Each chapter row has a
text status plus an icon, title, one-line description if available, and one
action. Thread counts appear only when nonzero and actionable.

### Chapter

```text
┌ Change Saga  ‹ Overview / Reviewer experience ─── In progress [Review] ┐
│                                                                        │
│  Reviewer experience                                                   │
│  Chapter narrative                                                     │
│                                                                        │
│  Annotation state                                      [linked code 2] │
│  ┌──────────────────── active fragment content ─────────────────────┐  │
│  │                                                                    │  │
│  └────────────────────────────────────────────────────────────────────┘  │
│  Thread: concise summary…                                     [Open]   │
│                                                                        │
│  Next section…                                                         │
│                                                                        │
│       ┌ Pointer | Comment | Highlight | Rectangle | Freehand | Done ┐  │
└───────┴ persistent annotation toolbar; one per page ────────────────┴──┘
```

The toolbox is sticky and enters annotation mode only after a fragment is
focused or chosen. `Pointer` is the safe default. A fragment’s overflow menu
contains “Review fragment”; no approval form is visible inline. The chapter
header’s **Review** button opens a compact dialog with status, optional note,
and actions: Approve, Request changes, Close, or Reopen. It contains no identity
field; attribution appears only after Git can resolve the introducing commit.

Linked code is an icon button only when links exist. It opens a non-modal panel
on wide screens and a modal drawer on narrow screens, grouped by file with a
“Open in Code” deep link.

### Code diff

```text
┌ Change Saga  ‹ Overview / Code ─ appjs.go ─── [Viewed ✓] [Review ▾] ┐
├ Files (41) ───────────┬──────────────────────────────┬ Related (4) ┤
│ [Filter paths…] [⋯]   │ internal/server/appjs.go     │ Chapter     │
│ ▾ internal            │ Split | Inline  −18 +34     │  Reviewer…  │
│   ▾ server            │ ┌ old ───────┬ new ───────┐ │  excerpt…   │
│     ● appjs.go +34−18 │ │  41 code   │  41 code   │ │ [Open ↗]   │
│     ○ server.go +8−2  │ │  42 code   │  42 code   │ │             │
│   ▸ coverage          │ │ … 12 unchanged lines …  │ │ Fragment    │
│ ▸ schema              │ │  55 old   │  55 new   + │ │  State…     │
│                       │ └────────────┴──────────────┘ │ [Open ↗]   │
├ [Hide files] ─────────┴──────── [Prev] [Next unviewed]┴[Hide related]┤
```

The tree and related-fragments panel collapse independently. The center always
shows one file. Selecting a line filters related entries to owners of that atom;
with no line selected, entries include all owners in the file, de-duplicated and
grouped by chapter/section. Each entry shows title, a short plain-text excerpt,
relationship scope (“lines 41–55” or “file event”), local review/thread state,
and a deep link to the exact narrative target. Section- or chapter-owned changes
appear as “Chapter context,” so reverse coverage is never silently lost.

If there are no owners, say “No related narrative for this file” with a link to
diagnostics. If ownership points to missing/stale content, show the target name,
“Link is stale,” and diagnostics; never render an empty panel shell as success.

## Interaction and state matrix

| Surface | State / trigger | Visible result | Persistence and focus |
| --- | --- | --- | --- |
| Overview | No saved location | First unfinished chapter is primary. | Focus remains on page heading after load. |
| Overview | Valid saved location for current head | “Resume · {chapter/file}.” | Activating follows a real URL; Back returns to the same overview row. |
| Chapter row | `not started` / `open` / `rejected` / `approved` / `closed` | “Not started” / “In progress” / “Needs changes” / “Approved” / “Closed,” with distinct icon and text. | Latest append-only event wins; never color-only. |
| Decision | Review invoked | Labeled modal dialog; current state selected, optional note, explicit submit, no identity input. | Initial focus on dialog heading, focus trapped, Escape cancels, close restores invoker. On save, focus returns to status/action cluster and live region announces “Saved locally; commit to attribute.” |
| Annotation | Pointer | Content behaves normally; overlays are inert. | Default on page load and after save/cancel. |
| Annotation | Tool chosen, no active fragment | Toolbar says “Choose a fragment”; compatible fragments get a subtle focus target. | Focus stays on chosen toolbar button (`aria-pressed=true`). |
| Annotation | Drawing/highlighting | Preview appears; composer opens only after a valid anchor exists. | First Escape removes preview and returns Pointer; second Escape closes composer. No partial record is written. |
| Annotation | Unsupported content/tool | Tool stays discoverable but disabled with a reason; target comment remains available. | Spatial annotation is never required to perform a review. |
| File tree | Folder collapsed/expanded | Children hidden/shown; aggregate changed and unviewed counts remain. | Expansion and filter are per-browser preferences; selection is URL-owned. |
| File tree | Filter or “Hide viewed” | Matching paths plus ancestors remain; no-match message replaces the tree body. | Clearing restores expansion state; selected file remains visible or a warning offers next match. |
| File | Mark viewed | Text/icon state changes and next-unviewed becomes available. | Append a diff-review event for the exact base/head file URI; announce save. |
| Event attribution | Committed / uncommitted / history unavailable / legacy explicit author | Git committer name/email / “Local · uncommitted” / “Attribution unavailable” / clearly labeled legacy fallback. | The introducing commit’s committer is authoritative; Git author fields and payload identity are not editable review identity. |
| Diff | Loading / ready / too large / failed | Skeleton / editor / explicit size fallback / retry plus diagnostics. | Never mark viewed automatically on failed or partial render. |
| Related fragments | Hidden / file scope / line scope / none / stale | Toggle / grouped owners / exact owners first / explanatory empty state / warning. | Toggle preference persists. Opening a fragment uses a real URL and retains the diff URL for Back. |
| Async mutation | Saving / saved / error | Action disabled with progress / quiet confirmation / inline actionable error. | Do not optimistically discard input; focus moves only on success. |

## Keyboard focus and accessibility behavior

- Use landmarks (`header`, labeled `nav`, `main`, `aside`) and one `h1` per
  route. On same-document route replacement, focus the new `h1`; on native page
  navigation, retain browser behavior. Never move focus merely because a file
  tree item receives focus.
- Implement the nested file browser as a single-select ARIA tree. Up/Down move
  among visible nodes; Right opens a folder or moves to its first child; Left
  closes or moves to parent; Home/End jump; Enter selects a file. Keep keyboard
  focus distinct from `aria-current`/selected file.
- Implement annotation controls as a labeled ARIA toolbar with one Tab stop and
  roving tabindex. Left/Right move tools; Home/End jump; Enter/Space selects.
  Provide “Focus annotation tools” in shortcut help; do not trap focus there.
- Decision UI and narrow-screen drawers use `role="dialog"`, an accessible
  title, `aria-modal="true"`, inert background, contained Tab order, Escape to
  close, and focus restoration. Wide-screen related/linked-code panels are
  labeled complementary regions and do not steal focus when toggled.
- Diff shortcuts, following established review conventions: `[`/`]` previous or
  next file, `v` toggle viewed, `Shift+F` toggle tree, `Shift+R` toggle related,
  `n`/`p` next or previous open thread, and `Shift+D` inline/split. `?` opens a
  shortcut dialog. Disable single-key shortcuts in inputs, contenteditable
  elements, dialogs, and while Monaco has text focus.
- Monaco is read-only and receives explicit original/modified accessible names.
  Enable split resizing, change indicators, hidden-region reveal controls, and
  accessible diff review mode. Preserve syntax semantics in high-contrast mode.
- Every icon has a visible tooltip on hover/focus and an accessible name. Added,
  removed, viewed, selected, status, and stale states have text/shape indicators
  in addition to color. Focus indicators remain visible above sticky chrome.
- Annotation shapes have a navigable textual counterpart in the thread list:
  Git-derived committer identity, tool/anchor type, target, comment, and a “Show
  annotation” action.
  Pointer drawing is an enhancement; target and text comments provide full
  keyboard alternatives.
- Use a polite live region for “File selected,” “Viewed,” and save outcomes;
  avoid announcing scroll position or every diff line. Honor reduced motion and
  never animate a drawer when it would obscure the focused control.

## Responsive behavior

| Width | Overview / chapter | Code diff |
| --- | --- | --- |
| `>= 1280px` | Centered narrative, sticky toolbox, optional linked-code panel. | Three panes: 240–320 px tree, fluid editor, 280–360 px related panel; both rails independently resizable/collapsible. |
| `900–1279px` | Same content; linked code becomes an overlay drawer. | One rail visible at a time; editor remains primary. Split diff is allowed when each side retains at least 45 characters. |
| `< 900px` | Single column; chapter list remains visible; toolbox docks to bottom with horizontal tool navigation. | Inline diff only. Tree and related fragments are separate full-height modal drawers. Prev/Next file stays sticky. |
| `< 480px` | Status/action cluster wraps below title; labels may shorten but accessible names do not. | File path truncates visually with full value on focus; line actions open a compact action menu rather than occupying a grid column. |

At 320 CSS px with 400% zoom, content reflows without two-dimensional page
scroll. The code viewport may scroll horizontally, but navigation, comments,
dialogs, and actions may not require horizontal scrolling.

## Clutter-removal checklist

Remove from normal review pages:

- [ ] Coverage percentage/bar, uncovered/orphaned counters, and “complete”
  celebration; incomplete coverage routes to a blocking diagnostics experience.
- [ ] Repository URL, full base/head hashes, internal display paths, diff URIs,
  media types, schema version, and generation timestamp.
- [ ] Literal “Chapter:” labels and repeated card borders where heading hierarchy
  and spacing already communicate structure.
- [ ] All author/name fields, repeated review forms, fragment tool rows, empty diff
  buttons/drawers, and always-visible line action buttons.
- [ ] Duplicate “reviewed” totals in the top bar, page heading, tree, and file
  header; keep the local state and one overview summary only.

Retain or add only when useful:

- [ ] PR/saga title, current page breadcrumb, current chapter/file state, and one
  primary next action.
- [ ] Changed-line counts and viewed state in the file tree; nonzero unresolved
  thread counts beside their target.
- [ ] Diagnostics details behind a dedicated route and an overflow link for
  source metadata needed during troubleshooting.
- [ ] Persistent, compact controls whose position does not shift as the active
  fragment or diff line changes.

## Concrete acceptance tests

1. **IA-01 — overview isolation:** Given a saga with three chapters, GET `/`
   contains the overview and three chapter summaries but none of their fragment
   bodies.
2. **IA-02 — chapter isolation:** GET `/chapters/reviewer-experience` contains
   that chapter’s recursive content and no sibling chapter content; its heading
   is the only `h1`.
3. **IA-03 — history and resume:** Navigate overview → chapter fragment → linked
   file → related fragment. Browser Back traverses each exact route/anchor. A
   reload and new browser session on the same head exposes that location as the
   overview Resume action.
4. **IA-04 — changed head:** With a saved location and reviews for head A, load
   head B. The saved route is not auto-resumed; file viewed state is clear and
   chapter state is either revision-bound or visibly “possibly stale.”
5. **UI-01 — no internal chrome:** Normal overview/chapter pages contain no
   media type, schema version, diff URI, internal path, source hash, generation
   timestamp, coverage percentage, or zero-count action.
6. **UI-02 — compact decisions:** Before invocation no approval form or author
   input is present. Activating Review opens one dialog for the target; a saved
   decision adds one valid append-only event and preserves existing records.
7. **AN-01 — one toolbox:** A chapter with ten fragments renders exactly one
   labeled annotation toolbar. Choosing Rectangle, drawing, and saving creates a
   normalized region anchor against the active fragment.
8. **AN-02 — cancel and keyboard parity:** Escape during drawing removes the
   preview and writes nothing. Every fragment can receive a target comment and
   every selectable text fragment a text comment without pointer input.
9. **DIFF-01 — focused file route:** Selecting `internal/server/appjs.go` changes
   the URL and main editor without rendering another file diff in the DOM.
10. **DIFF-02 — nested tree:** A fixture with sibling and nested paths exposes
    correct tree/treeitem/group roles, levels, expanded state, selected state,
    aggregate counts, filtering, and hide-viewed behavior. APG arrow/Home/End/
    Enter keys work with focus distinct from selection.
11. **DIFF-03 — diff semantics:** Additions and removals have distinct text or
    glyph labels plus non-color styling; both old/new line numbers, syntax
    language, split/inline switch, hidden-context expansion, and a readable
    high-contrast mode are present.
12. **DIFF-04 — responsive fallback:** At 899 px the editor is inline, each
    sidebar opens as an independently labeled modal drawer, and the focused
    control is neither obscured nor lost after close.
13. **REL-01 — reverse ownership:** For an atom owned by two fragments and one
    section, selecting its line lists all three under the correct chapters,
    de-duplicates repeated ranges, and each deep link lands on the exact target.
14. **REL-02 — related empty/stale:** A file with no owner shows the explicit
    no-related-narrative state; an orphaned owner shows “Link is stale” and a
    diagnostics link, with no blank sidebar.
15. **PERSIST-01 — compatibility:** Existing comment, reply, suggestion,
    approval, thread-state, annotation, and diff-review fixtures load; each new
    mutation still creates one event file and validates against v2 schemas.
16. **PERSIST-02 — Git attribution:** No mutation form contains a name/author
    control. Before commit, a new event reads “Local · uncommitted.” After a
    commit whose author and committer differ, the rendered identity matches the
    introducing commit’s committer name/email; timestamp/commit ID also match
    that commit. Rewritten or unavailable history has an honest fallback, and
    legacy explicit-author fixtures remain readable.
17. **A11Y-01 — focus:** Automated checks plus keyboard tests find no unlabeled
    controls, focus trap, hidden focused element, or modal background focus.
    Dialogs restore their invoker and route changes focus the destination `h1`.
18. **A11Y-02 — zoom/reflow:** At 320×640 CSS px and at 400% zoom, overview and
    chapter have no horizontal page scroll; code navigation and comments reflow,
    with horizontal scrolling confined to the code viewport.

## Primary-source rationale

- GitHub recommends reviewing one file at a time, marking files Viewed to
  collapse them and track progress, and submitting one final review decision.
  Its file tree supports path filtering/navigation and can hide viewed files:
  [reviewing proposed changes](https://docs.github.com/en/pull-requests/how-tos/review-pull-requests/reviewing-proposed-changes-in-a-pull-request?tool=webui),
  [filtering files](https://docs.github.com/en/enterprise-server@3.21/pull-requests/collaborating-with-pull-requests/reviewing-changes-in-pull-requests/filtering-files-in-a-pull-request).
- GitLab documents nested tree/list modes, one-file-at-a-time review, durable
  Viewed state, inline/side-by-side modes, and file/thread navigation shortcuts:
  [merge request changes](https://docs.gitlab.com/user/project/merge_requests/changes/),
  [keyboard shortcuts](https://docs.gitlab.com/user/shortcuts/).
- Gerrit separates its change/file list from a focused side-by-side diff, keeps
  inline comments as drafts until a single Reply action, and conditionally
  offers quick approval:
  [Review UI overview](https://gerrit-review.googlesource.com/Documentation/user-review-ui.html).
- Monaco exposes a resizable split, responsive inline fallback, hidden unchanged
  regions, change indicators, and an accessible-only difference review mode:
  [diff editor options](https://microsoft.github.io/monaco-editor/typedoc/interfaces/editor_editor_api.editor.IDiffEditorBaseOptions.html).
- The focus behavior follows WAI-ARIA APG patterns for
  [tree views](https://www.w3.org/WAI/ARIA/apg/patterns/treeview/),
  [toolbars](https://www.w3.org/WAI/ARIA/apg/patterns/toolbar/), and
  [modal dialogs](https://www.w3.org/WAI/ARIA/apg/patterns/dialog-modal/), plus
  WCAG guidance that state must not rely on
  [color alone](https://www.w3.org/WAI/WCAG22/Understanding/use-of-color) and
  focused controls must not be
  [obscured](https://www.w3.org/WAI/WCAG22/Understanding/focus-not-obscured-minimum).

## Delivery risks and order

1. **Resolve stale chapter approval semantics first.** Without a source OID or
   deterministic invalidation rule, the most important resume signal can lie.
2. **Define Git attribution fallbacks.** Shallow clones, rebases, squashes, and
   uncommitted files must not produce a false or editable author identity.
3. Establish isolated route/view-model contracts and reverse target metadata.
4. Consolidate decisions and annotations without changing the append-only
   storage layout or asking for identity.
5. Replace the diff surface and add the nested tree plus related fragments.
6. Finish with browser-level route, responsive, keyboard, and assistive-
   technology checks. Keep Monaco and all runtime assets local/offline.
