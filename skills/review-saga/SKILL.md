---
name: review-saga
description: Build, update, reconcile, validate, or open a Git-native Review Saga for a large pull request or working-tree change. Use for authoring `.saga` overview, chapter, section, and fragment hierarchies, creating Markdown/SVG/image/interactive HTML review content, attaching absolute cross-repository diff URIs, accounting for uncovered changes, repairing stale evidence, or working with anchored review threads.
---

# Review Saga

Use the `saga` CLI as the source of truth for format validity and diff coverage.
Treat completeness as an omission check, not proof that the narrative is good.

## Locate the CLI

Prefer an installed `saga` executable. In the Review Saga source repository, use
`go run ./cmd/saga` when the executable is unavailable. Keep one invocation form
for the whole task.

Read [references/format.md](references/format.md) before creating or editing a
saga. Run `saga spec` if the installed CLI disagrees with the reference; follow
the installed CLI's active version and report the mismatch.

## Author a saga

1. Inspect the repository, source comparison, and existing `.saga` directories.
   Do not assume `main`; identify the actual PR base and head when available.
2. Initialize the saga when none exists:

   ```sh
   saga init --base <base> --head <head> --title "<title>" <name>.saga
   ```

   Use `WORKTREE` as the head only for tracked in-progress changes. Warn that the
   current engine does not account for untracked files.
3. Run `saga status --json <name>.saga`. Treat its uncovered atoms as the work
   queue and its stale diff URIs as reconciliation work.
4. Read the relevant code and diff context. Group changes by reviewer intent,
   such as architecture, request flow, data migration, frontend behavior,
   operational risk, or tests. Do not group solely by file extension or assign a
   broad range before understanding it.
5. Divide the change into a small set of independently reviewable chapters with
   `saga add-chapter`. Treat each chapter like a PR that could be assigned and
   approved on its own. Use recursive sections inside a chapter only when they
   improve a reviewer's path through that unit.
6. Build each chapter and section from focused fragments with `saga add-fragment`. Use
   Markdown for explanation, SVG or images for visual models, and sandboxed HTML
   with bundled JavaScript for interactions that materially improve
   understanding. Keep one review idea per fragment and avoid decorative media.
7. Attach only the atoms actually explained or demonstrated by a fragment (or a
   deliberately higher target) with `saga cover --target`. Use `old` for
   deletions and `new` for additions. Cover rename, mode, and binary events
   explicitly. Prefer the absolute URIs emitted by `status --json` when source
   and saga live in different repositories.
8. Repeat `saga status --json` until every product atom is covered and no stale
   selector remains. Inspect overlaps and keep them only when multiple reviewer
   journeys genuinely need the same change.
9. Run both `saga validate --json` and `saga status --json`. Summarize the section
   structure, coverage result, saga-only changes, and any limitations.

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

## Open or conduct a review

Run `saga open <name>.saga` when asked to present the review. The local UI can
anchor threads to whole fragments, selected text, rectangles, or freehand paths.
Thread messages are fragments and may include images, SVG, or HTML attachments.
Use Saga view to follow the narrative and open attached diffs in the side
drawer. Use Code Diff view for the complete file tree. Diff comments,
suggestions, reviewed-file state, and fragment approvals are committed overlay
data and remain visible across both views.
Do not resolve, reopen, approve, or reject on a person's behalf without explicit
instruction.

When reviewing without the UI, inspect assets and prose first, then use coverage
evidence to dive into the relevant code. Use uncovered status as a hard warning:
the review narrative is incomplete.
