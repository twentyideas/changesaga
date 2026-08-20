---
name: review-saga
description: Create, update, validate, open, or review a Git-native Review Saga for a large pull request number, URL, branch, commit range, or working-tree change. Use for requests like "make a review saga for PR 123," for authoring overview/chapter/section/fragment hierarchies, explaining complex changes with Markdown/diagrams/interactive HTML, attaching and accounting for absolute diff URIs, reconciling evolving changes, or conducting an incremental chapter-by-chapter review.
---

# Review Saga

Use the `saga` CLI as the source of truth for format validity and diff coverage.
Treat completeness as an omission check, not proof that the narrative is good.

## Locate the CLI

Prefer an installed `saga` executable. In the Review Saga source repository, use
`go run ./cmd/saga` when the executable is unavailable. Keep one invocation form
for the whole task.

Read [references/format.md](references/format.md) before changing saga files.
When authoring a new saga, also read
[references/authoring.md](references/authoring.md). Run `saga spec` if the
installed CLI disagrees with the references; follow the CLI and report the
mismatch.

## Author a saga

1. Resolve the request to an exact source comparison. For a PR number or URL,
   use the available hosting integration or CLI to obtain its title, URL, base,
   and head, and ensure the head is available locally. Never infer the base from
   the default branch when PR metadata is available.
2. Inspect the PR description, commit/file summary, full diff, tests, and any
   existing `.saga`. Do not modify product code while authoring unless asked.
3. Initialize the saga when none exists:

   ```sh
   saga init --base <base> --head <head> --title "<title>" \
     [--pr <number> --pr-url <url>] <name>.saga
   ```

   Use `WORKTREE` as the head only for tracked in-progress changes. Warn that the
   current engine does not account for untracked files.
4. Run `saga status --json <name>.saga`. Treat its uncovered atoms as the work
   queue and its stale diff URIs as reconciliation work.
5. Read the relevant code and diff context. Draft the overview and chapter map
   before attaching evidence. Group changes by reviewer intent,
   such as architecture, request flow, data migration, frontend behavior,
   operational risk, or tests. Do not group solely by file extension or assign a
   broad range before understanding it.
6. Divide the change into a small set of independently reviewable chapters with
   `saga add-chapter`. Treat each chapter like a PR that could be assigned and
   approved on its own. Use recursive sections inside a chapter only when they
   improve a reviewer's path through that unit.
7. Replace the root and chapter overview placeholders with the content contract
   in `references/authoring.md`. Build focused fragments with `saga add-fragment`. Use
   Markdown for explanation, SVG or images for visual models, and sandboxed HTML
   with bundled JavaScript for interactions that materially improve
   understanding. Make every meaningful subpart addressable using the landmark
   contract in `references/authoring.md`: annotate Markdown headings directly
   and add one `___landmarks/<id>.landmark/` package per addressable heading,
   HTML/SVG element, exact text, or image region. Attach exact diff atoms inside
   that package when code realizes the landmark. Keep one review idea per
   fragment and avoid decorative media.
8. Attach only the atoms actually explained or demonstrated by a fragment (or a
   deliberately higher target) with `saga cover --target`. Always pass `--note`
   with a concise, reviewer-facing explanation of what changed in that source
   file and why it belongs to this narrative target. Make the note useful before
   code is expanded; do not restate the path or say only “implementation” or
   “tests.” Use `old` for deletions and `new` for additions. Cover rename, mode,
   and binary events explicitly. Prefer the absolute URIs emitted by `status
   --json` when source and saga live in different repositories.
9. Repeat `saga status --json` until every product atom is covered and no stale
   selector remains. Inspect overlaps and keep them only when multiple reviewer
   journeys genuinely need the same change.
10. Run both `saga validate --json` and `saga status --json`, then perform the
    reviewer-readiness checks in `references/authoring.md`. Summarize the
    chapter structure, coverage result, saga-only changes, and limitations.

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

## Open or conduct a review

Run `saga open <name>.saga` when asked to present the review. The local UI can
anchor threads to whole fragments, selected text, rectangles, or freehand paths.
Thread messages are fragments and may include images, SVG, or HTML attachments.
Use Saga view to follow the narrative and open attached code in the side
drawer. Read the collapsed file summaries first, then expand a file to inspect
its linked ranges. Use Code Diff view for the complete file tree. Diff comments,
suggestions, reviewed-file state, and fragment approvals are committed overlay
data and remain visible across both views.
Before submitting an annotation, use Ctrl/Cmd+Z to undo the latest canvas edit
and Ctrl/Cmd+Shift+Z (or Ctrl+Y) to redo it. After submission, select a committed
shape to move, recolor, or remove it. Committed edits append anchor or state
events; never rewrite or delete the original thread or message.
Do not resolve, reopen, approve, or reject on a person's behalf without explicit
instruction.

When reviewing without the UI, inspect assets and prose first, then use coverage
evidence to dive into the relevant code. Use uncovered status as a hard warning:
the review narrative is incomplete.
