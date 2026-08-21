---
name: change-saga
description: 'Author, update, validate, and open a Git-native, visual successor to the pull-request description for a large PR number, URL, branch, commit range, or working-tree change. Use for requests like "make a change saga for PR 123" or "draft this change for review": explain the complete changeset through chapters, workflows, data-flow and data-model diagrams, interactive HTML, worked examples, and fully accounted diff URIs. The primary purpose is to create the artifact submitted for human review, not to perform the review; only conduct review actions when explicitly requested.'
---

# Change Saga

## Purpose and role boundary

A saga is the authored change proposal: the next-generation PR body submitted
alongside the code. It explains and demonstrates what changed, why, how it
behaves, and where every part is implemented. It is the thing to be reviewed,
not the review itself.

During authoring:

- speak as the change author and guide, not as an independent reviewer;
- create no review comments, approvals, rejections, or findings;
- document known risks, limitations, and tradeoffs as part of the proposal
  without turning them into review verdicts;
- optimize for a human reviewer to understand and inspect the change over time.

Only enter reviewer mode when the user explicitly asks to review, approve,
reject, annotate, or comment on an already-authored saga.

Use the `change-saga` CLI as the source of truth for format validity and diff coverage.
Treat completeness as an omission check, not proof that the authored proposal
is good. Prefer showing behavior and relationships over describing them in
dense prose.

## Locate the CLI

Prefer an installed `change-saga` executable. In the Change Saga source repository, use
`go run ./cmd/change-saga` when the executable is unavailable. Keep one invocation form
for the whole task.

Read [references/format.md](references/format.md) before changing saga files.
When authoring a new saga, also read
[references/authoring.md](references/authoring.md). Run `change-saga spec` if the
installed CLI disagrees with the references; follow the CLI and report the
mismatch.

## Read a saga through the query API

Never glob, grep, or read saga metadata files to learn what a saga contains.
The on-disk layout is an implementation detail with no compatibility promise.
Use `change-saga query`, the versioned read API, for every read of an existing
saga during both authoring and review. It is deterministic and paginated, never
starts the server, and never mutates either repository.

The operations are `overview`, `children`, `fragment`, `fragment-diffs`,
`diff-owners`, `reviews`, and `gaps`. Start at `query overview`, walk one level
at a time with `query children`, read narrative content through `query
fragment`, navigate evidence in both directions with `query fragment-diffs` and
`query diff-owners`, read the review overlay with `query reviews`, and page
completeness problems with `query gaps --kind uncovered|stale|overlap`.

Pass `--saga <path>` to every query, and `--repo <source-checkout>` when the
source repository is separate. Each invocation writes exactly one JSON envelope
with `schema`, `ok`, `snapshot`, `data`, `page.next_cursor`, and on failure
`error.code`. Branch on `ok` and `error.code`; never parse message text. Follow
`page.next_cursor` until it is null rather than raising `--limit`. `query
children` on a fragment lists its landmarks with the target URNs to pass to
`change-saga cover --target`.

## Author a saga

1. Resolve the request to an exact source comparison. For a PR number or URL,
   use the available hosting integration or CLI to obtain its title, URL, base,
   and head, and ensure the head is available locally. Never infer the base from
   the default branch when PR metadata is available.
2. Inspect the PR description, commit/file summary, full diff, tests, and any
   existing `.saga`. Do not modify product code while authoring unless asked.
3. Initialize the saga when none exists:

   ```sh
   change-saga init --base <base> --head <head> --title "<title>" \
     [--pr <number> --pr-url <url>] <name>.saga
   ```

   Use `WORKTREE` as the head only for tracked in-progress changes. Warn that the
   current engine does not account for untracked files.
4. Run `change-saga status --json <name>.saga`. Treat its uncovered atoms as the work
   queue and its stale diff URIs as reconciliation work. Its `uncovered`,
   `overlaps`, `orphans`, `targets`, `saga_changes`, and `schema_issues` arrays
   are always present, so an empty `uncovered` array is the completion signal.
5. Read the relevant code and diff context. Identify the affected end-to-end
   workflows, data flows, data models, state transitions, and concrete
   before/after examples. Use those to draft the overview and chapter map before
   attaching evidence. Group changes by reviewer intent,
   such as architecture, request flow, data migration, frontend behavior,
   operational risk, or tests. Do not group solely by file extension or assign a
   broad range before understanding it.
6. Divide the change into a small set of independently reviewable chapters with
   `change-saga add-chapter`. Treat each chapter like a PR that could be assigned and
   approved on its own. Use recursive sections inside a chapter only when they
   improve a reviewer's path through that unit.
7. Replace the root and chapter overview placeholders with the visual-first
   content contract in `references/authoring.md`. Lead the saga with a system or
   change map, and lead every substantial chapter with a diagram, interactive
   walkthrough, or worked example. Use SVG for architecture, data models, and
   stable flows. Use sandboxed HTML with bundled JavaScript for alternate paths,
   state transitions, before/after comparisons, and explorable examples. Use
   Markdown to orient and connect those artifacts, not as the default container
   for everything. Build focused fragments with `change-saga add-fragment`. Make every
   meaningful subpart addressable using the landmark
   contract in `references/authoring.md`: annotate Markdown headings directly
   and add one `___landmarks/<id>.landmark/` package per addressable heading,
   HTML/SVG element, exact text, or image region. Attach exact diff atoms inside
   that package when code realizes the landmark. Keep one review idea per
   fragment and avoid decorative media.
8. Attach only the atoms actually explained or demonstrated by a fragment (or a
   deliberately higher target) with `change-saga cover --target`. `--target`
   accepts a section or fragment path, a target URN, or the
   `<fragment-path>#<landmark-id>` shorthand. Always pass `--note`
   with a concise, reviewer-facing explanation of what changed in that source
   file and why it belongs to this narrative target. Make the note useful before
   code is expanded; do not restate the path or say only “implementation” or
   “tests.” Use `old` for deletions and `new` for additions. Cover rename, mode,
   and binary events explicitly. Prefer the absolute URIs emitted by `status
   --json` when source and saga live in different repositories. Use `--dry-run`
   to see exactly which records an invocation would write before writing them.
9. When attaching many selectors, pipe newline-delimited JSON records (or one
   JSON array) to `change-saga cover --batch -`. Each record carries its own
   `target`, `path`, `side`, `lines`, `event`, `old_path`, `new_path`, `note`,
   and `name`; `--target` and `--note` supply batch-wide defaults. The whole
   batch is resolved before anything is written and a failing record leaves the
   saga untouched. A batch is a delivery optimization only: every record still
   maps the exact atoms it explains, never a widened range. Give a record an
   explicit `name` only when you want a stable handle; a reused name is an
   error, not an overwrite.
10. Repeat `change-saga status --json` until every product atom is covered and no stale
    selector remains. Inspect overlaps and keep them only when multiple reviewer
    journeys genuinely need the same change.
11. Run both `change-saga validate --json` and `change-saga status --json`, then perform the
    visual and reviewer-readiness checks in `references/authoring.md`. Replace
    walls of text with diagrams or concrete examples before handoff.
    `change-saga validate --fix` adds missing stable anchors to Markdown
    headings in narrative fragments and changes nothing else; it never touches
    review history. Summarize the chapter structure, coverage result, saga-only
    changes, and limitations.

Never make a selector wider merely to reach 100%. If an atom does not fit the
current story, improve the structure or call out the unexplained change.

## Reconcile an evolving change

Run status against the new head, then handle both sides of drift:

- Remove or revise stale evidence files whose selectors no longer match.
- Place newly uncovered atoms only after reading their current diff context.
- Update fragment content when behavior changed, even if an old range still
  happens to match line numbers.
- Preserve comments and review events. They are history; do not rewrite or
  delete them to make the current state look cleaner.
- Never consolidate comments, replies, or state events into shared files. Each
  review action is an independent append-only record to minimize Git conflicts.
- Saga-only commits intentionally preserve the product diff identity. Product
  changes make old evidence stale and require reconciliation.

## Open the authored saga for review

Run `change-saga open <name>.saga` when asked to present the authored change for
review. Opening the UI does not authorize the AI to review it. The local UI can
anchor threads to whole fragments, selected text, rectangles, freehand paths, or
placed sticky notes.
Thread messages are fragments and may include images, SVG, or HTML attachments.
Use Saga view to follow the narrative and open attached code in the side
drawer. Read the collapsed file summaries first, then expand a file to inspect
its linked ranges. Use Code Diff view for the complete file tree. Diff comments,
suggestions, reviewed-file state, and fragment approvals are committed overlay
data and remain visible across both views.
Choose Sticky and click a fragment to place a note, type its text, then Add note
to commit it. Before submitting an annotation, use Ctrl/Cmd+Z to undo the latest
canvas edit and Ctrl/Cmd+Shift+Z (or Ctrl+Y) to redo it. After submission, select
a committed shape or note to move, recolor, reword, or remove it; Delete or
Backspace removes the current selection. Committed edits append anchor or state
events; never rewrite or delete the original thread or message.
Do not create comments or findings, or resolve, reopen, approve, or reject on a
person's behalf without an explicit request to conduct those review actions.

When reviewing without the UI, read the saga through the query API described
above rather than searching for or reading saga metadata files directly. Treat
uncovered results as a hard warning that the review narrative is incomplete.
