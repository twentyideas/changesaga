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

Treat generated content as a first draft, even when using a frontier model.
After the evidence and structure are correct, perform a separate editorial
pass: make explanations concise, direct, and factual; remove repetition and
vague framing; and revise prose and diagrams until each fragment communicates
one coherent idea. Expect iteration rather than assuming the first complete
draft is ready for review.

The structured directory format is intentionally friendly to parallel
development. When work is parallelized, partition ownership along independent
chapters and fragments, and let each lane add its own evidence, claims,
verifications, and review records. Avoid aggregating unrelated work into shared
files; merge the lanes before the final coverage and validation passes. This
localizes Git conflicts but does not make parallel edits conflict-free.

## Choose the workflow before authoring

First determine whether the user is documenting an existing implementation or
starting a new body of work.

For an existing PR, branch, or focused changeset, decide whether the change is
large enough to benefit from a guided review. Size means review complexity—not
just line count—including multiple behaviors, risks, systems, or workstreams.
A small focused change may be better served by the repository's normal PR
process. For a large change with no existing saga, author the saga from the
completed implementation and exact diff as the review guide. Requirements,
prototypes, technical design, and a work plan remain optional historical
context; do not invent them after the fact merely to fill every surface.

For a new feature or exploration, begin a living saga early. A common
progression is:

1. prototype the UX and UI aesthetic;
2. draft sourced user stories and acceptance criteria;
3. develop a technical design that traces to those requirements; and
4. organize delivery into dependency-aware waves, parallel workspace lanes,
   and explicit convergence points.

This progression is not a waterfall. Prototypes and stories may be created in
either order and iterated together. Design can proceed while they mature, and
work-plan drafting can overlap design. Treat revisions as normal living
changes, preserving their history and refreshing stale downstream links.

Parallel authoring is a core property of the document, not just of the code
change. Partition ownership by stable stories, prototype packages, design
fragments, and work items so agents can fan out and merge their Saga changes as
well as their implementation. Before peer review, consolidate the lanes and
connect the delivered commits and exact diffs through the acceptance criteria,
design, and work plan that explain them.

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

The operations are `schema`, `overview`, `children`, `fragment`, `fragment-diffs`,
`diff-owners`, `reviews`, `gaps`, `mappings`, `claims`, and `verifications`. Start at `query overview`, walk one level
at a time with `query children`, read narrative content through `query
fragment`, navigate evidence in both directions with `query fragment-diffs` and
`query diff-owners`, read the review overlay with `query reviews`, and page
completeness problems with `query gaps --kind uncovered|stale|overlap`.
Use `query mappings --sort scrutiny` to find coverage records whose breadth or
thin justification deserves the most skepticism. Use `query claims` and
`query verifications` to inspect falsifiable author assertions and their
append-only verification history.

Pass `--saga <path>` to every query, and `--repo <source-checkout>` when the
source repository is separate. The one exception is `change-saga query schema
<operation>`, which describes that operation's data paths and pagination
contract without opening a saga. Use it instead of probing or guessing response
shapes. Each invocation writes exactly one JSON envelope with `schema`, `ok`,
`snapshot`, `data`, and `page`; failures carry `error.code`. Branch on `ok` and
`error.code`; never parse message text. For cursor-paginated operations, the
current page length at `pagination.counted_path` must equal `page.returned`.
Follow `page.next_cursor` while `page.has_more` is true, and confirm the
aggregate count equals `page.total`. Do not raise `--limit` to silently swallow a partial
result. `query
children` on a fragment lists its landmarks with the target URNs to pass to
`change-saga cover --target`.
Hierarchy nodes report both direct and descendant diff counts. Treat
`diffs.current` and `diffs.stale` as inclusive totals; use `direct_current`,
`direct_stale`, `descendant_current`, and `descendant_stale` when deciding
whether evidence belongs to the node itself or to one of its landmarks or
children.

## Author a saga

1. Resolve the request to an exact source comparison. For a PR number or URL,
   use the available hosting integration or CLI to obtain its title, URL, base,
   and head, and ensure the head is available locally. Verify the returned head
   branch/OID and changed-file summary describe the checkout you are about to
   explain. Never guess a PR number from nearby context, and omit PR identity
   rather than recording one that cannot be verified. Never infer the base from
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
4. Page `change-saga query gaps --kind uncovered --saga <name>.saga` as the
   coverage work queue. Query `gaps --kind stale` for reconciliation work and
   `gaps --kind overlap` for mappings that need justification. Preserve the
   returned snapshot across the loop and restart if it changes unexpectedly.
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
7. Author the empty root and chapter overview files using the visual-first
   content contract in `references/authoring.md`; generated authoring text must
   never appear in the handed-off saga. Lead the saga with a system or
   change map, and lead every substantial chapter with a diagram, interactive
   walkthrough, or worked example. Use SVG for architecture, data models, and
   stable flows. Use sandboxed HTML with bundled JavaScript for alternate paths,
   state transitions, before/after comparisons, and explorable examples. Use
   Markdown to orient and connect those artifacts, not as the default container
   for everything. Build focused fragments with `change-saga add-fragment`,
   then write or replace their entrypoints through `change-saga
   set-fragment-content --target TARGET --source FILE|-`. Do not edit
   `content.md`, manifests, or other fragment-package files directly. Make every
   meaningful subpart addressable using the landmark
   contract in `references/authoring.md`: annotate Markdown headings directly,
   then use `change-saga add-landmark --description "<semantic meaning>"` for each addressable heading, HTML/SVG
   element, exact text, or image region. In prose, cite concrete implementation
   claims with Markdown footnotes and make the plain-text citation definition
   an exact-text landmark; evidence on that landmark lets the inline citation
   and its reference entry open the code drawer. Attach exact diff atoms to the returned
   landmark target when code realizes it. Treat this as a required
   addressability pass before broad coverage: every concrete prose statement
   about implementation, behavior, an invariant, or a data transition must
   either carry a focused footnote citation or live under a deliberately
   evidence-bearing heading landmark. Do not finish a substantive prose
   fragment with zero citations merely because its diff atoms are covered at
   fragment scope. Zero citations are appropriate only for pure orientation,
   background, or prose whose exact code ownership is already expressed by
   focused heading landmarks. Before moving on from a visual, enumerate its
   meaningful nodes **and edges** and confirm each code-bearing node, arrow,
   transition, or state has a stable element ID, a landmark, and focused
   evidence rather than relying on fragment-level coverage. For SVG,
   `--element-id` automatically creates the on-canvas link from the element's
   rendered bounds; use `--hotspot` only to override an awkward hit area. Keep
   Treat every fragment as one presentation slide, not a miniature document.
   Keep one review idea per slide and avoid decorative media. Markdown slides
   have a hard authoring target of at most 100 explanatory prose words (fenced
   examples and citation definitions are excluded). Authoring commands refuse
   an over-budget Markdown slide without writing; split the idea or replace
   prose with a visual when validation reports dense legacy content.
   Apply one completion rule to prose and visuals: a Markdown footnote is not
   linked merely because its marker and definition render. Its plain-text
   definition must be an exact-text landmark and that landmark must own focused
   diff evidence—the same requirement as a code-bearing diagram node or edge.
   Run validation and repair every per-footnote warning before handoff. Do not
   confuse prose diff citations with `citation add` provenance records;
   provenance explains where a requirement came from, not where it is
   implemented.
8. Attach only the atoms actually explained or demonstrated by a fragment (or a
   deliberately higher target) with `change-saga cover --target`. `--target`
   accepts a section or fragment path, a target URN, or the
   `<fragment-path>#<landmark-id>` shorthand. Always pass `--note`
   with a concise, reviewer-facing explanation of what changed in that source
   file and why it belongs to this narrative target. Make the note useful before
   code is expanded; do not restate the path or say only “implementation” or
   “tests.” Use `old` for deletions and `new` for additions. Cover rename, mode,
   and binary events explicitly. Prefer the absolute URIs emitted by `query
   gaps` when source and saga live in different repositories. Use `--dry-run`
   to see exactly which records an invocation would write before writing them.
   When every changed atom in one file genuinely belongs to the same narrative
   target, `--path FILE --changed-lines` derives its exact old/new line atoms,
   coalesces gapless lines into canonical dense ranges, and includes file
   events such as `add` automatically. Do not use this convenience to collapse
   multiple concerns into one broad record. Generated evidence paths identify
   their selector set, so a second explanation for the same selectors requires
   `replace-coverage` rather than creating a duplicate record.
9. When attaching many selectors, pipe newline-delimited JSON records (or one
   JSON array) to `change-saga cover --batch -`. Each record carries its own
   `target`, `path`, `side`, `lines`, `changed_lines`, `event`, `old_path`,
   `new_path`, `note`, and `name`; `--target` and `--note` supply batch-wide defaults. The whole
   batch is resolved before anything is written and a failing record leaves the
   saga untouched. A batch is a delivery optimization only: every record still
   maps the exact atoms it explains, never a widened range. Give a record an
   explicit `name` only when you want a stable handle; a reused name is an
   error, not an overwrite.
10. Run `change-saga query mappings --sort scrutiny`. Its `evidence_file` is
    the supported repair handle: use `change-saga replace-coverage --record
    PATH --batch -` to atomically split, retarget, or rewrite a record, and
    `change-saga remove-coverage --record PATH` to delete one. Prefer
    landmark-level ownership for broad visual fragments. A mapping
    score directs scrutiny; it is not a correctness grade and a low score is
    not proof that the explanation is true.
    Then audit addressability independently of atom completeness: use `query
    children` and `query fragment-diffs` on every substantive fragment. Move
    direct fragment-level evidence down to prose citations, heading landmarks,
    SVG nodes, or SVG edges whenever the authored content identifies that
    narrower concept. A complete saga with avoidably broad targets is not ready
    for handoff.
    If all mappings became stale only because the declared base advanced after
    that base was incorporated into the head, run `change-saga rebase-evidence
    --repo <source-checkout> --dry-run <saga>` instead of editing URIs. Proceed
    only when it reports the expected old/new base, unchanged product identity,
    atom count, selector count, and claim impact. The command refuses real
    product changes. Applying it rolls immutable claims forward; do not pass
    `--carry-verifications` unless the unchanged product identity justifies an
    explicit analysis carry-forward for this review.
11. Record falsifiable assertions with `change-saga add-claim`. Each claim must
    target the exact narrative element making it and cite exact supporting diff
    URIs. Claims never contribute to coverage. Append an explicit result with
    `change-saga verify-claim`: use `unverified` when the assertion has not
    actually been checked, and otherwise record the reproducible method,
    outcome, and command when applicable. Never convert prose confidence into a
    `verified` result.
12. Repeat the three `query gaps` views until no product atom is uncovered, no
    selector is stale, and every overlap is defensible because multiple
    reviewer journeys genuinely need the same change.
13. Run both `change-saga validate --json` and `change-saga status --json`.
    Treat untouched scaffolds and visual-mapping warnings as unfinished
    authoring, then perform the
    visual and reviewer-readiness checks in `references/authoring.md`. Replace
    walls of text with diagrams or concrete examples before handoff.
    Treat a citation-free implementation narrative or a code-bearing visual
    node/edge without its own landmark as unfinished even when status reports
    complete coverage.
    `change-saga validate --fix` adds missing stable anchors to Markdown
    headings in narrative fragments and changes nothing else; it never touches
    review history. Summarize the chapter structure, coverage result, saga-only
    changes, and limitations.

Never make a selector wider merely to reach 100%. If an atom does not fit the
current story, improve the structure or call out the unexplained change.

## Reconcile an evolving change

Before editing an existing Saga for a newly merged or proposed change, derive
the maintenance work queue from source evidence rather than comparing authored
content:

```sh
change-saga compare --json --repo <source-checkout> \
  --base <incoming-base> --head <incoming-head> <maintained.saga>
change-saga compare --json --repo <source-checkout> \
  --against-saga <incoming.saga> <maintained.saga>
```

The first Saga is the maintained document. `must_update` targets have a direct
conflicting intersection with removed, replaced, renamed, or otherwise
destructive source evidence. `consider_update` targets neighbor additive code
in the same implementation area. `new_content` atoms have no existing owner
and require a new or expanded explanation. Follow the returned target URNs,
`content_path`, and `evidence_files`; do not compare prose, SVG, HTML, or other
fragment bytes to infer impact. Stop and repair the baseline first when the
result reports `baseline_incomplete`, because the work queue is not exhaustive.

Run status against the new head, then handle both sides of drift:

- Remove or revise stale evidence with `remove-coverage` or
  `replace-coverage`; never delete metadata files directly.
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
the complete patch with its linked evidence highlighted. Use Code Diff view for the complete file tree. Diff comments,
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
`change-saga open` starts a managed background reviewer and prints its PID and
URL. Discover it later with `change-saga serve status [SAGA]` and stop it with
`change-saga serve stop [SAGA]`. Use `change-saga serve --open` only when the
reviewer should remain attached to the current terminal.

When reviewing without the UI, read the saga through the query API described
above rather than searching for or reading saga metadata files directly.
When recording an approval or rejection from the CLI, always declare the
reviewer persona. Use `--reviewer-kind human` only for a decision the human made
directly. For your own AI review, use `--reviewer-kind ai` together with an
independent `--reviewer-name`, `--agent`, and the exact `--model`; never turn an
AI pass into a human approval. Give simultaneous passes stable distinct names
such as `Claude 1` and `Claude 2` even when their model is identical.
Multiple reviewers may decide the same target, and your decision must not erase
or stand in for theirs.
Conduct correctness review in three passes:

1. Read the code diff independently before reading the author's conclusions.
   Record provisional findings so the narrative cannot anchor the first pass.
2. Run `query mappings --sort scrutiny`, `query claims`, and `query
   verifications`; use `query diff-owners` while inspecting atoms to see the
   relevant target and its mapping-quality signals. Read the saga narrative for
   architecture, intent, workflows, and tradeoffs, and independently test each
   claim rather than accepting its latest status.
3. Reconcile the two passes. Prioritize contradictions, unverified or failed
   claims, stale evidence, broad mappings, and code the narrative minimizes.

Treat uncovered results as a hard warning that the narrative is incomplete.
Treat all-atoms-mapped as an omission invariant only, never as approval,
correctness, or evidence that the explanation is sufficiently precise.
